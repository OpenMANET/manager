package network

import (
	"errors"
	"fmt"
	"strconv"
	"testing"

	"github.com/digineo/go-uci/v2"
	"github.com/openmanet/go-alfred"
	proto "github.com/openmanet/openmanetd/internal/api/openmanet/v1"
)

// mockDHCPConfigReader is a mock implementation of DHCPConfigReader for testing.
type mockDHCPConfigReader struct {
	data     map[string]map[string]map[string][]string // config -> section -> option -> values
	sections map[string]map[string]string              // config -> section -> type
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
	_ = m.SetType("dhcp", "dnsmasq", "localise_queries", uci.TypeOption, "1")
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

func (m *mockDHCPConfigReaderWithErrors) Get(config, section, option string) ([]string, bool) {
	return nil, false
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
		records      []alfred.Record
		networkAddr  string
		subnetMask   string
		desiredLimit int
		expectedMin  int // Minimum expected value
		expectedMax  int // Maximum expected value (or exact if min == max)
		expectError  bool
	}{
		{
			name:         "no existing ranges",
			records:      []alfred.Record{},
			networkAddr:  "10.41.0.0",
			subnetMask:   "255.255.0.0",
			desiredLimit: 150,
			expectedMin:  100,
			expectedMax:  100,
			expectError:  false,
		},
		{
			name: "one existing range - find gap after",
			records: []alfred.Record{
				{
					Data: mustMarshalAddressReservation(&proto.AddressReservation{
						UciDhcpStart: "100",
						UciDhcpLimit: "150",
					}),
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
			records: []alfred.Record{
				{
					Data: mustMarshalAddressReservation(&proto.AddressReservation{
						UciDhcpStart: "200",
						UciDhcpLimit: "50",
					}),
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
			records: []alfred.Record{
				{
					Data: mustMarshalAddressReservation(&proto.AddressReservation{
						UciDhcpStart: "100",
						UciDhcpLimit: "50",
					}),
				},
				{
					Data: mustMarshalAddressReservation(&proto.AddressReservation{
						UciDhcpStart: "200",
						UciDhcpLimit: "100",
					}),
				},
				{
					Data: mustMarshalAddressReservation(&proto.AddressReservation{
						UciDhcpStart: "400",
						UciDhcpLimit: "50",
					}),
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
			records: []alfred.Record{
				{
					Data: mustMarshalAddressReservation(&proto.AddressReservation{
						UciDhcpStart: "1",
						UciDhcpLimit: "50",
					}),
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
			records: []alfred.Record{
				{
					Data: mustMarshalAddressReservation(&proto.AddressReservation{
						UciDhcpStart: "10",
						UciDhcpLimit: "50",
					}),
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
			records:      []alfred.Record{},
			networkAddr:  "invalid",
			subnetMask:   "255.255.0.0",
			desiredLimit: 100,
			expectError:  true,
		},
		{
			name:         "invalid subnet mask",
			records:      []alfred.Record{},
			networkAddr:  "10.41.0.0",
			subnetMask:   "invalid",
			desiredLimit: 100,
			expectError:  true,
		},
		{
			name:         "zero desired limit",
			records:      []alfred.Record{},
			networkAddr:  "10.41.0.0",
			subnetMask:   "255.255.0.0",
			desiredLimit: 0,
			expectError:  true,
		},
		{
			name:         "negative desired limit",
			records:      []alfred.Record{},
			networkAddr:  "10.41.0.0",
			subnetMask:   "255.255.0.0",
			desiredLimit: -10,
			expectError:  true,
		},
		{
			name: "records with invalid data",
			records: []alfred.Record{
				{
					Data: []byte("invalid data"),
				},
				{
					Data: mustMarshalAddressReservation(&proto.AddressReservation{
						UciDhcpStart: "100",
						UciDhcpLimit: "50",
					}),
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
			name: "records with invalid start",
			records: []alfred.Record{
				{
					Data: mustMarshalAddressReservation(&proto.AddressReservation{
						UciDhcpStart: "invalid",
						UciDhcpLimit: "50",
					}),
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
			name: "records with invalid limit",
			records: []alfred.Record{
				{
					Data: mustMarshalAddressReservation(&proto.AddressReservation{
						UciDhcpStart: "100",
						UciDhcpLimit: "invalid",
					}),
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
			records: []alfred.Record{
				{
					Data: mustMarshalAddressReservation(&proto.AddressReservation{
						UciDhcpStart: "1",
						UciDhcpLimit: "200",
					}),
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
			records: []alfred.Record{
				{
					Data: mustMarshalAddressReservation(&proto.AddressReservation{
						UciDhcpStart: "256",
						UciDhcpLimit: "512",
					}),
				},
			},
			networkAddr:  "10.41.0.0",
			subnetMask:   "255.255.0.0",
			desiredLimit: 200,
			expectedMin:  768, // After the existing range (256 + 512 = 768)
			expectedMax:  768,
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, err := CalculateAvailableDHCPStart(tt.records, tt.networkAddr, tt.subnetMask, tt.desiredLimit)

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
			for _, record := range tt.records {
				var addrRes proto.AddressReservation
				if err := addrRes.UnmarshalVT(record.Data); err != nil {
					continue
				}

				existingStart, err := strconv.Atoi(addrRes.UciDhcpStart)
				if err != nil {
					continue
				}

				existingLimit, err := strconv.Atoi(addrRes.UciDhcpLimit)
				if err != nil {
					continue
				}

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
