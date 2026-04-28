package system

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// FactoryResetCapability summarizes whether the running OS can perform a
// factory reset (overlay wipe + reboot, equivalent to LuCI's "Perform
// Reset" button) and why or why not. All fields are derived from local
// filesystem state.
//
// Hostname is left empty by this provider; the manager fills it in from
// the SysInfoProvider so the UI can use it as the typed-confirmation
// target without an extra round trip.
type FactoryResetCapability struct {
	// Reason is a short human-readable summary: "ok", "no rootfs_data
	// partition or overlayfs mount", "firstboot not present at <path>",
	// etc.
	Reason string

	// OverlayMountpoint is the matched line from /proc/mounts when an
	// overlay was found at /, e.g. "overlayfs:/overlay /". Empty when
	// the overlay was discovered via /proc/mtd instead.
	OverlayMountpoint string

	// BackingFS describes where the overlay state actually lives. Either
	// the fstype reported by /proc/mounts ("overlay"), or "mtd:rootfs_data"
	// when only the MTD partition was found.
	BackingFS string

	// FirstbootPath is the resolved path to the firstboot helper. Set
	// only when Capable is true.
	FirstbootPath string

	// Hostname is populated by the manager from SysInfoProvider —
	// the capability provider itself does not read /etc/hostname.
	Hostname string

	// Capable is true when an overlay is present AND firstboot is
	// installed and executable.
	Capable bool
}

// FactoryResetCapabilityProvider abstracts capability detection for testing.
type FactoryResetCapabilityProvider interface {
	GetFactoryResetCapability() (*FactoryResetCapability, error)
}

// LinuxFactoryResetCapabilityProvider is the production implementation.
// Mirrors LuCI's flash.js render check: an overlayfs mount at / OR a
// rootfs_data partition in /proc/mtd, plus the firstboot helper.
type LinuxFactoryResetCapabilityProvider struct {
	// MountsPath is the path to the mounts file; default "/proc/mounts".
	MountsPath string
	// MtdPath is the path to the MTD listing; default "/proc/mtd".
	MtdPath string
	// FirstbootPath is the path to the firstboot helper binary; default
	// "/sbin/firstboot".
	FirstbootPath string
}

func (p *LinuxFactoryResetCapabilityProvider) mountsPath() string {
	if p.MountsPath != "" {
		return p.MountsPath
	}

	return "/proc/mounts"
}

func (p *LinuxFactoryResetCapabilityProvider) mtdPath() string {
	if p.MtdPath != "" {
		return p.MtdPath
	}

	return "/proc/mtd"
}

func (p *LinuxFactoryResetCapabilityProvider) firstbootPath() string {
	if p.FirstbootPath != "" {
		return p.FirstbootPath
	}

	return "/sbin/firstboot"
}

// GetFactoryResetCapability returns a populated capability struct
// describing whether factory reset can run on this device. Errors are
// returned only for unexpected I/O failures; a missing /proc/mtd or
// /proc/mounts is treated as "not capable", not an error, so dev
// environments are handled cleanly.
func (p *LinuxFactoryResetCapabilityProvider) GetFactoryResetCapability() (*FactoryResetCapability, error) {
	out := &FactoryResetCapability{}

	overlayLine, overlayFS, mountErr := scanForOverlayRoot(p.mountsPath())
	if mountErr != nil && !errors.Is(mountErr, fs.ErrNotExist) {
		return nil, fmt.Errorf("read mounts: %w", mountErr)
	}

	out.OverlayMountpoint = overlayLine
	out.BackingFS = overlayFS

	if out.OverlayMountpoint == "" {
		hasMTD, mtdErr := scanForRootfsData(p.mtdPath())
		if mtdErr != nil && !errors.Is(mtdErr, fs.ErrNotExist) {
			return nil, fmt.Errorf("read mtd: %w", mtdErr)
		}

		if hasMTD {
			out.BackingFS = "mtd:rootfs_data"
		}
	}

	if out.OverlayMountpoint == "" && out.BackingFS == "" {
		out.Capable = false
		out.Reason = "no rootfs_data partition or overlayfs mount"

		return out, nil
	}

	info, err := os.Stat(p.firstbootPath())
	if err != nil {
		out.Capable = false
		out.Reason = fmt.Sprintf("firstboot not present at %s", p.firstbootPath())

		return out, nil
	}

	if info.Mode()&0o111 == 0 {
		out.Capable = false
		out.Reason = fmt.Sprintf("firstboot not executable at %s", p.firstbootPath())

		return out, nil
	}

	out.FirstbootPath = p.firstbootPath()
	out.Capable = true
	out.Reason = "ok"

	return out, nil
}

// scanForOverlayRoot returns ("<source> /", "<fstype>") for the root
// overlay mount, or ("", "") when none is found. The overlay device on
// OpenWrt is typically named "overlayfs:/overlay" or simply "overlay";
// we accept either by matching on mountpoint=="/" + fstype=="overlay".
func scanForOverlayRoot(path string) (string, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}

		if fields[1] == "/" && fields[2] == "overlay" {
			return fields[0] + " " + fields[1], fields[2], nil
		}
	}

	if err := scanner.Err(); err != nil {
		return "", "", fmt.Errorf("scan mounts: %w", err)
	}

	return "", "", nil
}

// scanForRootfsData returns true when /proc/mtd contains a partition
// labeled "rootfs_data" — the storage backing the overlay on MIPS
// routers and other MTD-based devices. Mirrors LuCI's
// `procmtd.match(/"rootfs_data"/)` check exactly.
func scanForRootfsData(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), `"rootfs_data"`) {
			return true, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("scan mtd: %w", err)
	}

	return false, nil
}
