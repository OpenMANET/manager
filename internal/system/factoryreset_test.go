package system

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFile is a small helper that fails the test on any write error.
func writeFile(t *testing.T, path, contents string, mode os.FileMode) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(contents), mode))
}

func TestFactoryResetCapability_OverlayfsAndFirstboot(t *testing.T) {
	dir := t.TempDir()

	mounts := filepath.Join(dir, "mounts")
	mtd := filepath.Join(dir, "mtd")
	firstboot := filepath.Join(dir, "firstboot")

	writeFile(t, mounts, "rootfs / squashfs ro 0 0\noverlayfs:/overlay / overlay rw 0 0\ntmpfs /tmp tmpfs rw 0 0\n", 0o644)
	writeFile(t, mtd, "dev: size erasesize name\nmtd0: 00000000 00001000 \"boot\"\n", 0o644)
	writeFile(t, firstboot, "#!/bin/sh\nexit 0\n", 0o755)

	p := &LinuxFactoryResetCapabilityProvider{
		MountsPath:    mounts,
		MtdPath:       mtd,
		FirstbootPath: firstboot,
	}

	cap, err := p.GetFactoryResetCapability()
	require.NoError(t, err)
	require.NotNil(t, cap)
	assert.True(t, cap.Capable)
	assert.Equal(t, "ok", cap.Reason)
	assert.Equal(t, "overlayfs:/overlay /", cap.OverlayMountpoint)
	assert.Equal(t, "overlay", cap.BackingFS)
	assert.Equal(t, firstboot, cap.FirstbootPath)
}

func TestFactoryResetCapability_AcceptsAlternateOverlayDevice(t *testing.T) {
	// Some OpenWrt builds report the overlay device as bare "overlay"
	// rather than "overlayfs:/overlay". Match either by checking
	// mountpoint=="/" + fstype=="overlay".
	dir := t.TempDir()

	mounts := filepath.Join(dir, "mounts")
	firstboot := filepath.Join(dir, "firstboot")

	writeFile(t, mounts, "overlay / overlay rw 0 0\n", 0o644)
	writeFile(t, firstboot, "#!/bin/sh\n", 0o755)

	p := &LinuxFactoryResetCapabilityProvider{
		MountsPath:    mounts,
		MtdPath:       "/nonexistent",
		FirstbootPath: firstboot,
	}

	cap, err := p.GetFactoryResetCapability()
	require.NoError(t, err)
	assert.True(t, cap.Capable)
	assert.Equal(t, "overlay /", cap.OverlayMountpoint)
}

func TestFactoryResetCapability_MtdOnlyNoOverlayMount(t *testing.T) {
	// MIPS routers may have a rootfs_data MTD partition without it
	// being currently mounted as overlayfs (extroot edge cases).
	// LuCI's render check accepts that case; we should too.
	dir := t.TempDir()

	mounts := filepath.Join(dir, "mounts")
	mtd := filepath.Join(dir, "mtd")
	firstboot := filepath.Join(dir, "firstboot")

	writeFile(t, mounts, "/dev/root / squashfs ro 0 0\n", 0o644)
	writeFile(t, mtd, `dev:    size   erasesize  name
mtd0: 00080000 00010000 "u-boot"
mtd1: 00010000 00010000 "u-boot-env"
mtd2: 00f80000 00010000 "rootfs"
mtd3: 00d00000 00010000 "rootfs_data"
`, 0o644)
	writeFile(t, firstboot, "#!/bin/sh\n", 0o755)

	p := &LinuxFactoryResetCapabilityProvider{
		MountsPath:    mounts,
		MtdPath:       mtd,
		FirstbootPath: firstboot,
	}

	cap, err := p.GetFactoryResetCapability()
	require.NoError(t, err)
	assert.True(t, cap.Capable)
	assert.Empty(t, cap.OverlayMountpoint)
	assert.Equal(t, "mtd:rootfs_data", cap.BackingFS)
}

func TestFactoryResetCapability_NoOverlayNoMtd(t *testing.T) {
	dir := t.TempDir()

	mounts := filepath.Join(dir, "mounts")
	mtd := filepath.Join(dir, "mtd")

	writeFile(t, mounts, "/dev/sda1 / ext4 rw 0 0\n", 0o644)
	writeFile(t, mtd, "dev: size erasesize name\n", 0o644)

	p := &LinuxFactoryResetCapabilityProvider{
		MountsPath:    mounts,
		MtdPath:       mtd,
		FirstbootPath: "/usr/bin/true",
	}

	cap, err := p.GetFactoryResetCapability()
	require.NoError(t, err)
	assert.False(t, cap.Capable)
	assert.Contains(t, cap.Reason, "no rootfs_data partition or overlayfs mount")
}

func TestFactoryResetCapability_FirstbootMissing(t *testing.T) {
	dir := t.TempDir()

	mounts := filepath.Join(dir, "mounts")
	writeFile(t, mounts, "overlayfs:/overlay / overlay rw 0 0\n", 0o644)

	p := &LinuxFactoryResetCapabilityProvider{
		MountsPath:    mounts,
		MtdPath:       "/nonexistent",
		FirstbootPath: filepath.Join(dir, "no-such-firstboot"),
	}

	cap, err := p.GetFactoryResetCapability()
	require.NoError(t, err)
	assert.False(t, cap.Capable)
	assert.Contains(t, cap.Reason, "firstboot not present")
}

func TestFactoryResetCapability_FirstbootNotExecutable(t *testing.T) {
	dir := t.TempDir()

	mounts := filepath.Join(dir, "mounts")
	firstboot := filepath.Join(dir, "firstboot")

	writeFile(t, mounts, "overlayfs:/overlay / overlay rw 0 0\n", 0o644)
	writeFile(t, firstboot, "#!/bin/sh\n", 0o644)

	p := &LinuxFactoryResetCapabilityProvider{
		MountsPath:    mounts,
		MtdPath:       "/nonexistent",
		FirstbootPath: firstboot,
	}

	cap, err := p.GetFactoryResetCapability()
	require.NoError(t, err)
	assert.False(t, cap.Capable)
	assert.Contains(t, cap.Reason, "firstboot not executable")
}

func TestFactoryResetCapability_MountsMissing(t *testing.T) {
	// Missing /proc/mounts is typical of a non-Linux test environment.
	// /proc/mtd may also be missing. The provider should treat both as
	// "not capable" rather than returning an error.
	dir := t.TempDir()

	p := &LinuxFactoryResetCapabilityProvider{
		MountsPath:    filepath.Join(dir, "no-mounts"),
		MtdPath:       filepath.Join(dir, "no-mtd"),
		FirstbootPath: "/usr/bin/true",
	}

	cap, err := p.GetFactoryResetCapability()
	require.NoError(t, err)
	assert.False(t, cap.Capable)
	assert.Contains(t, cap.Reason, "no rootfs_data partition or overlayfs mount")
}
