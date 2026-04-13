package comms

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/rs/zerolog"
)

func TestFindBS22Device_PrefersConnectedDevice(t *testing.T) {
	managed := bluezManagedObjectMap{
		dbus.ObjectPath("/org/bluez/hci0/dev_AA"): {
			bluezDeviceInterface: {
				"Alias":     dbus.MakeVariant("BS-22"),
				"Connected": dbus.MakeVariant(false),
				"Address":   dbus.MakeVariant("AA"),
			},
		},
		dbus.ObjectPath("/org/bluez/hci0/dev_BB"): {
			bluezDeviceInterface: {
				"Alias":     dbus.MakeVariant("BS-22"),
				"Connected": dbus.MakeVariant(true),
				"Address":   dbus.MakeVariant("BB"),
			},
		},
	}

	device, ok := findBS22Device(managed)
	if !ok {
		t.Fatal("findBS22Device() = false, want true")
	}

	if device.Path != dbus.ObjectPath("/org/bluez/hci0/dev_BB") {
		t.Fatalf("findBS22Device().Path = %q, want %q", device.Path, "/org/bluez/hci0/dev_BB")
	}
}

func TestFindBS22BLEBindingForDevice(t *testing.T) {
	device := bs22DeviceInfo{
		Path:      dbus.ObjectPath("/org/bluez/hci0/dev_41_42_86_99_1D_61"),
		Alias:     "BS-22",
		Address:   "41:42:86:99:1D:61",
		Connected: true,
	}

	managed := bluezManagedObjectMap{
		device.Path: {
			bluezDeviceInterface: {
				"Alias":     dbus.MakeVariant("BS-22"),
				"Connected": dbus.MakeVariant(true),
			},
		},
		dbus.ObjectPath("/org/bluez/hci0/dev_41_42_86_99_1D_61/service0011"): {
			bluezGattServiceInterface: {
				"UUID":   dbus.MakeVariant(bs22HMServiceUUID),
				"Device": dbus.MakeVariant(device.Path),
			},
		},
		dbus.ObjectPath("/org/bluez/hci0/dev_41_42_86_99_1D_61/service0011/char0012"): {
			bluezGattCharacteristicInterface: {
				"UUID":    dbus.MakeVariant(bs22HMWriteUUID),
				"Service": dbus.MakeVariant(dbus.ObjectPath("/org/bluez/hci0/dev_41_42_86_99_1D_61/service0011")),
			},
		},
		dbus.ObjectPath("/org/bluez/hci0/dev_41_42_86_99_1D_61/service0011/char0013"): {
			bluezGattCharacteristicInterface: {
				"UUID":    dbus.MakeVariant(bs22HMNotifyUUID),
				"Service": dbus.MakeVariant(dbus.ObjectPath("/org/bluez/hci0/dev_41_42_86_99_1D_61/service0011")),
			},
		},
	}

	binding, ok := findBS22BLEBindingForDevice(managed, device)
	if !ok {
		t.Fatal("findBS22BLEBindingForDevice() = false, want true")
	}

	if binding.WritePath != dbus.ObjectPath("/org/bluez/hci0/dev_41_42_86_99_1D_61/service0011/char0012") {
		t.Fatalf("binding.WritePath = %q", binding.WritePath)
	}
	if binding.NotifyPath != dbus.ObjectPath("/org/bluez/hci0/dev_41_42_86_99_1D_61/service0011/char0013") {
		t.Fatalf("binding.NotifyPath = %q", binding.NotifyPath)
	}
}

func TestParseBS22HMPacket(t *testing.T) {
	packet, ok := parseBS22HMPacket([]byte{0x10, 0x0A, 0x01, 0x01, 0x10, 0x11})
	if !ok {
		t.Fatal("parseBS22HMPacket() = false, want true")
	}

	if packet.VendorID != bs22HMVendorID {
		t.Fatalf("packet.VendorID = %#x, want %#x", packet.VendorID, bs22HMVendorID)
	}

	if packet.CommandCode() != bs22HMKeyEventInd {
		t.Fatalf("packet.CommandCode() = %#x, want %#x", packet.CommandCode(), bs22HMKeyEventInd)
	}

	if len(packet.Payload) != 2 || packet.Payload[0] != 0x10 || packet.Payload[1] != 0x11 {
		t.Fatalf("packet.Payload = %v, want [16 17]", packet.Payload)
	}
}

func TestBS22PTTEventFromPacket(t *testing.T) {
	tests := []struct {
		name   string
		packet bs22HMPacket
		want   PTTEvent
		ok     bool
	}{
		{
			name:   "ptt down",
			packet: bs22HMPacket{VendorID: bs22HMVendorID, Command: bs22HMKeyEventInd, Payload: []byte{16, 16}},
			want:   PTTDown,
			ok:     true,
		},
		{
			name:   "ptt up",
			packet: bs22HMPacket{VendorID: bs22HMVendorID, Command: bs22HMKeyEventInd, Payload: []byte{16, 17}},
			want:   PTTUp,
			ok:     true,
		},
		{
			name:   "next channel ignored",
			packet: bs22HMPacket{VendorID: bs22HMVendorID, Command: bs22HMKeyEventInd, Payload: []byte{19, 18}},
			ok:     false,
		},
		{
			name:   "wrong command ignored",
			packet: bs22HMPacket{VendorID: bs22HMVendorID, Command: bs22HMReadSettings, Payload: []byte{}},
			ok:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := bs22PTTEventFromPacket(tt.packet)
			if ok != tt.ok {
				t.Fatalf("bs22PTTEventFromPacket() ok = %v, want %v", ok, tt.ok)
			}

			if got != tt.want {
				t.Fatalf("bs22PTTEventFromPacket() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBS22HMCommandBytes(t *testing.T) {
	got := bs22HMCommandBytes(bs22HMVendorID, bs22HMSetBLEAudio, bs22HMSetBLEAudioEnabled)
	want := []byte{0x10, 0x0A, 0x00, 0x0F, 0x01}

	if len(got) != len(want) {
		t.Fatalf("len(bs22HMCommandBytes()) = %d, want %d", len(got), len(want))
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("bs22HMCommandBytes()[%d] = %#x, want %#x", i, got[i], want[i])
		}
	}
}

func TestBS22EventSource_PrimeBLE(t *testing.T) {
	var writes [][]byte
	src := &bs22EventSource{
		log: zerolog.Nop(),
		writeValue: func(_ *dbus.Conn, _ dbus.ObjectPath, data []byte) error {
			writes = append(writes, append([]byte(nil), data...))
			return nil
		},
	}

	err := src.primeBLE(nil, bs22BLEBinding{WritePath: dbus.ObjectPath("/org/bluez/hci0/dev_test/service/char")})
	if err != nil {
		t.Fatalf("primeBLE() error = %v", err)
	}

	want := [][]byte{
		{0x10, 0x0A, 0x00, 0x0D},
		{0x10, 0x0A, 0x00, 0x0F, 0x01},
	}

	if len(writes) != len(want) {
		t.Fatalf("len(writes) = %d, want %d", len(writes), len(want))
	}

	for i := range want {
		for j := range want[i] {
			if writes[i][j] != want[i][j] {
				t.Fatalf("writes[%d][%d] = %#x, want %#x", i, j, writes[i][j], want[i][j])
			}
		}
	}
}

func TestBS22EventSource_DedupesMergedEvents(t *testing.T) {
	src := &bs22EventSource{log: zerolog.Nop()}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	out := make(chan PTTEvent, 4)
	if !src.emitMergedEvent(ctx, out, PTTDown) {
		t.Fatal("emitMergedEvent(first) = false, want true")
	}
	if !src.emitMergedEvent(ctx, out, PTTDown) {
		t.Fatal("emitMergedEvent(second) = false, want true")
	}

	if len(out) != 1 {
		t.Fatalf("len(out) = %d, want 1", len(out))
	}
}

func TestBluezCharacteristicValue(t *testing.T) {
	sig := &dbus.Signal{
		Name: bluezPropertiesChangedSignal,
		Path: dbus.ObjectPath("/org/bluez/hci0/dev_test/service0011/char0013"),
		Body: []any{
			bluezGattCharacteristicInterface,
			map[string]dbus.Variant{
				"Value": dbus.MakeVariant([]byte{0x10, 0x0A, 0x01, 0x01, 0x10, 0x10}),
			},
		},
	}

	value, ok := bluezCharacteristicValue(sig, sig.Path)
	if !ok {
		t.Fatal("bluezCharacteristicValue() = false, want true")
	}

	if len(value) != 6 || value[4] != 0x10 || value[5] != 0x10 {
		t.Fatalf("value = %v, want key event payload", value)
	}
}

func TestNormalizeBlueZAddressType(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "random", in: "random", want: bluezAddressTypeRandom},
		{name: "random upper", in: "RANDOM", want: bluezAddressTypeRandom},
		{name: "public", in: "public", want: bluezAddressTypePublic},
		{name: "empty", in: "", want: bluezAddressTypePublic},
		{name: "other", in: "foo", want: bluezAddressTypePublic},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeBlueZAddressType(tt.in)
			if got != tt.want {
				t.Fatalf("normalizeBlueZAddressType(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestIsIgnorableBlueZConnectError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "already connected dbus", err: dbus.Error{Name: "org.bluez.Error.AlreadyConnected"}, want: true},
		{name: "in progress dbus", err: dbus.Error{Name: "org.bluez.Error.InProgress"}, want: true},
		{name: "already exists dbus", err: dbus.Error{Name: "org.bluez.Error.AlreadyExists"}, want: true},
		{name: "already connected text", err: errors.New("already connected"), want: true},
		{name: "in progress text", err: errors.New("operation in progress"), want: true},
		{name: "other", err: errors.New("permission denied"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isIgnorableBlueZConnectError(tt.err)
			if got != tt.want {
				t.Fatalf("isIgnorableBlueZConnectError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestIsBlueZUnsupportedMethodError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "unknown method dbus", err: dbus.Error{Name: "org.freedesktop.DBus.Error.UnknownMethod"}, want: true},
		{name: "unknown interface dbus", err: dbus.Error{Name: "org.freedesktop.DBus.Error.UnknownInterface"}, want: true},
		{name: "not supported dbus", err: dbus.Error{Name: "org.bluez.Error.NotSupported"}, want: true},
		{name: "unknown method text", err: errors.New("method doesn't exist"), want: true},
		{name: "unsupported text", err: errors.New("not supported"), want: true},
		{name: "other", err: errors.New("permission denied"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isBlueZUnsupportedMethodError(tt.err)
			if got != tt.want {
				t.Fatalf("isBlueZUnsupportedMethodError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
