package vm

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	dkrcont "github.com/docker/docker/api/types/container"
	dkrclient "github.com/docker/docker/client"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"bosh-docker-cpi/config"
)

// fakeDockerAPI answers just enough of the Docker Engine API for Create to reach
// ContainerCreate. It refuses to start the container, so Create returns before
// it needs the agent env written into a running container; what matters here is
// the request Create sent.
type fakeDockerAPI struct {
	server *httptest.Server

	mu              sync.Mutex
	networkCreates  int
	containerCreate []byte
	handlerErr      error // assertions cannot run on the server goroutine
}

func newFakeDockerAPI() *fakeDockerAPI {
	f := &fakeDockerAPI{}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	return f
}

func (f *fakeDockerAPI) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/networks/create"):
		f.networkCreates++
		f.reply(w, http.StatusCreated, `{"Id": "network-id"}`)

	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/create"):
		body, err := io.ReadAll(r.Body)
		if err != nil {
			f.handlerErr = err
		}
		f.containerCreate = body
		f.reply(w, http.StatusCreated, `{"Id": "container-id", "Warnings": []}`)

	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/start"):
		f.reply(w, http.StatusInternalServerError, `{"message": "start refused by test"}`)

	case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/containers/"):
		w.WriteHeader(http.StatusNoContent)

	default:
		f.reply(w, http.StatusNotFound, `{"message": "unexpected request in test"}`)
	}
}

func (f *fakeDockerAPI) reply(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := io.WriteString(w, body); err != nil {
		f.handlerErr = err
	}
}

func (f *fakeDockerAPI) err() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.handlerErr
}

func (f *fakeDockerAPI) client() *dkrclient.Client {
	c, err := dkrclient.NewClientWithOpts(
		dkrclient.WithHost("tcp://"+strings.TrimPrefix(f.server.URL, "http://")),
		dkrclient.WithVersion("1.44"),
	)
	Expect(err).NotTo(HaveOccurred())
	return c
}

// hostConfig returns the HostConfig Create sent with ContainerCreate.
func (f *fakeDockerAPI) hostConfig() dkrcont.HostConfig {
	f.mu.Lock()
	defer f.mu.Unlock()

	Expect(f.containerCreate).NotTo(BeEmpty(), "Create never called ContainerCreate")

	var req struct{ HostConfig dkrcont.HostConfig }
	Expect(json.Unmarshal(f.containerCreate, &req)).To(Succeed())
	return req.HostConfig
}

type stubStemcell struct{}

func (stubStemcell) ID() apiv1.StemcellCID { return apiv1.NewStemcellCID("stemcell-image") }
func (stubStemcell) Delete() error         { return nil }

type stubUUIDGen struct{}

func (stubUUIDGen) Generate() (string, error) { return "uuid", nil }

var _ = Describe("Factory.Create", func() {
	var docker *fakeDockerAPI

	BeforeEach(func() {
		docker = newFakeDockerAPI()
		DeferCleanup(docker.server.Close)
		DeferCleanup(func() { Expect(docker.err()).NotTo(HaveOccurred()) })
	})

	create := func(cfg config.Config, vmCloudProps string) error {
		var networks apiv1.Networks
		Expect(json.Unmarshal([]byte(`{"default": {
			"type": "dynamic",
			"cloud_properties": {"name": "test-net"}
		}}`), &networks)).To(Succeed())

		factory := NewFactory(docker.client(), stubUUIDGen{}, apiv1.AgentOptions{},
			boshlog.NewLogger(boshlog.LevelNone), cfg)

		_, err := factory.Create(
			apiv1.NewAgentID("agent-id"),
			stubStemcell{},
			apiv1.CloudPropsImpl{RawMessage: json.RawMessage(vmCloudProps)},
			networks,
			nil,
			apiv1.NewVMEnv(nil),
		)
		return err
	}

	// Create fails at ContainerStart by design of the fake; anything earlier is a
	// real failure.
	expectReachedStart := func(err error) {
		Expect(err).To(MatchError(ContainSubstring("Starting container")))
	}

	It("runs every container privileged, with ports published and the ephemeral disk mounted", func() {
		expectReachedStart(create(config.Config{}, `{}`))

		hostConfig := docker.hostConfig()
		Expect(hostConfig.Privileged).To(BeTrue())
		Expect(hostConfig.PublishAllPorts).To(BeTrue())
		Expect(hostConfig.Binds).To(ContainElement(HaveSuffix(":/var/vcap/data/")))
	})

	Describe("cgroup namespace", func() {
		It("uses the host namespace for systemd containers by default", func() {
			cfg := config.NewDefaultConfig()
			cfg.StartContainersWithSystemD = true
			cfg.MountCgroupfs = true

			expectReachedStart(create(cfg, `{}`))

			hostConfig := docker.hostConfig()
			Expect(hostConfig.CgroupnsMode).To(Equal(dkrcont.CgroupnsModeHost))
			Expect(hostConfig.Binds).To(ContainElement("/sys/fs/cgroup:/sys/fs/cgroup:rw"))
		})

		It("sends an explicit private namespace when opted in", func() {
			expectReachedStart(create(config.Config{
				StartContainersWithSystemD: true,
				UseHostCgroupNamespace:     false,
			}, `{}`))

			hostConfig := docker.hostConfig()
			Expect(hostConfig.CgroupnsMode).To(Equal(dkrcont.CgroupnsModePrivate))
			Expect(hostConfig.Binds).NotTo(ContainElement(ContainSubstring("/sys/fs/cgroup")))
		})

		It("leaves the namespace to Docker when systemd is not in use", func() {
			expectReachedStart(create(config.Config{UseHostCgroupNamespace: false}, `{}`))

			Expect(docker.hostConfig().CgroupnsMode).To(BeEmpty())
		})

		It("follows the per-VM systemd override", func() {
			expectReachedStart(create(config.Config{UseHostCgroupNamespace: false},
				`{"force_start_with_systemd": true}`))

			Expect(docker.hostConfig().CgroupnsMode).To(Equal(dkrcont.CgroupnsModePrivate))
		})

		It("rejects a private namespace combined with the host cgroup bind before creating any network", func() {
			err := create(config.Config{
				StartContainersWithSystemD: true,
				MountCgroupfs:              true,
				UseHostCgroupNamespace:     false,
			}, `{}`)

			Expect(err).To(MatchError(ContainSubstring("mount_cgroupfs")))
			Expect(docker.networkCreates).To(BeZero())
			Expect(docker.containerCreate).To(BeEmpty())
		})
	})

	Describe("LXCFS binds", func() {
		lxcfsBindsIn := func(binds []string) []string {
			var found []string
			for _, bind := range binds {
				if strings.HasPrefix(bind, "/var/lib/lxcfs/") {
					found = append(found, bind)
				}
			}
			return found
		}

		It("binds meminfo, cpuinfo and stat when enabled, and never uptime", func() {
			expectReachedStart(create(config.Config{EnableLXCFSSupport: true}, `{}`))

			binds := docker.hostConfig().Binds
			Expect(lxcfsBindsIn(binds)).To(ConsistOf(
				"/var/lib/lxcfs/proc/meminfo:/proc/meminfo:rw",
				"/var/lib/lxcfs/proc/cpuinfo:/proc/cpuinfo:rw",
				"/var/lib/lxcfs/proc/stat:/proc/stat:rw",
			))
			Expect(binds).NotTo(ContainElement(ContainSubstring("uptime")))
		})

		It("binds nothing from LXCFS when disabled", func() {
			expectReachedStart(create(config.Config{}, `{}`))

			Expect(lxcfsBindsIn(docker.hostConfig().Binds)).To(BeEmpty())
		})

		It("follows the per-VM force_lxcfs_enabled override", func() {
			expectReachedStart(create(config.Config{}, `{"force_lxcfs_enabled": true}`))

			Expect(lxcfsBindsIn(docker.hostConfig().Binds)).To(HaveLen(3))
		})

		It("follows the per-VM force_lxcfs_disabled override", func() {
			expectReachedStart(create(config.Config{EnableLXCFSSupport: true}, `{"force_lxcfs_disabled": true}`))

			Expect(lxcfsBindsIn(docker.hostConfig().Binds)).To(BeEmpty())
		})
	})
})
