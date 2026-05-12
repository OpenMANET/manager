package sysupgrade

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatchAsset_ManifestPriority(t *testing.T) {
	manifest := &Manifest{Boards: map[string]string{
		"bcm2711,mm8108-usb": "openmanet-1.8.0-bcm27xx-bcm2711-rpi-4-squashfs-sysupgrade.img.gz",
	}}

	assets := []Asset{
		{Name: "openmanet-1.8.0-bcm27xx-bcm2711-rpi-4-squashfs-sysupgrade.img.gz", DownloadURL: "u1"},
		{Name: "openmanet-1.8.0-ipq40xx-generic-gw7100-squashfs-sysupgrade.bin", DownloadURL: "u2"},
		{Name: "manifest.json", DownloadURL: "u3"},
	}

	got, err := MatchAsset("bcm2711,mm8108-usb", "bcm27xx/bcm2711", manifest, assets)
	require.NoError(t, err)
	assert.Equal(t, assets[0].Name, got.Name)
}

func TestMatchAsset_ManifestPointsAtMissingAsset(t *testing.T) {
	manifest := &Manifest{Boards: map[string]string{
		"bcm2711,mm8108-usb": "missing.img.gz",
	}}

	assets := []Asset{
		{Name: "openmanet-1.8.0-bcm27xx-bcm2711-mm8108-usb-squashfs-sysupgrade.img.gz"},
	}

	_, err := MatchAsset("bcm2711,mm8108-usb", "bcm27xx/bcm2711", manifest, assets)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoMatchingAsset))
}

func TestMatchAsset_HeuristicHit(t *testing.T) {
	assets := []Asset{
		{Name: "openmanet-1.8.0-bcm27xx-bcm2711-mm8108-usb-squashfs-sysupgrade.img.gz", DownloadURL: "u1"},
		{Name: "openmanet-1.8.0-ipq40xx-generic-gw7100-squashfs-sysupgrade.bin", DownloadURL: "u2"},
	}

	got, err := MatchAsset("bcm2711,mm8108-usb", "bcm27xx/bcm2711", nil, assets)
	require.NoError(t, err)
	assert.Equal(t, assets[0].Name, got.Name)
}

func TestMatchAsset_HeuristicNoMatch(t *testing.T) {
	assets := []Asset{
		{Name: "openmanet-1.8.0-x86-64-generic-squashfs-sysupgrade.bin"},
	}

	_, err := MatchAsset("bcm2711,mm8108-usb", "bcm27xx/bcm2711", nil, assets)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoMatchingAsset))
}

func TestMatchAsset_HeuristicAmbiguous(t *testing.T) {
	assets := []Asset{
		{Name: "openmanet-1.8.0-bcm27xx-bcm2711-mm8108-usb-squashfs-sysupgrade.img.gz"},
		{Name: "openmanet-1.8.0-bcm27xx-bcm2711-mm8108-usb-squashfs-sysupgrade.bin"},
	}

	_, err := MatchAsset("bcm2711,mm8108-usb", "bcm27xx/bcm2711", nil, assets)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAmbiguousAsset))
}

func TestMatchAsset_RejectNonSysupgrade(t *testing.T) {
	assets := []Asset{
		{Name: "openmanet-1.8.0-bcm27xx-bcm2711-mm8108-usb-squashfs-factory.bin"},
		{Name: "openmanet-1.8.0-bcm27xx-bcm2711-mm8108-usb-rootfs.tar.gz"},
	}

	_, err := MatchAsset("bcm2711,mm8108-usb", "bcm27xx/bcm2711", nil, assets)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoMatchingAsset))
}

func TestTokenizeBoardName(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{input: "bcm2711,mm8108-usb", want: []string{"bcm2711", "mm8108", "usb"}},
		{input: "gateworks,imx8mm-gw71xx-2x", want: []string{"gateworks", "imx8mm", "gw71xx", "2x"}},
		{input: "", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := tokenizeBoardName(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}
