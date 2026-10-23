package stemcell

import (
	"context"
	"encoding/json"
	"io"
	"strings"

	digest "github.com/opencontainers/go-digest"

	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	boshsys "github.com/cloudfoundry/bosh-utils/system"
	boshuuid "github.com/cloudfoundry/bosh-utils/uuid"

	dkrimages "github.com/docker/docker/api/types/image"
	dkrclient "github.com/docker/docker/client"
)

// LightImporter imports light stemcells by pulling Docker images from registries
type LightImporter struct {
	dkrClient *dkrclient.Client

	fs             boshsys.FileSystem
	uuidGen        boshuuid.Generator
	metadataParser MetadataParser
	verifyDigest   bool

	logTag string
	logger boshlog.Logger
}

// NewLightImporter creates a new light stemcell importer
func NewLightImporter(
	dkrClient *dkrclient.Client,
	fs boshsys.FileSystem,
	uuidGen boshuuid.Generator,
	verifyDigest bool,
	logger boshlog.Logger,
) LightImporter {
	return LightImporter{
		dkrClient: dkrClient,

		fs:             fs,
		uuidGen:        uuidGen,
		metadataParser: NewMetadataParser(fs),
		verifyDigest:   verifyDigest,

		logTag: "LightImporter",
		logger: logger,
	}
}

// ImportFromPath imports a light stemcell by pulling the referenced Docker image
func (i LightImporter) ImportFromPath(imagePath string) (Stemcell, error) {
	i.logger.Debug(i.logTag, "Importing light stemcell from path '%s'", imagePath)

	// Parse metadata
	metadata, isLight, err := i.metadataParser.ParseFromPath(imagePath)
	if err != nil {
		return nil, bosherr.WrapError(err, "Parsing stemcell metadata")
	}

	if !isLight {
		return nil, bosherr.Error("Not a light stemcell")
	}

	// Get image reference from metadata (supports both new and legacy formats)
	imageReference := metadata.GetImageReference()
	if imageReference == "" {
		return nil, bosherr.Error("Light stemcell metadata missing image reference")
	}

	// Validate image reference for security
	err = i.validateImageReference(imageReference)
	if err != nil {
		return nil, bosherr.WrapError(err, "Validating image reference")
	}

	// Pull the image
	i.logger.Debug(i.logTag, "Pulling image '%s'", imageReference)

	pullOpts := dkrimages.PullOptions{}
	responseBody, err := i.dkrClient.ImagePull(context.TODO(), imageReference, pullOpts)
	if err != nil {
		return nil, bosherr.WrapErrorf(err, "Pulling image '%s'", imageReference)
	}
	defer responseBody.Close() //nolint:errcheck

	// Read and log pull progress
	decoder := json.NewDecoder(responseBody)
	for {
		var event dockerPullEvent
		if err := decoder.Decode(&event); err != nil {
			if err == io.EOF {
				break
			}
			return nil, bosherr.WrapError(err, "Reading pull response")
		}

		if event.Error != "" {
			return nil, bosherr.Errorf("Pull error: %s", event.Error)
		}

		if event.Status != "" {
			i.logger.Debug(i.logTag, "Pull: %s %s", event.Status, event.Progress)
		}
	}

	i.logger.Debug(i.logTag, "Image pulled successfully")

	// Inspect once: verification and the returned immutable image ID must refer
	// to the same snapshot of daemon state even if another pull changes the tag.
	inspect, err := i.dkrClient.ImageInspect(context.TODO(), imageReference)
	if err != nil {
		return nil, bosherr.WrapError(err, "Inspecting pulled image")
	}
	imageID, err := digest.Parse(inspect.ID)
	if err != nil || imageID.Algorithm() != digest.SHA256 {
		return nil, bosherr.Error("Pulled image has no valid SHA256 image ID")
	}

	// Verify image digest if required
	if i.verifyDigest {
		expectedDigest := metadata.GetDigest()
		if expectedDigest == "" {
			return nil, bosherr.Error("Image verification required but no digest provided in stemcell metadata")
		}
		err = i.verifyImageDigest(inspect, expectedDigest)
		if err != nil {
			return nil, bosherr.WrapError(err, "Verifying image digest")
		}
	}

	i.logger.Debug(i.logTag, "Imported light stemcell from path '%s'", imagePath)

	return NewImage(apiv1.NewStemcellCID(inspect.ID), i.dkrClient, i.logger), nil
}

// validateImageReference performs basic validation on the image reference
func (i LightImporter) validateImageReference(imageRef string) error {
	if imageRef == "" {
		return bosherr.Error("Image reference cannot be empty")
	}

	// Check for malicious patterns
	if strings.Contains(imageRef, "..") {
		return bosherr.Error("Image reference contains invalid path traversal pattern")
	}

	// Basic validation: should contain at least a registry hostname or image name
	// Format can be:
	// - registry.com/repo/image:tag
	// - registry.com/repo/image@sha256:digest
	// - repo/image:tag (defaults to docker.io)
	// - image:tag (defaults to docker.io/library)

	// Check for at least one character before and after separator
	if strings.HasPrefix(imageRef, "/") || strings.HasPrefix(imageRef, ":") || strings.HasPrefix(imageRef, "@") {
		return bosherr.Errorf("Image reference has invalid format: %s", imageRef)
	}

	i.logger.Debug(i.logTag, "Image reference validated: %s", imageRef)
	return nil
}

// verifyImageDigest verifies the pulled image matches the expected SHA256 digest
func (i LightImporter) verifyImageDigest(inspect dkrimages.InspectResponse, expectedDigest string) error {
	// Docker image IDs are in the format sha256:hash
	// We need to extract just the hash part
	actualDigest := inspect.ID
	if len(actualDigest) > 7 && actualDigest[:7] == "sha256:" {
		actualDigest = actualDigest[7:]
	}

	// The expected digest might also have the sha256: prefix
	expectedDigestClean := expectedDigest
	if len(expectedDigest) > 7 && expectedDigest[:7] == "sha256:" {
		expectedDigestClean = expectedDigest[7:]
	}

	if actualDigest != expectedDigestClean {
		// Also check RepoDigests which contain the content-addressable digest
		digestMatch := false
		for _, repoDigest := range inspect.RepoDigests {
			// RepoDigests are in the format "registry/repo@sha256:hexdigest"
			parts := strings.Split(repoDigest, "@sha256:")
			if len(parts) == 2 {
				digest := parts[1]
				if digest == expectedDigestClean {
					digestMatch = true
					break
				}
			}
		}

		if !digestMatch {
			return bosherr.Errorf(
				"Image digest mismatch: expected %s, got %s (repo digests: %v)",
				expectedDigestClean, actualDigest, inspect.RepoDigests,
			)
		}
	}

	i.logger.Debug(i.logTag, "Image digest verified successfully")
	return nil
}

// dockerPullEvent represents a Docker pull progress event
type dockerPullEvent struct {
	Status   string `json:"status,omitempty"`
	Progress string `json:"progress,omitempty"`
	Error    string `json:"error,omitempty"`
}
