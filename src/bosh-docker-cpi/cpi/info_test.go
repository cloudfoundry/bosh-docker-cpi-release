package cpi_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"bosh-docker-cpi/cpi"
)

var _ = Describe("InfoMethod", func() {
	var method cpi.InfoMethod

	BeforeEach(func() {
		method = cpi.NewInfoMethod()
	})

	It("returns supported stemcell formats", func() {
		info, err := method.Info()
		Expect(err).NotTo(HaveOccurred())
		Expect(info.StemcellFormats).To(ConsistOf("warden-tar", "general-tar", "docker-light"))
	})

	It("returns API version 2", func() {
		info, err := method.Info()
		Expect(err).NotTo(HaveOccurred())
		Expect(info.APIVersion).To(Equal(2))
	})
})
