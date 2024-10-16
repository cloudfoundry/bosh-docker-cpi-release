package cpi

import (
	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	bosherr "github.com/cloudfoundry/bosh-utils/errors"

	bstem "bosh-docker-cpi/stemcell"
)

type DeleteStemcellMethod struct {
	stemcellFinder bstem.Finder
}

func NewDeleteStemcellMethod(stemcellFinder bstem.Finder) DeleteStemcellMethod {
	return DeleteStemcellMethod{stemcellFinder: stemcellFinder}
}

func (a DeleteStemcellMethod) DeleteStemcell(cid apiv1.StemcellCID) error {
	stemcell, err := a.stemcellFinder.Find(cid)
	if err != nil {
		return bosherr.WrapErrorf(err, "Finding stemcell '%s'", cid)
	}

	err = stemcell.Delete()
	if err != nil {
		return bosherr.WrapErrorf(err, "Deleting stemcell '%s'", cid)
	}

	return nil
}
