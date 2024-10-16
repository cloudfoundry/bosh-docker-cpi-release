package cpi

import (
	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	bosherr "github.com/cloudfoundry/bosh-utils/errors"

	bstem "bosh-docker-cpi/stemcell"
)

type CreateStemcellMethod struct {
	stemcellImporter bstem.Importer
}

func NewCreateStemcellMethod(stemcellImporter bstem.Importer) CreateStemcellMethod {
	return CreateStemcellMethod{stemcellImporter: stemcellImporter}
}

func (a CreateStemcellMethod) CreateStemcell(
	imagePath string, _ apiv1.StemcellCloudProps) (apiv1.StemcellCID, error) {

	stemcell, err := a.stemcellImporter.ImportFromPath(imagePath)
	if err != nil {
		return apiv1.StemcellCID{}, bosherr.WrapErrorf(err, "Importing stemcell from '%s'", imagePath)
	}

	return stemcell.ID(), nil
}
