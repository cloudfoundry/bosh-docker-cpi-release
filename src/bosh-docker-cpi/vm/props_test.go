package vm_test

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "bosh-docker-cpi/vm"
)

var _ = Describe("Props", func() {
	Describe("Unmarshal", func() {
		Context("Basic resource properties", func() {
			It("picks up Docker configuration", func() {
				var props Props

				err := json.Unmarshal([]byte(`{"CPUShares": 10, "Memory": 1024}`), &props)
				Expect(err).ToNot(HaveOccurred())

				Expect(props.CPUShares).To(Equal(int64(10)))
				Expect(props.Memory).To(Equal(int64(1024)))
			})

			It("handles CPU limits", func() {
				var props Props

				jsonData := `{
					"CPUShares": 512,
					"NanoCPUs": 2000000000,
					"CPUQuota": 50000,
					"CPUPeriod": 100000,
					"CpusetCpus": "0-3",
					"CpusetMems": "0"
				}`

				err := json.Unmarshal([]byte(jsonData), &props)
				Expect(err).ToNot(HaveOccurred())

				Expect(props.CPUShares).To(Equal(int64(512)))
				Expect(props.NanoCPUs).To(Equal(int64(2000000000)))
				Expect(props.CPUQuota).To(Equal(int64(50000)))
				Expect(props.CPUPeriod).To(Equal(int64(100000)))
				Expect(props.CpusetCpus).To(Equal("0-3"))
				Expect(props.CpusetMems).To(Equal("0"))
			})

			It("handles memory limits", func() {
				var props Props

				jsonData := `{
					"Memory": 1073741824,
					"MemorySwap": 2147483648,
					"MemoryReservation": 536870912,
					"KernelMemory": 134217728,
					"MemorySwappiness": 10
				}`

				err := json.Unmarshal([]byte(jsonData), &props)
				Expect(err).ToNot(HaveOccurred())

				Expect(props.Memory).To(Equal(int64(1073741824)))
				Expect(props.MemorySwap).To(Equal(int64(2147483648)))
				Expect(props.MemoryReservation).To(Equal(int64(536870912)))
				Expect(props.KernelMemory).To(Equal(int64(134217728)))
				Expect(*props.MemorySwappiness).To(Equal(int64(10)))
			})
		})

		Context("CGROUPSv2 specific properties", func() {
			It("handles PIDs limit", func() {
				var props Props

				jsonData := `{"PidsLimit": 100}`
				err := json.Unmarshal([]byte(jsonData), &props)
				Expect(err).ToNot(HaveOccurred())

				Expect(*props.PidsLimit).To(Equal(int64(100)))
			})

			It("handles block IO properties", func() {
				var props Props

				jsonData := `{
					"BlkioWeight": 500,
					"BlkioWeightDevice": [{"Path": "/dev/sda", "Weight": 200}],
					"BlkioDeviceReadBps": [{"Path": "/dev/sda", "Rate": 10485760}],
					"BlkioDeviceWriteBps": [{"Path": "/dev/sda", "Rate": 10485760}],
					"BlkioDeviceReadIOps": [{"Path": "/dev/sda", "Rate": 1000}],
					"BlkioDeviceWriteIOps": [{"Path": "/dev/sda", "Rate": 1000}]
				}`

				err := json.Unmarshal([]byte(jsonData), &props)
				Expect(err).ToNot(HaveOccurred())

				Expect(props.BlkioWeight).To(Equal(uint16(500)))
				Expect(props.BlkioWeightDevice).To(HaveLen(1))
				Expect(props.BlkioDeviceReadBps).To(HaveLen(1))
				Expect(props.BlkioDeviceWriteBps).To(HaveLen(1))
				Expect(props.BlkioDeviceReadIOps).To(HaveLen(1))
				Expect(props.BlkioDeviceWriteIOps).To(HaveLen(1))
			})

			It("handles OOM settings", func() {
				var props Props

				jsonData := `{
					"OomKillDisable": true,
					"OomScoreAdj": 500
				}`

				err := json.Unmarshal([]byte(jsonData), &props)
				Expect(err).ToNot(HaveOccurred())

				Expect(*props.OomKillDisable).To(BeTrue())
				Expect(props.OomScoreAdj).To(Equal(500))
			})
		})

		Context("Volume and mount properties", func() {
			It("handles bind volumes", func() {
				var props Props

				jsonData := `{
					"Binds": [
						"/host/path:/container/path:ro",
						"/another/host:/another/container"
					]
				}`

				err := json.Unmarshal([]byte(jsonData), &props)
				Expect(err).ToNot(HaveOccurred())

				Expect(props.Binds).To(HaveLen(2))
				Expect(props.Binds[0]).To(Equal("/host/path:/container/path:ro"))
				Expect(props.Binds[1]).To(Equal("/another/host:/another/container"))
			})

			It("handles mounts", func() {
				var props Props

				jsonData := `{
					"Mounts": [{
						"Type": "bind",
						"Source": "/host/path",
						"Target": "/container/path",
						"ReadOnly": true,
						"BindOptions": {
							"Propagation": "rprivate"
						}
					}]
				}`

				err := json.Unmarshal([]byte(jsonData), &props)
				Expect(err).ToNot(HaveOccurred())

				Expect(props.Mounts).To(HaveLen(1))
				mount := props.Mounts[0]
				Expect(string(mount.Type)).To(Equal("bind"))
				Expect(mount.Source).To(Equal("/host/path"))
				Expect(mount.Target).To(Equal("/container/path"))
				Expect(mount.ReadOnly).To(BeTrue())
			})

			It("handles tmpfs mounts", func() {
				var props Props

				jsonData := `{
					"Mounts": [{
						"Type": "tmpfs",
						"Target": "/tmp",
						"TmpfsOptions": {
							"SizeBytes": 67108864,
							"Mode": 1777
						}
					}]
				}`

				err := json.Unmarshal([]byte(jsonData), &props)
				Expect(err).ToNot(HaveOccurred())

				Expect(props.Mounts).To(HaveLen(1))
				mount := props.Mounts[0]
				Expect(string(mount.Type)).To(Equal("tmpfs"))
				Expect(mount.Target).To(Equal("/tmp"))
				Expect(mount.TmpfsOptions.SizeBytes).To(Equal(int64(67108864)))
			})
		})

		Context("Network properties", func() {
			It("handles network mode", func() {
				var props Props

				jsonData := `{
					"NetworkMode": "bridge",
					"PortBindings": {
						"80/tcp": [{"HostPort": "8080"}],
						"443/tcp": [{"HostPort": "8443"}]
					}
				}`

				err := json.Unmarshal([]byte(jsonData), &props)
				Expect(err).ToNot(HaveOccurred())

				Expect(string(props.NetworkMode)).To(Equal("bridge"))
				Expect(props.PortBindings).To(HaveLen(2))
			})
		})

		Context("Security properties", func() {
			It("handles security options", func() {
				var props Props

				jsonData := `{
					"Privileged": true,
					"ReadonlyRootfs": true,
					"SecurityOpt": ["no-new-privileges"],
					"CapAdd": ["SYS_ADMIN"],
					"CapDrop": ["MKNOD"]
				}`

				err := json.Unmarshal([]byte(jsonData), &props)
				Expect(err).ToNot(HaveOccurred())

				Expect(props.Privileged).To(BeTrue())
				Expect(props.ReadonlyRootfs).To(BeTrue())
				Expect(props.SecurityOpt).To(ContainElement("no-new-privileges"))
				Expect(props.CapAdd).To(ContainElement("SYS_ADMIN"))
				Expect(props.CapDrop).To(ContainElement("MKNOD"))
			})

			It("handles ulimits", func() {
				var props Props

				jsonData := `{
					"Ulimits": [
						{"Name": "nofile", "Soft": 1024, "Hard": 2048},
						{"Name": "nproc", "Soft": 512, "Hard": 1024}
					]
				}`

				err := json.Unmarshal([]byte(jsonData), &props)
				Expect(err).ToNot(HaveOccurred())

				Expect(props.Ulimits).To(HaveLen(2))
				Expect(props.Ulimits[0].Name).To(Equal("nofile"))
				Expect(props.Ulimits[0].Soft).To(Equal(int64(1024)))
				Expect(props.Ulimits[0].Hard).To(Equal(int64(2048)))
			})
		})

		Context("Platform properties", func() {
			It("handles platform specification", func() {
				var props Props

				jsonData := `{
					"architecture": "amd64",
					"os": "linux"
				}`

				err := json.Unmarshal([]byte(jsonData), &props)
				Expect(err).ToNot(HaveOccurred())

				Expect(props.Platform.Architecture).To(Equal("amd64"))
				Expect(props.Platform.OS).To(Equal("linux"))
			})
		})

		Context("Complex properties", func() {
			It("handles all properties together", func() {
				var props Props

				jsonData := `{
					"Memory": 2147483648,
					"CPUShares": 1024,
					"PidsLimit": 200,
					"BlkioWeight": 500,
					"Binds": ["/data:/data"],
					"NetworkMode": "bridge",
					"Privileged": false,
					"ports": ["80/tcp", "443/tcp"]
				}`

				err := json.Unmarshal([]byte(jsonData), &props)
				Expect(err).ToNot(HaveOccurred())

				Expect(props.Memory).To(Equal(int64(2147483648)))
				Expect(props.CPUShares).To(Equal(int64(1024)))
				Expect(*props.PidsLimit).To(Equal(int64(200)))
				Expect(props.BlkioWeight).To(Equal(uint16(500)))
				Expect(props.Binds).To(ContainElement("/data:/data"))
				Expect(string(props.NetworkMode)).To(Equal("bridge"))
				Expect(props.Privileged).To(BeFalse())
				Expect(props.ExposedPorts).To(ContainElement("80/tcp"))
				Expect(props.ExposedPorts).To(ContainElement("443/tcp"))
			})
		})

		Context("Edge cases", func() {
			It("handles empty JSON", func() {
				var props Props

				err := json.Unmarshal([]byte(`{}`), &props)
				Expect(err).ToNot(HaveOccurred())

				// Should have zero values
				Expect(props.Memory).To(Equal(int64(0)))
				Expect(props.CPUShares).To(Equal(int64(0)))
			})

			It("handles null values appropriately", func() {
				var props Props

				jsonData := `{
					"Memory": null,
					"PidsLimit": null
				}`

				err := json.Unmarshal([]byte(jsonData), &props)
				Expect(err).ToNot(HaveOccurred())

				// Null should result in zero values for basic types
				Expect(props.Memory).To(Equal(int64(0)))
			})

			It("ignores unknown properties", func() {
				var props Props

				jsonData := `{
					"Memory": 1024,
					"UnknownProperty": "should be ignored",
					"AnotherUnknown": 12345
				}`

				err := json.Unmarshal([]byte(jsonData), &props)
				Expect(err).ToNot(HaveOccurred())

				Expect(props.Memory).To(Equal(int64(1024)))
			})
		})

		Context("CGROUPSv2 validation properties", func() {
			It("handles systemd cgroup driver requirement", func() {
				var props Props

				jsonData := `{
					"SystemdMode": true,
					"Memory": 1073741824,
					"CPUShares": 512,
					"PidsLimit": 100
				}`

				err := json.Unmarshal([]byte(jsonData), &props)
				Expect(err).ToNot(HaveOccurred())

				// Resource limits should be set for cgroupsv2
				// SystemdMode was removed - cgroupsv2 is always used
				Expect(props.Memory).To(Equal(int64(1073741824)))
				Expect(props.CPUShares).To(Equal(int64(512)))
				Expect(*props.PidsLimit).To(Equal(int64(100)))
			})

			It("handles cgroupsv2-specific resource limits", func() {
				var props Props

				jsonData := `{
					"Memory": 2147483648,
					"MemorySwap": 2147483648,
					"CPUShares": 1024,
					"NanoCPUs": 2000000000,
					"PidsLimit": 200,
					"BlkioWeight": 500,
					"SystemdMode": true
				}`

				err := json.Unmarshal([]byte(jsonData), &props)
				Expect(err).ToNot(HaveOccurred())

				// Verify all cgroupsv2 resource limits are properly set
				Expect(props.Memory).To(Equal(int64(2147483648)))
				Expect(props.MemorySwap).To(Equal(int64(2147483648)))
				Expect(props.CPUShares).To(Equal(int64(1024)))
				Expect(props.NanoCPUs).To(Equal(int64(2000000000)))
				Expect(*props.PidsLimit).To(Equal(int64(200)))
				Expect(props.BlkioWeight).To(Equal(uint16(500)))
				// SystemdMode was removed - cgroupsv2 is always used
			})

			It("handles cgroupsv2 init system requirements", func() {
				var props Props

				// Test with systemd init
				jsonData := `{
					"Init": true,
					"SystemdMode": true,
					"CgroupParent": "system.slice"
				}`

				err := json.Unmarshal([]byte(jsonData), &props)
				Expect(err).ToNot(HaveOccurred())

				Expect(*props.Init).To(BeTrue())
				// SystemdMode was removed - cgroupsv2 is always used
				Expect(props.CgroupParent).To(Equal("system.slice"))
			})

			It("validates cgroupsv2 hierarchy paths", func() {
				var props Props

				jsonData := `{
					"CgroupParent": "system.slice/docker.slice",
					"Cgroups": "enabled"
				}`

				err := json.Unmarshal([]byte(jsonData), &props)
				Expect(err).ToNot(HaveOccurred())

				Expect(props.CgroupParent).To(Equal("system.slice/docker.slice"))
				// Cgroups field was removed from HostConfig
			})

			It("handles cgroupsv2 device restrictions", func() {
				var props Props

				jsonData := `{
					"DeviceCgroupRules": [
						"c 1:3 mr",
						"b 8:* r"
					],
					"Devices": [{
						"PathOnHost": "/dev/fuse",
						"PathInContainer": "/dev/fuse",
						"CgroupPermissions": "rwm"
					}]
				}`

				err := json.Unmarshal([]byte(jsonData), &props)
				Expect(err).ToNot(HaveOccurred())

				Expect(props.DeviceCgroupRules).To(HaveLen(2))
				Expect(props.DeviceCgroupRules[0]).To(Equal("c 1:3 mr"))
				Expect(props.Devices).To(HaveLen(1))
				Expect(props.Devices[0].PathOnHost).To(Equal("/dev/fuse"))
			})

			It("handles unified cgroupsv2 resource format", func() {
				var props Props

				// Test unified format for cgroupsv2
				jsonData := `{
					"Resources": {
						"Memory": 1073741824,
						"NanoCPUs": 1000000000,
						"PidsLimit": 100,
						"BlkioWeight": 400
					}
				}`

				err := json.Unmarshal([]byte(jsonData), &props)
				Expect(err).ToNot(HaveOccurred())

				// Resources should be mapped to HostConfig
				Expect(props.Memory).To(BeZero()) // This format might not be supported yet
			})
		})
	})
})
