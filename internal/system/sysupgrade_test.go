package system

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeMounts is a small helper that writes a procfs-shaped mounts file
// and returns its path.
func writeMounts(t *testing.T, dir, content string) string {
	t.Helper()

	p := filepath.Join(dir, "mounts")
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))

	return p
}

// writeExecutable writes an executable file at <dir>/<name>.
func writeExecutable(t *testing.T, dir, name string) string {
	t.Helper()

	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755))

	return p
}

func TestLinuxSysupgradeCapabilityProvider_Capable(t *testing.T) {
	tmp := t.TempDir()

	mounts := writeMounts(t, tmp, "rootfs / squashfs ro 0 0\nproc /proc proc rw 0 0\n")
	bin := writeExecutable(t, tmp, "sysupgrade")
	overlay := filepath.Join(tmp, "overlay")
	require.NoError(t, os.MkdirAll(overlay, 0o755))

	p := &LinuxSysupgradeCapabilityProvider{
		BinaryPath:  bin,
		MountsPath:  mounts,
		OverlayPath: overlay,
	}

	got, err := p.GetSysupgradeCapability()
	require.NoError(t, err)
	assert.True(t, got.Capable, "expected Capable=true")
	assert.Equal(t, "ok", got.Reason)
	assert.Equal(t, "squashfs", got.RootFSType)
	assert.True(t, got.BinaryPresent)
	assert.True(t, got.OverlayPresent)
}

// TestLinuxSysupgradeCapabilityProvider_OverlayRootIsCapable covers the
// standard OpenWrt squashfs+overlay layout: /proc/mounts reports rootfs as
// "overlay" (the writable upper mounted on the squashfs base) and /overlay
// is mounted. This is the on-device shape of every shipping OpenMANET
// firmware.
func TestLinuxSysupgradeCapabilityProvider_OverlayRootIsCapable(t *testing.T) {
	tmp := t.TempDir()

	mounts := writeMounts(t, tmp, "overlayfs:/overlay / overlay rw 0 0\nproc /proc proc rw 0 0\n")
	bin := writeExecutable(t, tmp, "sysupgrade")
	overlay := filepath.Join(tmp, "overlay")
	require.NoError(t, os.MkdirAll(overlay, 0o755))

	p := &LinuxSysupgradeCapabilityProvider{
		BinaryPath:  bin,
		MountsPath:  mounts,
		OverlayPath: overlay,
	}

	got, err := p.GetSysupgradeCapability()
	require.NoError(t, err)
	assert.True(t, got.Capable, "overlay rootfs + /overlay mount must be capable")
	assert.Equal(t, "ok", got.Reason)
	assert.Equal(t, "overlay", got.RootFSType)
}

func TestLinuxSysupgradeCapabilityProvider_NoBinary(t *testing.T) {
	tmp := t.TempDir()

	mounts := writeMounts(t, tmp, "rootfs / squashfs ro 0 0\n")

	p := &LinuxSysupgradeCapabilityProvider{
		BinaryPath:  filepath.Join(tmp, "missing-sysupgrade"),
		MountsPath:  mounts,
		OverlayPath: filepath.Join(tmp, "overlay"),
	}

	got, err := p.GetSysupgradeCapability()
	require.NoError(t, err)
	assert.False(t, got.Capable)
	assert.Contains(t, got.Reason, "no /sbin/sysupgrade")
}

func TestLinuxSysupgradeCapabilityProvider_WrongRootFS(t *testing.T) {
	tmp := t.TempDir()

	mounts := writeMounts(t, tmp, "rootfs / ext4 rw 0 0\n")
	bin := writeExecutable(t, tmp, "sysupgrade")
	overlay := filepath.Join(tmp, "overlay")
	require.NoError(t, os.MkdirAll(overlay, 0o755))

	p := &LinuxSysupgradeCapabilityProvider{
		BinaryPath:  bin,
		MountsPath:  mounts,
		OverlayPath: overlay,
	}

	got, err := p.GetSysupgradeCapability()
	require.NoError(t, err)
	assert.False(t, got.Capable)
	assert.Equal(t, "ext4", got.RootFSType)
	assert.Contains(t, got.Reason, "ext4")
	assert.Contains(t, got.Reason, "overlay/squashfs")
}

func TestLinuxSysupgradeCapabilityProvider_NoOverlay(t *testing.T) {
	tmp := t.TempDir()

	mounts := writeMounts(t, tmp, "rootfs / squashfs ro 0 0\n")
	bin := writeExecutable(t, tmp, "sysupgrade")

	p := &LinuxSysupgradeCapabilityProvider{
		BinaryPath:  bin,
		MountsPath:  mounts,
		OverlayPath: filepath.Join(tmp, "missing-overlay"),
	}

	got, err := p.GetSysupgradeCapability()
	require.NoError(t, err)
	assert.False(t, got.Capable)
	assert.Contains(t, got.Reason, "overlay")
}

func TestLinuxSysupgradeCapabilityProvider_MissingMountsFile(t *testing.T) {
	tmp := t.TempDir()
	bin := writeExecutable(t, tmp, "sysupgrade")

	p := &LinuxSysupgradeCapabilityProvider{
		BinaryPath:  bin,
		MountsPath:  filepath.Join(tmp, "missing-mounts"),
		OverlayPath: tmp,
	}

	got, err := p.GetSysupgradeCapability()
	require.NoError(t, err)
	assert.False(t, got.Capable)
	assert.Empty(t, got.RootFSType)
}
