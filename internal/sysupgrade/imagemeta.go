package sysupgrade

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
)

// FWx0 trailer constants. Layout follows the OpenWrt fwtool repository
// (https://git.openwrt.org/?p=project/fwtool.git;a=blob;f=fwimage.h),
// which OpenWrt 24.10's package/system/fwtool/Makefile pins to commit
// 8f7fe925ca205c8e8e2d0d1b16218c1e148d5173 (2019-11-12).
//
// Each chunk is a sequence of:
//
//	type=INFO:  fwimage_header(8) || metadata.json bytes || fwimage_trailer(16)
//	type=SIG:                       signature data       || fwimage_trailer(16)
//
// The trailer's `size` field is the total byte length of the chunk
// including the trailer itself; chunks are byte-packed at the end of
// the firmware file with no padding. Multi-byte trailer fields are
// big-endian.
const (
	fwimageMagic      uint32 = 0x46577830 // "FWx0"
	fwimageTypeSig    uint8  = 0
	fwimageTypeInfo   uint8  = 1
	fwimageHeaderLen         = 8
	fwimageTrailerLen        = 16
	fwimageMetaMaxLen        = 30 * 1024 // METADATA_MAXLEN in fwtool.c
	fwimageWalkLimit         = 8         // defensive cap on chunk-walk iterations
)

// ErrNoImageMetadata is returned by ParseImageMetadata when the file
// has no FWx0 trailer. This is the normal case for factory images and
// for builds whose Makefile did not declare SUPPORTED_DEVICES.
var ErrNoImageMetadata = errors.New("sysupgrade: no FWx0 metadata trailer")

// ImageVersion is the optional `version` block embedded in metadata.json.
// Informational only; the compatibility decision does not consult it.
type ImageVersion struct {
	Dist     string `json:"dist,omitempty"`
	Version  string `json:"version,omitempty"`
	Revision string `json:"revision,omitempty"`
	Target   string `json:"target,omitempty"`
	Board    string `json:"board,omitempty"`
}

// ImageMetadata is the metadata.json blob produced by OpenWrt's
// `metadata_json` macro (include/image-commands.mk) and embedded in
// every sysupgrade image whose target Makefile sets SUPPORTED_DEVICES.
type ImageMetadata struct {
	Version             ImageVersion `json:"version"`
	MetadataVersion     string       `json:"metadata_version,omitempty"`
	CompatVersion       string       `json:"compat_version,omitempty"`
	CompatMessage       string       `json:"compat_message,omitempty"`
	SupportedDevices    []string     `json:"supported_devices,omitempty"`
	NewSupportedDevices []string     `json:"new_supported_devices,omitempty"`
}

// EffectiveSupportedDevices returns the device list that the OpenWrt
// fwtool_check_image shell consults. When `compat_version` differs
// from "1.0", `supported_devices` carries a poison-pill warning string
// for old fwtool readers, and the real list is in
// `new_supported_devices` — see image-commands.mk:78-81.
func (m *ImageMetadata) EffectiveSupportedDevices() []string {
	if m == nil {
		return nil
	}

	if m.CompatVersion != "" && m.CompatVersion != "1.0" && len(m.NewSupportedDevices) > 0 {
		return m.NewSupportedDevices
	}

	return m.SupportedDevices
}

// MatchesDevice reports whether boardName appears verbatim in the
// effective supported-devices list. Comparison is byte-exact, mirroring
// fwtool.sh which uses `[ "$dev" = "$device" ]`.
func (m *ImageMetadata) MatchesDevice(boardName string) bool {
	if m == nil || boardName == "" {
		return false
	}

	return slices.Contains(m.EffectiveSupportedDevices(), boardName)
}

// ParseImageMetadata reads the FWx0 trailer chain at the end of `path`,
// walks any optional signature chunk(s), and decodes the embedded
// metadata.json payload.
//
// Returns ErrNoImageMetadata when the file has no FWx0 trailer at all
// (factory images, third-party builds without SUPPORTED_DEVICES). All
// other errors indicate corruption.
//
// The parser does NOT decompress: fwtool itself is gzip-unaware and the
// build pipeline runs `append-metadata` AFTER `gzip`, so the trailer
// always lives at the very end of the on-disk file regardless of the
// `.img.gz` outer compression.
func ParseImageMetadata(path string) (*ImageMetadata, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("sysupgrade: open image %q: %w", path, err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("sysupgrade: stat image %q: %w", path, err)
	}

	return parseImageMetadataFromReader(f, fi.Size())
}

func parseImageMetadataFromReader(r io.ReaderAt, size int64) (*ImageMetadata, error) {
	if size < int64(fwimageTrailerLen) {
		return nil, ErrNoImageMetadata
	}

	var (
		infoOffset int64 = -1
		infoLen    int64 = -1
		cursor           = size
		foundAny   bool
	)

	for range fwimageWalkLimit {
		if cursor < int64(fwimageTrailerLen) {
			break
		}

		var tr [fwimageTrailerLen]byte
		if _, err := r.ReadAt(tr[:], cursor-int64(fwimageTrailerLen)); err != nil {
			return nil, fmt.Errorf("sysupgrade: read FWx0 trailer: %w", err)
		}

		magic := binary.BigEndian.Uint32(tr[0:4])
		if magic != fwimageMagic {
			break
		}

		chunkType := tr[8]
		chunkSize := int64(binary.BigEndian.Uint32(tr[12:16]))

		if chunkSize < int64(fwimageTrailerLen) || chunkSize > cursor {
			return nil, fmt.Errorf("sysupgrade: malformed FWx0 chunk size %d (cursor=%d)", chunkSize, cursor)
		}

		chunkStart := cursor - chunkSize
		foundAny = true

		switch chunkType {
		case fwimageTypeInfo:
			// Chunk = fwimage_header(8) || json || trailer(16).
			jsonStart := chunkStart + int64(fwimageHeaderLen)

			jsonLen := chunkSize - int64(fwimageHeaderLen) - int64(fwimageTrailerLen)
			if jsonLen <= 0 || jsonLen > int64(fwimageMetaMaxLen) {
				return nil, fmt.Errorf("sysupgrade: implausible FWx0 metadata length %d", jsonLen)
			}

			infoOffset = jsonStart
			infoLen = jsonLen
		case fwimageTypeSig:
			// Signature chunk: skip past it and keep walking. Daemon
			// does not validate ucert signatures today.
		default:
			return nil, fmt.Errorf("sysupgrade: unknown FWx0 chunk type %d", chunkType)
		}

		cursor = chunkStart
	}

	if !foundAny || infoOffset < 0 {
		return nil, ErrNoImageMetadata
	}

	payload := make([]byte, infoLen)
	if _, err := r.ReadAt(payload, infoOffset); err != nil {
		return nil, fmt.Errorf("sysupgrade: read FWx0 metadata payload: %w", err)
	}

	var meta ImageMetadata
	if err := json.Unmarshal(bytes.TrimSpace(payload), &meta); err != nil {
		return nil, fmt.Errorf("sysupgrade: decode metadata.json: %w", err)
	}

	return &meta, nil
}

// readDeviceCompatString returns the device's first compatible string
// — the same value the OpenWrt fwtool_check_image shell consults. The
// preferred source is /etc/board.json (BoardProvider.GetBoard()); the
// fallback is the first NUL-separated token of /proc/device-tree/compatible,
// matching the behavior of preinit's 02_sysinfo helper.
func (m *Manager) readDeviceCompatString() string {
	if b, err := m.board.GetBoard(); err == nil && b != nil && b.Model.ID != "" {
		return strings.TrimSpace(b.Model.ID)
	}

	data, err := os.ReadFile("/proc/device-tree/compatible")
	if err != nil {
		return ""
	}

	if i := bytes.IndexByte(data, 0); i > 0 {
		return strings.TrimSpace(string(data[:i]))
	}

	return strings.TrimSpace(string(data))
}
