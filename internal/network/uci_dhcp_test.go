package network

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/digineo/go-uci/v2"
	proto "github.com/openmanet/openmanetd/internal/api/openmanet/network/v1"
	"github.com/openmanet/openmanetd/internal/database/models"
)

// mockDHCPConfigReader is a mock implementation of DHCPConfigReader for testing.
type mockDHCPConfigReader struct {
	data     map[string]map[string]map[string][]string // config -> section -> option -> values
	sections map[string]map[string]string              // config -> section -> type
}

// Commit is a no-op for the mock, simulating a successful commit.
func (m *mockDHCPConfigReader) Commit() error {
	return nil
}

// ReloadConfig is a no-op for the mock, simulating a successful reload.
func (m *mockDHCPConfigReader) ReloadConfig() error {
	return nil
}

func newMockDHCPConfigReader() *mockDHCPConfigReader {
	return &mockDHCPConfigReader{
		data:     make(map[string]map[string]map[string][]string),
		sections: make(map[string]map[string]string),
	}
}

func (m *mockDHCPConfigReader) Get(config, section, option string) ([]string, bool) {
	if m.data[config] == nil {
		return nil, false
	}

	if m.data[config][section] == nil {
		return nil, false
	}

	values, ok := m.data[config][section][option]

	return values, ok
}

func (m *mockDHCPConfigReader) GetSections(config, secType string) ([]string, error) {
	var sections []string
	if m.sections[config] != nil {
		for section, typ := range m.sections[config] {
			if typ == secType {
				sections = append(sections, section)
			}
		}
	}

	return sections, nil
}

func (m *mockDHCPConfigReader) SetType(config, section, option string, typ uci.OptionType, values ...string) error {
	if m.data[config] == nil {
		m.data[config] = make(map[string]map[string][]string)
	}

	if m.data[config][section] == nil {
		m.data[config][section] = make(map[string][]string)
	}

	m.data[config][section][option] = values

	return nil
}

func (m *mockDHCPConfigReader) Del(config, section, option string) error {
	if m.data[config] != nil && m.data[config][section] != nil {
		delete(m.data[config][section], option)
	}

	return nil
}

func (m *mockDHCPConfigReader) AddSection(config, section, typ string) error {
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

func (m *mockDHCPConfigReader) DelSection(config, section string) error {
	if m.data[config] != nil {
		delete(m.data[config], section)
	}

	if m.sections[config] != nil {
		delete(m.sections[config], section)
	}

	return nil
}

// setupMockDnsmasqData initializes the mock with sample dnsmasq configuration.
func setupMockDnsmasqData(m *mockDHCPConfigReader) {
	_ = m.AddSection("dhcp", "dnsmasq", "dnsmasq")
	_ = m.SetType("dhcp", "dnsmasq", "domainneeded", uci.TypeOption, "1")
	_ = m.SetType("dhcp", "dnsmasq", "localize_queries", uci.TypeOption, "1")
	_ = m.SetType("dhcp", "dnsmasq", "rebind_localhost", uci.TypeOption, "1")
	_ = m.SetType("dhcp", "dnsmasq", "local", uci.TypeOption, "/lan/")
	_ = m.SetType("dhcp", "dnsmasq", "domain", uci.TypeOption, "lan")
	_ = m.SetType("dhcp", "dnsmasq", "expandhosts", uci.TypeOption, "1")
	_ = m.SetType("dhcp", "dnsmasq", "cachesize", uci.TypeOption, "1000")
	_ = m.SetType("dhcp", "dnsmasq", "authoritative", uci.TypeOption, "1")
	_ = m.SetType("dhcp", "dnsmasq", "readethers", uci.TypeOption, "1")
	_ = m.SetType("dhcp", "dnsmasq", "localservice", uci.TypeOption, "1")
	_ = m.SetType("dhcp", "dnsmasq", "ednspacket_max", uci.TypeOption, "1232")
}

// setupMockDHCPData initializes the mock with sample DHCP pool configurations.
func setupMockDHCPData(m *mockDHCPConfigReader) {
	// LAN DHCP
	_ = m.AddSection("dhcp", "lan", "dhcp")
	_ = m.SetType("dhcp", "lan", "interface", uci.TypeOption, "lan")
	_ = m.SetType("dhcp", "lan", "start", uci.TypeOption, "100")
	_ = m.SetType("dhcp", "lan", "limit", uci.TypeOption, "150")
	_ = m.SetType("dhcp", "lan", "leasetime", uci.TypeOption, "12h")
	_ = m.SetType("dhcp", "lan", "ra", uci.TypeOption, "server")
	_ = m.SetType("dhcp", "lan", "ra_default", uci.TypeOption, "1")

	// WAN DHCP (disabled)
	_ = m.AddSection("dhcp", "wan", "dhcp")
	_ = m.SetType("dhcp", "wan", "interface", uci.TypeOption, "wan")
	_ = m.SetType("dhcp", "wan", "ignore", uci.TypeOption, "1")

	// AHWLAN DHCP
	_ = m.AddSection("dhcp", "ahwlan", "dhcp")
	_ = m.SetType("dhcp", "ahwlan", "interface", uci.TypeOption, "ahwlan")
	_ = m.SetType("dhcp", "ahwlan", "start", uci.TypeOption, "100")
	_ = m.SetType("dhcp", "ahwlan", "limit", uci.TypeOption, "150")
	_ = m.SetType("dhcp", "ahwlan", "leasetime", uci.TypeOption, "12h")
	_ = m.SetType("dhcp", "ahwlan", "force", uci.TypeOption, "1")
}

func TestGetDnsmasqConfigWithReader(t *testing.T) {
	mock := newMockDHCPConfigReader()
	setupMockDnsmasqData(mock)

	config, err := GetDnsmasqConfigWithReader(mock)
	if err != nil {
		t.Fatalf("GetDnsmasqConfigWithReader failed: %v", err)
	}

	if config.DomainNeeded != "1" {
		t.Errorf("Expected DomainNeeded=1, got %s", config.DomainNeeded)
	}

	if config.Domain != "lan" {
		t.Errorf("Expected Domain=lan, got %s", config.Domain)
	}

	if config.CacheSize != "1000" {
		t.Errorf("Expected CacheSize=1000, got %s", config.CacheSize)
	}

	if config.EdnsPacketMax != "1232" {
		t.Errorf("Expected EdnsPacketMax=1232, got %s", config.EdnsPacketMax)
	}
}

func TestGetDHCPConfigWithReader(t *testing.T) {
	mock := newMockDHCPConfigReader()
	setupMockDHCPData(mock)

	// Test LAN DHCP
	lanConfig, err := GetDHCPConfigWithReader("lan", mock)
	if err != nil {
		t.Fatalf("GetDHCPConfigWithReader(lan) failed: %v", err)
	}

	if lanConfig.Interface != "lan" {
		t.Errorf("Expected Interface=lan, got %s", lanConfig.Interface)
	}

	if lanConfig.Start != "100" {
		t.Errorf("Expected Start=100, got %s", lanConfig.Start)
	}

	if lanConfig.Limit != "150" {
		t.Errorf("Expected Limit=150, got %s", lanConfig.Limit)
	}

	if lanConfig.LeaseTime != "12h" {
		t.Errorf("Expected LeaseTime=12h, got %s", lanConfig.LeaseTime)
	}

	if lanConfig.Ra != "server" {
		t.Errorf("Expected Ra=server, got %s", lanConfig.Ra)
	}

	// Test WAN DHCP (disabled)
	wanConfig, err := GetDHCPConfigWithReader("wan", mock)
	if err != nil {
		t.Fatalf("GetDHCPConfigWithReader(wan) failed: %v", err)
	}

	if wanConfig.Interface != "wan" {
		t.Errorf("Expected Interface=wan, got %s", wanConfig.Interface)
	}

	if wanConfig.Ignore != "1" {
		t.Errorf("Expected Ignore=1, got %s", wanConfig.Ignore)
	}

	// Test AHWLAN DHCP (with Force option)
	ahwlanConfig, err := GetDHCPConfigWithReader("ahwlan", mock)
	if err != nil {
		t.Fatalf("GetDHCPConfigWithReader(ahwlan) failed: %v", err)
	}

	if ahwlanConfig.Interface != "ahwlan" {
		t.Errorf("Expected Interface=ahwlan, got %s", ahwlanConfig.Interface)
	}

	if ahwlanConfig.Force != "1" {
		t.Errorf("Expected Force=1, got %s", ahwlanConfig.Force)
	}
}

func TestSetDHCPConfigWithReader(t *testing.T) {
	mock := newMockDHCPConfigReader()

	config := &UCIDHCP{
		Interface: "guest",
		Start:     "50",
		Limit:     "100",
		LeaseTime: "6h",
		Ignore:    "0",
		Force:     "1",
	}

	err := SetDHCPConfigWithReader("guest", config, mock)
	if err != nil {
		t.Fatalf("SetDHCPConfigWithReader failed: %v", err)
	}

	// Verify the values were set
	readConfig, err := GetDHCPConfigWithReader("guest", mock)
	if err != nil {
		t.Fatalf("GetDHCPConfigWithReader failed: %v", err)
	}

	if readConfig.Interface != "guest" {
		t.Errorf("Expected Interface=guest, got %s", readConfig.Interface)
	}

	if readConfig.Start != "50" {
		t.Errorf("Expected Start=50, got %s", readConfig.Start)
	}

	if readConfig.Limit != "100" {
		t.Errorf("Expected Limit=100, got %s", readConfig.Limit)
	}

	if readConfig.LeaseTime != "6h" {
		t.Errorf("Expected LeaseTime=6h, got %s", readConfig.LeaseTime)
	}

	if readConfig.Force != "1" {
		t.Errorf("Expected Force=1, got %s", readConfig.Force)
	}
}

func TestSetDHCPConfigWithReader_NilConfig(t *testing.T) {
	mock := newMockDHCPConfigReader()

	err := SetDHCPConfigWithReader("test", nil, mock)
	if err == nil {
		t.Error("Expected error for nil config, got nil")
	}
}

func TestDeleteDHCPConfigWithReader(t *testing.T) {
	mock := newMockDHCPConfigReader()
	setupMockDHCPData(mock)

	// Delete lan section
	err := DeleteDHCPConfigWithReader("lan", mock)
	if err != nil {
		t.Fatalf("DeleteDHCPConfigWithReader failed: %v", err)
	}

	// Verify it's deleted
	config, _ := GetDHCPConfigWithReader("lan", mock)
	if config.Interface != "" {
		t.Error("Expected empty config after deletion")
	}
}

func TestDHCPSectionExistsWithReader(t *testing.T) {
	tests := []struct {
		setup   func(*mockDHCPConfigReader)
		name    string
		section string
		want    bool
	}{
		{
			name:    "section_exists",
			section: "lan",
			setup: func(m *mockDHCPConfigReader) {
				_ = m.AddSection("dhcp", "lan", "dhcp")
				_ = m.SetType("dhcp", "lan", "interface", uci.TypeOption, "lan")
			},
			want: true,
		},
		{
			name:    "section_does_not_exist",
			section: "wan",
			setup:   func(m *mockDHCPConfigReader) {},
			want:    false,
		},
		{
			name:    "section_exists_no_interface",
			section: "guest",
			setup: func(m *mockDHCPConfigReader) {
				_ = m.AddSection("dhcp", "guest", "dhcp")
				_ = m.SetType("dhcp", "guest", "start", uci.TypeOption, "100")
			},
			want: false,
		},
		{
			name:    "section_exists_with_interface",
			section: "ahwlan",
			setup: func(m *mockDHCPConfigReader) {
				_ = m.AddSection("dhcp", "ahwlan", "dhcp")
				_ = m.SetType("dhcp", "ahwlan", "interface", uci.TypeOption, "ahwlan")
				_ = m.SetType("dhcp", "ahwlan", "start", uci.TypeOption, "100")
				_ = m.SetType("dhcp", "ahwlan", "limit", uci.TypeOption, "150")
			},
			want: true,
		},
		{
			name:    "multiple_sections_check_specific",
			section: "lan",
			setup: func(m *mockDHCPConfigReader) {
				_ = m.AddSection("dhcp", "lan", "dhcp")
				_ = m.SetType("dhcp", "lan", "interface", uci.TypeOption, "lan")
				_ = m.AddSection("dhcp", "wan", "dhcp")
				_ = m.SetType("dhcp", "wan", "interface", uci.TypeOption, "wan")
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newMockDHCPConfigReader()
			if tt.setup != nil {
				tt.setup(mock)
			}

			got := DHCPSectionExistsWithReader(tt.section, mock)
			if got != tt.want {
				t.Errorf("DHCPSectionExistsWithReader() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEnableDHCPWithReader(t *testing.T) {
	mock := newMockDHCPConfigReader()
	_ = mock.AddSection("dhcp", "test", "dhcp")
	_ = mock.SetType("dhcp", "test", "ignore", uci.TypeOption, "1")

	err := EnableDHCPWithReader("test", mock)
	if err != nil {
		t.Fatalf("EnableDHCPWithReader failed: %v", err)
	}

	values, ok := mock.Get("dhcp", "test", "ignore")
	if !ok || len(values) == 0 || values[0] != "0" {
		t.Errorf("Expected ignore=0, got %v", values)
	}
}

func TestDisableDHCPWithReader(t *testing.T) {
	mock := newMockDHCPConfigReader()
	_ = mock.AddSection("dhcp", "test", "dhcp")
	_ = mock.SetType("dhcp", "test", "ignore", uci.TypeOption, "0")

	err := DisableDHCPWithReader("test", mock)
	if err != nil {
		t.Fatalf("DisableDHCPWithReader failed: %v", err)
	}

	values, ok := mock.Get("dhcp", "test", "ignore")
	if !ok || len(values) == 0 || values[0] != "1" {
		t.Errorf("Expected ignore=1, got %v", values)
	}
}

func TestIsDHCPEnabledWithReader(t *testing.T) {
	mock := newMockDHCPConfigReader()
	setupMockDHCPData(mock)

	// Test enabled DHCP (lan)
	enabled, err := IsDHCPEnabledWithReader("lan", mock)
	if err != nil {
		t.Fatalf("IsDHCPEnabledWithReader(lan) failed: %v", err)
	}

	if !enabled {
		t.Error("Expected lan DHCP to be enabled")
	}

	// Test disabled DHCP (wan)
	enabled, err = IsDHCPEnabledWithReader("wan", mock)
	if err != nil {
		t.Fatalf("IsDHCPEnabledWithReader(wan) failed: %v", err)
	}

	if enabled {
		t.Error("Expected wan DHCP to be disabled")
	}
}

func TestSetDHCPRangeWithReader(t *testing.T) {
	mock := newMockDHCPConfigReader()
	_ = mock.AddSection("dhcp", "test", "dhcp")

	err := SetDHCPRangeWithReader("test", "200", "50", mock)
	if err != nil {
		t.Fatalf("SetDHCPRangeWithReader failed: %v", err)
	}

	start, _ := mock.Get("dhcp", "test", "start")
	if len(start) == 0 || start[0] != "200" {
		t.Errorf("Expected start=200, got %v", start)
	}

	limit, _ := mock.Get("dhcp", "test", "limit")
	if len(limit) == 0 || limit[0] != "50" {
		t.Errorf("Expected limit=50, got %v", limit)
	}
}

func TestSetDHCPRangeWithReader_InvalidStart(t *testing.T) {
	mock := newMockDHCPConfigReader()

	err := SetDHCPRangeWithReader("test", "invalid", "50", mock)
	if err == nil {
		t.Error("Expected error for invalid start value")
	}
}

func TestSetDHCPRangeWithReader_InvalidLimit(t *testing.T) {
	mock := newMockDHCPConfigReader()

	err := SetDHCPRangeWithReader("test", "100", "invalid", mock)
	if err == nil {
		t.Error("Expected error for invalid limit value")
	}
}

func TestSetDHCPLeaseTimeWithReader(t *testing.T) {
	mock := newMockDHCPConfigReader()
	_ = mock.AddSection("dhcp", "test", "dhcp")

	err := SetDHCPLeaseTimeWithReader("test", "24h", mock)
	if err != nil {
		t.Fatalf("SetDHCPLeaseTimeWithReader failed: %v", err)
	}

	leasetime, _ := mock.Get("dhcp", "test", "leasetime")
	if len(leasetime) == 0 || leasetime[0] != "24h" {
		t.Errorf("Expected leasetime=24h, got %v", leasetime)
	}
}

// mockDHCPConfigReaderWithErrors is a mock that returns errors for testing error paths.
type mockDHCPConfigReaderWithErrors struct{}

// Commit always returns an error for error simulation.
func (m *mockDHCPConfigReaderWithErrors) Commit() error {
	return errors.New("mock error")
}

// ReloadConfig always returns an error for error simulation.
func (m *mockDHCPConfigReaderWithErrors) ReloadConfig() error {
	return errors.New("mock error")
}

func (m *mockDHCPConfigReaderWithErrors) Get(config, section, option string) ([]string, bool) {
	return nil, false
}

func (m *mockDHCPConfigReaderWithErrors) GetSections(config, secType string) ([]string, error) {
	return nil, errors.New("mock error")
}

func (m *mockDHCPConfigReaderWithErrors) SetType(config, section, option string, typ uci.OptionType, values ...string) error {
	return errors.New("mock error")
}

func (m *mockDHCPConfigReaderWithErrors) Del(config, section, option string) error {
	return errors.New("mock error")
}

func (m *mockDHCPConfigReaderWithErrors) AddSection(config, section, typ string) error {
	return errors.New("mock error")
}

func (m *mockDHCPConfigReaderWithErrors) DelSection(config, section string) error {
	return errors.New("mock error")
}

func TestSetDHCPConfigWithReader_ErrorHandling(t *testing.T) {
	mock := &mockDHCPConfigReaderWithErrors{}

	config := &UCIDHCP{
		Interface: "test",
	}

	err := SetDHCPConfigWithReader("test", config, mock)
	if err == nil {
		t.Error("Expected error from SetDHCPConfigWithReader")
	}
}

func TestCommitWithReader(t *testing.T) {
	mock := newMockDHCPConfigReader()
	// Should succeed (no error)
	if err := mock.Commit(); err != nil {
		t.Errorf("Expected Commit to succeed, got error: %v", err)
	}

	mockErr := &mockDHCPConfigReaderWithErrors{}
	// Should fail (return error)
	if err := mockErr.Commit(); err == nil {
		t.Error("Expected Commit to fail, got nil error")
	}
}

func TestDeleteDHCPConfigWithReader_ErrorHandling(t *testing.T) {
	mock := &mockDHCPConfigReaderWithErrors{}

	err := DeleteDHCPConfigWithReader("test", mock)
	if err == nil {
		t.Error("Expected error from DeleteDHCPConfigWithReader")
	}
}

func TestEnableDHCPWithReader_ErrorHandling(t *testing.T) {
	mock := &mockDHCPConfigReaderWithErrors{}

	err := EnableDHCPWithReader("test", mock)
	if err == nil {
		t.Error("Expected error from EnableDHCPWithReader")
	}
}

func TestDisableDHCPWithReader_ErrorHandling(t *testing.T) {
	mock := &mockDHCPConfigReaderWithErrors{}

	err := DisableDHCPWithReader("test", mock)
	if err == nil {
		t.Error("Expected error from DisableDHCPWithReader")
	}
}

func TestSetDHCPRangeWithReader_ErrorHandling(t *testing.T) {
	mock := &mockDHCPConfigReaderWithErrors{}

	err := SetDHCPRangeWithReader("test", "100", "50", mock)
	if err == nil {
		t.Error("Expected error from SetDHCPRangeWithReader")
	}
}

func TestSetDHCPLeaseTimeWithReader_ErrorHandling(t *testing.T) {
	mock := &mockDHCPConfigReaderWithErrors{}

	err := SetDHCPLeaseTimeWithReader("test", "12h", mock)
	if err == nil {
		t.Error("Expected error from SetDHCPLeaseTimeWithReader")
	}
}

func TestCalculateAvailableDHCPStart(t *testing.T) {
	tests := []struct {
		name         string
		networkAddr  string
		subnetMask   string
		nodes        []models.MeshNode
		desiredLimit int
		expectedMin  int
		expectedMax  int
		expectError  bool
	}{
		{
			name:         "no existing ranges",
			nodes:        []models.MeshNode{},
			networkAddr:  "10.41.0.0",
			subnetMask:   "255.255.0.0",
			desiredLimit: 150,
			expectedMin:  100,
			expectedMax:  100,
			expectError:  false,
		},
		{
			name: "one existing range - find gap after",
			nodes: []models.MeshNode{
				{
					UciDhcpStart: sql.NullInt64{Int64: 100, Valid: true},
					UciDhcpLimit: sql.NullInt64{Int64: 150, Valid: true},
				},
			},
			networkAddr:  "10.41.0.0",
			subnetMask:   "255.255.0.0",
			desiredLimit: 100,
			expectedMin:  250,
			expectedMax:  250,
			expectError:  false,
		},
		{
			name: "one existing range - find gap before",
			nodes: []models.MeshNode{
				{
					UciDhcpStart: sql.NullInt64{Int64: 200, Valid: true},
					UciDhcpLimit: sql.NullInt64{Int64: 50, Valid: true},
				},
			},
			networkAddr:  "10.41.0.0",
			subnetMask:   "255.255.0.0",
			desiredLimit: 50,
			expectedMin:  100,
			expectedMax:  100,
			expectError:  false,
		},
		{
			name: "multiple existing ranges",
			nodes: []models.MeshNode{
				{
					UciDhcpStart: sql.NullInt64{Int64: 100, Valid: true},
					UciDhcpLimit: sql.NullInt64{Int64: 50, Valid: true},
				},
				{
					UciDhcpStart: sql.NullInt64{Int64: 200, Valid: true},
					UciDhcpLimit: sql.NullInt64{Int64: 100, Valid: true},
				},
				{
					UciDhcpStart: sql.NullInt64{Int64: 400, Valid: true},
					UciDhcpLimit: sql.NullInt64{Int64: 50, Valid: true},
				},
			},
			networkAddr:  "10.41.0.0",
			subnetMask:   "255.255.0.0",
			desiredLimit: 90,
			expectedMin:  300,
			expectedMax:  300,
			expectError:  false,
		},
		{
			name: "ranges starting from offset 1",
			nodes: []models.MeshNode{
				{
					UciDhcpStart: sql.NullInt64{Int64: 1, Valid: true},
					UciDhcpLimit: sql.NullInt64{Int64: 50, Valid: true},
				},
			},
			networkAddr:  "10.41.0.0",
			subnetMask:   "255.255.0.0",
			desiredLimit: 40,
			expectedMin:  51,
			expectedMax:  100, // Will use 100 as default start
			expectError:  false,
		},
		{
			name: "subnet class C",
			nodes: []models.MeshNode{
				{
					UciDhcpStart: sql.NullInt64{Int64: 10, Valid: true},
					UciDhcpLimit: sql.NullInt64{Int64: 50, Valid: true},
				},
			},
			networkAddr:  "192.168.1.0",
			subnetMask:   "255.255.255.0",
			desiredLimit: 100,
			expectedMin:  100,
			expectedMax:  100,
			expectError:  false,
		},
		{
			name:         "invalid network address",
			nodes:        []models.MeshNode{},
			networkAddr:  "invalid",
			subnetMask:   "255.255.0.0",
			desiredLimit: 100,
			expectError:  true,
		},
		{
			name:         "invalid subnet mask",
			nodes:        []models.MeshNode{},
			networkAddr:  "10.41.0.0",
			subnetMask:   "invalid",
			desiredLimit: 100,
			expectError:  true,
		},
		{
			name:         "zero desired limit",
			nodes:        []models.MeshNode{},
			networkAddr:  "10.41.0.0",
			subnetMask:   "255.255.0.0",
			desiredLimit: 0,
			expectError:  true,
		},
		{
			name:         "negative desired limit",
			nodes:        []models.MeshNode{},
			networkAddr:  "10.41.0.0",
			subnetMask:   "255.255.0.0",
			desiredLimit: -10,
			expectError:  true,
		},
		{
			name: "nodes with invalid DHCP data",
			nodes: []models.MeshNode{
				{
					UciDhcpStart: sql.NullInt64{Valid: false},
					UciDhcpLimit: sql.NullInt64{Valid: false},
				},
				{
					UciDhcpStart: sql.NullInt64{Int64: 100, Valid: true},
					UciDhcpLimit: sql.NullInt64{Int64: 50, Valid: true},
				},
			},
			networkAddr:  "10.41.0.0",
			subnetMask:   "255.255.0.0",
			desiredLimit: 50,
			expectedMin:  150,
			expectedMax:  150,
			expectError:  false,
		},
		{
			name: "nodes with invalid start (null)",
			nodes: []models.MeshNode{
				{
					UciDhcpStart: sql.NullInt64{Valid: false},
					UciDhcpLimit: sql.NullInt64{Int64: 50, Valid: true},
				},
			},
			networkAddr:  "10.41.0.0",
			subnetMask:   "255.255.0.0",
			desiredLimit: 50,
			expectedMin:  100,
			expectedMax:  100,
			expectError:  false,
		},
		{
			name: "nodes with invalid limit (null)",
			nodes: []models.MeshNode{
				{
					UciDhcpStart: sql.NullInt64{Int64: 100, Valid: true},
					UciDhcpLimit: sql.NullInt64{Valid: false},
				},
			},
			networkAddr:  "10.41.0.0",
			subnetMask:   "255.255.0.0",
			desiredLimit: 50,
			expectedMin:  100,
			expectedMax:  100,
			expectError:  false,
		},
		{
			name: "network too small for desired limit",
			nodes: []models.MeshNode{
				{
					UciDhcpStart: sql.NullInt64{Int64: 1, Valid: true},
					UciDhcpLimit: sql.NullInt64{Int64: 200, Valid: true},
				},
			},
			networkAddr:  "192.168.1.0",
			subnetMask:   "255.255.255.0",
			desiredLimit: 100,
			expectedMin:  0,
			expectedMax:  0,
			expectError:  true, // Should fail because there's not enough space
		},
		{
			name: "large subnet with spanning ranges",
			nodes: []models.MeshNode{
				{
					UciDhcpStart: sql.NullInt64{Int64: 256, Valid: true},
					UciDhcpLimit: sql.NullInt64{Int64: 512, Valid: true},
				},
			},
			networkAddr:  "10.41.0.0",
			subnetMask:   "255.255.0.0",
			desiredLimit: 200,
			expectedMin:  768, // After the existing range (256 + 512 = 768)
			expectedMax:  768,
			expectError:  false,
		},
		{
			name: "skip nodes with null start",
			nodes: []models.MeshNode{
				{
					UciDhcpStart: sql.NullInt64{Valid: false},
					UciDhcpLimit: sql.NullInt64{Int64: 150, Valid: true},
				},
				{
					UciDhcpStart: sql.NullInt64{Int64: 200, Valid: true},
					UciDhcpLimit: sql.NullInt64{Int64: 50, Valid: true},
				},
			},
			networkAddr:  "10.41.0.0",
			subnetMask:   "255.255.0.0",
			desiredLimit: 50,
			expectedMin:  100, // Should get 100 since first node is skipped
			expectedMax:  100,
			expectError:  false,
		},
		{
			name: "skip nodes with null limit",
			nodes: []models.MeshNode{
				{
					UciDhcpStart: sql.NullInt64{Int64: 100, Valid: true},
					UciDhcpLimit: sql.NullInt64{Valid: false},
				},
				{
					UciDhcpStart: sql.NullInt64{Int64: 200, Valid: true},
					UciDhcpLimit: sql.NullInt64{Int64: 50, Valid: true},
				},
			},
			networkAddr:  "10.41.0.0",
			subnetMask:   "255.255.0.0",
			desiredLimit: 50,
			expectedMin:  100, // Should get 100 since first node is skipped
			expectedMax:  100,
			expectError:  false,
		},
		{
			name: "mixed valid and invalid nodes",
			nodes: []models.MeshNode{
				{
					UciDhcpStart: sql.NullInt64{Valid: false},
					UciDhcpLimit: sql.NullInt64{Int64: 50, Valid: true},
				},
				{
					UciDhcpStart: sql.NullInt64{Int64: 100, Valid: true},
					UciDhcpLimit: sql.NullInt64{Int64: 50, Valid: true},
				},
				{
					UciDhcpStart: sql.NullInt64{Int64: 200, Valid: true},
					UciDhcpLimit: sql.NullInt64{Valid: false},
				},
			},
			networkAddr:  "10.41.0.0",
			subnetMask:   "255.255.0.0",
			desiredLimit: 40,
			expectedMin:  150, // After the confirmed range (100-149)
			expectedMax:  150,
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, err := CalculateAvailableDHCPStart(tt.nodes, tt.networkAddr, tt.subnetMask, tt.desiredLimit)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error, got nil")
				}

				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if start < tt.expectedMin || start > tt.expectedMax {
				t.Errorf("Expected start between %d and %d, got %d", tt.expectedMin, tt.expectedMax, start)
			}

			// Verify no conflicts with existing ranges
			for _, node := range tt.nodes {
				// Skip nodes with invalid DHCP data (same as the function logic)
				if !node.UciDhcpStart.Valid || !node.UciDhcpLimit.Valid {
					continue
				}

				existingStart := int(node.UciDhcpStart.Int64)
				existingLimit := int(node.UciDhcpLimit.Int64)
				existingEnd := existingStart + existingLimit - 1
				proposedEnd := start + tt.desiredLimit - 1

				if rangesOverlap(start, proposedEnd, existingStart, existingEnd) {
					t.Errorf("Calculated range [%d-%d] conflicts with existing range [%d-%d]",
						start, proposedEnd, existingStart, existingEnd)
				}
			}
		})
	}
}

func TestDHCPRangeOverlap(t *testing.T) {
	tests := []struct {
		name          string
		start1, end1  int
		start2, end2  int
		expectOverlap bool
	}{
		{
			name:          "completely separate ranges",
			start1:        100,
			end1:          149,
			start2:        200,
			end2:          249,
			expectOverlap: false,
		},
		{
			name:          "adjacent ranges no overlap",
			start1:        100,
			end1:          149,
			start2:        150,
			end2:          199,
			expectOverlap: false,
		},
		{
			name:          "partial overlap",
			start1:        100,
			end1:          150,
			start2:        140,
			end2:          180,
			expectOverlap: true,
		},
		{
			name:          "complete containment",
			start1:        100,
			end1:          200,
			start2:        120,
			end2:          150,
			expectOverlap: true,
		},
		{
			name:          "exact same range",
			start1:        100,
			end1:          150,
			start2:        100,
			end2:          150,
			expectOverlap: true,
		},
		{
			name:          "reversed order partial overlap",
			start1:        140,
			end1:          180,
			start2:        100,
			end2:          150,
			expectOverlap: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			overlap := rangesOverlap(tt.start1, tt.end1, tt.start2, tt.end2)
			if overlap != tt.expectOverlap {
				t.Errorf("Expected overlap=%v, got %v for ranges [%d-%d] and [%d-%d]",
					tt.expectOverlap, overlap, tt.start1, tt.end1, tt.start2, tt.end2)
			}

			// Test symmetry
			overlapReverse := rangesOverlap(tt.start2, tt.end2, tt.start1, tt.end1)
			if overlapReverse != tt.expectOverlap {
				t.Errorf("Overlap is not symmetric: [%d-%d] vs [%d-%d]",
					tt.start1, tt.end1, tt.start2, tt.end2)
			}
		})
	}
}

// mustMarshalAddressReservation marshals an AddressReservation or panics.
func mustMarshalAddressReservation(ar *proto.AddressReservation) []byte {
	data, err := ar.MarshalVT()
	if err != nil {
		panic(fmt.Sprintf("failed to marshal AddressReservation: %v", err))
	}

	return data
}

// mockUbusExecutor is a mock implementation of UbusCommandExecutor for testing.
type mockUbusExecutor struct {
	err    error
	output []byte
}

// Execute returns the pre-configured output and error.
func (m *mockUbusExecutor) Execute(args ...string) ([]byte, error) {
	return m.output, m.err
}

func TestDHCPLease_GetMethods(t *testing.T) {
	lease := DHCPLease{
		Expires:  43141,
		Hostname: "TestHost",
		MacAddr:  "AA:BB:CC:DD:EE:FF",
		DUID:     "01aabbccddeeff",
		IPAddr:   "10.41.0.100",
	}

	if lease.GetExpires() != 43141 {
		t.Errorf("GetExpires() = %d, want 43141", lease.GetExpires())
	}

	if lease.GetHostname() != "TestHost" {
		t.Errorf("GetHostname() = %s, want TestHost", lease.GetHostname())
	}

	if lease.GetMacAddr() != "AA:BB:CC:DD:EE:FF" {
		t.Errorf("GetMacAddr() = %s, want AA:BB:CC:DD:EE:FF", lease.GetMacAddr())
	}

	if lease.GetDUID() != "01aabbccddeeff" {
		t.Errorf("GetDUID() = %s, want 01aabbccddeeff", lease.GetDUID())
	}

	if lease.GetIPAddr() != "10.41.0.100" {
		t.Errorf("GetIPAddr() = %s, want 10.41.0.100", lease.GetIPAddr())
	}
}

func TestDHCPLeasesResponse_GetMethods(t *testing.T) {
	dhcpLeases := []DHCPLease{
		{
			Expires:  43141,
			Hostname: "Host1",
			MacAddr:  "AA:BB:CC:DD:EE:FF",
			DUID:     "01aabbccddeeff",
			IPAddr:   "10.41.0.100",
		},
		{
			Expires: 42229,
			MacAddr: "11:22:33:44:55:66",
			DUID:    "01112233445566",
			IPAddr:  "10.41.0.101",
		},
	}

	dhcp6Leases := []DHCPLease{
		{
			Expires:  50000,
			Hostname: "IPv6Host",
			MacAddr:  "77:88:99:AA:BB:CC",
			DUID:     "017788899aabbcc",
			IPAddr:   "fe80::1",
		},
	}

	response := DHCPLeasesResponse{
		DHCPLeases:  dhcpLeases,
		DHCP6Leases: dhcp6Leases,
	}

	// Test GetDHCPLeases
	gotDHCP := response.GetDHCPLeases()
	if len(gotDHCP) != 2 {
		t.Errorf("GetDHCPLeases() returned %d leases, want 2", len(gotDHCP))
	}

	// Test GetDHCP6Leases
	gotDHCP6 := response.GetDHCP6Leases()
	if len(gotDHCP6) != 1 {
		t.Errorf("GetDHCP6Leases() returned %d leases, want 1", len(gotDHCP6))
	}

	// Test GetAllLeases
	gotAll := response.GetAllLeases()
	if len(gotAll) != 3 {
		t.Errorf("GetAllLeases() returned %d leases, want 3", len(gotAll))
	}

	// Verify the order is correct (DHCP4 first, then DHCP6)
	if gotAll[0].GetIPAddr() != "10.41.0.100" {
		t.Errorf("GetAllLeases()[0] IP = %s, want 10.41.0.100", gotAll[0].GetIPAddr())
	}

	if gotAll[2].GetIPAddr() != "fe80::1" {
		t.Errorf("GetAllLeases()[2] IP = %s, want fe80::1", gotAll[2].GetIPAddr())
	}
}

func TestGetCurrentDHCPLeasesWithExecutor(t *testing.T) {
	tests := []struct {
		mockErr       error
		validateFirst func(*testing.T, DHCPLease)
		name          string
		mockOutput    string
		expectDHCP    int
		expectDHCP6   int
		expectErr     bool
	}{
		{
			name: "successful response with leases",
			mockOutput: `{
				"dhcp_leases": [
					{
						"expires": 43141,
						"hostname": "Mac",
						"macaddr": "9A:67:9D:6C:6E:92",
						"duid": "019a679d6c6e92",
						"ipaddr": "10.41.0.180"
					},
					{
						"expires": 42229,
						"macaddr": "26:D2:E5:9A:BF:55",
						"duid": "0126d2e59abf55",
						"ipaddr": "10.41.0.187"
					}
				],
				"dhcp6_leases": []
			}`,
			mockErr:     nil,
			expectErr:   false,
			expectDHCP:  2,
			expectDHCP6: 0,
			validateFirst: func(t *testing.T, lease DHCPLease) {
				if lease.GetHostname() != "Mac" {
					t.Errorf("First lease hostname = %s, want Mac", lease.GetHostname())
				}

				if lease.GetMacAddr() != "9A:67:9D:6C:6E:92" {
					t.Errorf("First lease MAC = %s, want 9A:67:9D:6C:6E:92", lease.GetMacAddr())
				}

				if lease.GetIPAddr() != "10.41.0.180" {
					t.Errorf("First lease IP = %s, want 10.41.0.180", lease.GetIPAddr())
				}

				if lease.GetExpires() != 43141 {
					t.Errorf("First lease expires = %d, want 43141", lease.GetExpires())
				}
			},
		},
		{
			name: "empty leases response",
			mockOutput: `{
				"dhcp_leases": [],
				"dhcp6_leases": []
			}`,
			mockErr:     nil,
			expectErr:   false,
			expectDHCP:  0,
			expectDHCP6: 0,
		},
		{
			name: "with IPv6 leases",
			mockOutput: `{
				"dhcp_leases": [
					{
						"expires": 1000,
						"hostname": "test",
						"macaddr": "AA:BB:CC:DD:EE:FF",
						"duid": "01aabbccddeeff",
						"ipaddr": "10.41.0.1"
					}
				],
				"dhcp6_leases": [
					{
						"expires": 2000,
						"hostname": "testv6",
						"macaddr": "11:22:33:44:55:66",
						"duid": "01112233445566",
						"ipaddr": "fe80::1"
					}
				]
			}`,
			mockErr:     nil,
			expectErr:   false,
			expectDHCP:  1,
			expectDHCP6: 1,
		},
		{
			name:        "ubus command fails",
			mockOutput:  "",
			mockErr:     errors.New("ubus: command failed"),
			expectErr:   true,
			expectDHCP:  0,
			expectDHCP6: 0,
		},
		{
			name:        "invalid JSON response",
			mockOutput:  `{"invalid": json}`,
			mockErr:     nil,
			expectErr:   true,
			expectDHCP:  0,
			expectDHCP6: 0,
		},
		{
			name:        "empty response",
			mockOutput:  "",
			mockErr:     nil,
			expectErr:   true,
			expectDHCP:  0,
			expectDHCP6: 0,
		},
		{
			name: "lease without hostname",
			mockOutput: `{
				"dhcp_leases": [
					{
						"expires": 42229,
						"macaddr": "26:D2:E5:9A:BF:55",
						"duid": "0126d2e59abf55",
						"ipaddr": "10.41.0.187"
					}
				],
				"dhcp6_leases": []
			}`,
			mockErr:     nil,
			expectErr:   false,
			expectDHCP:  1,
			expectDHCP6: 0,
			validateFirst: func(t *testing.T, lease DHCPLease) {
				if lease.GetHostname() != "" {
					t.Errorf("Lease without hostname should have empty string, got %s", lease.GetHostname())
				}

				if lease.GetMacAddr() != "26:D2:E5:9A:BF:55" {
					t.Errorf("MAC address = %s, want 26:D2:E5:9A:BF:55", lease.GetMacAddr())
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockUbusExecutor{
				output: []byte(tt.mockOutput),
				err:    tt.mockErr,
			}

			response, err := GetCurrentDHCPLeasesWithExecutor(mock)

			if tt.expectErr {
				if err == nil {
					t.Error("Expected error but got none")
				}

				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)

				return
			}

			if response == nil {
				t.Fatal("Response is nil")
			}

			if len(response.DHCPLeases) != tt.expectDHCP {
				t.Errorf("DHCPLeases count = %d, want %d", len(response.DHCPLeases), tt.expectDHCP)
			}

			if len(response.DHCP6Leases) != tt.expectDHCP6 {
				t.Errorf("DHCP6Leases count = %d, want %d", len(response.DHCP6Leases), tt.expectDHCP6)
			}

			if tt.validateFirst != nil && len(response.DHCPLeases) > 0 {
				tt.validateFirst(t, response.DHCPLeases[0])
			}
		})
	}
}

func TestDefaultUbusExecutor_Execute(t *testing.T) {
	// This test verifies that DefaultUbusExecutor implements UbusCommandExecutor interface.
	// We can't actually run ubus in the test environment.
	var _ UbusCommandExecutor = &DefaultUbusExecutor{}
	// The actual execution is tested in integration tests
}
