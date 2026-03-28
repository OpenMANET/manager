package system

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// FirmwareInfo holds parsed OpenWrt release metadata.
type FirmwareInfo struct {
	// Description is the full firmware string, e.g.
	// "OpenWrt 23.05.3 / OpenMANET 1.7.0".
	Description string
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
// It looks for DISTRIB_DESCRIPTION and returns it as the description.
func ParseOpenWrtRelease(r io.Reader) (*FirmwareInfo, error) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "DISTRIB_DESCRIPTION=") {
			val := strings.TrimPrefix(line, "DISTRIB_DESCRIPTION=")
			val = strings.Trim(val, "'\"")

			return &FirmwareInfo{Description: val}, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan openwrt_release: %w", err)
	}

	return &FirmwareInfo{}, nil
}
