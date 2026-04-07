// Package device provides hardware device discovery helpers for the comms
// subsystem. It is the single place responsible for locating CM108-family
// USB audio/HID dongles (used by OpenVLM) so that both HID (PTT) and
// PortAudio/ALSA (audio) call sites can share one /sys walk instead of
// independently rescanning the system.
//
// See .claude/plans/comms-refactor.md section "2. Unified CM108 device
// descriptor" for the broader refactor context.
package device

import (
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"sync"
)

// CM108-family USB identifiers. C-Media's CM108/CM109/CM119 chips all share
// the 0x0D8C vendor ID; the product IDs below are the ones known to appear on
// OpenVLM hardware. New IDs may be appended here without touching the walk.
const (
	cmediaVendorID uint16 = 0x0D8C

	// CM108ProductOpenVLM is the product ID used by the OpenVLM USB dongle.
	CM108ProductOpenVLM uint16 = 0x0012
	// CM108ProductCM108AH is the CM108AH revision occasionally seen in the wild.
	CM108ProductCM108AH uint16 = 0x013C
	// CM108ProductCM119 is the CM119 revision.
	CM108ProductCM119 uint16 = 0x0008
)

// cm108ProductIDs enumerates the product IDs considered part of the CM108
// family for discovery purposes. Kept as a small package-level slice so the
// per-device loop can scan it without allocation.
var cm108ProductIDs = [...]uint16{ //nolint:gochecknoglobals
	CM108ProductOpenVLM,
	CM108ProductCM108AH,
	CM108ProductCM119,
}

// CM108Descriptor describes a single CM108-family USB device discovered on
// the system. A single device has at most one hidraw child and at most one
// ALSA sound card child; fields are zero-valued when the corresponding child
// could not be resolved.
type CM108Descriptor struct {
	HIDPath     string
	Serial      string
	SysPath     string
	ALSACardIdx int
	PADeviceIdx int
	VID         uint16
	PID         uint16
}

// PALookupFunc maps an ALSA card index to a PortAudio device index. It must
// return (-1, false) if no PortAudio device corresponds to the given ALSA
// card. Callers that do not use PortAudio may pass nil.
type PALookupFunc func(alsaCard int) (int, bool)

// DiscoverCM108 walks the USB device tree rooted at fsys (expected to be a
// filesystem whose root corresponds to /sys) and returns one
// CM108Descriptor per matching USB device.
//
// The walk only inspects top-level USB device directories (those whose
// directory name does not contain a ':' — the ':' separates the interface
// suffix from the device portion). For each matching device the walk
// resolves the first hidraw child and the first ALSA sound/cardN child and
// consults paLookup to map the ALSA card index to a PortAudio device index.
//
// Devices whose idVendor / idProduct files are missing or malformed are
// skipped with no error; a malformed file for one device does not prevent
// discovery of the remaining devices. A non-nil error is returned only for
// errors reading the top-level directory itself.
//
// Concurrency: DiscoverCM108 is safe for concurrent use; it holds no shared
// state of its own.
//
// TODO(comms-refactor phase 3+): add a udev netlink hotplug watcher so the
// OpenVLM control source can react to unplug/replug without polling. See
// .claude/plans/comms-refactor.md section "2. Unified CM108 device
// descriptor".
func DiscoverCM108(fsys fs.FS, paLookup PALookupFunc) ([]CM108Descriptor, error) {
	const usbRoot = "bus/usb/devices"

	entries, err := fs.ReadDir(fsys, usbRoot)
	if err != nil {
		// Missing /sys/bus/usb/devices on non-Linux or in tests is not an
		// error — it simply means "no devices discovered".
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}

		return nil, fmt.Errorf("device: read %s: %w", usbRoot, err)
	}

	// Preallocate: most systems have at most a handful of CM108 dongles.
	results := make([]CM108Descriptor, 0, 4)

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		name := e.Name()
		// Skip interface directories like "1-1.2:1.0" — we only want
		// top-level USB device directories.
		if strings.ContainsRune(name, ':') {
			continue
		}

		devPath := usbRoot + "/" + name

		vid, vidOK := readHexID(fsys, devPath+"/idVendor")
		if !vidOK || vid != cmediaVendorID {
			continue
		}

		pid, pidOK := readHexID(fsys, devPath+"/idProduct")
		if !pidOK || !isCM108Product(pid) {
			continue
		}

		desc := CM108Descriptor{
			HIDPath:     findHIDPath(fsys, devPath),
			ALSACardIdx: findALSACard(fsys, devPath),
			PADeviceIdx: -1,
			VID:         vid,
			PID:         pid,
			Serial:      strings.TrimSpace(readString(fsys, devPath+"/serial")),
			SysPath:     devPath,
		}

		if paLookup != nil && desc.ALSACardIdx >= 0 {
			if idx, ok := paLookup(desc.ALSACardIdx); ok {
				desc.PADeviceIdx = idx
			}
		}

		results = append(results, desc)
	}

	return results, nil
}

// isCM108Product reports whether the given USB product ID belongs to the
// CM108 family table.
func isCM108Product(pid uint16) bool {
	for _, p := range cm108ProductIDs {
		if p == pid {
			return true
		}
	}

	return false
}

// readHexID reads a /sys "idVendor" or "idProduct" file and parses its
// contents as a 4-digit hexadecimal value. Returns (0, false) if the file
// is missing or malformed.
func readHexID(fsys fs.FS, path string) (uint16, bool) {
	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		return 0, false
	}

	s := strings.TrimSpace(string(data))
	if s == "" {
		return 0, false
	}

	n, err := strconv.ParseUint(s, 16, 16)
	if err != nil {
		return 0, false
	}

	return uint16(n), true
}

// readString reads a /sys attribute file and returns its raw contents. An
// empty string is returned on any error so callers can treat missing and
// empty attributes uniformly.
func readString(fsys fs.FS, path string) string {
	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		return ""
	}

	return string(data)
}

// findHIDPath walks the USB device's interface subdirectories looking for a
// hidraw child. The hidraw directory under
// "<devPath>/<iface>/<hid>/hidraw/hidrawN" maps directly to the
// "/dev/hidrawN" character device. Returns the first match, or "" if none
// is found.
func findHIDPath(fsys fs.FS, devPath string) string {
	ifaces, err := fs.ReadDir(fsys, devPath)
	if err != nil {
		return ""
	}

	for _, iface := range ifaces {
		if !iface.IsDir() || !strings.Contains(iface.Name(), ":") {
			continue
		}

		hidDirPath := devPath + "/" + iface.Name()

		// Look for a "<x>:<y>/.../hidraw/hidrawN" descendant. Walk one
		// level at a time to avoid pulling in filepath.Walk's depth
		// recursion for what is a shallow tree.
		hidChildren, err := fs.ReadDir(fsys, hidDirPath)
		if err != nil {
			continue
		}

		for _, child := range hidChildren {
			if !child.IsDir() {
				continue
			}

			// Either "hidraw" sits here directly, or under a
			// "<vid>:<pid>.<inst>" HID-core directory.
			candidates := []string{
				hidDirPath + "/" + child.Name() + "/hidraw",
				hidDirPath + "/hidraw",
			}

			for _, candidate := range candidates {
				rawEntries, rErr := fs.ReadDir(fsys, candidate)
				if rErr != nil {
					continue
				}

				for _, raw := range rawEntries {
					if strings.HasPrefix(raw.Name(), "hidraw") {
						return "/dev/" + raw.Name()
					}
				}
			}
		}
	}

	return ""
}

// findALSACard walks the USB device's interface subdirectories looking for
// a "sound/cardN" child. Returns the numeric card index, or -1 if none is
// found.
func findALSACard(fsys fs.FS, devPath string) int {
	ifaces, err := fs.ReadDir(fsys, devPath)
	if err != nil {
		return -1
	}

	for _, iface := range ifaces {
		if !iface.IsDir() || !strings.Contains(iface.Name(), ":") {
			continue
		}

		soundPath := devPath + "/" + iface.Name() + "/sound"

		entries, err := fs.ReadDir(fsys, soundPath)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			name := entry.Name()
			if !strings.HasPrefix(name, "card") {
				continue
			}

			n, err := strconv.Atoi(strings.TrimPrefix(name, "card"))
			if err != nil {
				continue
			}

			return n
		}
	}

	return -1
}

// ─── LookupCM108: cached single-descriptor lookup ─────────────────────────────

// Cache holds the result of a single DiscoverCM108 walk so that the two call
// sites (OpenVLM HID open and broadcast stream PortAudio selection) can
// share the result inside the scope of one stream-open instead of each
// performing the walk independently.
//
// Cache is safe for concurrent use.
type Cache struct {
	fsys     fs.FS
	err      error
	paLookup PALookupFunc
	descs    []CM108Descriptor
	mu       sync.Mutex
	cached   bool
}

// NewCache constructs a Cache that will call DiscoverCM108(fsys, paLookup)
// on first access. Subsequent calls return the same result until Invalidate
// is called.
func NewCache(fsys fs.FS, paLookup PALookupFunc) *Cache {
	return &Cache{fsys: fsys, paLookup: paLookup}
}

// Descriptors returns the cached CM108 descriptors, performing the walk
// lazily on first call.
func (c *Cache) Descriptors() ([]CM108Descriptor, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cached {
		return c.descs, c.err
	}

	c.descs, c.err = DiscoverCM108(c.fsys, c.paLookup)
	c.cached = true

	return c.descs, c.err
}

// Invalidate clears the cached result so the next Descriptors call will
// walk /sys again. Intended for use with a future hotplug watcher.
func (c *Cache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cached = false
	c.descs = nil
	c.err = nil
}
