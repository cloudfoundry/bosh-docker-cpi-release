package vm_test

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"net"
	"time"

	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "bosh-docker-cpi/vm"
)

// testConn is a net.Conn that supports CloseWrite via separate io.Pipes for
// each direction, allowing accurate half-close simulation in tests.
type testConn struct {
	// rr/rw: server-to-client pipe. Server writes to rw; client reads from rr.
	rr *io.PipeReader
	rw *io.PipeWriter
	// wr/ww: client-to-server pipe. Client writes to ww; server reads from wr.
	wr *io.PipeReader
	ww *io.PipeWriter
}

func newTestConn() *testConn {
	rr, rw := io.Pipe()
	wr, ww := io.Pipe()
	return &testConn{rr: rr, rw: rw, wr: wr, ww: ww}
}

func (c *testConn) Read(b []byte) (int, error)         { return c.rr.Read(b) }
func (c *testConn) Write(b []byte) (int, error)        { return c.ww.Write(b) }
func (c *testConn) CloseWrite() error                  { return c.ww.Close() }
func (c *testConn) SetDeadline(_ time.Time) error      { return nil }
func (c *testConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *testConn) SetWriteDeadline(_ time.Time) error { return nil }
func (c *testConn) LocalAddr() net.Addr                { return &net.TCPAddr{} }
func (c *testConn) RemoteAddr() net.Addr               { return &net.TCPAddr{} }

func (c *testConn) Close() error {
	var firstErr error
	for _, closeFn := range []func() error{
		c.rr.Close,
		c.rw.Close,
		c.wr.Close,
		c.ww.Close,
	} {
		if err := closeFn(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// stubDockerExecClient implements DockerExecClient for unit tests.
type stubDockerExecClient struct {
	execCreateFn  func(ctx context.Context, containerID string, options container.ExecOptions) (container.ExecCreateResponse, error)
	execAttachFn  func(ctx context.Context, execID string, config container.ExecAttachOptions) (types.HijackedResponse, error)
	execInspectFn func(ctx context.Context, execID string) (container.ExecInspect, error)
}

func (s *stubDockerExecClient) ContainerExecCreate(ctx context.Context, containerID string, options container.ExecOptions) (container.ExecCreateResponse, error) {
	return s.execCreateFn(ctx, containerID, options)
}
func (s *stubDockerExecClient) ContainerExecAttach(ctx context.Context, execID string, config container.ExecAttachOptions) (types.HijackedResponse, error) {
	return s.execAttachFn(ctx, execID, config)
}
func (s *stubDockerExecClient) ContainerExecInspect(ctx context.Context, execID string) (container.ExecInspect, error) {
	return s.execInspectFn(ctx, execID)
}

// buildFramedTar returns bytes containing a stdcopy-framed Docker exec stdout
// stream holding a single-file tar archive. Panics if archive construction fails.
func buildFramedTar(name string, content []byte) []byte {
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	if err := tw.WriteHeader(&tar.Header{
		Name:     name,
		Size:     int64(len(content)),
		Mode:     0644,
		Typeflag: tar.TypeReg,
	}); err != nil {
		panic(err)
	}
	if _, err := tw.Write(content); err != nil {
		panic(err)
	}
	if err := tw.Close(); err != nil {
		panic(err)
	}

	var framed bytes.Buffer
	if _, err := stdcopy.NewStdWriter(&framed, stdcopy.Stdout).Write(tarBuf.Bytes()); err != nil {
		panic(err)
	}
	return framed.Bytes()
}

// buildFramedStderr returns bytes containing a stdcopy-framed Docker exec
// stderr stream with the given message. Panics on write failure.
func buildFramedStderr(msg string) []byte {
	var buf bytes.Buffer
	if _, err := stdcopy.NewStdWriter(&buf, stdcopy.Stderr).Write([]byte(msg)); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// buildFramedTarHeader returns bytes containing a stdcopy-framed Docker exec
// stdout stream holding a single tar entry described by hdr. Panics on failure.
func buildFramedTarHeader(hdr *tar.Header, content []byte) []byte {
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	if err := tw.WriteHeader(hdr); err != nil {
		panic(err)
	}
	if len(content) > 0 {
		if _, err := tw.Write(content); err != nil {
			panic(err)
		}
	}
	if err := tw.Close(); err != nil {
		panic(err)
	}
	var framed bytes.Buffer
	if _, err := stdcopy.NewStdWriter(&framed, stdcopy.Stdout).Write(tarBuf.Bytes()); err != nil {
		panic(err)
	}
	return framed.Bytes()
}

var _ = Describe("FileService", func() {
	var (
		stub   *stubDockerExecClient
		logger boshlog.Logger
		vmCID  apiv1.VMCID
	)

	BeforeEach(func() {
		logger = boshlog.NewLogger(boshlog.LevelNone)
		vmCID = apiv1.NewVMCID("test-container")
		stub = &stubDockerExecClient{}
		stub.execCreateFn = func(_ context.Context, _ string, _ container.ExecOptions) (container.ExecCreateResponse, error) {
			return container.ExecCreateResponse{ID: "exec-id"}, nil
		}
	})

	Describe("Download", func() {
		DescribeTable("returns an error for invalid sourcePaths",
			func(p string) {
				svc := NewFileService(stub, vmCID, logger)
				_, err := svc.Download(p)
				Expect(err).To(HaveOccurred())
			},
			Entry("empty string", ""),
			Entry("root directory", "/"),
			Entry("relative path", "etc/resolv.conf"),
			Entry("dot-dot at end", "/etc/foo/.."),
			Entry("dot-dot in middle", "/etc/../foo"),
			Entry("trailing-slash directory", "/etc/"),
		)

		It("returns the file content from the container tar stream", func() {
			fileContent := []byte("agent-env-contents")
			// tar -c -f - -C /etc -- file.json produces an entry named "file.json".
			framedTar := buildFramedTar("file.json", fileContent)

			stub.execAttachFn = func(_ context.Context, _ string, _ container.ExecAttachOptions) (types.HijackedResponse, error) {
				tc := newTestConn()
				go func() {
					defer GinkgoRecover()
					_, err := tc.rw.Write(framedTar)
					Expect(err).NotTo(HaveOccurred())
					Expect(tc.rw.Close()).To(Succeed())
				}()
				return types.NewHijackedResponse(tc, ""), nil
			}
			stub.execInspectFn = func(_ context.Context, _ string) (container.ExecInspect, error) {
				return container.ExecInspect{Running: false, ExitCode: 0}, nil
			}

			svc := NewFileService(stub, vmCID, logger)
			got, err := svc.Download("/etc/file.json")
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(fileContent))
		})

		It("includes the tar command's stderr in the error when the exit code is non-zero", func() {
			errMsg := "tar: cannot open: No such file or directory\n"

			stub.execAttachFn = func(_ context.Context, _ string, _ container.ExecAttachOptions) (types.HijackedResponse, error) {
				tc := newTestConn()
				go func() {
					defer GinkgoRecover()
					_, err := tc.rw.Write(buildFramedStderr(errMsg))
					Expect(err).NotTo(HaveOccurred())
					Expect(tc.rw.Close()).To(Succeed())
				}()
				return types.NewHijackedResponse(tc, ""), nil
			}
			stub.execInspectFn = func(_ context.Context, _ string) (container.ExecInspect, error) {
				return container.ExecInspect{Running: false, ExitCode: 1}, nil
			}

			svc := NewFileService(stub, vmCID, logger)
			_, err := svc.Download("/missing/file")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("exit status 1"))
			Expect(err.Error()).To(ContainSubstring(errMsg))
		})

		It("returns an error when the tar entry is not a regular file", func() {
			framedTar := buildFramedTarHeader(&tar.Header{
				Name:     "file.json",
				Typeflag: tar.TypeDir,
			}, nil)

			stub.execAttachFn = func(_ context.Context, _ string, _ container.ExecAttachOptions) (types.HijackedResponse, error) {
				tc := newTestConn()
				go func() {
					defer GinkgoRecover()
					_, err := tc.rw.Write(framedTar)
					Expect(err).NotTo(HaveOccurred())
					Expect(tc.rw.Close()).To(Succeed())
				}()
				return types.NewHijackedResponse(tc, ""), nil
			}
			stub.execInspectFn = func(_ context.Context, _ string) (container.ExecInspect, error) {
				return container.ExecInspect{Running: false, ExitCode: 0}, nil
			}

			svc := NewFileService(stub, vmCID, logger)
			_, err := svc.Download("/etc/file.json")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not a regular file"))
		})

		It("returns an error when the tar entry name does not match the expected file", func() {
			framedTar := buildFramedTarHeader(&tar.Header{
				Name:     "other.json",
				Size:     4,
				Mode:     0644,
				Typeflag: tar.TypeReg,
			}, []byte("data"))

			stub.execAttachFn = func(_ context.Context, _ string, _ container.ExecAttachOptions) (types.HijackedResponse, error) {
				tc := newTestConn()
				go func() {
					defer GinkgoRecover()
					_, err := tc.rw.Write(framedTar)
					Expect(err).NotTo(HaveOccurred())
					Expect(tc.rw.Close()).To(Succeed())
				}()
				return types.NewHijackedResponse(tc, ""), nil
			}
			stub.execInspectFn = func(_ context.Context, _ string) (container.ExecInspect, error) {
				return container.ExecInspect{Running: false, ExitCode: 0}, nil
			}

			svc := NewFileService(stub, vmCID, logger)
			_, err := svc.Download("/etc/file.json")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unexpected name"))
			Expect(err.Error()).To(ContainSubstring("other.json"))
		})
	})

	Describe("Upload", func() {
		It("returns an error when destinationPath is invalid", func() {
			svc := NewFileService(stub, vmCID, logger)
			for _, badPath := range []string{"", "/", "relative/path", "/etc/foo/.."} {
				err := svc.Upload(badPath, []byte("data"))
				Expect(err).To(HaveOccurred(), "expected error for path: %q", badPath)
				Expect(err.Error()).To(ContainSubstring("clean absolute path"), "for path: %q", badPath)
			}
		})

		It("streams a tar archive to the container with the correct path and permissions", func() {
			fileContent := []byte(`{"agent":"settings"}`)
			destPath := "/var/vcap/bosh/settings.json"

			stub.execCreateFn = func(_ context.Context, _ string, opts container.ExecOptions) (container.ExecCreateResponse, error) {
				Expect(opts.Cmd).To(ContainElements("tar", "-x"))
				Expect(opts.AttachStdin).To(BeTrue())
				return container.ExecCreateResponse{ID: "exec-id"}, nil
			}

			// Use a channel to safely pass tar bytes from the server goroutine to
			// the main test goroutine, establishing the required happens-before.
			tarCh := make(chan []byte, 1)
			stub.execAttachFn = func(_ context.Context, _ string, _ container.ExecAttachOptions) (types.HijackedResponse, error) {
				tc := newTestConn()
				go func() {
					defer GinkgoRecover()
					data, err := io.ReadAll(tc.wr)
					Expect(err).NotTo(HaveOccurred())
					tarCh <- data
					Expect(tc.rw.Close()).To(Succeed())
				}()
				return types.NewHijackedResponse(tc, ""), nil
			}
			stub.execInspectFn = func(_ context.Context, _ string) (container.ExecInspect, error) {
				return container.ExecInspect{Running: false, ExitCode: 0}, nil
			}

			svc := NewFileService(stub, vmCID, logger)
			err := svc.Upload(destPath, fileContent)
			Expect(err).NotTo(HaveOccurred())

			// Verify the received bytes form a valid tar archive with the right
			// path (leading "/" stripped) and file mode.
			tr := tar.NewReader(bytes.NewReader(<-tarCh))
			hdr, err := tr.Next()
			Expect(err).NotTo(HaveOccurred())
			Expect(hdr.Name).To(Equal("var/vcap/bosh/settings.json"))
			Expect(hdr.Mode).To(Equal(int64(0640)))
			body, err := io.ReadAll(tr)
			Expect(err).NotTo(HaveOccurred())
			Expect(body).To(Equal(fileContent))
		})

		It("returns an error with stderr when the tar extraction fails", func() {
			errMsg := "tar: /var/vcap: Permission denied\n"

			stub.execAttachFn = func(_ context.Context, _ string, _ container.ExecAttachOptions) (types.HijackedResponse, error) {
				tc := newTestConn()
				go func() {
					defer GinkgoRecover()
					// Simulate tar writing stderr before consuming all stdin
					// (e.g., it detects an error from the first tar header block
					// before the full archive has been sent). Without concurrent
					// stderr draining in dockerExecWithStdin this would deadlock:
					// io.Copy blocks waiting for stdin to be read, while the
					// server blocks waiting for its stderr write to be consumed.
					_, err := tc.rw.Write(buildFramedStderr(errMsg))
					Expect(err).NotTo(HaveOccurred())
					if _, err := io.Copy(io.Discard, tc.wr); err != nil {
						Expect(err).NotTo(HaveOccurred())
					}
					Expect(tc.rw.Close()).To(Succeed())
				}()
				return types.NewHijackedResponse(tc, ""), nil
			}
			stub.execInspectFn = func(_ context.Context, _ string) (container.ExecInspect, error) {
				return container.ExecInspect{Running: false, ExitCode: 2}, nil
			}

			svc := NewFileService(stub, vmCID, logger)
			err := svc.Upload("/var/vcap/settings.json", []byte("data"))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("exit status 2"))
			Expect(err.Error()).To(ContainSubstring(errMsg))
		})
	})
})
