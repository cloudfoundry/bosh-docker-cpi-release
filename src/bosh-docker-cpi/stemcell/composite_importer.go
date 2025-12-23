package stemcell

import (
	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	boshsys "github.com/cloudfoundry/bosh-utils/system"
	boshuuid "github.com/cloudfoundry/bosh-utils/uuid"

	dkrclient "github.com/docker/docker/client"
)

// CompositeImporter detects stemcell type and routes to the appropriate importer
type CompositeImporter struct {
	fsImporter     FSImporter
	lightImporter  LightImporter
	metadataParser MetadataParser

	logTag string
	logger boshlog.Logger
}

// NewCompositeImporter creates a new composite importer
func NewCompositeImporter(
	dkrClient *dkrclient.Client,
	fs boshsys.FileSystem,
	uuidGen boshuuid.Generator,
	verifyDigest bool,
	logger boshlog.Logger,
) CompositeImporter {
	return CompositeImporter{
		fsImporter:     NewFSImporter(dkrClient, fs, uuidGen, logger),
		lightImporter:  NewLightImporter(dkrClient, fs, uuidGen, verifyDigest, logger),
		metadataParser: NewMetadataParser(fs),

		logTag: "CompositeImporter",
		logger: logger,
	}
}

// ImportFromPath imports a stemcell, automatically detecting if it's light or traditional
func (i CompositeImporter) ImportFromPath(imagePath string) (Stemcell, error) {
	i.logger.Debug(i.logTag, "Detecting stemcell type for '%s'", imagePath)

	// Try to parse metadata to detect if it's a light stemcell
	_, isLight, err := i.metadataParser.ParseFromPath(imagePath)
	if err != nil {
		return nil, bosherr.WrapError(err, "Parsing stemcell metadata")
	}

	if isLight {
		i.logger.Debug(i.logTag, "Detected light stemcell, using LightImporter")
		return i.lightImporter.ImportFromPath(imagePath)
	}

	i.logger.Debug(i.logTag, "Detected traditional stemcell, using FSImporter")
	return i.fsImporter.ImportFromPath(imagePath)
}
