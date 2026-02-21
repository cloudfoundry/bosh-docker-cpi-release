package vm_test

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "bosh-docker-cpi/vm"
)

var _ = Describe("NetProps", func() {
	It("unmarshals JSON with enable_ipv6 field", func() {
		var props NetProps
		err := json.Unmarshal([]byte(`{"name": "my-net", "driver": "overlay", "enable_ipv6": true}`), &props)
		Expect(err).NotTo(HaveOccurred())
		Expect(props.Name).To(Equal("my-net"))
		Expect(props.Driver).To(Equal("overlay"))
		Expect(props.EnableIPv6).To(BeTrue())
	})

	It("defaults enable_ipv6 to false", func() {
		var props NetProps
		err := json.Unmarshal([]byte(`{"name": "my-net"}`), &props)
		Expect(err).NotTo(HaveOccurred())
		Expect(props.EnableIPv6).To(BeFalse())
	})
})
