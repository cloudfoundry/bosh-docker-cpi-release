package apiv1

type VMs interface {
	CreateVM(AgentID, StemcellCID, VMCloudProps, Networks, []DiskCID, VMEnv) (VMCID, error)
	DeleteVM(VMCID) error

	SetVMMetadata(VMCID, VMMeta) error
	HasVM(VMCID) (bool, error)

	RebootVM(VMCID) error
	GetDisks(VMCID) ([]DiskCID, error)
}

type VMCloudProps interface {
	As(interface{}) error
}

type VMCID struct {
	cloudID
}

type AgentID struct {
	cloudID
}

type VMMeta struct {
	cloudKVs
}

type VMEnv struct {
	cloudKVs
}

func NewVMCID(cid string) VMCID {
	if cid == "" {
		panic("Internal incosistency: VM CID must not be empty")
	}
	return VMCID{cloudID{cid}}
}

func NewAgentID(id string) AgentID {
	if id == "" {
		panic("Internal incosistency: Agent ID must not be empty")
	}
	return AgentID{cloudID{id}}
}

func NewVMMeta(meta map[string]interface{}) VMMeta {
	return VMMeta{cloudKVs{meta}}
}

func NewVMEnv(env map[string]interface{}) VMEnv {
	return VMEnv{cloudKVs{env}}
}
