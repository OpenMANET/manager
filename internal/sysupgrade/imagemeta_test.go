package sysupgrade

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildFWx0Image returns the byte sequence that fwtool produces for an
// image with `payload` core data and `metaJSON` as the INFO chunk
// payload. When `signaturePayload` is non-nil, a trailing SIGNATURE
// chunk is appended after the INFO chunk to exercise the multi-chunk
// walk path.
func buildFWx0Image(t *testing.T, payload, metaJSON, signaturePayload []byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	buf.Write(payload)

	// INFO chunk: fwimage_header(8) || metaJSON || trailer(16).
	header := [fwimageHeaderLen]byte{} // version=0, flags=0
	buf.Write(header[:])
	buf.Write(metaJSON)

	infoChunkSize := uint32(fwimageHeaderLen + len(metaJSON) + fwimageTrailerLen)

	var infoTrailer [fwimageTrailerLen]byte
	binary.BigEndian.PutUint32(infoTrailer[0:4], fwimageMagic)
	binary.BigEndian.PutUint32(infoTrailer[4:8], 0) // crc32 — parser ignores it
	infoTrailer[8] = fwimageTypeInfo
	binary.BigEndian.PutUint32(infoTrailer[12:16], infoChunkSize)
	buf.Write(infoTrailer[:])

	if signaturePayload != nil {
		// SIGNATURE chunk: signature data || trailer(16). No header.
		buf.Write(signaturePayload)

		sigChunkSize := uint32(len(signaturePayload) + fwimageTrailerLen)

		var sigTrailer [fwimageTrailerLen]byte
		binary.BigEndian.PutUint32(sigTrailer[0:4], fwimageMagic)
		binary.BigEndian.PutUint32(sigTrailer[4:8], 0)
		sigTrailer[8] = fwimageTypeSig
		binary.BigEndian.PutUint32(sigTrailer[12:16], sigChunkSize)
		buf.Write(sigTrailer[:])
	}

	return buf.Bytes()
}

func writeTempImage(t *testing.T, contents []byte) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "image.bin")

	require.NoError(t, os.WriteFile(path, contents, 0o600))

	return path
}

func TestParseImageMetadata_HappyPath(t *testing.T) {
	meta := []byte(`{
        "metadata_version":"1.1",
        "compat_version":"1.0",
        "supported_devices":["raspberrypi,4-model-b","brcm,bcm2711"],
        "version":{"dist":"OpenWrt","version":"24.10.0","target":"bcm27xx/bcm2711"}
    }`)

	path := writeTempImage(t, buildFWx0Image(t, []byte("CORE FIRMWARE BYTES"), meta, nil))

	got, err := ParseImageMetadata(path)
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, "1.1", got.MetadataVersion)
	assert.Equal(t, "1.0", got.CompatVersion)
	assert.Equal(t, []string{"raspberrypi,4-model-b", "brcm,bcm2711"}, got.SupportedDevices)
	assert.Equal(t, "bcm27xx/bcm2711", got.Version.Target)
}

func TestParseImageMetadata_WithSignatureChunk(t *testing.T) {
	meta := []byte(`{"compat_version":"1.0","supported_devices":["raspberrypi,4-model-b"]}`)
	path := writeTempImage(t, buildFWx0Image(t, []byte("CORE"), meta, []byte("ucert-signature-bytes-here")))

	got, err := ParseImageMetadata(path)
	require.NoError(t, err)

	assert.Equal(t, []string{"raspberrypi,4-model-b"}, got.SupportedDevices)
}

func TestParseImageMetadata_NoTrailer(t *testing.T) {
	path := writeTempImage(t, []byte("just a plain firmware blob with no FWx0 trailer"))

	_, err := ParseImageMetadata(path)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoImageMetadata))
}

func TestParseImageMetadata_TooSmall(t *testing.T) {
	// File shorter than a single trailer can possibly fit.
	path := writeTempImage(t, []byte{0x01, 0x02, 0x03})

	_, err := ParseImageMetadata(path)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoImageMetadata))
}

func TestParseImageMetadata_MalformedSize(t *testing.T) {
	meta := []byte(`{"compat_version":"1.0","supported_devices":["x"]}`)
	body := buildFWx0Image(t, []byte("CORE"), meta, nil)

	// Corrupt the size field of the INFO trailer to claim it's larger
	// than the whole file. Trailer is the last 16 bytes; size field
	// is at offset len-4.
	binary.BigEndian.PutUint32(body[len(body)-4:], 1<<30)

	path := writeTempImage(t, body)

	_, err := ParseImageMetadata(path)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrNoImageMetadata)
}

func TestImageMetadata_EffectiveSupportedDevices_Default(t *testing.T) {
	m := &ImageMetadata{
		CompatVersion:    "1.0",
		SupportedDevices: []string{"a", "b"},
		// new_supported_devices is ignored when compat_version == "1.0"
		NewSupportedDevices: []string{"poison"},
	}
	assert.Equal(t, []string{"a", "b"}, m.EffectiveSupportedDevices())
}

func TestImageMetadata_EffectiveSupportedDevices_BumpedCompat(t *testing.T) {
	// When compat_version is bumped, supported_devices carries a poison
	// pill string for old fwtool readers and the real list moves to
	// new_supported_devices. Mirror that.
	m := &ImageMetadata{
		CompatVersion:       "2.0",
		SupportedDevices:    []string{"DO NOT USE — old fwtool"},
		NewSupportedDevices: []string{"raspberrypi,4-model-b"},
	}
	assert.Equal(t, []string{"raspberrypi,4-model-b"}, m.EffectiveSupportedDevices())
}

func TestImageMetadata_EffectiveSupportedDevices_BumpedNoNew(t *testing.T) {
	// Defensive: if compat_version was bumped but new_supported_devices
	// is empty (malformed image), fall back to supported_devices so
	// callers still see something.
	m := &ImageMetadata{
		CompatVersion:    "2.0",
		SupportedDevices: []string{"only-list"},
	}
	assert.Equal(t, []string{"only-list"}, m.EffectiveSupportedDevices())
}

func TestImageMetadata_MatchesDevice(t *testing.T) {
	m := &ImageMetadata{
		CompatVersion:    "1.0",
		SupportedDevices: []string{"raspberrypi,4-model-b", "brcm,bcm2711"},
	}

	assert.True(t, m.MatchesDevice("raspberrypi,4-model-b"))
	assert.True(t, m.MatchesDevice("brcm,bcm2711"))
	assert.False(t, m.MatchesDevice("other,vendor-board"))
	// Comparison is byte-exact: no case folding, no substring.
	assert.False(t, m.MatchesDevice("RASPBERRYPI,4-MODEL-B"))
	assert.False(t, m.MatchesDevice("raspberrypi"))
	// Empty board name yields no match.
	assert.False(t, m.MatchesDevice(""))
	// Nil receiver is safe and yields no match.
	var nilMeta *ImageMetadata
	assert.False(t, nilMeta.MatchesDevice("raspberrypi,4-model-b"))
}

func TestStoreStagedImage_PopulatesMetadataFields(t *testing.T) {
	// makeManager wires a board provider whose Model.ID is
	// "bcm2711,mm8108-usb"; an image whose supported_devices contains
	// that string is reported compatible.
	meta := []byte(`{"compat_version":"1.0","supported_devices":["bcm2711,mm8108-usb","brcm,bcm2711"]}`)
	body := buildFWx0Image(t, []byte("CORE FIRMWARE"), meta, nil)

	mgr := makeManager(t, &fakeReleasesFetcher{}, &fakeRunner{}, "1.7.0")
	staged, err := mgr.StoreStagedImage(t.Context(), bytes.NewReader(body), "openmanet.img")
	require.NoError(t, err)
	require.NotNil(t, staged)

	assert.True(t, staged.MetadataPresent)
	assert.Equal(t, "1.0", staged.CompatVersion)
	assert.Equal(t, []string{"bcm2711,mm8108-usb", "brcm,bcm2711"}, staged.SupportedDevices)
	assert.Equal(t, "bcm2711,mm8108-usb", staged.DeviceCompat)
	assert.True(t, staged.ImageCompatible)
}

func TestStoreStagedImage_MetadataMissing_LeavesCompatFalse(t *testing.T) {
	// Plain payload — no FWx0 trailer. Compat fields stay zeroed but
	// DeviceCompat is still populated from the board provider.
	mgr := makeManager(t, &fakeReleasesFetcher{}, &fakeRunner{}, "1.7.0")
	staged, err := mgr.StoreStagedImage(t.Context(), bytes.NewReader([]byte("plain")), "x.img")
	require.NoError(t, err)
	require.NotNil(t, staged)

	assert.False(t, staged.MetadataPresent)
	assert.Empty(t, staged.SupportedDevices)
	assert.Equal(t, "bcm2711,mm8108-usb", staged.DeviceCompat)
	assert.False(t, staged.ImageCompatible)
}

func TestStoreStagedImage_MetadataPresent_DeviceMismatch(t *testing.T) {
	// Image declares support for a different device — should mark
	// MetadataPresent=true, ImageCompatible=false.
	meta := []byte(`{"compat_version":"1.0","supported_devices":["other,vendor-board"]}`)
	body := buildFWx0Image(t, []byte("CORE"), meta, nil)

	mgr := makeManager(t, &fakeReleasesFetcher{}, &fakeRunner{}, "1.7.0")
	staged, err := mgr.StoreStagedImage(t.Context(), bytes.NewReader(body), "wrong.img")
	require.NoError(t, err)

	assert.True(t, staged.MetadataPresent)
	assert.False(t, staged.ImageCompatible)
	assert.Equal(t, []string{"other,vendor-board"}, staged.SupportedDevices)
}
