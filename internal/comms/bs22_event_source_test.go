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
				"Alias":       dbus.MakeVariant("BS-22"),
				"Connected":   dbus.MakeVariant(false),
				"Paired":      dbus.MakeVariant(false),
				"Address":     dbus.MakeVariant("AA"),
				"AddressType": dbus.MakeVariant("public"),
				"Adapter":     dbus.MakeVariant(dbus.ObjectPath("/org/bluez/hci0")),
			},
		},
		dbus.ObjectPath("/org/bluez/hci0/dev_BB"): {
			bluezDeviceInterface: {
				"Alias":       dbus.MakeVariant("BS-22"),
				"Connected":   dbus.MakeVariant(true),
				"Paired":      dbus.MakeVariant(true),
				"Address":     dbus.MakeVariant("BB"),
				"AddressType": dbus.MakeVariant("random"),
				"Adapter":     dbus.MakeVariant(dbus.ObjectPath("/org/bluez/hci0")),
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
	if device.Adapter != dbus.ObjectPath("/org/bluez/hci0") {
		t.Fatalf("findBS22Device().Adapter = %q, want %q", device.Adapter, "/org/bluez/hci0")
	}
	if device.AddressType != "random" {
		t.Fatalf("findBS22Device().AddressType = %q, want %q", device.AddressType, "random")
	}
	if !device.Paired {
		t.Fatal("findBS22Device().Paired = false, want true")
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

func TestBS22LEConnectProperties(t *testing.T) {
	props := bs22LEConnectProperties(bs22DeviceInfo{
		Address:     "41:42:86:99:1D:61",
		AddressType: "PUBLIC",
	})

	if got := props["Address"].Value().(string); got != "41:42:86:99:1D:61" {
		t.Fatalf("Address = %q", got)
	}
	if got := props["AddressType"].Value().(string); got != "public" {
		t.Fatalf("AddressType = %q, want public", got)
	}
}

func TestNormalizeBS22AddressType(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "", want: "public"},
		{in: "public", want: "public"},
		{in: "PUBLIC", want: "public"},
		{in: "random", want: "random"},
		{in: "RANDOM", want: "random"},
		{in: "other", want: "public"},
	}

	for _, tt := range tests {
		if got := normalizeBS22AddressType(tt.in); got != tt.want {
			t.Fatalf("normalizeBS22AddressType(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestIsIgnorableBlueZPairError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "already exists", err: dbus.MakeFailedError(&dbus.Error{Name: "org.bluez.Error.AlreadyExists"}), want: true},
		{name: "already paired text", err: errors.New("already paired"), want: true},
		{name: "in progress", err: errors.New("operation in progress"), want: true},
		{name: "other", err: errors.New("failed"), want: false},
	}

	for _, tt := range tests {
		if got := isIgnorableBlueZPairError(tt.err); got != tt.want {
			t.Fatalf("%s: isIgnorableBlueZPairError() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestIsMissingBlueZMethodError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "doesn't exist", err: errors.New(`Method "ConnectDevice" with signature "a{sv}" on interface "org.bluez.Adapter1" doesn't exist`) , want: true},
		{name: "does not exist", err: errors.New("method does not exist"), want: true},
		{name: "unknown method", err: errors.New("Unknown method"), want: true},
		{name: "other", err: errors.New("failed"), want: false},
	}

	for _, tt := range tests {
		if got := isMissingBlueZMethodError(tt.err); got != tt.want {
			t.Fatalf("%s: isMissingBlueZMethodError() = %v, want %v", tt.name, got, tt.want)
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

func TestBS22EventSource_PlayToneUsesPrimedBinding(t *testing.T) {
	var writes [][]byte
	src := &bs22EventSource{
		log: zerolog.Nop(),
		writeValue: func(_ *dbus.Conn, path dbus.ObjectPath, data []byte) error {
			if path != dbus.ObjectPath("/org/bluez/hci0/dev_test/service/char") {
				t.Fatalf("write path = %q", path)
			}
			writes = append(writes, append([]byte(nil), data...))
			return nil
		},
	}

	src.setToneState(&dbus.Conn{}, bs22BLEBinding{
		WritePath: dbus.ObjectPath("/org/bluez/hci0/dev_test/service/char"),
	}, true)

	if !src.PlayStartTone() {
		t.Fatal("PlayStartTone() = false, want true")
	}
	if !src.PlayStopTone() {
		t.Fatal("PlayStopTone() = false, want true")
	}

	want := [][]byte{
		{0x10, 0x0A, 0x00, 0x0A, 0x00, 0x01},
		{0x10, 0x0A, 0x00, 0x0A, 0x00, 0x02},
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

func TestBS22EventSource_PlayToneRequiresPrimedBinding(t *testing.T) {
	src := &bs22EventSource{log: zerolog.Nop()}
	src.setToneState(&dbus.Conn{}, bs22BLEBinding{
		WritePath: dbus.ObjectPath("/org/bluez/hci0/dev_test/service/char"),
	}, false)

	if src.PlayStartTone() {
		t.Fatal("PlayStartTone() = true, want false when not primed")
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
