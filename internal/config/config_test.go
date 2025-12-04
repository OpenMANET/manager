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
