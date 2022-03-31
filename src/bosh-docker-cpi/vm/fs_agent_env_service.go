package vm

import (
	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"
)

type fsAgentEnvService struct {
	fileService  FileService
	settingsPath string

	logTag string
	logger boshlog.Logger
}

func NewFSAgentEnvService(fileService FileService, logger boshlog.Logger) AgentEnvService {
	return fsAgentEnvService{
		fileService:  fileService,
		settingsPath: "/var/vcap/bosh/warden-cpi-agent-env.json",

		logTag: "vm.FSAgentEnvService",
		logger: logger,
	}
}

func (s fsAgentEnvService) Fetch() (apiv1.AgentEnv, error) {
	bytes, err := s.fileService.Download(s.settingsPath)
	if err != nil {
		return nil, bosherr.WrapError(err, "Downloading agent env from container")
	}

	agentEnv, err := apiv1.AgentEnvFactory{}.FromBytes(bytes)
	if err != nil {
		return nil, bosherr.WrapError(err, "Unmarshalling agent env")
	}

	return agentEnv, nil
}

func (s fsAgentEnvService) Update(agentEnv apiv1.AgentEnv) error {
	bytes, err := agentEnv.AsBytes()
	if err != nil {
		return bosherr.WrapError(err, "Marshalling agent env")
	}

	return s.fileService.Upload(s.settingsPath, bytes)
}
