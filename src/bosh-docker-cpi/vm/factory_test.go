package vm

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Factory", func() {
	Describe("cgroupNamespaceConflict", func() {
		It("reports a conflict when a private namespace is asked for alongside the host cgroup bind", func() {
			Expect(cgroupNamespaceConflict(true, true, false)).To(BeTrue())
		})

		It("reports no conflict when the host namespace is in use", func() {
			Expect(cgroupNamespaceConflict(true, true, true)).To(BeFalse())
		})

		It("reports no conflict when the host cgroup hierarchy is not bound in", func() {
			Expect(cgroupNamespaceConflict(true, false, false)).To(BeFalse())
		})

		It("reports no conflict when systemd is not in use, since the namespace is not set then", func() {
			Expect(cgroupNamespaceConflict(false, true, false)).To(BeFalse())
		})
	})

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
