package stemcell

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"

	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshsys "github.com/cloudfoundry/bosh-utils/system"
	"gopkg.in/yaml.v3"
)

// Metadata represents the metadata for a light stemcell
type Metadata struct {
	Name            string          `yaml:"name"`
	Version         string          `yaml:"version"`
	StemcellFormats []string        `yaml:"stemcell_formats"`
	CloudProperties CloudProperties `yaml:"cloud_properties"`
}

// CloudProperties represents IaaS-specific stemcell properties
type CloudProperties struct {
	ImageReference string `yaml:"image_reference"`
	Digest         string `yaml:"digest"`
}

// IsLightStemcell returns true if this is a light stemcell
func (m *Metadata) IsLightStemcell() bool {
	// Check if stemcell_formats contains "docker-light"
	for _, format := range m.StemcellFormats {
		if format == "docker-light" {
			return true
		}
	}
	return false
}

// GetImageReference returns the Docker image reference from metadata
func (m *Metadata) GetImageReference() string {
	return m.CloudProperties.ImageReference
}

// GetDigest returns the expected SHA256 digest if available
func (m *Metadata) GetDigest() string {
	return m.CloudProperties.Digest
}

// MetadataParser handles parsing stemcell metadata
type MetadataParser struct {
	fs boshsys.FileSystem
}

// NewMetadataParser creates a new metadata parser
func NewMetadataParser(fs boshsys.FileSystem) MetadataParser {
	return MetadataParser{fs: fs}
}

// ParseFromPath parses stemcell metadata from a path
// Returns metadata and true if it's a light stemcell, or nil and false if it's a traditional stemcell
// The path can be either:
// - A path to an 'image' file (BOSH Director extracts stemcells and passes the path to the image file)
// - A directory containing the extracted stemcell
// - A tar.gz archive
func (p MetadataParser) ParseFromPath(imagePath string) (*Metadata, bool, error) {
	// Check if imagePath exists
	fileInfo, err := p.fs.Stat(imagePath)
	if err != nil {
		return nil, false, bosherr.WrapErrorf(err, "Checking stemcell path '%s'", imagePath)
	}

	// If it's a directory, look for stemcell.MF in it
	if fileInfo.IsDir() {
		return p.parseFromDirectory(imagePath)
	}

	// If it's a file, check if it's named 'image' (extracted stemcell)
	// In this case, look for stemcell.MF in the parent directory
	if filepath.Base(imagePath) == "image" {
		parentDir := filepath.Dir(imagePath)
		return p.parseFromDirectory(parentDir)
	}

	// Otherwise, try to parse as tar.gz archive
	return p.parseFromArchive(imagePath)
}

func (p MetadataParser) parseFromDirectory(dirPath string) (*Metadata, bool, error) {
	// Try stemcell.MF first, then stemcell_metadata.yml
	metadataFiles := []string{"stemcell.MF", "stemcell_metadata.yml"}

	for _, filename := range metadataFiles {
		metadataPath := filepath.Join(dirPath, filename)
		if p.fs.FileExists(metadataPath) {
			data, err := p.fs.ReadFile(metadataPath)
			if err != nil {
				return nil, false, bosherr.WrapErrorf(err, "Reading metadata file '%s'", metadataPath)
			}

			var metadata Metadata
			err = yaml.Unmarshal(data, &metadata)
			if err != nil {
				return nil, false, bosherr.WrapErrorf(err, "Unmarshaling metadata from '%s'", metadataPath)
			}

			// Check if this is a light stemcell using the helper method
			if metadata.IsLightStemcell() {
				return &metadata, true, nil
			}

			// Has metadata but not a light stemcell
			return nil, false, nil
		}
	}

	// No metadata file found, it's a traditional stemcell
	return nil, false, nil
}

func (p MetadataParser) parseFromArchive(archivePath string) (*Metadata, bool, error) {
	file, err := p.fs.OpenFile(archivePath, os.O_RDONLY, 0)
	if err != nil {
		return nil, false, bosherr.WrapErrorf(err, "Opening stemcell archive '%s'", archivePath)
	}
	defer file.Close() //nolint:errcheck

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		// If it's not a gzip file, it's not a light stemcell
		return nil, false, nil
	}
	defer gzipReader.Close() //nolint:errcheck

	tarReader := tar.NewReader(gzipReader)

	// Look for stemcell.MF or stemcell_metadata.yml in the archive
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, false, bosherr.WrapError(err, "Reading tar archive")
		}

		// Check for metadata files that indicate a light stemcell
		if header.Name == "stemcell.MF" || header.Name == "stemcell_metadata.yml" {
			data, err := io.ReadAll(tarReader)
			if err != nil {
				return nil, false, bosherr.WrapError(err, "Reading metadata file")
			}

			var metadata Metadata
			err = yaml.Unmarshal(data, &metadata)
			if err != nil {
				return nil, false, bosherr.WrapError(err, "Unmarshaling metadata")
			}

			// Check if this is a light stemcell using the helper method
			if metadata.IsLightStemcell() {
				return &metadata, true, nil
			}

			// Has metadata but not a light stemcell
			return nil, false, nil
		}
	}

	// No metadata file found, it's a traditional stemcell
	return nil, false, nil
}
