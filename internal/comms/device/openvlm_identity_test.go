package device_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openmanet/openmanetd/internal/comms/device"
)

// stubReader returns the fixed report/err pair from a HIDInputReader,
// regardless of the path it is invoked with.
func stubReader(report []byte, err error) device.HIDInputReader {
	return func(_ string) ([]byte, error) {
		return report, err
	}
}

func TestCheckOpenVLMIdentity_StrapHigh(t *testing.T) {
	// [ReportID=0, IR0=0, IR1=0x01 (GPIO1 high), IR2=0, IR3=0]
	report := []byte{0x00, 0x00, 0x01, 0x00, 0x00}
	d := device.CM108Descriptor{HIDPath: "/dev/hidraw0"}

	ok, err := device.CheckOpenVLMIdentity(d, stubReader(report, nil))
	require.NoError(t, err)
	assert.True(t, ok, "OpenVLM strap should read as high")
}

func TestCheckOpenVLMIdentity_StrapLow(t *testing.T) {
	report := []byte{0x00, 0x00, 0x00, 0x00, 0x00}
	d := device.CM108Descriptor{HIDPath: "/dev/hidraw0"}

	ok, err := device.CheckOpenVLMIdentity(d, stubReader(report, nil))
	require.NoError(t, err)
	assert.False(t, ok, "generic CM108 (no strap) should read as low")
}

func TestCheckOpenVLMIdentity_OtherGPIOsIgnored(t *testing.T) {
	// IR1 = 0x0E → GPIO4..GPIO2 high, GPIO1 low. Must return false.
	report := []byte{0x00, 0x00, 0x0E, 0x00, 0x00}
	d := device.CM108Descriptor{HIDPath: "/dev/hidraw0"}

	ok, err := device.CheckOpenVLMIdentity(d, stubReader(report, nil))
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestCheckOpenVLMIdentity_IR0NotGPIOMode(t *testing.T) {
	// IR0[7]=1 means IR1 is mapped from EEPROM_DATA0, not GPIO.
	report := []byte{0x00, 0x80, 0x01, 0x00, 0x00}
	d := device.CM108Descriptor{HIDPath: "/dev/hidraw0"}

	ok, err := device.CheckOpenVLMIdentity(d, stubReader(report, nil))
	require.Error(t, err)
	assert.False(t, ok)
	assert.Contains(t, err.Error(), "GPIO-input mode")
}

func TestCheckOpenVLMIdentity_EmptyHIDPath(t *testing.T) {
	d := device.CM108Descriptor{HIDPath: ""}

	ok, err := device.CheckOpenVLMIdentity(d, stubReader([]byte{0, 0, 1}, nil))
	require.Error(t, err)
	assert.False(t, ok)
	assert.Contains(t, err.Error(), "HID path")
}

func TestCheckOpenVLMIdentity_ShortReport(t *testing.T) {
	d := device.CM108Descriptor{HIDPath: "/dev/hidraw0"}

	ok, err := device.CheckOpenVLMIdentity(d, stubReader([]byte{0x00, 0x00}, nil))
	require.Error(t, err)
	assert.False(t, ok)
	assert.Contains(t, err.Error(), "too short")
}

func TestCheckOpenVLMIdentity_ReaderError(t *testing.T) {
	sentinel := errors.New("fake hidraw failure")
	d := device.CM108Descriptor{HIDPath: "/dev/hidraw0"}

	ok, err := device.CheckOpenVLMIdentity(d, stubReader(nil, sentinel))
	require.Error(t, err)
	assert.False(t, ok)
	assert.True(t, errors.Is(err, sentinel), "reader error must be wrapped with %%w")
}

func TestCheckOpenVLMIdentity_NilReaderDefaults(t *testing.T) {
	// Nil reader falls back to the real hidraw ioctl. Against a non-
	// existent path we should get the default reader's open error,
	// wrapped into the CheckOpenVLMIdentity prefix. This proves the
	// default path is invoked without requiring real hardware.
	d := device.CM108Descriptor{HIDPath: "/dev/does-not-exist-openvlm-test"}

	ok, err := device.CheckOpenVLMIdentity(d, nil)
	require.Error(t, err)
	assert.False(t, ok)
	assert.Contains(t, err.Error(), "probe OpenVLM identity")
}
