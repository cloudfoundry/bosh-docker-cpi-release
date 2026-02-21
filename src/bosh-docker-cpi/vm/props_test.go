package vm_test

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "bosh-docker-cpi/vm"
)

var _ = Describe("Props", func() {
	Describe("Unmarshal", func() {
		It("picks up Docker configuration", func() {
			var props Props

			err := json.Unmarshal([]byte(`{"CPUShares": 10, "Memory": 1024}`), &props)
			Expect(err).ToNot(HaveOccurred())

			Expect(props.HostConfig.CPUShares).To(Equal(int64(10)))
			Expect(props.HostConfig.Memory).To(Equal(int64(1024)))
		})

		It("unmarshals exposed ports", func() {
			var props Props
			err := json.Unmarshal([]byte(`{"ports": ["6868/tcp", "25555/tcp"]}`), &props)
			Expect(err).NotTo(HaveOccurred())
			Expect(props.ExposedPorts).To(ConsistOf("6868/tcp", "25555/tcp"))
		})

		It("unmarshals systemd force flags", func() {
			var props Props
			err := json.Unmarshal([]byte(`{"force_start_with_systemd": true}`), &props)
			Expect(err).NotTo(HaveOccurred())
			Expect(props.ForceStartWithSystemD).To(BeTrue())
			Expect(props.ForceStartWithoutSystemD).To(BeFalse())
		})

		It("unmarshals lxcfs force flags", func() {
			var props Props
			err := json.Unmarshal([]byte(`{"force_lxcfs_enabled": true}`), &props)
			Expect(err).NotTo(HaveOccurred())
			Expect(props.ForceLXCFSEnabled).To(BeTrue())
			Expect(props.ForceLXCFSDisabled).To(BeFalse())
		})

		It("defaults all force flags to false", func() {
			var props Props
			err := json.Unmarshal([]byte(`{}`), &props)
			Expect(err).NotTo(HaveOccurred())
			Expect(props.ForceStartWithSystemD).To(BeFalse())
			Expect(props.ForceStartWithoutSystemD).To(BeFalse())
			Expect(props.ForceLXCFSEnabled).To(BeFalse())
			Expect(props.ForceLXCFSDisabled).To(BeFalse())
		})
	})
})
