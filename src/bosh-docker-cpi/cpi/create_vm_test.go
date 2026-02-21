package cpi_test

import (
	"errors"

	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"bosh-docker-cpi/cpi"
	"bosh-docker-cpi/stemcell/stemcellfakes"
	"bosh-docker-cpi/vm/vmfakes"
)

var _ = Describe("CreateVMMethod", func() {
	var (
		fakeStemcellFinder *stemcellfakes.FakeFinder
		fakeStemcell       *stemcellfakes.FakeStemcell
		fakeVMCreator      *vmfakes.FakeCreator
		fakeVM             *vmfakes.FakeVM
		method             cpi.CreateVMMethod

		agentID     apiv1.AgentID
		stemcellCID apiv1.StemcellCID
		cloudProps  apiv1.VMCloudProps
		networks    apiv1.Networks
		diskCIDs    []apiv1.DiskCID
		env         apiv1.VMEnv
	)

	BeforeEach(func() {
		fakeStemcellFinder = &stemcellfakes.FakeFinder{}
		fakeStemcell = &stemcellfakes.FakeStemcell{}
		fakeVMCreator = &vmfakes.FakeCreator{}
		fakeVM = &vmfakes.FakeVM{}
		method = cpi.NewCreateVMMethod(fakeStemcellFinder, fakeVMCreator)

		agentID = apiv1.NewAgentID("agent-123")
		stemcellCID = apiv1.NewStemcellCID("stemcell-123")
		cloudProps = apiv1.NewVMCloudPropsFromMap(map[string]interface{}{})
		networks = apiv1.Networks{}
		diskCIDs = []apiv1.DiskCID{}
		env = apiv1.VMEnv{}
	})

	Describe("CreateVMV2", func() {
		It("finds the stemcell and creates a VM", func() {
			expectedVMCID := apiv1.NewVMCID("c-new-vm")
			fakeStemcellFinder.FindReturns(fakeStemcell, nil)
			fakeVM.IDReturns(expectedVMCID)
			fakeVMCreator.CreateReturns(fakeVM, nil)

			vmCID, returnedNetworks, err := method.CreateVMV2(agentID, stemcellCID, cloudProps, networks, diskCIDs, env)
			Expect(err).NotTo(HaveOccurred())
			Expect(vmCID).To(Equal(expectedVMCID))
			Expect(returnedNetworks).To(Equal(networks))

			Expect(fakeStemcellFinder.FindCallCount()).To(Equal(1))
			Expect(fakeStemcellFinder.FindArgsForCall(0)).To(Equal(stemcellCID))

			Expect(fakeVMCreator.CreateCallCount()).To(Equal(1))
			passedAgentID, passedStemcell, _, _, _, _ := fakeVMCreator.CreateArgsForCall(0)
			Expect(passedAgentID).To(Equal(agentID))
			Expect(passedStemcell).To(Equal(fakeStemcell))
		})

		It("returns error when finding the stemcell fails", func() {
			fakeStemcellFinder.FindReturns(nil, errors.New("stemcell-find-error"))

			_, _, err := method.CreateVMV2(agentID, stemcellCID, cloudProps, networks, diskCIDs, env)
			Expect(err).To(MatchError(ContainSubstring("Finding stemcell")))
			Expect(err).To(MatchError(ContainSubstring("stemcell-find-error")))
		})

		It("returns error when creating the VM fails", func() {
			fakeStemcellFinder.FindReturns(fakeStemcell, nil)
			fakeVMCreator.CreateReturns(nil, errors.New("vm-create-error"))

			_, _, err := method.CreateVMV2(agentID, stemcellCID, cloudProps, networks, diskCIDs, env)
			Expect(err).To(MatchError(ContainSubstring("Creating VM")))
			Expect(err).To(MatchError(ContainSubstring("vm-create-error")))
		})
	})

	Describe("CreateVM", func() {
		It("delegates to CreateVMV2 and returns the VM CID", func() {
			expectedVMCID := apiv1.NewVMCID("c-new-vm")
			fakeStemcellFinder.FindReturns(fakeStemcell, nil)
			fakeVM.IDReturns(expectedVMCID)
			fakeVMCreator.CreateReturns(fakeVM, nil)

			vmCID, err := method.CreateVM(agentID, stemcellCID, cloudProps, networks, diskCIDs, env)
			Expect(err).NotTo(HaveOccurred())
			Expect(vmCID).To(Equal(expectedVMCID))
		})
	})
})
