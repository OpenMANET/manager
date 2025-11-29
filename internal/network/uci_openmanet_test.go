package network

import (
	"errors"
	"testing"

	"github.com/digineo/go-uci/v2"
)

// mockOpenMANETConfigReader is a mock implementation of OpenMANETConfigReader for testing.
type mockOpenMANETConfigReader struct {
	data     map[string]map[string]map[string][]string // config -> section -> option -> values
	sections map[string]map[string]string              // config -> section -> type
}

// Commit is a no-op for the mock, simulating a successful commit.
func (m *mockOpenMANETConfigReader) Commit() error {
	return nil
}

func newMockOpenMANETConfigReader() *mockOpenMANETConfigReader {
	return &mockOpenMANETConfigReader{
		data:     make(map[string]map[string]map[string][]string),
		sections: make(map[string]map[string]string),
	}
}

func (m *mockOpenMANETConfigReader) Get(config, section, option string) ([]string, bool) {
	if m.data[config] == nil {
		return nil, false
	}
	if m.data[config][section] == nil {
		return nil, false
	}
	values, ok := m.data[config][section][option]
	return values, ok
}

func (m *mockOpenMANETConfigReader) SetType(config, section, option string, typ uci.OptionType, values ...string) error {
	if m.data[config] == nil {
		m.data[config] = make(map[string]map[string][]string)
	}
	if m.data[config][section] == nil {
		m.data[config][section] = make(map[string][]string)
	}
	m.data[config][section][option] = values
	return nil
}

func (m *mockOpenMANETConfigReader) Del(config, section, option string) error {
	if m.data[config] != nil && m.data[config][section] != nil {
		delete(m.data[config][section], option)
	}
	return nil
}

func (m *mockOpenMANETConfigReader) AddSection(config, section, typ string) error {
	if m.sections[config] == nil {
		m.sections[config] = make(map[string]string)
	}
	m.sections[config][section] = typ
	if m.data[config] == nil {
		m.data[config] = make(map[string]map[string][]string)
	}
	if m.data[config][section] == nil {
		m.data[config][section] = make(map[string][]string)
	}
	return nil
}

func (m *mockOpenMANETConfigReader) DelSection(config, section string) error {
	if m.data[config] != nil {
		delete(m.data[config], section)
	}
	if m.sections[config] != nil {
		delete(m.sections[config], section)
	}
	return nil
}

// setupMockOpenMANETData initializes the mock with sample OpenMANET configuration.
func setupMockOpenMANETData(m *mockOpenMANETConfigReader) {
	_ = m.AddSection("openmanetd", "config", "openmanet")
	_ = m.SetType("openmanetd", "config", "dhcpconfigured", uci.TypeOption, "0")
	_ = m.SetType("openmanetd", "config", "config", uci.TypeOption, "/etc/openmanet/config.yml")
}

func TestGetOpenMANETConfigWithReader(t *testing.T) {
	mock := newMockOpenMANETConfigReader()
	setupMockOpenMANETData(mock)

	config, err := GetOpenMANETConfigWithReader(mock)
	if err != nil {
		t.Fatalf("GetOpenMANETConfigWithReader failed: %v", err)
	}

	if config.DHCPConfigured != "0" {
		t.Errorf("Expected DHCPConfigured=0, got %s", config.DHCPConfigured)
	}
	if config.Config != "/etc/openmanet/config.yml" {
		t.Errorf("Expected Config=/etc/openmanet/config.yml, got %s", config.Config)
	}
}

func TestGetOpenMANETConfigWithReader_Empty(t *testing.T) {
	mock := newMockOpenMANETConfigReader()

	config, err := GetOpenMANETConfigWithReader(mock)
	if err != nil {
		t.Fatalf("GetOpenMANETConfigWithReader failed: %v", err)
	}

	if config.DHCPConfigured != "" {
		t.Errorf("Expected empty DHCPConfigured, got %s", config.DHCPConfigured)
	}
	if config.Config != "" {
		t.Errorf("Expected empty Config, got %s", config.Config)
	}
}

func TestSetOpenMANETConfigWithReader(t *testing.T) {
	mock := newMockOpenMANETConfigReader()

	config := &UCIOpenMANET{
		DHCPConfigured: "1",
		Config:         "/custom/path/config.yml",
	}

	err := SetOpenMANETConfigWithReader(config, mock)
	if err != nil {
		t.Fatalf("SetOpenMANETConfigWithReader failed: %v", err)
	}

	// Verify the values were set
	readConfig, err := GetOpenMANETConfigWithReader(mock)
	if err != nil {
		t.Fatalf("GetOpenMANETConfigWithReader failed: %v", err)
	}

	if readConfig.DHCPConfigured != "1" {
		t.Errorf("Expected DHCPConfigured=1, got %s", readConfig.DHCPConfigured)
	}
	if readConfig.Config != "/custom/path/config.yml" {
		t.Errorf("Expected Config=/custom/path/config.yml, got %s", readConfig.Config)
	}
}

func TestSetOpenMANETConfigWithReader_NilConfig(t *testing.T) {
	mock := newMockOpenMANETConfigReader()

	err := SetOpenMANETConfigWithReader(nil, mock)
	if err == nil {
		t.Error("Expected error for nil config, got nil")
	}
}

func TestSetOpenMANETConfigWithReader_PartialConfig(t *testing.T) {
	mock := newMockOpenMANETConfigReader()

	// Set only DHCPConfigured
	config := &UCIOpenMANET{
		DHCPConfigured: "1",
	}

	err := SetOpenMANETConfigWithReader(config, mock)
	if err != nil {
		t.Fatalf("SetOpenMANETConfigWithReader failed: %v", err)
	}

	readConfig, err := GetOpenMANETConfigWithReader(mock)
	if err != nil {
		t.Fatalf("GetOpenMANETConfigWithReader failed: %v", err)
	}

	if readConfig.DHCPConfigured != "1" {
		t.Errorf("Expected DHCPConfigured=1, got %s", readConfig.DHCPConfigured)
	}
	if readConfig.Config != "" {
		t.Errorf("Expected empty Config, got %s", readConfig.Config)
	}
}

func TestIsDHCPConfiguredWithReader(t *testing.T) {
	tests := []struct {
		name           string
		dhcpConfigured string
		expected       bool
		expectError    bool
	}{
		{
			name:           "configured",
			dhcpConfigured: "1",
			expected:       true,
			expectError:    false,
		},
		{
			name:           "not configured",
			dhcpConfigured: "0",
			expected:       false,
			expectError:    false,
		},
		{
			name:           "empty",
			dhcpConfigured: "",
			expected:       false,
			expectError:    false,
		},
		{
			name:           "invalid value",
			dhcpConfigured: "invalid",
			expected:       false,
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newMockOpenMANETConfigReader()
			if tt.dhcpConfigured != "" {
				_ = mock.AddSection("openmanetd", "config", "openmanet")
				_ = mock.SetType("openmanetd", "config", "dhcpconfigured", uci.TypeOption, tt.dhcpConfigured)
			}

			configured, err := IsDHCPConfiguredWithReader(mock)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("IsDHCPConfiguredWithReader failed: %v", err)
			}

			if configured != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, configured)
			}
		})
	}
}

func TestSetDHCPConfiguredWithReader(t *testing.T) {
	mock := newMockOpenMANETConfigReader()

	err := SetDHCPConfiguredWithReader(mock)
	if err != nil {
		t.Fatalf("SetDHCPConfiguredWithReader failed: %v", err)
	}

	values, ok := mock.Get("openmanetd", "config", "dhcpconfigured")
	if !ok || len(values) == 0 || values[0] != "1" {
		t.Errorf("Expected dhcpconfigured=1, got %v", values)
	}

	// Verify using IsDHCPConfigured
	configured, err := IsDHCPConfiguredWithReader(mock)
	if err != nil {
		t.Fatalf("IsDHCPConfiguredWithReader failed: %v", err)
	}
	if !configured {
		t.Error("Expected DHCP to be configured")
	}
}

func TestClearDHCPConfiguredWithReader(t *testing.T) {
	mock := newMockOpenMANETConfigReader()
	_ = mock.AddSection("openmanetd", "config", "openmanet")
	_ = mock.SetType("openmanetd", "config", "dhcpconfigured", uci.TypeOption, "1")

	err := ClearDHCPConfiguredWithReader(mock)
	if err != nil {
		t.Fatalf("ClearDHCPConfiguredWithReader failed: %v", err)
	}

	values, ok := mock.Get("openmanetd", "config", "dhcpconfigured")
	if !ok || len(values) == 0 || values[0] != "0" {
		t.Errorf("Expected dhcpconfigured=0, got %v", values)
	}

	// Verify using IsDHCPConfigured
	configured, err := IsDHCPConfiguredWithReader(mock)
	if err != nil {
		t.Fatalf("IsDHCPConfiguredWithReader failed: %v", err)
	}
	if configured {
		t.Error("Expected DHCP to not be configured")
	}
}

func TestGetConfigPathWithReader(t *testing.T) {
	tests := []struct {
		name         string
		configPath   string
		expectedPath string
	}{
		{
			name:         "custom path",
			configPath:   "/custom/path/config.yml",
			expectedPath: "/custom/path/config.yml",
		},
		{
			name:         "default path",
			configPath:   "",
			expectedPath: "/etc/openmanet/config.yml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newMockOpenMANETConfigReader()
			if tt.configPath != "" {
				_ = mock.AddSection("openmanetd", "config", "openmanet")
				_ = mock.SetType("openmanetd", "config", "config", uci.TypeOption, tt.configPath)
			}

			path, err := GetConfigPathWithReader(mock)
			if err != nil {
				t.Fatalf("GetConfigPathWithReader failed: %v", err)
			}

			if path != tt.expectedPath {
				t.Errorf("Expected %s, got %s", tt.expectedPath, path)
			}
		})
	}
}

func TestSetConfigPathWithReader(t *testing.T) {
	mock := newMockOpenMANETConfigReader()

	path := "/new/config/path.yml"
	err := SetConfigPathWithReader(path, mock)
	if err != nil {
		t.Fatalf("SetConfigPathWithReader failed: %v", err)
	}

	values, ok := mock.Get("openmanetd", "config", "config")
	if !ok || len(values) == 0 || values[0] != path {
		t.Errorf("Expected config=%s, got %v", path, values)
	}

	// Verify using GetConfigPath
	readPath, err := GetConfigPathWithReader(mock)
	if err != nil {
		t.Fatalf("GetConfigPathWithReader failed: %v", err)
	}
	if readPath != path {
		t.Errorf("Expected %s, got %s", path, readPath)
	}
}

func TestSetConfigPathWithReader_EmptyPath(t *testing.T) {
	mock := newMockOpenMANETConfigReader()

	err := SetConfigPathWithReader("", mock)
	if err == nil {
		t.Error("Expected error for empty path, got nil")
	}
}

// mockOpenMANETConfigReaderWithErrors is a mock that returns errors for testing error paths.
type mockOpenMANETConfigReaderWithErrors struct{}

// Commit always returns an error for error simulation.
func (m *mockOpenMANETConfigReaderWithErrors) Commit() error {
	return errors.New("mock error")
}

func (m *mockOpenMANETConfigReaderWithErrors) Get(config, section, option string) ([]string, bool) {
	return nil, false
}

func (m *mockOpenMANETConfigReaderWithErrors) SetType(config, section, option string, typ uci.OptionType, values ...string) error {
	return errors.New("mock error")
}

func (m *mockOpenMANETConfigReaderWithErrors) Del(config, section, option string) error {
	return errors.New("mock error")
}

func (m *mockOpenMANETConfigReaderWithErrors) AddSection(config, section, typ string) error {
	return errors.New("mock error")
}

func (m *mockOpenMANETConfigReaderWithErrors) DelSection(config, section string) error {
	return errors.New("mock error")
}

func TestSetOpenMANETConfigWithReader_ErrorHandling(t *testing.T) {
	mock := &mockOpenMANETConfigReaderWithErrors{}

	config := &UCIOpenMANET{
		DHCPConfigured: "1",
	}

	err := SetOpenMANETConfigWithReader(config, mock)
	if err == nil {
		t.Error("Expected error from SetOpenMANETConfigWithReader")
	}
}

func TestCommitWithOpenMANETReader(t *testing.T) {
	mock := newMockOpenMANETConfigReader()
	// Should succeed (no error)
	if err := mock.Commit(); err != nil {
		t.Errorf("Expected Commit to succeed, got error: %v", err)
	}

	mockErr := &mockOpenMANETConfigReaderWithErrors{}
	// Should fail (return error)
	if err := mockErr.Commit(); err == nil {
		t.Error("Expected Commit to fail, got nil error")
	}
}

func TestSetDHCPConfiguredWithReader_ErrorHandling(t *testing.T) {
	mock := &mockOpenMANETConfigReaderWithErrors{}

	err := SetDHCPConfiguredWithReader(mock)
	if err == nil {
		t.Error("Expected error from SetDHCPConfiguredWithReader")
	}
}

func TestClearDHCPConfiguredWithReader_ErrorHandling(t *testing.T) {
	mock := &mockOpenMANETConfigReaderWithErrors{}

	err := ClearDHCPConfiguredWithReader(mock)
	if err == nil {
		t.Error("Expected error from ClearDHCPConfiguredWithReader")
	}
}

func TestSetConfigPathWithReader_ErrorHandling(t *testing.T) {
	mock := &mockOpenMANETConfigReaderWithErrors{}

	err := SetConfigPathWithReader("/some/path", mock)
	if err == nil {
		t.Error("Expected error from SetConfigPathWithReader")
	}
}

func TestSetDHCPConfigured_UpdatesExistingValue(t *testing.T) {
	mock := newMockOpenMANETConfigReader()

	// Start with value set to 0
	_ = mock.AddSection("openmanetd", "config", "openmanet")
	_ = mock.SetType("openmanetd", "config", "dhcpconfigured", uci.TypeOption, "0")

	configured, _ := IsDHCPConfiguredWithReader(mock)
	if configured {
		t.Error("Expected initial state to be not configured")
	}

	// Set to configured
	err := SetDHCPConfiguredWithReader(mock)
	if err != nil {
		t.Fatalf("SetDHCPConfiguredWithReader failed: %v", err)
	}

	configured, _ = IsDHCPConfiguredWithReader(mock)
	if !configured {
		t.Error("Expected state to be configured after SetDHCPConfigured")
	}

	// Clear configured state
	err = ClearDHCPConfiguredWithReader(mock)
	if err != nil {
		t.Fatalf("ClearDHCPConfiguredWithReader failed: %v", err)
	}

	configured, _ = IsDHCPConfiguredWithReader(mock)
	if configured {
		t.Error("Expected state to be not configured after ClearDHCPConfigured")
	}
}

func TestCompleteWorkflow(t *testing.T) {
	mock := newMockOpenMANETConfigReader()

	// Step 1: Initial configuration
	config := &UCIOpenMANET{
		DHCPConfigured: "0",
		Config:         "/etc/openmanet/config.yml",
	}

	err := SetOpenMANETConfigWithReader(config, mock)
	if err != nil {
		t.Fatalf("Failed to set initial config: %v", err)
	}

	// Step 2: Verify initial state
	configured, err := IsDHCPConfiguredWithReader(mock)
	if err != nil {
		t.Fatalf("Failed to check DHCP configured: %v", err)
	}
	if configured {
		t.Error("Expected DHCP to not be configured initially")
	}

	// Step 3: Mark DHCP as configured
	err = SetDHCPConfiguredWithReader(mock)
	if err != nil {
		t.Fatalf("Failed to set DHCP configured: %v", err)
	}

	// Step 4: Verify DHCP is configured
	configured, err = IsDHCPConfiguredWithReader(mock)
	if err != nil {
		t.Fatalf("Failed to check DHCP configured: %v", err)
	}
	if !configured {
		t.Error("Expected DHCP to be configured")
	}

	// Step 5: Change config path
	newPath := "/new/location/config.yml"
	err = SetConfigPathWithReader(newPath, mock)
	if err != nil {
		t.Fatalf("Failed to set config path: %v", err)
	}

	// Step 6: Verify config path
	path, err := GetConfigPathWithReader(mock)
	if err != nil {
		t.Fatalf("Failed to get config path: %v", err)
	}
	if path != newPath {
		t.Errorf("Expected path %s, got %s", newPath, path)
	}

	// Step 7: Get full config
	finalConfig, err := GetOpenMANETConfigWithReader(mock)
	if err != nil {
		t.Fatalf("Failed to get final config: %v", err)
	}
	if finalConfig.DHCPConfigured != "1" {
		t.Errorf("Expected DHCPConfigured=1, got %s", finalConfig.DHCPConfigured)
	}
	if finalConfig.Config != newPath {
		t.Errorf("Expected Config=%s, got %s", newPath, finalConfig.Config)
	}
}
