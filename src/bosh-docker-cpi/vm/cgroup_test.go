package vm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"bosh-docker-cpi/config"
	"bosh-docker-cpi/stemcell"

	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	boshuuid "github.com/cloudfoundry/bosh-utils/uuid"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type cgroupTransport func(*http.Request) (*http.Response, error)

func (f cgroupTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func cgroupResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func cgroupCloudProps(val map[string]interface{}) apiv1.CloudPropsImpl {
	raw, err := json.Marshal(val)
	Expect(err).ToNot(HaveOccurred())
	return apiv1.CloudPropsImpl{RawMessage: raw}
}

func cgroupClient(transport cgroupTransport, opts ...client.Opt) *client.Client {
	opts = append([]client.Opt{client.WithHost("http://docker.invalid"), client.WithHTTPClient(&http.Client{Transport: transport})}, opts...)
	cli, err := client.NewClientWithOpts(opts...)
	Expect(err).ToNot(HaveOccurred())
	DeferCleanup(cli.Close)
	return cli
}

var _ = Describe("cgroup_v2", func() {
	logger := boshlog.NewLogger(boshlog.LevelNone)

	BeforeEach(func() {
		original := cgroupPreflightGrace
		cgroupPreflightGrace = 20 * time.Millisecond
		DeferCleanup(func() { cgroupPreflightGrace = original })
	})

	Describe("Factory.Create request", func() {
		type tc struct {
			private, systemd, mount, lxcfs bool
			mode                           string
			hostVersion                    string
			defaultV2                      bool
			override                       *bool
		}

		yes, no := true, false
		// Exercises Factory.Create and the real SDK serializer, stopping at Start.
		DescribeTable("host config and startup command",
			func(c tc) {
				var created *container.CreateRequest
				var requests []string
				cli := cgroupClient(func(r *http.Request) (*http.Response, error) {
					requests = append(requests, r.Method+" "+r.URL.Path)
					switch {
					case strings.HasSuffix(r.URL.Path, "/version"):
						return cgroupResponse(200, `{"ApiVersion":"1.44"}`), nil
					case strings.HasSuffix(r.URL.Path, "/info"):
						hostVersion := c.hostVersion
						if hostVersion == "" {
							hostVersion = "2"
						}
						return cgroupResponse(200, `{"CgroupVersion":"`+hostVersion+`"}`), nil
					case strings.HasSuffix(r.URL.Path, "/networks/create"):
						return cgroupResponse(201, `{"Id":"network"}`), nil
					case strings.HasSuffix(r.URL.Path, "/containers/create"):
						created = &container.CreateRequest{}
						Expect(json.NewDecoder(r.Body).Decode(created)).To(Succeed())
						return cgroupResponse(201, `{"Id":"node"}`), nil
					case strings.HasSuffix(r.URL.Path, "/start"):
						return cgroupResponse(500, `{"message":"test stops at start"}`), nil
					case r.Method == http.MethodDelete:
						return cgroupResponse(204, ""), nil
					}
					Fail("unexpected request " + r.URL.Path)
					return nil, nil
				}, client.WithVersion("1.44"))
				f := NewFactory(cli, boshuuid.NewGenerator(), apiv1.AgentOptions{}, logger,
					config.Config{StartContainersWithSystemD: c.systemd, MountCgroupfs: c.mount, EnableLXCFSSupport: c.lxcfs, CgroupV2: c.defaultV2})

				props := cgroupCloudProps(map[string]interface{}{"cgroup_v2": c.private, "Memory": 4194304000,
					"Mounts": []map[string]string{{"Type": "bind", "Source": "unix:///var/run/inner.sock", "Target": "/run/inner.sock"}}})
				if c.hostVersion != "" || c.defaultV2 || c.override != nil {
					values := map[string]interface{}{"Memory": 4194304000, "Mounts": []map[string]string{{"Type": "bind", "Source": "unix:///var/run/inner.sock", "Target": "/run/inner.sock"}}}
					if c.override != nil {
						values["cgroup_v2"] = *c.override
					}
					props = cgroupCloudProps(values)
				}
				if !c.systemd {
					props = cgroupCloudProps(map[string]interface{}{"Memory": 4194304000})
				}
				var networks apiv1.Networks
				Expect(json.Unmarshal([]byte(`{"default":{"ip":"10.0.0.2","netmask":"255.255.255.0","gateway":"10.0.0.1","cloud_properties":{}}}`), &networks)).To(Succeed())

				_, err := f.Create(apiv1.NewAgentID("agent"), stemcell.NewImage(apiv1.NewStemcellCID("image"), cli, logger), props, networks, nil, apiv1.VMEnv{})
				Expect(err).To(MatchError(ContainSubstring("test stops at start")))
				Expect(created).ToNot(BeNil())

				h := created.HostConfig
				Expect(string(h.CgroupnsMode)).To(Equal(c.mode))
				Expect(h.Memory).To(BeEquivalentTo(4194304000))
				Expect(h.Privileged).To(BeTrue())
				Expect(h.PublishAllPorts).To(BeTrue())

				binds := strings.Join(h.Binds, "\n")
				Expect(strings.Contains(binds, "/sys/fs/cgroup")).To(Equal(c.systemd && c.mount && !c.private))
				Expect(strings.Contains(binds, "/var/lib/lxcfs/proc/meminfo:/proc/meminfo:rw")).To(Equal(c.lxcfs))
				Expect(binds).To(ContainSubstring("vol-eph-c-"))
				Expect(binds).To(ContainSubstring("/lib/modules:/usr/lib/modules"))
				if c.systemd {
					Expect(h.Mounts).To(HaveLen(1))
					Expect(h.Mounts[0].Source).To(Equal("/var/run/inner.sock"))
				}

				command := strings.Join(created.Cmd, " ")
				Expect(strings.Contains(command, cgroupV2Preflight)).To(Equal(c.private))
				Expect(strings.Contains(strings.Join(created.Env, " "), "container=docker")).To(Equal(c.private))
				Expect(created.Labels[cgroupV2Label] == "true").To(Equal(c.private))
				if !c.private {
					// legacy VMs must be created exactly as before
					Expect(created.Env).To(Equal([]string{"reschedule:on-node-failure"}))
					Expect(created.Labels).To(BeEmpty())
					Expect(command).ToNot(ContainSubstring("cgroup"))
				}
				if c.systemd {
					Expect(command).To(HaveSuffix("exec /sbin/init"))
				}
				Expect(created.NetworkingConfig.EndpointsConfig["10.0.0.0/24"].IPAMConfig.IPv4Address).To(Equal("10.0.0.2"))

				// network create, [info], container create, start, cleanup
				expected := 4
				if c.private || c.hostVersion == "1" {
					expected++
				}
				Expect(requests).To(HaveLen(expected))
			},
			Entry("private overrides legacy bind", tc{private: true, systemd: true, mount: true, lxcfs: true, mode: "private"}),
			Entry("private without legacy bind", tc{private: true, systemd: true, mode: "private"}),
			Entry("legacy systemd", tc{systemd: true, mount: true, lxcfs: true, mode: "host"}),
			Entry("legacy systemd no bind", tc{systemd: true, mode: "host"}),
			Entry("legacy runit", tc{mount: true}),
			Entry("global default on v2", tc{private: true, systemd: true, mount: true, mode: "private", defaultV2: true}),
			Entry("global default falls back on v1 with bind", tc{systemd: true, mount: true, mode: "host", defaultV2: true, hostVersion: "1"}),
			Entry("global default falls back on v1 without bind", tc{systemd: true, mode: "host", defaultV2: true, hostVersion: "1"}),
			Entry("VM opt-in falls back on v1", tc{systemd: true, mode: "host", hostVersion: "1", override: &yes}),
			Entry("VM opts out of global default", tc{systemd: true, mount: true, mode: "host", defaultV2: true, override: &no}),
		)
	})

	Describe("validateCgroupV2", func() {
		DescribeTable("rejects conflicting config only when enabled",
			func(raw string) {
				var props Props
				Expect(json.Unmarshal([]byte(raw), &props)).To(Succeed())
				Expect(validateCgroupV2(props, false, true)).To(Succeed())
				Expect(validateCgroupV2(props, true, true)).ToNot(Succeed())
			},
			Entry("bind source", `{"Mounts":[{"Type":"bind","Source":"/sys/fs/cgroup","Target":"/host-cgroups"}]}`),
			Entry("recursive /sys bind", `{"Mounts":[{"Type":"bind","Source":"/sys","Target":"/host-sys"}]}`),
			Entry("mount target", `{"Mounts":[{"Type":"bind","Source":"/elsewhere","Target":"/sys/fs/cgroup"}]}`),
			Entry("child target", `{"Mounts":[{"Type":"volume","Source":"cg","Target":"/sys/fs/cgroup/child"}]}`),
			Entry("unclean path", `{"Mounts":[{"Type":"bind","Source":"/elsewhere","Target":"/sys/./fs"}]}`),
			Entry("tmpfs", `{"Tmpfs":{"/sys/fs/cgroup":"rw"}}`),
			Entry("VolumesFrom", `{"VolumesFrom":["other"]}`),
			Entry("host cgroup namespace", `{"CgroupnsMode":"host"}`),
		)

		It("ignores cloud_properties Binds because Create replaces them", func() {
			var props Props
			Expect(json.Unmarshal([]byte(`{"Binds":["/sys/fs/cgroup:/sys/fs/cgroup:rw"]}`), &props)).To(Succeed())
			Expect(validateCgroupV2(props, true, true)).To(Succeed())
		})

		It("requires systemd", func() {
			Expect(validateCgroupV2(Props{}, true, false)).ToNot(Succeed())
		})

		It("accepts the default and an explicit private namespace", func() {
			Expect(validateCgroupV2(Props{}, true, true)).To(Succeed())
			props := Props{}
			props.HostConfig.CgroupnsMode = container.CgroupnsModePrivate //nolint:staticcheck
			Expect(validateCgroupV2(props, true, true)).To(Succeed())
		})
	})

	Describe("cgroupV2Enabled", func() {
		yes, no := true, false

		DescribeTable("resolves the CPI default and the VM override",
			func(cfgDefault bool, override *bool, systemd, expected bool) {
				Expect(cgroupV2Enabled(config.Config{CgroupV2: cfgDefault}, Props{CgroupV2: override}, systemd)).To(Equal(expected))
			},
			Entry("off by default", false, nil, true, false),
			Entry("CPI default applies to systemd VMs", true, nil, true, true),
			Entry("CPI default skips runit VMs", true, nil, false, false),
			Entry("VM opts in", false, &yes, true, true),
			Entry("VM opts out of the CPI default", true, &no, true, false),
			Entry("explicit VM opt-in is kept for runit so validation can reject it", false, &yes, false, true),
		)
	})

	Describe("host gate", func() {
		DescribeTable("does not fall back when host detection fails",
			func(failingEndpoint string) {
				cli := cgroupClient(func(r *http.Request) (*http.Response, error) {
					if strings.HasSuffix(r.URL.Path, failingEndpoint) {
						return cgroupResponse(500, `{"message":"host detection failed"}`), nil
					}
					if strings.HasSuffix(r.URL.Path, "/version") {
						return cgroupResponse(200, `{"ApiVersion":"1.44"}`), nil
					}
					Fail("created resources after failed detection: " + r.URL.Path)
					return nil, nil
				}, client.WithVersion("1.44"))
				f := NewFactory(cli, nil, apiv1.AgentOptions{}, logger,
					config.Config{StartContainersWithSystemD: true, CgroupV2: true})
				_, err := f.Create(apiv1.NewAgentID("agent"), nil, cgroupCloudProps(nil), nil, nil, apiv1.VMEnv{})
				Expect(err).To(MatchError(ContainSubstring("host detection failed")))
			},
			Entry("info request fails", "/info"),
		)

		DescribeTable("checks the pinned client, the daemon API and the host cgroup version",
			func(pinned, daemon, cgroupVersion, wantErr string) {
				cli := cgroupClient(func(r *http.Request) (*http.Response, error) {
					switch {
					case strings.HasSuffix(r.URL.Path, "/version"):
						return cgroupResponse(200, `{"ApiVersion":"`+daemon+`"}`), nil
					case strings.HasSuffix(r.URL.Path, "/info"):
						return cgroupResponse(200, `{"CgroupVersion":"`+cgroupVersion+`"}`), nil
					}
					Fail("created resources before validation: " + r.URL.Path)
					return nil, nil
				}, client.WithVersion(pinned))
				f := NewFactory(cli, nil, apiv1.AgentOptions{}, logger, config.Config{StartContainersWithSystemD: true})

				enabled, err := f.checkCgroupV2Host(context.TODO())
				if wantErr == "" {
					Expect(err).ToNot(HaveOccurred())
					Expect(enabled).To(Equal(cgroupVersion == "2"))
				} else {
					Expect(err).To(MatchError(ContainSubstring(wantErr)))
				}
			},
			Entry("old pinned client", "1.40", "1.44", "2", "CPI client is pinned to 1.40"),
			Entry("cgroup v1 host", "1.44", "1.44", "1", ""),
			Entry("cgroup version unreported", "1.44", "1.44", "", "cannot detect a supported Docker host cgroup version"),
			Entry("minimum versions on a v2 host", "1.41", "1.41", "2", ""),
			Entry("new versions on a v2 host", "1.44", "1.44", "2", ""),
		)

		It("rejects an unknown host cgroup version before creating anything", func() {
			cli := cgroupClient(func(r *http.Request) (*http.Response, error) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/version"):
					return cgroupResponse(200, `{"ApiVersion":"1.44"}`), nil
				case strings.HasSuffix(r.URL.Path, "/info"):
					return cgroupResponse(200, `{"CgroupVersion":"unknown"}`), nil
				}
				Fail("created resources before validation: " + r.URL.Path)
				return nil, nil
			}, client.WithVersion("1.44"))
			f := NewFactory(cli, nil, apiv1.AgentOptions{}, logger, config.Config{StartContainersWithSystemD: true})
			_, err := f.Create(apiv1.NewAgentID("agent"), nil, cgroupCloudProps(map[string]interface{}{"cgroup_v2": true}), nil, nil, apiv1.VMEnv{})
			Expect(err).To(MatchError(ContainSubstring("cannot detect a supported Docker host cgroup version")))
		})
	})

	Describe("preflight failure", func() {
		var networks apiv1.Networks

		BeforeEach(func() {
			Expect(json.Unmarshal([]byte(`{"default":{"ip":"10.0.0.2","netmask":"255.255.255.0","gateway":"10.0.0.1","cloud_properties":{}}}`), &networks)).To(Succeed())
		})

		// failingStart simulates a container whose preflight exited right after start.
		failingStart := func(logs func() (*http.Response, error), removed *[]string) cgroupTransport {
			return func(r *http.Request) (*http.Response, error) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/info"):
					return cgroupResponse(200, `{"CgroupVersion":"2"}`), nil
				case strings.HasSuffix(r.URL.Path, "/networks/create"):
					return cgroupResponse(201, `{"Id":"network"}`), nil
				case strings.HasSuffix(r.URL.Path, "/containers/create"):
					return cgroupResponse(201, `{"Id":"node"}`), nil
				case strings.HasSuffix(r.URL.Path, "/start"):
					return cgroupResponse(204, ""), nil
				case strings.HasSuffix(r.URL.Path, "/archive"), strings.HasSuffix(r.URL.Path, "/exec"):
					return cgroupResponse(409, `{"message":"container is not running"}`), nil
				case strings.HasSuffix(r.URL.Path, "/wait"):
					return cgroupResponse(200, `{"StatusCode":1}`), nil
				case strings.HasSuffix(r.URL.Path, "/logs"):
					return logs()
				case r.Method == http.MethodDelete:
					*removed = append(*removed, r.URL.Path)
					return cgroupResponse(204, ""), nil
				}
				Fail("unexpected request " + r.URL.Path)
				return nil, nil
			}
		}

		create := func(transport cgroupTransport) error {
			cli := cgroupClient(transport, client.WithVersion("1.44"))
			f := NewFactory(cli, boshuuid.NewGenerator(), apiv1.AgentOptions{}, logger, config.Config{StartContainersWithSystemD: true})
			_, err := f.Create(apiv1.NewAgentID("agent"), stemcell.NewImage(apiv1.NewStemcellCID("image"), cli, logger),
				cgroupCloudProps(map[string]interface{}{"cgroup_v2": true}), networks, nil, apiv1.VMEnv{})
			return err
		}

		It("surfaces the failed check instead of a generic upload error and cleans up the container and volume", func() {
			var removed []string
			err := create(failingStart(func() (*http.Response, error) {
				return cgroupResponse(200, cgroupV2PreflightMessage+": memory.max is missing"), nil
			}, &removed))

			Expect(err).To(MatchError(ContainSubstring("cgroup_v2 preflight failed: memory.max is missing")))
			Expect(err).To(MatchError(ContainSubstring("exited with code 1")))
			Expect(removed).To(HaveLen(2))
			Expect(removed[0]).To(ContainSubstring("/containers/"))
			Expect(removed[1]).To(ContainSubstring("/volumes/vol-eph-c-"))
		})

		It("reports the output of a container that exited for another reason", func() {
			var removed []string
			err := create(failingStart(func() (*http.Response, error) {
				return cgroupResponse(200, "umount: /etc/hosts: not mounted"), nil
			}, &removed))
			Expect(err).To(MatchError(ContainSubstring("exited during startup with code 1: umount: /etc/hosts: not mounted")))
		})

		It("still reports the exit when the logs cannot be read", func() {
			var removed []string
			err := create(failingStart(func() (*http.Response, error) {
				return cgroupResponse(500, `{"message":"logging driver does not support reading"}`), nil
			}, &removed))
			Expect(err).To(MatchError(ContainSubstring("exited during startup with code 1: output unavailable")))
		})

		It("treats a container that keeps running as having passed", func() {
			cli := cgroupClient(func(r *http.Request) (*http.Response, error) {
				Expect(r.URL.Path).To(HaveSuffix("/wait"))
				<-r.Context().Done()
				return nil, r.Context().Err()
			}, client.WithVersion("1.44"))
			Expect(cgroupPreflightFailure(cli, logger, "test", apiv1.NewVMCID("node"))).To(Succeed())
		})
	})

	Describe("disk attachment", func() {
		It("keeps the legacy node binds across attach and detach", func() {
			c := Container{}
			binds := []string{"vol-eph-node:/var/vcap/data/", "/var/lib/lxcfs/proc/meminfo:/proc/meminfo:rw"}
			attached := c.updateBinds(append([]string{}, binds...), apiv1.NewDiskCID("disk"), "/warden-cpi-dev/disk")
			detached := c.updateBinds(attached, apiv1.NewDiskCID("disk"), "")
			Expect(detached).To(Equal(binds))
		})

		const cgroupEnv = `["container=docker","reschedule:on-node-failure","constraint:node==old"]`
		const cgroupLabels = `{"bosh-docker-cpi.cgroup_v2":"true"}`

		// recreate runs restartByRecreating against a fake daemon; waitStatus is the
		// body of /wait, or empty if the container keeps running.
		recreate := func(volumes, env, labels, waitStatus string) (*container.CreateRequest, error) {
			var created *container.CreateRequest
			cli := cgroupClient(func(r *http.Request) (*http.Response, error) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/json"):
					return cgroupResponse(200, `{"Id":"node","Config":{"Image":"image","Env":`+env+`,"Labels":`+labels+`,"Cmd":["bash","-c","exec /sbin/init"]},"HostConfig":{"CgroupnsMode":"private","Privileged":true,"Memory":4194304000,"Binds":["vol-eph-node:/var/vcap/data/","/var/lib/lxcfs/proc/meminfo:/proc/meminfo:rw"],"Mounts":[{"Type":"bind","Source":"/data","Target":"/data"}]},"NetworkSettings":{"Networks":{"net":{"IPAMConfig":{"IPv4Address":"10.0.0.2"}}}}}`), nil
				case strings.HasSuffix(r.URL.Path, "/volumes"):
					return cgroupResponse(200, volumes), nil
				case strings.HasSuffix(r.URL.Path, "/containers/create"):
					created = &container.CreateRequest{}
					Expect(json.NewDecoder(r.Body).Decode(created)).To(Succeed())
					return cgroupResponse(201, `{"Id":"node"}`), nil
				case strings.HasSuffix(r.URL.Path, "/wait"):
					if waitStatus == "" {
						<-r.Context().Done()
						return nil, r.Context().Err()
					}
					return cgroupResponse(200, waitStatus), nil
				case strings.HasSuffix(r.URL.Path, "/logs"):
					return cgroupResponse(200, cgroupV2PreflightMessage+": cgroup.procs is not writable"), nil
				case strings.HasSuffix(r.URL.Path, "/kill"), strings.HasSuffix(r.URL.Path, "/start"), r.Method == http.MethodDelete:
					return cgroupResponse(204, ""), nil
				}
				Fail("unexpected request " + r.URL.Path)
				return nil, nil
			}, client.WithVersion("1.44"))
			c := NewContainer(apiv1.NewVMCID("node"), cli, nil, logger)
			err := c.restartByRecreating(apiv1.NewDiskCID("disk"), "/warden-cpi-dev/disk")
			return created, err
		}

		It("preserves private cgroups on recreate", func() {
			created, err := recreate(`{"Volumes":[{"Name":"disk"}]}`, cgroupEnv, cgroupLabels, "")
			Expect(err).ToNot(HaveOccurred())
			h := created.HostConfig
			Expect(string(h.CgroupnsMode)).To(Equal("private"))
			Expect(h.Memory).To(BeEquivalentTo(4194304000))
			Expect(h.Privileged).To(BeTrue())
			Expect(h.Mounts).To(HaveLen(1))
			Expect(h.Binds).To(Equal([]string{"vol-eph-node:/var/vcap/data/", "/var/lib/lxcfs/proc/meminfo:/proc/meminfo:rw", "disk:/warden-cpi-dev/disk"}))
			Expect(created.NetworkingConfig.EndpointsConfig["net"].IPAMConfig.IPv4Address).To(Equal("10.0.0.2"))
			Expect(created.Env).To(ConsistOf("container=docker", "reschedule:on-node-failure", "constraint:node==old"))
			Expect(created.Labels).To(HaveKeyWithValue(cgroupV2Label, "true"))
		})

		It("replaces the env as before for legacy containers when a node constraint is added", func() {
			created, err := recreate(`{"Volumes":[{"Name":"node1/disk"}]}`, `["reschedule:on-node-failure","constraint:node==old"]`, `{}`, "")
			Expect(err).ToNot(HaveOccurred())
			Expect(created.Env).To(Equal([]string{"constraint:node==node1"}))
		})

		It("does not treat an image-provided container=docker as a cgroup_v2 container", func() {
			created, err := recreate(`{"Volumes":[{"Name":"node1/disk"}]}`, `["container=docker"]`, `{}`, "")
			Expect(err).ToNot(HaveOccurred())
			Expect(created.Env).To(Equal([]string{"constraint:node==node1"}))
		})

		It("keeps container=docker when a node constraint is added", func() {
			created, err := recreate(`{"Volumes":[{"Name":"node1/disk"}]}`, cgroupEnv, cgroupLabels, "")
			Expect(err).ToNot(HaveOccurred())
			Expect(created.Env).To(ConsistOf("container=docker", "constraint:node==node1"))
		})

		It("surfaces a preflight failure of the recreated container", func() {
			_, err := recreate(`{"Volumes":[{"Name":"disk"}]}`, cgroupEnv, cgroupLabels, `{"StatusCode":1}`)
			Expect(err).To(MatchError(ContainSubstring("cgroup_v2 preflight failed: cgroup.procs is not writable")))
		})
	})
})
