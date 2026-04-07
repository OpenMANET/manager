package device_test

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openmanet/openmanetd/internal/comms/device"
)

// mkFS constructs a fake /sys layout for a single USB device under
// bus/usb/devices/<name>. vendor/product are hex strings like "0d8c". Any of
// the optional parameters may be empty to omit the corresponding subtree.
func mkFS(name, vendor, product, serial, iface, hidraw, alsaCard string) fstest.MapFS {
	fsys := fstest.MapFS{}
	base := "bus/usb/devices/" + name
	if vendor != "" {
		fsys[base+"/idVendor"] = &fstest.MapFile{Data: []byte(vendor + "\n")}
	}
	if product != "" {
		fsys[base+"/idProduct"] = &fstest.MapFile{Data: []byte(product + "\n")}
	}
	if serial != "" {
		fsys[base+"/serial"] = &fstest.MapFile{Data: []byte(serial + "\n")}
	}
	if iface != "" && hidraw != "" {
		fsys[base+"/"+iface+"/0003:0D8C:0012.0001/hidraw/"+hidraw+"/dev"] =
			&fstest.MapFile{Data: []byte("180:0\n")}
	}
	if iface != "" && alsaCard != "" {
		fsys[base+"/"+iface+"/sound/"+alsaCard+"/id"] =
			&fstest.MapFile{Data: []byte("OpenVLM\n")}
	}
	return fsys
}

func TestDiscoverCM108_HappyPath(t *testing.T) {
	fsys := mkFS("1-1", "0d8c", "0012", "ABC123", "1-1:1.3", "hidraw0", "card2")

	descs, err := device.DiscoverCM108(fsys, func(card int) (int, bool) {
		assert.Equal(t, 2, card)
		return 7, true
	})
	require.NoError(t, err)
	require.Len(t, descs, 1)

	d := descs[0]
	assert.Equal(t, "/dev/hidraw0", d.HIDPath)
	assert.Equal(t, 2, d.ALSACardIdx)
	assert.Equal(t, 7, d.PADeviceIdx)
	assert.Equal(t, uint16(0x0D8C), d.VID)
	assert.Equal(t, uint16(0x0012), d.PID)
	assert.Equal(t, "ABC123", d.Serial)
}

func TestDiscoverCM108_NonMatchingVendorSkipped(t *testing.T) {
	fsys := mkFS("1-2", "1234", "5678", "", "1-2:1.0", "hidraw9", "card9")

	descs, err := device.DiscoverCM108(fsys, nil)
	require.NoError(t, err)
	assert.Empty(t, descs)
}

func TestDiscoverCM108_NoHIDChild(t *testing.T) {
	// CM108-family device with no hidraw / no sound children — should still
	// return a descriptor with empty HIDPath and ALSACardIdx=-1.
	fsys := mkFS("1-3", "0d8c", "0012", "", "", "", "")

	descs, err := device.DiscoverCM108(fsys, nil)
	require.NoError(t, err)
	require.Len(t, descs, 1)
	assert.Empty(t, descs[0].HIDPath)
	assert.Equal(t, -1, descs[0].ALSACardIdx)
	assert.Equal(t, -1, descs[0].PADeviceIdx)
}

func TestDiscoverCM108_MultipleDevices(t *testing.T) {
	fsys := fstest.MapFS{}
	for k, v := range mkFS("1-1", "0d8c", "0012", "AAA", "1-1:1.3", "hidraw0", "card2") {
		fsys[k] = v
	}
	for k, v := range mkFS("2-1", "0d8c", "013c", "BBB", "2-1:1.3", "hidraw1", "card3") {
		fsys[k] = v
	}
	// A non-matching sibling.
	for k, v := range mkFS("3-1", "1d6b", "0002", "", "", "", "") {
		fsys[k] = v
	}

	descs, err := device.DiscoverCM108(fsys, func(card int) (int, bool) {
		return card + 10, true
	})
	require.NoError(t, err)
	require.Len(t, descs, 2)

	bySerial := map[string]device.CM108Descriptor{}
	for _, d := range descs {
		bySerial[d.Serial] = d
	}
	assert.Equal(t, "/dev/hidraw0", bySerial["AAA"].HIDPath)
	assert.Equal(t, 12, bySerial["AAA"].PADeviceIdx)
	assert.Equal(t, "/dev/hidraw1", bySerial["BBB"].HIDPath)
	assert.Equal(t, uint16(0x013C), bySerial["BBB"].PID)
	assert.Equal(t, 13, bySerial["BBB"].PADeviceIdx)
}

func TestDiscoverCM108_EmptyFS(t *testing.T) {
	descs, err := device.DiscoverCM108(fstest.MapFS{}, nil)
	require.NoError(t, err)
	assert.Empty(t, descs)
}

func TestDiscoverCM108_MalformedIDVendor(t *testing.T) {
	fsys := mkFS("1-1", "nothex", "0012", "", "1-1:1.3", "hidraw0", "card2")
	// A second, valid device should still be discovered despite the first
	// being malformed.
	for k, v := range mkFS("2-1", "0d8c", "0012", "OK", "2-1:1.3", "hidraw1", "card3") {
		fsys[k] = v
	}

	descs, err := device.DiscoverCM108(fsys, nil)
	require.NoError(t, err)
	require.Len(t, descs, 1)
	assert.Equal(t, "OK", descs[0].Serial)
}

func TestDiscoverCM108_InterfaceDirsSkipped(t *testing.T) {
	// An entry whose name contains ':' is an interface dir and must be
	// skipped by the top-level walk.
	fsys := fstest.MapFS{
		"bus/usb/devices/1-1:1.0/idVendor": &fstest.MapFile{Data: []byte("0d8c\n")},
		"bus/usb/devices/1-1:1.0/idProduct": &fstest.MapFile{Data: []byte("0012\n")},
	}
	descs, err := device.DiscoverCM108(fsys, nil)
	require.NoError(t, err)
	assert.Empty(t, descs)
}

func TestCache_LazyAndInvalidate(t *testing.T) {
	calls := 0
	fsys := mkFS("1-1", "0d8c", "0012", "X", "1-1:1.3", "hidraw0", "card2")
	paLookup := func(card int) (int, bool) {
		calls++
		return 0, true
	}

	c := device.NewCache(fsys, paLookup)
	d1, err := c.Descriptors()
	require.NoError(t, err)
	require.Len(t, d1, 1)
	assert.Equal(t, 1, calls)

	// Second call must not re-walk.
	_, err = c.Descriptors()
	require.NoError(t, err)
	assert.Equal(t, 1, calls)

	c.Invalidate()
	_, err = c.Descriptors()
	require.NoError(t, err)
	assert.Equal(t, 2, calls)
}
