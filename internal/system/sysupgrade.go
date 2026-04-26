package system

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// SysupgradeCapability summarizes whether the running OS can perform a
// sysupgrade and why (or why not). All fields are derived from local
// filesystem state — no ubus calls.
type SysupgradeCapability struct {
	// BinaryPath is the resolved path to the sysupgrade binary.
	BinaryPath string

	// RootFSType is the filesystem type of the root mount as reported in
	// /proc/mounts ("squashfs", "ext4", "jffs2", or "" if unknown).
	RootFSType string

	// Reason is a short human-readable summary: "ok",
	// "no /sbin/sysupgrade", "rootfs not squashfs", etc.
	Reason string

	// BinaryPresent is true when the sysupgrade binary exists and is
	// executable.
	BinaryPresent bool

	// OverlayPresent is true when /overlay is mounted (squashfs OpenWrt
	// shape). Required for runtime persistence; absent on read-only
	// initramfs setups.
	OverlayPresent bool

	// Capable is true when all preconditions are satisfied.
	Capable bool
}

// SysupgradeCapabilityProvider abstracts capability detection for testing.
type SysupgradeCapabilityProvider interface {
	GetSysupgradeCapability() (*SysupgradeCapability, error)
}

// LinuxSysupgradeCapabilityProvider is the production implementation. All
// fields default to OpenWrt-canonical paths and may be overridden in tests.
type LinuxSysupgradeCapabilityProvider struct {
	// BinaryPath is the path to the sysupgrade binary; default
	// "/sbin/sysupgrade".
	BinaryPath string
	// MountsPath is the path to the mounts file; default "/proc/mounts".
	MountsPath string
	// OverlayPath is the overlay mount point; default "/overlay".
	OverlayPath string
}

func (p *LinuxSysupgradeCapabilityProvider) binaryPath() string {
	if p.BinaryPath != "" {
		return p.BinaryPath
	}

	return "/sbin/sysupgrade"
}

func (p *LinuxSysupgradeCapabilityProvider) mountsPath() string {
	if p.MountsPath != "" {
		return p.MountsPath
	}

	return "/proc/mounts"
}

func (p *LinuxSysupgradeCapabilityProvider) overlayPath() string {
	if p.OverlayPath != "" {
		return p.OverlayPath
	}

	return "/overlay"
}

// GetSysupgradeCapability inspects the running system and returns a
// non-nil SysupgradeCapability describing whether sysupgrade can run. An
// error is returned only for unexpected I/O failures (a missing
// /proc/mounts is treated as an unknown fstype, not an error, so the
// daemon stays useful on minimal test environments).
func (p *LinuxSysupgradeCapabilityProvider) GetSysupgradeCapability() (*SysupgradeCapability, error) {
	out := &SysupgradeCapability{
		BinaryPath: p.binaryPath(),
	}

	if info, err := os.Stat(out.BinaryPath); err == nil {
		// Executable bit set anywhere is sufficient for our purposes.
		out.BinaryPresent = info.Mode()&0o111 != 0
	}

	rootfs, mountsErr := readRootFSType(p.mountsPath())
	if mountsErr != nil && !errors.Is(mountsErr, fs.ErrNotExist) {
		return nil, fmt.Errorf("read mounts: %w", mountsErr)
	}

	out.RootFSType = rootfs

	if _, err := os.Stat(p.overlayPath()); err == nil {
		out.OverlayPresent = true
	}

	out.Reason, out.Capable = evaluateSysupgradeCapability(out)

	return out, nil
}

// evaluateSysupgradeCapability returns the (reason, capable) pair derived
// from the populated probe fields.
//
// On the standard OpenWrt squashfs+overlay layout, /proc/mounts reports the
// rootfs as "overlay" — the writable upper layer mounted over the read-only
// squashfs base. /overlay being mounted is the load-bearing signal that
// sysupgrade has somewhere to flash to. A bare "squashfs" rootfs (e.g. an
// initramfs or pre-overlay boot) is also accepted.
func evaluateSysupgradeCapability(c *SysupgradeCapability) (string, bool) {
	switch {
	case !c.BinaryPresent:
		return "no /sbin/sysupgrade", false
	case c.RootFSType == "":
		return "rootfs type unknown", false
	case !c.OverlayPresent:
		return "no /overlay mount", false
	case c.RootFSType != "overlay" && c.RootFSType != "squashfs":
		return fmt.Sprintf("rootfs is %s, not overlay/squashfs", c.RootFSType), false
	default:
		return "ok", true
	}
}

// readRootFSType returns the fstype of the root mount from a procfs-shaped
// mounts file. Lines look like "device mountpoint fstype options 0 0".
// Returns ("", nil) when the file exists but has no root entry.
func readRootFSType(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}

		if fields[1] == "/" {
			return fields[2], nil
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan mounts: %w", err)
	}

	return "", nil
}
