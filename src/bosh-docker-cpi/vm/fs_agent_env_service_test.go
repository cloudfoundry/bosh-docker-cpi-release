package vm_test

import (
	"errors"

	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "bosh-docker-cpi/vm"
	"bosh-docker-cpi/vm/vmfakes"
)

var _ = Describe("FSAgentEnvService", func() {
	var (
		fakeFileService *vmfakes.FakeFileService
		service         AgentEnvService
		logger          boshlog.Logger
	)

	BeforeEach(func() {
		fakeFileService = &vmfakes.FakeFileService{}
		logger = boshlog.NewLogger(boshlog.LevelNone)
		service = NewFSAgentEnvService(fakeFileService, logger)
	})

	Describe("Update", func() {
		It("uploads the agent env as JSON to the settings path", func() {
			agentEnv := apiv1.AgentEnvFactory{}.ForVM(
				apiv1.NewAgentID("agent-1"),
				apiv1.NewVMCID("vm-1"),
				apiv1.Networks{},
				apiv1.VMEnv{},
				apiv1.AgentOptions{Mbus: "https://user:pass@127.0.0.1:4321/agent"},
			)

			err := service.Update(agentEnv)
			Expect(err).NotTo(HaveOccurred())

			Expect(fakeFileService.UploadCallCount()).To(Equal(1))
			path, contents := fakeFileService.UploadArgsForCall(0)
			Expect(path).To(Equal("/var/vcap/bosh/warden-cpi-agent-env.json"))
			Expect(contents).NotTo(BeEmpty())
		})

		It("returns error when upload fails", func() {
			agentEnv := apiv1.AgentEnvFactory{}.ForVM(
				apiv1.NewAgentID("agent-1"),
				apiv1.NewVMCID("vm-1"),
				apiv1.Networks{},
				apiv1.VMEnv{},
				apiv1.AgentOptions{Mbus: "https://user:pass@127.0.0.1:4321/agent"},
			)

			fakeFileService.UploadReturns(errors.New("upload-error"))

			err := service.Update(agentEnv)
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(ContainSubstring("upload-error")))
		})
	})

	Describe("Fetch", func() {
		It("downloads and parses the agent env from the settings path", func() {
			agentEnvJSON := `{
				"agent_id": "agent-1",
				"vm": {"name": "vm-1", "id": "vm-1"},
				"mbus": "https://user:pass@127.0.0.1:4321/agent"
			}`
			fakeFileService.DownloadReturns([]byte(agentEnvJSON), nil)

			agentEnv, err := service.Fetch()
			Expect(err).NotTo(HaveOccurred())
			Expect(agentEnv).NotTo(BeNil())

			Expect(fakeFileService.DownloadCallCount()).To(Equal(1))
			Expect(fakeFileService.DownloadArgsForCall(0)).To(Equal("/var/vcap/bosh/warden-cpi-agent-env.json"))
		})

		It("returns error when download fails", func() {
			fakeFileService.DownloadReturns(nil, errors.New("download-error"))

			_, err := service.Fetch()
			Expect(err).To(MatchError(ContainSubstring("Downloading agent env")))
			Expect(err).To(MatchError(ContainSubstring("download-error")))
		})

		It("returns error when agent env JSON is invalid", func() {
			fakeFileService.DownloadReturns([]byte("not-json"), nil)

			_, err := service.Fetch()
			Expect(err).To(MatchError(ContainSubstring("Unmarshalling agent env")))
		})
	})
})
