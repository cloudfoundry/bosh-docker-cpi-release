package vm

import (
	"archive/tar"
	"bytes"
	"io"
	"path/filepath"

	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	dkrtypes "github.com/docker/docker/api/types"
	dkrclient "github.com/docker/docker/client"
	"golang.org/x/net/context"
)

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

	defer readCloser.Close()

	tarReader := tar.NewReader(readCloser)

	_, err = tarReader.Next()
	if err != nil {
		return []byte{}, bosherr.WrapErrorf(err, "Reading tar header for '%s'", sourceFileName)
	}

	return io.ReadAll(tarReader)
}

func (s *fileService) Upload(destinationPath string, contents []byte) error {
	destinationFileName := filepath.Base(destinationPath)

	// Stream in settings file to a temporary directory
	// so that tar (running as vcap) has permission to unpack into dir.
	tarReader, err := s.tarReader(destinationFileName, contents)
	if err != nil {
		return bosherr.WrapError(err, "Creating tar")
	}

	copyOpts := dkrtypes.CopyToContainerOptions{}

	err = s.dkrClient.CopyToContainer(
		context.TODO(), s.vmCID.AsString(), filepath.Dir(destinationPath), tarReader, copyOpts)
	if err != nil {
		return bosherr.WrapError(err, "Copying to container")
	}

	return nil
}

func (s *fileService) tarReader(fileName string, contents []byte) (io.Reader, error) {
	tarBytes := &bytes.Buffer{}

	tarWriter := tar.NewWriter(tarBytes)

	fileHeader := &tar.Header{
		Name: fileName,
		Size: int64(len(contents)),
		Mode: 0640,
	}

	err := tarWriter.WriteHeader(fileHeader)
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
