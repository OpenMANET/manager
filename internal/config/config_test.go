package config

import (
	"testing"

	"github.com/spf13/viper"
)

// Helper functions for test cases
func boolPtr(b bool) *bool {
	return &b
}

func intPtr(i int) *int {
	return &i
}

func strPtr(s string) *string {
	return &s
}

func float64Ptr(f float64) *float64 {
	return &f
}

func TestGetMeshNetInterface(t *testing.T) {
	tests := []struct {
		name     string
		setValue *string
		want     string
	}{
		{
			name:     "returns configured value",
			setValue: strPtr("wlan0"),
			want:     "wlan0",
		},
		{
			name:     "returns default when empty",
			setValue: strPtr(""),
			want:     DefaultMeshNetInterface,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("meshNetInterface", *tt.setValue)
			}

			cfg := New(v)

			got := cfg.GetMeshNetInterface()
			if got != tt.want {
				t.Errorf("GetMeshNetInterface() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetAlfredMode(t *testing.T) {
	tests := []struct {
		name     string
		setValue *string
		want     string
	}{
		{
			name:     "returns primary mode",
			setValue: strPtr("primary"),
			want:     "primary",
		},
		{
			name:     "returns secondary mode",
			setValue: strPtr("secondary"),
			want:     "secondary",
		},
		{
			name:     "returns default when empty",
			setValue: strPtr(""),
			want:     DefaultAlfredMode,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("alfred.mode", *tt.setValue)
			}

			cfg := New(v)

			got := cfg.GetAlfredMode()
			if got != tt.want {
				t.Errorf("GetAlfredMode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetCommsProtocol(t *testing.T) {
	tests := []struct {
		name     string
		setValue *string
		want     string
	}{
		{
			name:     "returns configured protocol",
			setValue: strPtr("rtp"),
			want:     "rtp",
		},
		{
			name:     "returns default when not set",
			setValue: nil,
			want:     DefaultCommsProtocol,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("comms.protocol", *tt.setValue)
			}

			cfg := New(v)

			got := cfg.GetCommsProtocol()
			if got != tt.want {
				t.Errorf("GetCommsProtocol() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetCommsEnable(t *testing.T) {
	tests := []struct {
		setValue *bool
		name     string
		want     bool
	}{
		{
			name:     "returns true when enabled",
			setValue: boolPtr(true),
			want:     true,
		},
		{
			name:     "returns false when disabled",
			setValue: boolPtr(false),
			want:     false,
		},
		{
			name:     "returns default when not set",
			setValue: nil,
			want:     DefaultCommsEnable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("comms.enable", *tt.setValue)
			}

			cfg := New(v)

			got := cfg.GetCommsEnable()
			if got != tt.want {
				t.Errorf("GetCommsEnable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetAlfredDataTypeGateway(t *testing.T) {
	tests := []struct {
		setValue *bool
		name     string
		want     bool
	}{
		{
			name:     "returns true when enabled",
			setValue: boolPtr(true),
			want:     true,
		},
		{
			name:     "returns false when disabled",
			setValue: boolPtr(false),
			want:     false,
		},
		{
			name:     "returns default when not set",
			setValue: nil,
			want:     DefaultAlfredDataTypeGateway,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("alfred.dataTypes.gateway", *tt.setValue)
			}

			cfg := New(v)

			got := cfg.GetAlfredDataTypeGateway()
			if got != tt.want {
				t.Errorf("GetAlfredDataTypeGateway() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetAlfredSocketPath(t *testing.T) {
	tests := []struct {
		name     string
		setValue *string
		want     string
	}{
		{
			name:     "returns configured path",
			setValue: strPtr("/custom/path/alfred.sock"),
			want:     "/custom/path/alfred.sock",
		},
		{
			name:     "returns default when empty",
			setValue: strPtr(""),
			want:     DefaultAlfredSocketPath,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("alfred.socketPath", *tt.setValue)
			}

			cfg := New(v)

			got := cfg.GetAlfredSocketPath()
			if got != tt.want {
				t.Errorf("GetAlfredSocketPath() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetDBFile(t *testing.T) {
	tests := []struct {
		name     string
		setValue *string
		want     string
	}{
		{
			name:     "returns configured path",
			setValue: strPtr("/custom/db/openmanetd.db"),
			want:     "/custom/db/openmanetd.db",
		},
		{
			name:     "returns default when empty",
			setValue: strPtr(""),
			want:     DefaultDBFile,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("dbFile", *tt.setValue)
			}

			cfg := New(v)

			got := cfg.GetDBFile()
			if got != tt.want {
				t.Errorf("GetDBFile() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetResetDBOnStart(t *testing.T) {
	tests := []struct {
		setValue *bool
		name     string
		want     bool
	}{
		{
			name:     "returns true when enabled",
			setValue: boolPtr(true),
			want:     true,
		},
		{
			name:     "returns false when disabled",
			setValue: boolPtr(false),
			want:     false,
		},
		{
			name:     "returns default when not set",
			setValue: nil,
			want:     DefaultResetDBOnStart,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("resetDBOnStart", *tt.setValue)
			}

			cfg := New(v)

			got := cfg.GetResetDBOnStart()
			if got != tt.want {
				t.Errorf("GetResetDBOnStart() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetAlfredBatInterface(t *testing.T) {
	tests := []struct {
		name     string
		setValue *string
		want     string
	}{
		{
			name:     "returns configured interface",
			setValue: strPtr("bat1"),
			want:     "bat1",
		},
		{
			name:     "returns default when empty",
			setValue: strPtr(""),
			want:     DefaultAlfredBatInterface,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("alfred.batInterface", *tt.setValue)
			}

			cfg := New(v)

			got := cfg.GetAlfredBatInterface()
			if got != tt.want {
				t.Errorf("GetAlfredBatInterface() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetAlfredDataTypeNode(t *testing.T) {
	tests := []struct {
		setValue *bool
		name     string
		want     bool
	}{
		{
			name:     "returns true when enabled",
			setValue: boolPtr(true),
			want:     true,
		},
		{
			name:     "returns false when disabled",
			setValue: boolPtr(false),
			want:     false,
		},
		{
			name:     "returns default when not set",
			setValue: nil,
			want:     DefaultAlfredDataTypeNode,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("alfred.dataTypes.node", *tt.setValue)
			}

			cfg := New(v)

			got := cfg.GetAlfredDataTypeNode()
			if got != tt.want {
				t.Errorf("GetAlfredDataTypeNode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetAlfredDataTypePosition(t *testing.T) {
	tests := []struct {
		setValue *bool
		name     string
		want     bool
	}{
		{
			name:     "returns true when enabled",
			setValue: boolPtr(true),
			want:     true,
		},
		{
			name:     "returns false when disabled",
			setValue: boolPtr(false),
			want:     false,
		},
		{
			name:     "returns default when not set",
			setValue: nil,
			want:     DefaultAlfredDataTypePosition,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("alfred.dataTypes.position", *tt.setValue)
			}

			cfg := New(v)

			got := cfg.GetAlfredDataTypePosition()
			if got != tt.want {
				t.Errorf("GetAlfredDataTypePosition() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetAlfredDataTypeAddressReservation(t *testing.T) {
	tests := []struct {
		setValue *bool
		name     string
		want     bool
	}{
		{
			name:     "returns true when enabled",
			setValue: boolPtr(true),
			want:     true,
		},
		{
			name:     "returns false when disabled",
			setValue: boolPtr(false),
			want:     false,
		},
		{
			name:     "returns default when not set",
			setValue: nil,
			want:     DefaultAlfredDataTypeAddressReserv,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("alfred.dataTypes.addressReservation", *tt.setValue)
			}

			cfg := New(v)

			got := cfg.GetAlfredDataTypeAddressReservation()
			if got != tt.want {
				t.Errorf("GetAlfredDataTypeAddressReservation() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetCommsDebug(t *testing.T) {
	tests := []struct {
		setValue *bool
		name     string
		want     bool
	}{
		{
			name:     "returns true when enabled",
			setValue: boolPtr(true),
			want:     true,
		},
		{
			name:     "returns false when disabled",
			setValue: boolPtr(false),
			want:     false,
		},
		{
			name:     "returns default when not set",
			setValue: nil,
			want:     DefaultCommsDebug,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("comms.debug", *tt.setValue)
			}

			cfg := New(v)

			got := cfg.GetCommsDebug()
			if got != tt.want {
				t.Errorf("GetCommsDebug() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetCommsLoopback(t *testing.T) {
	tests := []struct {
		setValue *bool
		name     string
		want     bool
	}{
		{
			name:     "returns true when enabled",
			setValue: boolPtr(true),
			want:     true,
		},
		{
			name:     "returns false when disabled",
			setValue: boolPtr(false),
			want:     false,
		},
		{
			name:     "returns default when not set",
			setValue: nil,
			want:     DefaultCommsLoopback,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("comms.loopback", *tt.setValue)
			}

			cfg := New(v)

			got := cfg.GetCommsLoopback()
			if got != tt.want {
				t.Errorf("GetCommsLoopback() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetCommsTrace(t *testing.T) {
	tests := []struct {
		setValue *bool
		name     string
		want     bool
	}{
		{
			name:     "returns true when enabled",
			setValue: boolPtr(true),
			want:     true,
		},
		{
			name:     "returns false when disabled",
			setValue: boolPtr(false),
			want:     false,
		},
		{
			name:     "returns default when not set",
			setValue: nil,
			want:     DefaultCommsTrace,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("comms.trace", *tt.setValue)
			}

			cfg := New(v)

			got := cfg.GetCommsTrace()
			if got != tt.want {
				t.Errorf("GetCommsTrace() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetCommsNanoPTTDevicePath(t *testing.T) {
	tests := []struct {
		name     string
		setValue *string
		want     string
	}{
		{
			name:     "returns configured device path",
			setValue: strPtr("/dev/hidraw1/*"),
			want:     "/dev/hidraw1/*",
		},
		{
			name:     "returns default when empty",
			setValue: strPtr(""),
			want:     DefaultCommsNanoPTTDevicePath,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("comms.nanoPTT.devicePath", *tt.setValue)
			}

			cfg := New(v)

			got := cfg.GetCommsNanoPTTDevicePath()
			if got != tt.want {
				t.Errorf("GetCommsNanoPTTDevicePath() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetCommsNanoPTTDeviceName(t *testing.T) {
	tests := []struct {
		name     string
		setValue *string
		want     string
	}{
		{
			name:     "returns configured device name",
			setValue: strPtr("Custom NanoPTT Device"),
			want:     "Custom NanoPTT Device",
		},
		{
			name:     "returns default when empty",
			setValue: strPtr(""),
			want:     DefaultCommsNanoPTTDeviceName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("comms.nanoPTT.deviceName", *tt.setValue)
			}

			cfg := New(v)

			got := cfg.GetCommsNanoPTTDeviceName()
			if got != tt.want {
				t.Errorf("GetCommsNanoPTTDeviceName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetCommsControlSource(t *testing.T) {
	tests := []struct {
		name     string
		setValue *string
		want     string
	}{
		{
			name:     "returns configured control source",
			setValue: strPtr("bluealsa_xevent"),
			want:     "bluealsa_xevent",
		},
		{
			name:     "returns default when empty",
			setValue: strPtr(""),
			want:     DefaultCommsControlSource,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("comms.controlSource", *tt.setValue)
			}

			cfg := New(v)

			got := cfg.GetCommsControlSource()
			if got != tt.want {
				t.Errorf("GetCommsControlSource() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetCommsBluetoothPttBluetoothAudioDeviceHint(t *testing.T) {
	tests := []struct {
		name     string
		setValue *string
		want     string
	}{
		{
			name:     "returns configured audio hint",
			setValue: strPtr("BS-22"),
			want:     "BS-22",
		},
		{
			name:     "returns default when empty",
			setValue: strPtr(""),
			want:     DefaultCommsBluetoothPttBluetoothAudioDeviceHint,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("comms.bluetoothPtt.BluetoothAudioDeviceHint", *tt.setValue)
			}

			cfg := New(v)

			got := cfg.GetCommsBluetoothPttBluetoothAudioDeviceHint()
			if got != tt.want {
				t.Errorf("GetCommsBluetoothPttBluetoothAudioDeviceHint() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetCommsMicGain(t *testing.T) {
	tests := []struct {
		name     string
		setValue *float64
		want     float32
	}{
		{
			name:     "returns configured gain",
			setValue: float64Ptr(3.0),
			want:     3.0,
		},
		{
			name:     "returns default when not set",
			setValue: nil,
			want:     DefaultCommsMicGain,
		},
		{
			name:     "returns default when zero",
			setValue: float64Ptr(0),
			want:     DefaultCommsMicGain,
		},
		{
			name:     "returns default when negative",
			setValue: float64Ptr(-1.0),
			want:     DefaultCommsMicGain,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("comms.micGain", *tt.setValue)
			}

			cfg := New(v)

			got := cfg.GetCommsMicGain()
			if got != tt.want {
				t.Errorf("GetCommsMicGain() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetCommsNanoPTTEnable(t *testing.T) {
	tests := []struct {
		setValue *bool
		name     string
		want     bool
	}{
		{
			name:     "returns true when enabled",
			setValue: boolPtr(true),
			want:     true,
		},
		{
			name:     "returns false when disabled",
			setValue: boolPtr(false),
			want:     false,
		},
		{
			name:     "returns default when not set",
			setValue: nil,
			want:     DefaultCommsNanoPTTEnable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("comms.nanoPTT.enable", *tt.setValue)
			}

			cfg := New(v)

			got := cfg.GetCommsNanoPTTEnable()
			if got != tt.want {
				t.Errorf("GetCommsNanoPTTEnable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetCommsBluetoothPttEnable(t *testing.T) {
	tests := []struct {
		setValue *bool
		name     string
		want     bool
	}{
		{
			name:     "returns true when enabled",
			setValue: boolPtr(true),
			want:     true,
		},
		{
			name:     "returns false when disabled",
			setValue: boolPtr(false),
			want:     false,
		},
		{
			name:     "returns default when not set",
			setValue: nil,
			want:     DefaultCommsBluetoothPttEnable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("comms.bluetoothPtt.enable", *tt.setValue)
			}

			cfg := New(v)

			got := cfg.GetCommsBluetoothPttEnable()
			if got != tt.want {
				t.Errorf("GetCommsBluetoothPttEnable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetCommsPlaybackBuffer(t *testing.T) {
	tests := []struct {
		setValue *int
		name     string
		want     int
	}{
		{
			name:     "returns configured buffer",
			setValue: intPtr(4),
			want:     4,
		},
		{
			name:     "returns default when zero",
			setValue: intPtr(0),
			want:     DefaultCommsPlaybackBuffer,
		},
		{
			name:     "returns default when not set",
			setValue: nil,
			want:     DefaultCommsPlaybackBuffer,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("comms.playbackBuffer", *tt.setValue)
			}

			cfg := New(v)

			got := cfg.GetCommsPlaybackBuffer()
			if got != tt.want {
				t.Errorf("GetCommsPlaybackBuffer() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfigReload(t *testing.T) {
	v := viper.New()
	v.Set("meshNetInterface", "eth0")
	v.Set("gatewayMode", true)
	v.Set("comms.playbackBuffer", 4)

	cfg := New(v)

	// Check initial values
	if got := cfg.GetMeshNetInterface(); got != "eth0" {
		t.Errorf("Initial GetMeshNetInterface() = %v, want eth0", got)
	}

	if got := cfg.GetCommsPlaybackBuffer(); got != 4 {
		t.Errorf("Initial GetCommsPlaybackBuffer() = %v, want 4", got)
	}

	// Change configuration values
	v.Set("meshNetInterface", "wlan0")
	v.Set("comms.playbackBuffer", 8)

	// Manually trigger reload (simulating config file change)
	cfg.reload()

	// Check updated values
	if got := cfg.GetMeshNetInterface(); got != "wlan0" {
		t.Errorf("After reload GetMeshNetInterface() = %v, want wlan0", got)
	}

	if got := cfg.GetCommsPlaybackBuffer(); got != 8 {
		t.Errorf("After reload GetCommsPlaybackBuffer() = %v, want 8", got)
	}
}

func TestConfigOnChangeCallback(t *testing.T) {
	v := viper.New()
	v.Set("meshNetInterface", "eth0")

	cfg := New(v)

	callbackCalled := false

	var receivedConfig *Config

	cfg.OnConfigChange(func(c *Config) {
		callbackCalled = true
		receivedConfig = c
	})

	// Change config and trigger reload
	v.Set("meshNetInterface", "wlan0")
	cfg.reload()
	cfg.notifyCallbacks()

	if !callbackCalled {
		t.Error("OnConfigChange callback was not called")
	}

	if receivedConfig != cfg {
		t.Error("Callback did not receive the correct Config instance")
	}

	if got := receivedConfig.GetMeshNetInterface(); got != "wlan0" {
		t.Errorf("Callback config GetMeshNetInterface() = %v, want wlan0", got)
	}
}

func TestGetEnableGNSS(t *testing.T) {
	tests := []struct {
		setValue *bool
		name     string
		want     bool
	}{
		{
			name:     "returns true when set to true",
			setValue: boolPtr(true),
			want:     true,
		},
		{
			name:     "returns false when set to false",
			setValue: boolPtr(false),
			want:     false,
		},
		{
			name:     "returns default when not set",
			setValue: nil,
			want:     DefaultEnableGNSS,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("gnss.enable", *tt.setValue)
			}

			cfg := New(v)

			got := cfg.GetEnableGNSS()
			if got != tt.want {
				t.Errorf("GetEnableGNSS() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetGNSSSendAsNMEA(t *testing.T) {
	tests := []struct {
		setValue *bool
		name     string
		want     bool
	}{
		{
			name:     "returns true when enabled",
			setValue: boolPtr(true),
			want:     true,
		},
		{
			name:     "returns false when disabled",
			setValue: boolPtr(false),
			want:     false,
		},
		{
			name:     "returns default when not set",
			setValue: nil,
			want:     DefaultGNSSSendAsNMEA,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("gnss.sendAsExternalGNSSSource.sendAsNMEA", *tt.setValue)
			}

			cfg := New(v)

			got := cfg.GetGNSSSendAsNMEA()
			if got != tt.want {
				t.Errorf("GetGNSSSendAsNMEA() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetGNSSSendAsCoT(t *testing.T) {
	tests := []struct {
		setValue *bool
		name     string
		want     bool
	}{
		{
			name:     "returns true when enabled",
			setValue: boolPtr(true),
			want:     true,
		},
		{
			name:     "returns false when disabled",
			setValue: boolPtr(false),
			want:     false,
		},
		{
			name:     "returns default when not set",
			setValue: nil,
			want:     DefaultGNSSSendAsCoT,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("gnss.sendAsExternalGNSSSource.sendAsCoT", *tt.setValue)
			}

			cfg := New(v)

			got := cfg.GetGNSSSendAsCoT()
			if got != tt.want {
				t.Errorf("GetGNSSSendAsCoT() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestGetBLOSStatusWorkerInterval tests the GetBLOSStatusWorkerInterval method.
func TestGetBLOSStatusWorkerInterval(t *testing.T) {
	tests := []struct {
		name     string
		setValue int
		setKey   bool
		expected int
	}{
		{
			name:     "returns configured interval",
			setValue: 60,
			setKey:   true,
			expected: 60,
		},
		{
			name:     "returns default when zero",
			setValue: 0,
			setKey:   true,
			expected: DefaultBLOSStatusWorkerInterval,
		},
		{
			name:     "returns default when not set",
			setKey:   false,
			expected: DefaultBLOSStatusWorkerInterval,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setKey {
				v.Set("blos.statusWorkerInterval", tt.setValue)
			}

			c := New(v)

			result := c.GetBLOSStatusWorkerInterval()
			if result != tt.expected {
				t.Errorf("GetBLOSStatusWorkerInterval() = %d, want %d", result, tt.expected)
			}
		})
	}
}

// TestBLOSEnabled tests the BLOSEnabled method.
func TestBLOSEnabled(t *testing.T) {
	tests := []struct {
		setValue *bool
		name     string
		want     bool
	}{
		{
			name:     "returns true when enabled",
			setValue: boolPtr(true),
			want:     true,
		},
		{
			name:     "returns false when disabled",
			setValue: boolPtr(false),
			want:     false,
		},
		{
			name:     "returns default when not set",
			setValue: nil,
			want:     DefaultEnableBLOS,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("blos.enable", *tt.setValue)
			}

			cfg := New(v)

			got := cfg.BLOSEnabled()
			if got != tt.want {
				t.Errorf("BLOSEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetEnableBatmanMulticastEnhancements(t *testing.T) {
	tests := []struct {
		setValue *bool
		name     string
		want     bool
	}{
		{
			name:     "returns true when enabled",
			setValue: boolPtr(true),
			want:     true,
		},
		{
			name:     "returns false when disabled",
			setValue: boolPtr(false),
			want:     false,
		},
		{
			name:     "returns default when not set",
			setValue: nil,
			want:     DefaultBatmanMulticastEnhancementsEnabled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("batman.multicastEnhancementsEnabled", *tt.setValue)
			}

			cfg := New(v)

			got := cfg.GetEnableBatmanMulticastEnhancements()
			if got != tt.want {
				t.Errorf("GetEnableBatmanMulticastEnhancements() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetEnableBLOS(t *testing.T) {
	tests := []struct {
		setValue *bool
		name     string
		want     bool
	}{
		{
			name:     "returns true when enabled",
			setValue: boolPtr(true),
			want:     true,
		},
		{
			name:     "returns false when disabled",
			setValue: boolPtr(false),
			want:     false,
		},
		{
			name:     "returns default when not set",
			setValue: nil,
			want:     DefaultEnableBLOS,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("blos.enable", *tt.setValue)
			}

			cfg := New(v)

			got := cfg.GetEnableBLOS()
			if got != tt.want {
				t.Errorf("GetEnableBLOS() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetOpenMANETFrontendURL(t *testing.T) {
	tests := []struct {
		name     string
		setValue *string
		want     string
	}{
		{
			name:     "returns configured value",
			setValue: strPtr("https://example.com:3000"),
			want:     "https://example.com:3000",
		},
		{
			name:     "returns default when empty",
			setValue: strPtr(""),
			want:     DefaultOpenMANETFrontendURL,
		},
		{
			name:     "returns default when not set",
			setValue: nil,
			want:     DefaultOpenMANETFrontendURL,
		},
		{
			name:     "returns custom port",
			setValue: strPtr("http://192.168.1.1:9090"),
			want:     "http://192.168.1.1:9090",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("openmanetFrontendURL", *tt.setValue)
			}

			cfg := NewWithoutWatch(v)

			got := cfg.GetOpenMANETFrontendURL()
			if got != tt.want {
				t.Errorf("GetOpenMANETFrontendURL() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetOpenMANETAPIAddress(t *testing.T) {
	tests := []struct {
		name     string
		setValue *string
		want     string
	}{
		{
			name:     "returns configured value",
			setValue: strPtr("http://127.0.0.1:9000"),
			want:     "http://127.0.0.1:9000",
		},
		{
			name:     "returns default when empty",
			setValue: strPtr(""),
			want:     DefaultOpenMANETAPIAddress,
		},
		{
			name:     "returns default when not set",
			setValue: nil,
			want:     DefaultOpenMANETAPIAddress,
		},
		{
			name:     "returns custom address",
			setValue: strPtr("http://10.0.0.1:8080"),
			want:     "http://10.0.0.1:8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("openmanetAPIAddress", *tt.setValue)
			}

			cfg := NewWithoutWatch(v)

			got := cfg.GetOpenMANETAPIAddress()
			if got != tt.want {
				t.Errorf("GetOpenMANETAPIAddress() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetOpenMANETWebsocketPort(t *testing.T) {
	tests := []struct {
		name     string
		setValue *int
		want     int
	}{
		{
			name:     "returns configured value",
			setValue: intPtr(9090),
			want:     9090,
		},
		{
			name:     "returns default when zero",
			setValue: intPtr(0),
			want:     DefaultOpenMANETWebsocketPort,
		},
		{
			name:     "returns default when not set",
			setValue: nil,
			want:     DefaultOpenMANETWebsocketPort,
		},
		{
			name:     "returns custom port",
			setValue: intPtr(8443),
			want:     8443,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("openmanetWebsocketPort", *tt.setValue)
			}

			cfg := NewWithoutWatch(v)

			got := cfg.GetOpenMANETWebsocketPort()
			if got != tt.want {
				t.Errorf("GetOpenMANETWebsocketPort() = %v, want %v", got, tt.want)
			}
		})
	}
}
