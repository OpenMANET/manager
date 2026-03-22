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

func TestGetGatewayMode(t *testing.T) {
	tests := []struct {
		name     string
		setValue *bool
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
			want:     DefaultGatewayMode,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("gatewayMode", *tt.setValue)
			}

			cfg := New(v)
			got := cfg.GetGatewayMode()
			if got != tt.want {
				t.Errorf("GetGatewayMode() = %v, want %v", got, tt.want)
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

func TestGetPTTMcastPort(t *testing.T) {
	tests := []struct {
		name     string
		setValue *int
		want     int
	}{
		{
			name:     "returns configured port",
			setValue: intPtr(8080),
			want:     8080,
		},
		{
			name:     "returns default when zero",
			setValue: intPtr(0),
			want:     DefaultPTTMcastPort,
		},
		{
			name:     "returns default when not set",
			setValue: nil,
			want:     DefaultPTTMcastPort,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("ptt.mcastPort", *tt.setValue)
			}

			cfg := New(v)
			got := cfg.GetPTTMcastPort()
			if got != tt.want {
				t.Errorf("GetPTTMcastPort() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetPTTEnable(t *testing.T) {
	tests := []struct {
		name     string
		setValue *bool
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
			want:     DefaultPTTEnable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("ptt.enable", *tt.setValue)
			}

			cfg := New(v)
			got := cfg.GetPTTEnable()
			if got != tt.want {
				t.Errorf("GetPTTEnable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetAlfredDataTypeGateway(t *testing.T) {
	tests := []struct {
		name     string
		setValue *bool
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

func TestGetPTTMcastAddr(t *testing.T) {
	tests := []struct {
		name     string
		setValue *string
		want     string
	}{
		{
			name:     "returns configured address",
			setValue: strPtr("224.0.0.2"),
			want:     "224.0.0.2",
		},
		{
			name:     "returns default when empty",
			setValue: strPtr(""),
			want:     DefaultPTTMcastAddr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("ptt.mcastAddr", *tt.setValue)
			}

			cfg := New(v)
			got := cfg.GetPTTMcastAddr()
			if got != tt.want {
				t.Errorf("GetPTTMcastAddr() = %v, want %v", got, tt.want)
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
		name     string
		setValue *bool
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
		name     string
		setValue *bool
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
		name     string
		setValue *bool
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
		name     string
		setValue *bool
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

func TestGetPTTPttKey(t *testing.T) {
	tests := []struct {
		name     string
		setValue *string
		want     string
	}{
		{
			name:     "returns configured key",
			setValue: strPtr("space"),
			want:     "space",
		},
		{
			name:     "returns default when empty",
			setValue: strPtr(""),
			want:     DefaultPTTPttKey,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("ptt.pttKey", *tt.setValue)
			}

			cfg := New(v)
			got := cfg.GetPTTPttKey()
			if got != tt.want {
				t.Errorf("GetPTTPttKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetPTTControlSource(t *testing.T) {
	tests := []struct {
		name     string
		setValue *string
		want     string
	}{
		{
			name:     "returns configured source",
			setValue: strPtr("bluetooth"),
			want:     "bluetooth",
		},
		{
			name:     "returns default when empty",
			setValue: strPtr(""),
			want:     DefaultPTTControlSource,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("ptt.controlSource", *tt.setValue)
			}

			cfg := New(v)
			got := cfg.GetPTTControlSource()
			if got != tt.want {
				t.Errorf("GetPTTControlSource() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetPTTDebug(t *testing.T) {
	tests := []struct {
		name     string
		setValue *bool
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
			want:     DefaultPTTDebug,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("ptt.debug", *tt.setValue)
			}

			cfg := New(v)
			got := cfg.GetPTTDebug()
			if got != tt.want {
				t.Errorf("GetPTTDebug() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetPTTLoopback(t *testing.T) {
	tests := []struct {
		name     string
		setValue *bool
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
			want:     DefaultPTTLoopback,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("ptt.loopback", *tt.setValue)
			}

			cfg := New(v)
			got := cfg.GetPTTLoopback()
			if got != tt.want {
				t.Errorf("GetPTTLoopback() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetPTTPttDevice(t *testing.T) {
	tests := []struct {
		name     string
		setValue *string
		want     string
	}{
		{
			name:     "returns configured device",
			setValue: strPtr("/dev/hidraw1/*"),
			want:     "/dev/hidraw1/*",
		},
		{
			name:     "returns default when empty",
			setValue: strPtr(""),
			want:     DefaultPTTPttDevice,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("ptt.pttDevice", *tt.setValue)
			}

			cfg := New(v)
			got := cfg.GetPTTPttDevice()
			if got != tt.want {
				t.Errorf("GetPTTPttDevice() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetPTTPttDeviceName(t *testing.T) {
	tests := []struct {
		name     string
		setValue *string
		want     string
	}{
		{
			name:     "returns configured device name",
			setValue: strPtr("Custom PTT Device"),
			want:     "Custom PTT Device",
		},
		{
			name:     "returns default when empty",
			setValue: strPtr(""),
			want:     DefaultPTTPttDeviceName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			if tt.setValue != nil {
				v.Set("ptt.pttDeviceName", *tt.setValue)
			}

			cfg := New(v)
			got := cfg.GetPTTPttDeviceName()
			if got != tt.want {
				t.Errorf("GetPTTPttDeviceName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfigReload(t *testing.T) {
	v := viper.New()
	v.Set("meshNetInterface", "eth0")
	v.Set("gatewayMode", true)
	v.Set("ptt.mcastPort", 8080)

	cfg := New(v)

	// Check initial values
	if got := cfg.GetMeshNetInterface(); got != "eth0" {
		t.Errorf("Initial GetMeshNetInterface() = %v, want eth0", got)
	}
	if got := cfg.GetGatewayMode(); got != true {
		t.Errorf("Initial GetGatewayMode() = %v, want true", got)
	}
	if got := cfg.GetPTTMcastPort(); got != 8080 {
		t.Errorf("Initial GetPTTMcastPort() = %v, want 8080", got)
	}

	// Change configuration values
	v.Set("meshNetInterface", "wlan0")
	v.Set("gatewayMode", false)
	v.Set("ptt.mcastPort", 9090)

	// Manually trigger reload (simulating config file change)
	cfg.reload()

	// Check updated values
	if got := cfg.GetMeshNetInterface(); got != "wlan0" {
		t.Errorf("After reload GetMeshNetInterface() = %v, want wlan0", got)
	}
	if got := cfg.GetGatewayMode(); got != false {
		t.Errorf("After reload GetGatewayMode() = %v, want false", got)
	}
	if got := cfg.GetPTTMcastPort(); got != 9090 {
		t.Errorf("After reload GetPTTMcastPort() = %v, want 9090", got)
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
		name     string
		setValue *bool
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
		name     string
		setValue *bool
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
		name     string
		setValue *bool
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
		name     string
		setValue *bool
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
