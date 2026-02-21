package cpi_test

import (
	"errors"

	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"bosh-docker-cpi/cpi"
	"bosh-docker-cpi/stemcell/stemcellfakes"
)

var _ = Describe("DeleteStemcellMethod", func() {
	var (
		fakeFinder   *stemcellfakes.FakeFinder
		fakeStemcell *stemcellfakes.FakeStemcell
		method       cpi.DeleteStemcellMethod
		stemcellCID  apiv1.StemcellCID
	)

	BeforeEach(func() {
		fakeFinder = &stemcellfakes.FakeFinder{}
		fakeStemcell = &stemcellfakes.FakeStemcell{}
		method = cpi.NewDeleteStemcellMethod(fakeFinder)
		stemcellCID = apiv1.NewStemcellCID("fake-stemcell-id")
	})

	It("finds and deletes the stemcell", func() {
		fakeFinder.FindReturns(fakeStemcell, nil)
		fakeStemcell.DeleteReturns(nil)

		err := method.DeleteStemcell(stemcellCID)
		Expect(err).NotTo(HaveOccurred())

		Expect(fakeFinder.FindCallCount()).To(Equal(1))
		Expect(fakeFinder.FindArgsForCall(0)).To(Equal(stemcellCID))
		Expect(fakeStemcell.DeleteCallCount()).To(Equal(1))
	})

	It("returns error when finding the stemcell fails", func() {
		fakeFinder.FindReturns(nil, errors.New("find-error"))

		err := method.DeleteStemcell(stemcellCID)
		Expect(err).To(MatchError(ContainSubstring("Finding stemcell")))
		Expect(err).To(MatchError(ContainSubstring("find-error")))
	})

	It("returns error when deleting the stemcell fails", func() {
		fakeFinder.FindReturns(fakeStemcell, nil)
		fakeStemcell.DeleteReturns(errors.New("delete-error"))

		err := method.DeleteStemcell(stemcellCID)
		Expect(err).To(MatchError(ContainSubstring("Deleting stemcell")))
		Expect(err).To(MatchError(ContainSubstring("delete-error")))
	})
})
