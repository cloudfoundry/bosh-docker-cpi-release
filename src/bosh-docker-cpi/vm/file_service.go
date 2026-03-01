package vm

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"

	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	"github.com/docker/docker/api/types/container"
	dkrclient "github.com/docker/docker/client"
)

//counterfeiter:generate . FileService

type FileService interface {
	Upload(string, []byte) error
	Download(string) ([]byte, error)
}

type fileService struct {
	dkrClient *dkrclient.Client
	vmCID     apiv1.VMCID

	logTag string
	logger boshlog.Logger
}

func NewFileService(
	dkrClient *dkrclient.Client,
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

func (s *fileService) Download(sourcePath string) ([]byte, error) {
	sourceFileName := filepath.Base(sourcePath)

	ctx := context.Background()

	readCloser, _, err := s.dkrClient.CopyFromContainer(ctx, s.vmCID.AsString(), sourcePath)
	if err != nil {
		return nil, bosherr.WrapError(err, "Copying from container")
	}

	defer readCloser.Close() //nolint:errcheck

	tarReader := tar.NewReader(readCloser)

	_, err = tarReader.Next()
	if err != nil {
		return []byte{}, bosherr.WrapErrorf(err, "Reading tar header for '%s'", sourceFileName)
	}

	return io.ReadAll(tarReader)
}

func (s *fileService) Upload(destinationPath string, contents []byte) error {
	tarReader, err := s.tarReaderWithPath(destinationPath, contents)
	if err != nil {
		return bosherr.WrapError(err, "Creating tar")
	}

	copyOpts := container.CopyToContainerOptions{}
	err = s.dkrClient.CopyToContainer(
		context.TODO(), s.vmCID.AsString(), "/", tarReader, copyOpts)
	if err != nil {
		return bosherr.WrapError(err, "Copying to container")
	}

	return nil
}

func (s *fileService) tarReaderWithPath(fullPath string, contents []byte) (io.Reader, error) {
	cleanPath := filepath.Clean(fullPath)
	if len(cleanPath) > 0 && cleanPath[0] == '/' {
		cleanPath = cleanPath[1:]
	}

	tarBytes := &bytes.Buffer{}
	tarWriter := tar.NewWriter(tarBytes)

	// Add directory entries for all parent directories
	dir := filepath.Dir(cleanPath)
	if dir != "." {
		dirParts := strings.Split(dir, "/")
		for i := range dirParts {
			dirEntry := strings.Join(dirParts[:i+1], "/") + "/"
			err := tarWriter.WriteHeader(&tar.Header{
				Typeflag: tar.TypeDir,
				Name:     dirEntry,
				Mode:     0755,
			})
			if err != nil {
				return nil, bosherr.WrapErrorf(err, "Writing tar dir header for '%s'", dirEntry)
			}
		}
	}

	err := tarWriter.WriteHeader(&tar.Header{
		Name: cleanPath,
		Size: int64(len(contents)),
		Mode: 0640,
	})
	if err != nil {
		return nil, bosherr.WrapError(err, "Writing tar header")
	}

	_, err = tarWriter.Write(contents)
	if err != nil {
		return nil, bosherr.WrapError(err, "Writing file to tar")
	}

	err = tarWriter.Close()
	if err != nil {
		return nil, bosherr.WrapError(err, "Closing tar writer")
	}

	return tarBytes, nil
}
