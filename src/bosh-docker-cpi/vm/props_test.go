package vm_test

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "bosh-docker-cpi/vm"
)

var _ = Describe("VMProps", func() {
	Describe("Unmarshal", func() {
		It("picks up Docker configuration", func() {
			var props VMProps

			err := json.Unmarshal([]byte(`{"CPUShares": 10, "Memory": 1024}`), &props)
			Expect(err).ToNot(HaveOccurred())

			Expect(props.HostConfig.CPUShares).To(Equal(int64(10)))
			Expect(props.HostConfig.Memory).To(Equal(int64(1024)))
		})
	})
})
