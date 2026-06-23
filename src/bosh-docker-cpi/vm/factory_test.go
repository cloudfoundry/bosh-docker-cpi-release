package vm

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Factory", func() {
	Describe("cgroupBind", func() {
		It("returns true when startContainersWithSystemD and MountCgroupfs are both true", func() {
			Expect(cgroupBind(true, true)).To(BeTrue())
		})

		It("returns false when startContainersWithSystemD is false", func() {
			Expect(cgroupBind(false, true)).To(BeFalse())
		})

		It("returns false when MountCgroupfs is false", func() {
			Expect(cgroupBind(true, false)).To(BeFalse())
		})
	})
})
