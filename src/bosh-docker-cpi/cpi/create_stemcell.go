package cpi

import (
	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	bosherr "github.com/cloudfoundry/bosh-utils/errors"

	bstem "bosh-docker-cpi/stemcell"
)

// CreateStemcellMethod handles creating stemcells from images
type CreateStemcellMethod struct {
	stemcellImporter bstem.Importer
}

// NewCreateStemcellMethod creates a new CreateStemcellMethod with the given stemcell importer
func NewCreateStemcellMethod(stemcellImporter bstem.Importer) CreateStemcellMethod {
	return CreateStemcellMethod{stemcellImporter: stemcellImporter}
}

// CreateStemcell creates a stemcell from the specified image path
func (a CreateStemcellMethod) CreateStemcell(
	imagePath string, _ apiv1.StemcellCloudProps) (apiv1.StemcellCID, error) {

	stemcell, err := a.stemcellImporter.ImportFromPath(imagePath)
	if err != nil {
		return apiv1.StemcellCID{}, bosherr.WrapErrorf(err, "Importing stemcell from '%s'", imagePath)
	}

	return stemcell.ID(), nil
}
