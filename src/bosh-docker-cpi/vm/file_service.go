package vm

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"path"
	"strings"
	"time"

	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	dkrclient "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// execTimeout is the per-operation time limit for container exec calls.
const execTimeout = 5 * time.Minute

//counterfeiter:generate . FileService

// FileService transfers files between the host and a running container.
type FileService interface {
	Upload(string, []byte) error
	Download(string) ([]byte, error)
}

// DockerExecClient is the subset of the Docker API used by fileService for
// exec-based file transfers. *dkrclient.Client satisfies this interface.
type DockerExecClient interface {
	ContainerExecCreate(ctx context.Context, containerID string, options container.ExecOptions) (container.ExecCreateResponse, error)
	ContainerExecAttach(ctx context.Context, execID string, config container.ExecAttachOptions) (types.HijackedResponse, error)
	ContainerExecInspect(ctx context.Context, execID string) (container.ExecInspect, error)
}

var _ DockerExecClient = (*dkrclient.Client)(nil)

type fileService struct {
	dkrClient DockerExecClient
	vmCID     apiv1.VMCID

	logTag string
	logger boshlog.Logger
}

// NewFileService creates a FileService that transfers files between the host
// and a running container identified by vmCID. All file I/O uses exec-based
// tar streaming to avoid the Docker 29.5.x containerd-snapshotter parent-escape
// check that rejects the Ubuntu Noble stemcell's
// /etc/resolv.conf -> ../run/systemd/resolve/stub-resolv.conf symlink.
func NewFileService(
	dkrClient DockerExecClient,
	vmCID apiv1.VMCID,
	logger boshlog.Logger,
) FileService {
	return &fileService{
		dkrClient: dkrClient,
		vmCID:     vmCID,

		logTag: "vm.fileService",
		logger: logger,
	}
}

// Download reads a file from the container by running "tar -c" inside it and
// capturing the tar stream via exec stdout. This mirrors Docker's own
// CopyFromContainer wire format but operates within the container's running
// namespace, bypassing the Docker 29.5.x containerd-snapshotter security scan
// that rejects symlinks whose targets escape their parent directory
// (e.g. /etc/resolv.conf -> ../run/systemd/resolve/stub-resolv.conf in the
// Ubuntu Noble stemcell image layer).
//
// Docker exec output is multiplexed with 8-byte stream-type headers when
// Tty=false; stdcopy.StdCopy demultiplexes before the tar reader sees the data.
// An io.Pipe streams the demultiplexed output directly into tar.Reader so that
// only the extracted file bytes are buffered, avoiding an O(filesize) copy.
func (s *fileService) Download(sourcePath string) ([]byte, error) {
	cleaned := path.Clean(sourcePath)
	cleanedBase := path.Base(cleaned)
	if !path.IsAbs(cleaned) || cleanedBase == "." || cleanedBase == "/" || cleaned != sourcePath {
		return nil, bosherr.Errorf("sourcePath must be a clean absolute path to a single file, got '%s'", sourcePath)
	}

	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()

	// Use the path package (not filepath) so directory and base components are
	// always separated by '/' regardless of the host OS the CPI is compiled on.
	execProcess, err := s.dkrClient.ContainerExecCreate(ctx, s.vmCID.AsString(), container.ExecOptions{
		Cmd:          []string{"tar", "-c", "-f", "-", "-C", path.Dir(cleaned), "--", cleanedBase},
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return nil, bosherr.WrapErrorf(err, "Creating docker exec for reading '%s'", sourcePath)
	}

	resp, err := s.dkrClient.ContainerExecAttach(ctx, execProcess.ID, container.ExecAttachOptions{})
	if err != nil {
		return nil, bosherr.WrapErrorf(err, "Attaching to docker exec for reading '%s'", sourcePath)
	}
	defer resp.Close()

	// ContainerExecAttach returns a hijacked net.Conn; context cancellation does
	// not close it, so propagate the deadline directly onto the connection so
	// stdcopy.StdCopy cannot block past the timeout.
	if dl, ok := ctx.Deadline(); ok {
		if err := resp.Conn.SetDeadline(dl); err != nil {
			return nil, bosherr.WrapErrorf(err, "Setting deadline on exec connection for '%s'", sourcePath)
		}
	}

	// Pipe demultiplexed stdout into tar.Reader concurrently so that only the
	// extracted file bytes are held in memory, not the full tar stream.
	pr, pw := io.Pipe()
	var stderrBuf bytes.Buffer
	copyDone := make(chan error, 1)
	go func() {
		// Read from resp.Reader (bufio.Reader) rather than resp.Conn so that any
		// bytes already prefetched into the HTTP hijack buffer during the upgrade
		// handshake are not silently dropped.
		_, copyErr := stdcopy.StdCopy(pw, &stderrBuf, resp.Reader)
		pw.CloseWithError(copyErr)
		copyDone <- copyErr
	}()

	tr := tar.NewReader(pr)
	hdr, tarNextErr := tr.Next()
	if tarNextErr != nil {
		pr.CloseWithError(tarNextErr)
		<-copyDone
		// Poll until the exec process has actually exited so we can report the
		// real exit status and stderr rather than a confusing tar parse error.
		// A single inspect call is insufficient because the process may still
		// be marked running when we reach this point.
		for {
			inspectResp, iErr := s.dkrClient.ContainerExecInspect(ctx, execProcess.ID)
			if iErr != nil {
				// Cannot determine exec status; fall back to the tar error.
				break
			}
			if inspectResp.Running {
				select {
				case <-ctx.Done():
					return nil, bosherr.WrapError(ctx.Err(), "Exec polling interrupted")
				case <-time.After(50 * time.Millisecond):
				}
				continue
			}
			if inspectResp.ExitCode != 0 {
				return nil, bosherr.Errorf("tar of '%s' from container failed [exit status %d]: %s",
					sourcePath, inspectResp.ExitCode, stderrBuf.String())
			}
			break // exited 0; fall back to the tar parse error below
		}
		return nil, bosherr.WrapErrorf(tarNextErr, "Reading tar header for '%s'", cleanedBase)
	}

	// Validate the tar entry before reading its contents. Guard against the
	// container tar command returning a directory, symlink, or another
	// non-regular-file entry — which would cause io.ReadAll to return empty
	// bytes for a directory rather than the file contents, making the failure
	// silent and hard to diagnose. Also guard against an unexpected name, which
	// would indicate the container's working directory shifted or the tar
	// invocation produced an unrelated entry.
	if hdr.Typeflag != tar.TypeReg {
		pr.CloseWithError(bosherr.Errorf("unexpected entry type"))
		<-copyDone
		return nil, bosherr.Errorf(
			"tar entry for '%s' is not a regular file (type=%d)", cleanedBase, hdr.Typeflag)
	}
	// GNU tar sometimes prefixes the name with "./"; clean both sides for a
	// reliable comparison.
	if path.Clean(hdr.Name) != cleanedBase {
		pr.CloseWithError(bosherr.Errorf("unexpected entry name"))
		<-copyDone
		return nil, bosherr.Errorf(
			"tar entry for '%s' has unexpected name '%s'", cleanedBase, hdr.Name)
	}

	data, readErr := io.ReadAll(tr)
	// Drain any remaining tar stream (end-of-archive padding) so the copy
	// goroutine can complete its writes and exit cleanly. If the drain fails
	// it means the copy goroutine itself failed; report that via copyDone.
	if _, err = io.Copy(io.Discard, pr); err != nil {
		<-copyDone
		return nil, bosherr.WrapErrorf(err, "Draining tar stream from container for '%s'", sourcePath)
	}

	copyErr := <-copyDone
	if readErr != nil {
		return nil, bosherr.WrapErrorf(readErr, "Reading '%s' from container", cleanedBase)
	}
	if copyErr != nil {
		return nil, bosherr.WrapErrorf(copyErr, "Reading tar stream from container for '%s'", sourcePath)
	}

	for {
		inspectResp, err := s.dkrClient.ContainerExecInspect(ctx, execProcess.ID)
		if err != nil {
			return nil, bosherr.WrapErrorf(err, "Inspecting docker exec response for '%s'", sourcePath)
		}

		if inspectResp.Running {
			select {
			case <-ctx.Done():
				return nil, bosherr.WrapError(ctx.Err(), "Exec polling interrupted")
			case <-time.After(50 * time.Millisecond):
			}
			continue
		}

		if inspectResp.ExitCode != 0 {
			return nil, bosherr.Errorf("tar of '%s' from container failed [exit status %d]: %s",
				sourcePath, inspectResp.ExitCode, stderrBuf.String())
		}

		break
	}

	return data, nil
}

// Upload writes a file into the container by building a tar archive and
// streaming it via exec stdin to "tar -x -C /". This avoids both
// Docker's CopyToContainer (which triggers the 29.5.x parent-escape check) and
// any shell stdin-buffering limits that affect the previous bash+cat approach for
// files larger than ~2 MB. A single exec call handles directory creation and
// file writing atomically; GNU tar (standard in all BOSH stemcells) creates
// missing parent directories during extraction.
//
// The archive is streamed through an io.Pipe so the tar data is never fully
// buffered in memory before being sent to the container; only the in-memory
// contents slice is held. The archive path is derived from a validated,
// normalized absolute destination path with all leading "/" characters
// stripped so that "tar -x -C /" places the file at the correct absolute
// path inside the container.
func (s *fileService) Upload(destinationPath string, contents []byte) error {
	cleanedDest := path.Clean(destinationPath)
	destBase := path.Base(cleanedDest)
	if !path.IsAbs(cleanedDest) || destBase == "." || destBase == "/" || cleanedDest != destinationPath {
		return bosherr.Errorf("destinationPath must be a clean absolute path to a single file, got '%s'", destinationPath)
	}

	// Strip the leading "/" so that "tar -x -C /" places the file at the
	// correct absolute path inside the container while keeping the tar
	// header name relative.
	tarPath := strings.TrimLeft(cleanedDest, "/")

	// Stream the tar archive through an io.Pipe to avoid buffering the full
	// archive (header + contents + end-of-archive padding) before sending.
	pr, pw := io.Pipe()
	defer func() {
		// Close the reader to unblock the writer goroutine if dockerExecWithStdin
		// fails before io.Copy ever reads from pr (e.g., on ContainerExecCreate
		// or ContainerExecAttach failure). io.PipeReader.Close always returns nil.
		if closeErr := pr.Close(); closeErr != nil {
			s.logger.Debug(s.logTag, "Closing pipe reader: %v", closeErr)
		}
	}()
	go func() {
		tw := tar.NewWriter(pw)
		if err := tw.WriteHeader(&tar.Header{
			Name:     tarPath,
			Size:     int64(len(contents)),
			Mode:     0640,
			Typeflag: tar.TypeReg,
		}); err != nil {
			pw.CloseWithError(bosherr.WrapError(err, "Writing tar header"))
			return
		}
		if _, err := tw.Write(contents); err != nil {
			pw.CloseWithError(bosherr.WrapError(err, "Writing tar content"))
			return
		}
		pw.CloseWithError(tw.Close())
	}()

	if err := s.dockerExecWithStdin(pr, "tar", "-x", "-f", "-", "-C", "/"); err != nil {
		return bosherr.WrapErrorf(err, "Uploading '%s'", destinationPath)
	}
	return nil
}

// dockerExecWithStdin runs args inside the container, feeding stdin from the
// provided reader. It captures stderr and includes it in the error when the
// command exits with a non-zero status. The exec deadline is propagated to the
// underlying hijacked connection so a stalled write cannot block indefinitely.
func (s *fileService) dockerExecWithStdin(stdin io.Reader, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()

	execProcess, err := s.dkrClient.ContainerExecCreate(ctx, s.vmCID.AsString(), container.ExecOptions{
		Cmd:          args,
		AttachStdin:  true,
		AttachStderr: true,
	})
	if err != nil {
		return bosherr.WrapErrorf(err, "Creating docker exec")
	}

	resp, err := s.dkrClient.ContainerExecAttach(ctx, execProcess.ID, container.ExecAttachOptions{})
	if err != nil {
		return bosherr.WrapErrorf(err, "Attaching to docker exec")
	}
	defer resp.Close()

	// ContainerExecAttach returns a hijacked net.Conn; context cancellation does
	// not close it, so propagate the deadline directly onto the connection so
	// io.Copy cannot block past the timeout.
	if dl, ok := ctx.Deadline(); ok {
		if err := resp.Conn.SetDeadline(dl); err != nil {
			return bosherr.WrapErrorf(err, "Setting deadline on exec connection")
		}
	}

	// Drain stderr concurrently with stdin writes to prevent deadlock: if the
	// exec process writes to stderr before consuming all stdin (e.g., on a tar
	// parse error mid-stream), the exec's stderr buffer can fill and block the
	// process, while io.Copy below is blocked waiting for the process to read
	// more stdin — a classic bidirectional deadlock.
	var stderrBuf bytes.Buffer
	stderrDone := make(chan error, 1)
	go func() {
		_, copyErr := stdcopy.StdCopy(io.Discard, &stderrBuf, resp.Reader)
		stderrDone <- copyErr
	}()

	if _, err = io.Copy(resp.Conn, stdin); err != nil {
		writeErr := err
		// Unblock any goroutine feeding stdin through an io.Pipe.
		if c, ok := stdin.(io.Closer); ok {
			if closeErr := c.Close(); closeErr != nil {
				s.logger.Debug(s.logTag, "Closing stdin reader after write error: %v", closeErr)
			}
		}
		// CloseWrite may fail if the connection is already broken; proceed regardless
		// so we can still inspect the exec process for a better error message.
		if cwErr := resp.CloseWrite(); cwErr != nil {
			s.logger.Debug(s.logTag, "CloseWrite after write error: %v", cwErr)
		}
		// Drain stderr and poll the exec exit status so we can return the exec's
		// real failure reason (e.g., "permission denied") rather than the
		// broken-pipe write error that triggered the io.Copy failure.
		if stderrErr := <-stderrDone; stderrErr != nil {
			s.logger.Debug(s.logTag, "Reading exec output after write error: %v", stderrErr)
		}
		for {
			inspectResp, iErr := s.dkrClient.ContainerExecInspect(ctx, execProcess.ID)
			if iErr != nil {
				break
			}
			if inspectResp.Running {
				select {
				case <-ctx.Done():
					return bosherr.WrapError(ctx.Err(), "Exec polling interrupted")
				case <-time.After(50 * time.Millisecond):
				}
				continue
			}
			if inspectResp.ExitCode != 0 {
				return bosherr.Errorf("%v failed [exit status %d]: %s", args, inspectResp.ExitCode, stderrBuf.String())
			}
			break
		}
		return bosherr.WrapErrorf(writeErr, "Writing stdin to container")
	}

	if err = resp.CloseWrite(); err != nil {
		return bosherr.WrapErrorf(err, "Closing stdin")
	}

	if stderrErr := <-stderrDone; stderrErr != nil {
		return bosherr.WrapErrorf(stderrErr, "Reading exec output")
	}

	for {
		inspectResp, err := s.dkrClient.ContainerExecInspect(ctx, execProcess.ID)
		if err != nil {
			return bosherr.WrapErrorf(err, "Inspecting docker exec response")
		}

		if inspectResp.Running {
			select {
			case <-ctx.Done():
				return bosherr.WrapError(ctx.Err(), "Exec polling interrupted")
			case <-time.After(50 * time.Millisecond):
			}
			continue
		}

		if inspectResp.ExitCode != 0 {
			return bosherr.Errorf("%v failed [exit status %d]: %s", args, inspectResp.ExitCode, stderrBuf.String())
		}

		return nil
	}
}
