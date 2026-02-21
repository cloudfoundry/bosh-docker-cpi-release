package cpi_test

import (
	"errors"

	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"bosh-docker-cpi/cpi"
	"bosh-docker-cpi/stemcell/stemcellfakes"
)

var _ = Describe("CreateStemcellMethod", func() {
	var (
		fakeImporter *stemcellfakes.FakeImporter
		fakeStemcell *stemcellfakes.FakeStemcell
		method       cpi.CreateStemcellMethod
	)

	BeforeEach(func() {
		fakeImporter = &stemcellfakes.FakeImporter{}
		fakeStemcell = &stemcellfakes.FakeStemcell{}
		method = cpi.NewCreateStemcellMethod(fakeImporter)
	})

	It("imports the stemcell and returns its CID", func() {
		expectedCID := apiv1.NewStemcellCID("imported-stemcell-id")
		fakeStemcell.IDReturns(expectedCID)
		fakeImporter.ImportFromPathReturns(fakeStemcell, nil)

		cid, err := method.CreateStemcell("/path/to/image", nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(cid).To(Equal(expectedCID))

		Expect(fakeImporter.ImportFromPathCallCount()).To(Equal(1))
		Expect(fakeImporter.ImportFromPathArgsForCall(0)).To(Equal("/path/to/image"))
	})

	It("returns error when importing fails", func() {
		fakeImporter.ImportFromPathReturns(nil, errors.New("import-error"))

		_, err := method.CreateStemcell("/path/to/image", nil)
		Expect(err).To(MatchError(ContainSubstring("Importing stemcell")))
		Expect(err).To(MatchError(ContainSubstring("import-error")))
	})
})
