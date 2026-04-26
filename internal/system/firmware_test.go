package system

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseOpenWrtRelease(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    FirmwareInfo
		wantErr bool
	}{
		{
			name: "typical release file",
			input: `DISTRIB_ID='OpenWrt'
DISTRIB_RELEASE='23.05.3'
DISTRIB_REVISION='r23809-234f1a2efa'
DISTRIB_TARGET='ramips/mt76x8'
DISTRIB_ARCH='mipsel_24kc'
DISTRIB_DESCRIPTION='OpenWrt 23.05.3 / OpenMANET 1.7.0'
DISTRIB_TAINTS='no-hierarchical'
`,
			want: FirmwareInfo{
				Description:  "OpenWrt 23.05.3 / OpenMANET 1.7.0",
				Distribution: "OpenWrt",
				Release:      "23.05.3",
				Revision:     "r23809-234f1a2efa",
				Target:       "ramips/mt76x8",
				Arch:         "mipsel_24kc",
				OpenMANETVer: "1.7.0",
			},
		},
		{
			name: "openmanet branch-and-version description",
			input: `DISTRIB_ID='OpenMANET'
DISTRIB_RELEASE='24.10'
DISTRIB_TARGET='bcm27xx/bcm2711'
DISTRIB_DESCRIPTION='OpenMANET 24.10 1.7.0'
`,
			want: FirmwareInfo{
				Description:  "OpenMANET 24.10 1.7.0",
				Distribution: "OpenMANET",
				Release:      "24.10",
				Target:       "bcm27xx/bcm2711",
				OpenMANETVer: "1.7.0",
			},
		},
		{
			name: "double-quoted value",
			input: `DISTRIB_DESCRIPTION="OpenWrt 24.01.0"
`,
			want: FirmwareInfo{
				Description: "OpenWrt 24.01.0",
			},
		},
		{
			name:  "missing description",
			input: `DISTRIB_ID='OpenWrt'`,
			want:  FirmwareInfo{Distribution: "OpenWrt"},
		},
		{
			name:  "empty file",
			input: "",
			want:  FirmwareInfo{},
		},
		{
			name: "build date and codename",
			input: `DISTRIB_ID='OpenWrt'
DISTRIB_CODENAME='snapshot'
DISTRIB_BUILD_DATE='2026-04-01T12:00:00Z'
DISTRIB_DESCRIPTION='OpenMANET nightly'
`,
			want: FirmwareInfo{
				Distribution: "OpenWrt",
				Codename:     "snapshot",
				BuildDate:    "2026-04-01T12:00:00Z",
				Description:  "OpenMANET nightly",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseOpenWrtRelease(strings.NewReader(tt.input))
			if tt.wantErr {
				assert.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, &tt.want, got)
		})
	}
}

func TestOpenWrtFirmwareProvider_GetFirmwareInfo(t *testing.T) {
	// Write a temporary release file
	tmpFile := t.TempDir() + "/openwrt_release"
	content := `DISTRIB_ID='OpenWrt'
DISTRIB_TARGET='bcm27xx/bcm2711'
DISTRIB_DESCRIPTION='OpenWrt 23.05.3 / OpenMANET 1.7.0'
`
	require.NoError(t, os.WriteFile(tmpFile, []byte(content), 0o644))

	p := &OpenWrtFirmwareProvider{FilePath: tmpFile}
	info, err := p.GetFirmwareInfo()
	require.NoError(t, err)
	assert.Equal(t, "OpenWrt 23.05.3 / OpenMANET 1.7.0", info.Description)
	assert.Equal(t, "bcm27xx/bcm2711", info.Target)
	assert.Equal(t, "1.7.0", info.OpenMANETVer)
}

func TestOpenWrtFirmwareProvider_GetFirmwareInfo_MissingFile(t *testing.T) {
	p := &OpenWrtFirmwareProvider{FilePath: "/nonexistent/file"}
	_, err := p.GetFirmwareInfo()
	assert.Error(t, err)
}
