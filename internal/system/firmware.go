package system

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

// openmanetVersionRE pulls the OpenMANET semver out of a DISTRIB_DESCRIPTION
// such as "OpenWrt 23.05.3 / OpenMANET 1.7.0" or "OpenMANET 24.10 1.7.0".
// The match group is X.Y.Z (with optional pre-release suffix).
var openmanetVersionRE = regexp.MustCompile(`OpenMANET[^0-9]*(?:\d+\.\d+\s+)?(\d+\.\d+\.\d+(?:[-.][0-9A-Za-z.-]+)?)`)

// FirmwareInfo holds parsed OpenWrt release metadata. All fields come from
// /etc/openwrt_release except OpenMANETVer, which is derived by regex from
// Description.
type FirmwareInfo struct {
	// Description is the full firmware string, e.g.
	// "OpenWrt 23.05.3 / OpenMANET 1.7.0".
	Description string

	// Distribution is DISTRIB_ID, e.g. "OpenWrt".
	Distribution string

	// Release is DISTRIB_RELEASE, e.g. "23.05.3".
	Release string

	// Revision is DISTRIB_REVISION, e.g. "r23809-234f1a2efa".
	Revision string

	// Target is DISTRIB_TARGET, e.g. "bcm27xx/bcm2711". This is the key
	// used to match a board to the correct sysupgrade firmware image.
	Target string

	// Codename is DISTRIB_CODENAME, e.g. "snapshot" or "stable".
	Codename string

	// Arch is DISTRIB_ARCH, e.g. "aarch64_cortex-a72".
	Arch string

	// BuildDate is DISTRIB_BUILD_DATE, when present (newer images only).
	BuildDate string

	// OpenMANETVer is the OpenMANET semver parsed from Description, e.g.
	// "1.7.0". Empty when Description does not contain a recognizable
	// OpenMANET version.
	OpenMANETVer string
}

// FirmwareProvider abstracts firmware information retrieval.
type FirmwareProvider interface {
	GetFirmwareInfo() (*FirmwareInfo, error)
}

// OpenWrtFirmwareProvider reads /etc/openwrt_release.
type OpenWrtFirmwareProvider struct {
	// FilePath overrides the default path for testing.
	FilePath string
}

func (p *OpenWrtFirmwareProvider) filePath() string {
	if p.FilePath != "" {
		return p.FilePath
	}

	return "/etc/openwrt_release"
}

// GetFirmwareInfo reads and parses the OpenWrt release file.
func (p *OpenWrtFirmwareProvider) GetFirmwareInfo() (*FirmwareInfo, error) {
	f, err := os.Open(p.filePath())
	if err != nil {
		return nil, fmt.Errorf("open openwrt_release: %w", err)
	}
	defer f.Close()

	return ParseOpenWrtRelease(f)
}

// ParseOpenWrtRelease parses an /etc/openwrt_release-formatted stream.
// Each non-empty line has the form KEY='value' or KEY="value" (single-line
// values only). All recognized keys are projected onto FirmwareInfo.
func ParseOpenWrtRelease(r io.Reader) (*FirmwareInfo, error) {
	const maxKeys = 16

	values := make(map[string]string, maxKeys)

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()

		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}

		key := line[:eq]
		val := strings.Trim(line[eq+1:], "'\"")
		values[key] = val
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan openwrt_release: %w", err)
	}

	info := &FirmwareInfo{
		Description:  values["DISTRIB_DESCRIPTION"],
		Distribution: values["DISTRIB_ID"],
		Release:      values["DISTRIB_RELEASE"],
		Revision:     values["DISTRIB_REVISION"],
		Target:       values["DISTRIB_TARGET"],
		Codename:     values["DISTRIB_CODENAME"],
		Arch:         values["DISTRIB_ARCH"],
		BuildDate:    values["DISTRIB_BUILD_DATE"],
	}

	if m := openmanetVersionRE.FindStringSubmatch(info.Description); len(m) == 2 {
		info.OpenMANETVer = m[1]
	}

	return info, nil
}
