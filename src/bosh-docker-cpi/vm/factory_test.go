package vm

import (
	dkrcont "github.com/docker/docker/api/types/container"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Factory", func() {
	Describe("cgroupnsMode", func() {
		It("uses the host namespace when asked to", func() {
			Expect(cgroupnsMode(true, true)).To(Equal(dkrcont.CgroupnsModeHost))
		})

		It("names the private namespace explicitly rather than leaving it to the daemon", func() {
			Expect(cgroupnsMode(true, false)).To(Equal(dkrcont.CgroupnsModePrivate))
		})

		It("sets no mode at all when systemd is not in use", func() {
			Expect(cgroupnsMode(false, false)).To(BeEmpty())
			Expect(cgroupnsMode(false, true)).To(BeEmpty())
		})
	})

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
