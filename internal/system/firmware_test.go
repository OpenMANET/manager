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
		want    string
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
			want: "OpenWrt 23.05.3 / OpenMANET 1.7.0",
		},
		{
			name: "double-quoted value",
			input: `DISTRIB_DESCRIPTION="OpenWrt 24.01.0"
`,
			want: "OpenWrt 24.01.0",
		},
		{
			name:  "missing description",
			input: `DISTRIB_ID='OpenWrt'`,
			want:  "",
		},
		{
			name:  "empty file",
			input: "",
			want:  "",
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
			assert.Equal(t, tt.want, got.Description)
		})
	}
}

func TestOpenWrtFirmwareProvider_GetFirmwareInfo(t *testing.T) {
	// Write a temporary release file
	tmpFile := t.TempDir() + "/openwrt_release"
	content := `DISTRIB_ID='OpenWrt'
DISTRIB_DESCRIPTION='OpenWrt 23.05.3 / OpenMANET 1.7.0'
`
	require.NoError(t, os.WriteFile(tmpFile, []byte(content), 0o644))

	p := &OpenWrtFirmwareProvider{FilePath: tmpFile}
	info, err := p.GetFirmwareInfo()
	require.NoError(t, err)
	assert.Equal(t, "OpenWrt 23.05.3 / OpenMANET 1.7.0", info.Description)
}

func TestOpenWrtFirmwareProvider_GetFirmwareInfo_MissingFile(t *testing.T) {
	p := &OpenWrtFirmwareProvider{FilePath: "/nonexistent/file"}
	_, err := p.GetFirmwareInfo()
	assert.Error(t, err)
}
