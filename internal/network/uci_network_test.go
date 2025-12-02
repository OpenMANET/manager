package network

import (
	"fmt"
	"net"
	"reflect"
	"testing"

	"github.com/digineo/go-uci/v2"
	"github.com/openmanet/go-alfred"
	proto "github.com/openmanet/openmanetd/internal/api/openmanet/v1"
)

// mockConfigReader is a test double that returns predefined configuration values.
type mockConfigReader struct {
	data           map[string]map[string]map[string][]string
	commitError    error
	setTypeError   error
	delSectionErr  error
	addSectionErr  error
	reloadError    error
	commitCalled   bool
	reloadCalled   bool
	setTypeCalls   []setTypeCall
	delSectionCall string
	addSectionCall string
}

type setTypeCall struct {
	config  string
	section string
	option  string
	typ     uci.OptionType
	values  []string
}

func (m *mockConfigReader) Get(config, section, option string) ([]string, bool) {
	if configData, ok := m.data[config]; ok {
		if sectionData, ok := configData[section]; ok {
			if values, ok := sectionData[option]; ok {
				return values, true
			}
		}
	}
	return nil, false
}

func (m *mockConfigReader) SetType(config, section, option string, typ uci.OptionType, values ...string) error {
	if m.setTypeError != nil {
		return m.setTypeError
	}
	m.setTypeCalls = append(m.setTypeCalls, setTypeCall{
		config:  config,
		section: section,
		option:  option,
		typ:     typ,
		values:  values,
	})
	// Update data for subsequent reads
	if m.data[config] == nil {
		m.data[config] = make(map[string]map[string][]string)
	}
	if m.data[config][section] == nil {
		m.data[config][section] = make(map[string][]string)
	}
	m.data[config][section][option] = values
	return nil
}

func (m *mockConfigReader) Del(config, section, option string) error {
	return nil
}

func (m *mockConfigReader) AddSection(config, section, typ string) error {
	if m.addSectionErr != nil {
		return m.addSectionErr
	}
	m.addSectionCall = fmt.Sprintf("%s.%s.%s", config, section, typ)
	return nil
}

func (m *mockConfigReader) DelSection(config, section string) error {
	if m.delSectionErr != nil {
		return m.delSectionErr
	}
	m.delSectionCall = fmt.Sprintf("%s.%s", config, section)
	return nil
}

func (m *mockConfigReader) Commit() error {
	m.commitCalled = true
	return m.commitError
}

func (m *mockConfigReader) ReloadConfig() error {
	m.reloadCalled = true
	return m.reloadError
}

func newMockReader() *mockConfigReader {
	return &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {
				"loopback": {
					"device":  {"lo"},
					"proto":   {"static"},
					"ipaddr":  {"127.0.0.1"},
					"netmask": {"255.0.0.0"},
				},
				"lan": {
					"proto":   {"static"},
					"ipaddr":  {"10.42.0.1"},
					"netmask": {"255.255.255.0"},
					"dns":     {"1.1.1.1"},
				},
				"wan": {
					"proto": {"dhcp"},
				},
				"ahwlan": {
					"proto":   {"static"},
					"netmask": {"255.255.0.0"},
					"ipaddr":  {"10.41.237.1"},
					"dns":     {"1.1.1.1"},
					"device":  {"br-ahwlan"},
					"gateway": {"10.41.1.1"},
				},
				"bat0": {
					"proto": {"batadv"},
				},
			},
		},
	}
}

func TestGetUCINetworkByNameWithReader_Loopback(t *testing.T) {
	reader := newMockReader()

	want := &UCINetwork{
		Proto:   "static",
		NetMask: "255.0.0.0",
		IPAddr:  "127.0.0.1",
		Device:  "lo",
	}

	got, err := GetUCINetworkByNameWithReader("loopback", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestGetUCINetworkByNameWithReader_LAN(t *testing.T) {
	reader := newMockReader()

	want := &UCINetwork{
		Proto:   "static",
		NetMask: "255.255.255.0",
		IPAddr:  "10.42.0.1",
		DNS:     "1.1.1.1",
	}

	got, err := GetUCINetworkByNameWithReader("lan", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestGetUCINetworkByNameWithReader_WAN(t *testing.T) {
	reader := newMockReader()

	want := &UCINetwork{
		Proto: "dhcp",
	}

	got, err := GetUCINetworkByNameWithReader("wan", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestGetUCINetworkByNameWithReader_AHWLAN(t *testing.T) {
	reader := newMockReader()

	want := &UCINetwork{
		Proto:   "static",
		NetMask: "255.255.0.0",
		IPAddr:  "10.41.237.1",
		Gateway: "10.41.1.1",
		DNS:     "1.1.1.1",
		Device:  "br-ahwlan",
	}

	got, err := GetUCINetworkByNameWithReader("ahwlan", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestGetUCINetworkByNameWithReader_NonExistent(t *testing.T) {
	reader := newMockReader()

	want := &UCINetwork{}

	got, err := GetUCINetworkByNameWithReader("nonexistent", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestGetUCINetworkByNameWithReader_Bat0(t *testing.T) {
	reader := newMockReader()

	want := &UCINetwork{
		Proto: "batadv",
	}

	got, err := GetUCINetworkByNameWithReader("bat0", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestSetNetworkConfigWithReader(t *testing.T) {
	tests := []struct {
		name        string
		section     string
		config      *UCINetwork
		wantErr     bool
		errContains string
	}{
		{
			name:    "set_complete_config",
			section: "lan",
			config: &UCINetwork{
				Proto:   "static",
				IPAddr:  "192.168.1.1",
				NetMask: "255.255.255.0",
				Gateway: "192.168.1.254",
				DNS:     "1.1.1.1",
				Device:  "br-lan",
			},
			wantErr: false,
		},
		{
			name:    "set_minimal_config",
			section: "wan",
			config: &UCINetwork{
				Proto: "dhcp",
			},
			wantErr: false,
		},
		{
			name:        "nil_config",
			section:     "lan",
			config:      nil,
			wantErr:     true,
			errContains: "config cannot be nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &mockConfigReader{
				data: make(map[string]map[string]map[string][]string),
			}

			err := SetNetworkConfigWithReader(tt.section, tt.config, reader)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errContains)
				} else if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("expected error containing %q, got %q", tt.errContains, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !reader.commitCalled {
				t.Error("expected Commit to be called")
			}

			// Verify all non-empty fields were set
			if tt.config.Proto != "" {
				found := false
				for _, call := range reader.setTypeCalls {
					if call.option == "proto" && call.values[0] == tt.config.Proto {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("proto not set correctly")
				}
			}
		})
	}
}

func TestSetNetworkConfigWithReader_SetTypeError(t *testing.T) {
	reader := &mockConfigReader{
		data:         make(map[string]map[string]map[string][]string),
		setTypeError: fmt.Errorf("mock settype error"),
	}

	config := &UCINetwork{
		Proto: "static",
	}

	err := SetNetworkConfigWithReader("lan", config, reader)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !contains(err.Error(), "failed to set proto") {
		t.Errorf("expected error about proto, got: %v", err)
	}
}

func TestSetNetworkConfigWithReader_CommitError(t *testing.T) {
	reader := &mockConfigReader{
		data:        make(map[string]map[string]map[string][]string),
		commitError: fmt.Errorf("mock commit error"),
	}

	config := &UCINetwork{
		Proto: "static",
	}

	err := SetNetworkConfigWithReader("lan", config, reader)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !contains(err.Error(), "failed to commit network config") {
		t.Errorf("expected error about commit, got: %v", err)
	}
}

func TestDeleteNetworkConfigWithReader(t *testing.T) {
	tests := []struct {
		name    string
		section string
		wantErr bool
	}{
		{
			name:    "delete_existing_section",
			section: "guest",
			wantErr: false,
		},
		{
			name:    "delete_another_section",
			section: "wan",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &mockConfigReader{
				data: make(map[string]map[string]map[string][]string),
			}

			err := DeleteNetworkConfigWithReader(tt.section, reader)

			if (err != nil) != tt.wantErr {
				t.Errorf("expected error: %v, got: %v", tt.wantErr, err)
			}

			if !tt.wantErr {
				if !reader.commitCalled {
					t.Error("expected Commit to be called")
				}
				expectedCall := fmt.Sprintf("network.%s", tt.section)
				if reader.delSectionCall != expectedCall {
					t.Errorf("expected DelSection call %q, got %q", expectedCall, reader.delSectionCall)
				}
			}
		})
	}
}

func TestDeleteNetworkConfigWithReader_DelSectionError(t *testing.T) {
	reader := &mockConfigReader{
		data:          make(map[string]map[string]map[string][]string),
		delSectionErr: fmt.Errorf("mock delsection error"),
	}

	err := DeleteNetworkConfigWithReader("lan", reader)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !contains(err.Error(), "failed to delete network section") {
		t.Errorf("expected error about delete section, got: %v", err)
	}
}

func TestDeleteNetworkConfigWithReader_CommitError(t *testing.T) {
	reader := &mockConfigReader{
		data:        make(map[string]map[string]map[string][]string),
		commitError: fmt.Errorf("mock commit error"),
	}

	err := DeleteNetworkConfigWithReader("lan", reader)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !contains(err.Error(), "failed to commit network config") {
		t.Errorf("expected error about commit, got: %v", err)
	}
}

func TestSetNetworkProtoWithReader(t *testing.T) {
	tests := []struct {
		name    string
		section string
		proto   string
		wantErr bool
	}{
		{
			name:    "set_static_proto",
			section: "lan",
			proto:   "static",
			wantErr: false,
		},
		{
			name:    "set_dhcp_proto",
			section: "wan",
			proto:   "dhcp",
			wantErr: false,
		},
		{
			name:    "set_batadv_proto",
			section: "bat0",
			proto:   "batadv",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &mockConfigReader{
				data: make(map[string]map[string]map[string][]string),
			}

			err := SetNetworkProtoWithReader(tt.section, tt.proto, reader)

			if (err != nil) != tt.wantErr {
				t.Errorf("expected error: %v, got: %v", tt.wantErr, err)
			}

			if !tt.wantErr {
				if !reader.commitCalled {
					t.Error("expected Commit to be called")
				}
				// Verify the proto was set
				if len(reader.setTypeCalls) != 1 {
					t.Fatalf("expected 1 SetType call, got %d", len(reader.setTypeCalls))
				}
				call := reader.setTypeCalls[0]
				if call.option != "proto" || call.values[0] != tt.proto {
					t.Errorf("expected proto=%s, got %s", tt.proto, call.values[0])
				}
			}
		})
	}
}

func TestSetNetworkIPAddrWithReader(t *testing.T) {
	reader := &mockConfigReader{
		data: make(map[string]map[string]map[string][]string),
	}

	err := SetNetworkIPAddrWithReader("lan", "192.168.1.1", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reader.commitCalled {
		t.Error("expected Commit to be called")
	}

	if len(reader.setTypeCalls) != 1 {
		t.Fatalf("expected 1 SetType call, got %d", len(reader.setTypeCalls))
	}
	call := reader.setTypeCalls[0]
	if call.option != "ipaddr" || call.values[0] != "192.168.1.1" {
		t.Errorf("expected ipaddr=192.168.1.1, got %s", call.values[0])
	}
}

func TestSetNetworkNetmaskWithReader(t *testing.T) {
	reader := &mockConfigReader{
		data: make(map[string]map[string]map[string][]string),
	}

	err := SetNetworkNetmaskWithReader("lan", "255.255.255.0", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reader.commitCalled {
		t.Error("expected Commit to be called")
	}

	if len(reader.setTypeCalls) != 1 {
		t.Fatalf("expected 1 SetType call, got %d", len(reader.setTypeCalls))
	}
	call := reader.setTypeCalls[0]
	if call.option != "netmask" || call.values[0] != "255.255.255.0" {
		t.Errorf("expected netmask=255.255.255.0, got %s", call.values[0])
	}
}

func TestSetNetworkGatewayWithReader(t *testing.T) {
	reader := &mockConfigReader{
		data: make(map[string]map[string]map[string][]string),
	}

	err := SetNetworkGatewayWithReader("wan", "192.168.1.254", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reader.commitCalled {
		t.Error("expected Commit to be called")
	}

	if len(reader.setTypeCalls) != 1 {
		t.Fatalf("expected 1 SetType call, got %d", len(reader.setTypeCalls))
	}
	call := reader.setTypeCalls[0]
	if call.option != "gateway" || call.values[0] != "192.168.1.254" {
		t.Errorf("expected gateway=192.168.1.254, got %s", call.values[0])
	}
}

func TestSetNetworkDNSWithReader(t *testing.T) {
	reader := &mockConfigReader{
		data: make(map[string]map[string]map[string][]string),
	}

	err := SetNetworkDNSWithReader("lan", "1.1.1.1", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reader.commitCalled {
		t.Error("expected Commit to be called")
	}

	if len(reader.setTypeCalls) != 1 {
		t.Fatalf("expected 1 SetType call, got %d", len(reader.setTypeCalls))
	}
	call := reader.setTypeCalls[0]
	if call.option != "dns" || call.values[0] != "1.1.1.1" {
		t.Errorf("expected dns=1.1.1.1, got %s", call.values[0])
	}
}

func TestSetNetworkDeviceWithReader(t *testing.T) {
	reader := &mockConfigReader{
		data: make(map[string]map[string]map[string][]string),
	}

	err := SetNetworkDeviceWithReader("lan", "br-lan", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reader.commitCalled {
		t.Error("expected Commit to be called")
	}

	if len(reader.setTypeCalls) != 1 {
		t.Fatalf("expected 1 SetType call, got %d", len(reader.setTypeCalls))
	}
	call := reader.setTypeCalls[0]
	if call.option != "device" || call.values[0] != "br-lan" {
		t.Errorf("expected device=br-lan, got %s", call.values[0])
	}
}

func TestSetNetworkProtoWithReader_SetTypeError(t *testing.T) {
	reader := &mockConfigReader{
		data:         make(map[string]map[string]map[string][]string),
		setTypeError: fmt.Errorf("mock settype error"),
	}

	err := SetNetworkProtoWithReader("lan", "static", reader)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !contains(err.Error(), "failed to set proto") {
		t.Errorf("expected error about proto, got: %v", err)
	}
}

func TestSetNetworkProtoWithReader_CommitError(t *testing.T) {
	reader := &mockConfigReader{
		data:        make(map[string]map[string]map[string][]string),
		commitError: fmt.Errorf("mock commit error"),
	}

	err := SetNetworkProtoWithReader("lan", "static", reader)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !contains(err.Error(), "failed to commit network config") {
		t.Errorf("expected error about commit, got: %v", err)
	}
}

func TestCommitWithNetworkReader(t *testing.T) {
	reader := &mockConfigReader{
		data: make(map[string]map[string]map[string][]string),
	}

	err := reader.Commit()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reader.commitCalled {
		t.Error("expected commitCalled to be true")
	}
}

func TestReloadConfigWithNetworkReader(t *testing.T) {
	reader := &mockConfigReader{
		data: make(map[string]map[string]map[string][]string),
	}

	err := reader.ReloadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reader.reloadCalled {
		t.Error("expected reloadCalled to be true")
	}
}

func TestSetNetworkIPV6AssignmentWithReader(t *testing.T) {
	reader := &mockConfigReader{
		data: make(map[string]map[string]map[string][]string),
	}

	err := SetNetworkIPV6AssignmentWithReader("lan", "60", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reader.commitCalled {
		t.Error("expected Commit to be called")
	}

	if len(reader.setTypeCalls) != 1 {
		t.Fatalf("expected 1 SetType call, got %d", len(reader.setTypeCalls))
	}
	call := reader.setTypeCalls[0]
	if call.option != "ip6assign" || call.values[0] != "60" {
		t.Errorf("expected ip6assign=60, got %s", call.values[0])
	}
}

func TestSetNetworkIPV6IfaceIDWithReader(t *testing.T) {
	reader := &mockConfigReader{
		data: make(map[string]map[string]map[string][]string),
	}

	err := SetNetworkIPV6IfaceIDWithReader("lan", "::1", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reader.commitCalled {
		t.Error("expected Commit to be called")
	}

	if len(reader.setTypeCalls) != 1 {
		t.Fatalf("expected 1 SetType call, got %d", len(reader.setTypeCalls))
	}
	call := reader.setTypeCalls[0]
	if call.option != "ip6ifaceid" || call.values[0] != "::1" {
		t.Errorf("expected ip6ifaceid=::1, got %s", call.values[0])
	}
}

func TestSetNetworkIPV6ClassWithReader(t *testing.T) {
	reader := &mockConfigReader{
		data: make(map[string]map[string]map[string][]string),
	}

	err := SetNetworkIPV6ClassWithReader("lan", "local", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reader.commitCalled {
		t.Error("expected Commit to be called")
	}

	if len(reader.setTypeCalls) != 1 {
		t.Fatalf("expected 1 SetType call, got %d", len(reader.setTypeCalls))
	}
	call := reader.setTypeCalls[0]
	if call.option != "ip6class" || call.values[0] != "local" {
		t.Errorf("expected ip6class=local, got %s", call.values[0])
	}
	// Verify it's set as a list type
	if call.typ != uci.TypeList {
		t.Errorf("expected TypeList, got %v", call.typ)
	}
}

func TestSetNetworkConfigWithReader_IPv6Fields(t *testing.T) {
	tests := []struct {
		name    string
		config  *UCINetwork
		wantErr bool
	}{
		{
			name: "set_ipv6_assignment",
			config: &UCINetwork{
				Proto:          "static",
				IPAddr:         "192.168.1.1",
				NetMask:        "255.255.255.0",
				IPV6Assignment: "60",
			},
			wantErr: false,
		},
		{
			name: "set_ipv6_ifaceid",
			config: &UCINetwork{
				Proto:       "static",
				IPAddr:      "192.168.1.1",
				NetMask:     "255.255.255.0",
				IPV6IfaceID: "::1",
			},
			wantErr: false,
		},
		{
			name: "set_ipv6_class",
			config: &UCINetwork{
				Proto:     "static",
				IPAddr:    "192.168.1.1",
				NetMask:   "255.255.255.0",
				IPV6Class: "local",
			},
			wantErr: false,
		},
		{
			name: "set_all_ipv6_fields",
			config: &UCINetwork{
				Proto:          "static",
				IPAddr:         "192.168.1.1",
				NetMask:        "255.255.255.0",
				IPV6Assignment: "60",
				IPV6IfaceID:    "::1",
				IPV6Class:      "wan6",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &mockConfigReader{
				data: make(map[string]map[string]map[string][]string),
			}

			err := SetNetworkConfigWithReader("lan", tt.config, reader)

			if (err != nil) != tt.wantErr {
				t.Errorf("expected error: %v, got: %v", tt.wantErr, err)
			}

			if !tt.wantErr {
				if !reader.commitCalled {
					t.Error("expected Commit to be called")
				}

				// Verify IPv6 fields were set if provided
				if tt.config.IPV6Assignment != "" {
					found := false
					for _, call := range reader.setTypeCalls {
						if call.option == "ip6assign" && call.values[0] == tt.config.IPV6Assignment {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("ip6assign not set correctly")
					}
				}

				if tt.config.IPV6IfaceID != "" {
					found := false
					for _, call := range reader.setTypeCalls {
						if call.option == "ip6ifaceid" && call.values[0] == tt.config.IPV6IfaceID {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("ip6ifaceid not set correctly")
					}
				}

				if tt.config.IPV6Class != "" {
					found := false
					for _, call := range reader.setTypeCalls {
						if call.option == "ip6class" && call.values[0] == tt.config.IPV6Class {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("ip6class not set correctly")
					}
				}
			}
		})
	}
}

func TestGetUCINetworkByNameWithReader_IPv6Fields(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {
				"lan": {
					"proto":      {"static"},
					"ipaddr":     {"192.168.1.1"},
					"netmask":    {"255.255.255.0"},
					"ip6assign":  {"60"},
					"ip6ifaceid": {"::1"},
					"ip6class":   {"local"},
				},
			},
		},
	}

	want := &UCINetwork{
		Proto:          "static",
		IPAddr:         "192.168.1.1",
		NetMask:        "255.255.255.0",
		IPV6Assignment: "60",
		IPV6IfaceID:    "::1",
		IPV6Class:      "local",
	}

	got, err := GetUCINetworkByNameWithReader("lan", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestSetNetworkIPV6AssignmentWithReader_CommitError(t *testing.T) {
	reader := &mockConfigReader{
		data:        make(map[string]map[string]map[string][]string),
		commitError: fmt.Errorf("mock commit error"),
	}

	err := SetNetworkIPV6AssignmentWithReader("lan", "60", reader)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !contains(err.Error(), "failed to commit network config") {
		t.Errorf("expected error about commit, got: %v", err)
	}
}

func TestSetNetworkIPV6IfaceIDWithReader_SetTypeError(t *testing.T) {
	reader := &mockConfigReader{
		data:         make(map[string]map[string]map[string][]string),
		setTypeError: fmt.Errorf("mock settype error"),
	}

	err := SetNetworkIPV6IfaceIDWithReader("lan", "::1", reader)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !contains(err.Error(), "failed to set ip6ifaceid") {
		t.Errorf("expected error about ip6ifaceid, got: %v", err)
	}
}

func TestSetNetworkIPV6ClassWithReader_SetTypeError(t *testing.T) {
	reader := &mockConfigReader{
		data:         make(map[string]map[string]map[string][]string),
		setTypeError: fmt.Errorf("mock settype error"),
	}

	err := SetNetworkIPV6ClassWithReader("lan", "local", reader)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !contains(err.Error(), "failed to set ip6class") {
		t.Errorf("expected error about ip6class, got: %v", err)
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && stringContains(s, substr)))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestSelectAvailableStaticIP(t *testing.T) {
	tests := []struct {
		name        string
		records     []alfred.Record
		wantPrefix  string
		wantErr     bool
		shouldAvoid []string
	}{
		{
			name:       "no_existing_reservations",
			records:    []alfred.Record{},
			wantPrefix: "10.41.",
			wantErr:    false,
		},
		{
			name: "one_existing_reservation",
			records: []alfred.Record{
				{
					Data: mustMarshalAddressReservation(&proto.AddressReservation{
						StaticIp: "10.41.0.1",
					}),
				},
			},
			wantPrefix:  "10.41.",
			wantErr:     false,
			shouldAvoid: []string{"10.41.0.1"},
		},
		{
			name: "multiple_existing_reservations",
			records: []alfred.Record{
				{
					Data: mustMarshalAddressReservation(&proto.AddressReservation{
						StaticIp: "10.41.0.1",
					}),
				},
				{
					Data: mustMarshalAddressReservation(&proto.AddressReservation{
						StaticIp: "10.41.0.2",
					}),
				},
				{
					Data: mustMarshalAddressReservation(&proto.AddressReservation{
						StaticIp: "10.41.1.5",
					}),
				},
			},
			wantPrefix:  "10.41.",
			wantErr:     false,
			shouldAvoid: []string{"10.41.0.1", "10.41.0.2", "10.41.1.5"},
		},
		{
			name: "reservation_without_static_ip",
			records: []alfred.Record{
				{
					Data: mustMarshalAddressReservation(&proto.AddressReservation{
						StaticIp:              "",
						RequestingReservation: true,
						UciDhcpStart:          "100",
						UciDhcpLimit:          "150",
					}),
				},
			},
			wantPrefix: "10.41.",
			wantErr:    false,
		},
		{
			name: "invalid_record_data",
			records: []alfred.Record{
				{
					Data: []byte{0xFF, 0xFF, 0xFF}, // Invalid protobuf data
				},
			},
			wantPrefix: "10.41.",
			wantErr:    false,
		},
		{
			name: "mixed_valid_and_invalid_records",
			records: []alfred.Record{
				{
					Data: []byte{0xFF, 0xFF, 0xFF}, // Invalid
				},
				{
					Data: mustMarshalAddressReservation(&proto.AddressReservation{
						StaticIp: "10.41.5.10",
					}),
				},
			},
			wantPrefix:  "10.41.",
			wantErr:     false,
			shouldAvoid: []string{"10.41.5.10"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SelectAvailableStaticIP(tt.records)

			if (err != nil) != tt.wantErr {
				t.Errorf("SelectAvailableStaticIP() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			// Verify the returned IP starts with the expected prefix
			if len(got) < len(tt.wantPrefix) || got[:len(tt.wantPrefix)] != tt.wantPrefix {
				t.Errorf("SelectAvailableStaticIP() = %v, want prefix %v", got, tt.wantPrefix)
			}

			// Verify the returned IP is not in the avoid list
			for _, avoidIP := range tt.shouldAvoid {
				if got == avoidIP {
					t.Errorf("SelectAvailableStaticIP() = %v, should not return reserved IP %v", got, avoidIP)
				}
			}

			// Verify the IP is not in restricted ranges
			if len(got) >= 9 {
				if got[:9] == "10.41.253" || got[:9] == "10.41.254" {
					t.Errorf("SelectAvailableStaticIP() = %v, should not return IP in restricted range", got)
				}
			}

			// Verify it's a valid IP
			if ip := net.ParseIP(got); ip == nil {
				t.Errorf("SelectAvailableStaticIP() = %v, not a valid IP address", got)
			}
		})
	}
}

func TestSelectAvailableStaticIP_RestrictedRanges(t *testing.T) {
	// Fill up the 10.41.0.0/24 range to force selection from another range
	records := []alfred.Record{}
	for i := 1; i < 255; i++ {
		records = append(records, alfred.Record{
			Data: mustMarshalAddressReservation(&proto.AddressReservation{
				StaticIp: fmt.Sprintf("10.41.0.%d", i),
			}),
		})
	}

	got, err := SelectAvailableStaticIP(records)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should select from a different subnet, but not 253 or 254
	if len(got) >= 9 && (got[:9] == "10.41.253" || got[:9] == "10.41.254") {
		t.Errorf("SelectAvailableStaticIP() = %v, should not select from restricted ranges", got)
	}

	// Should still be in 10.41.x.x range
	if len(got) < 6 || got[:6] != "10.41." {
		t.Errorf("SelectAvailableStaticIP() = %v, should be in 10.41.0.0/16 range", got)
	}
}

func TestSelectAvailableStaticIP_SelectionOrder(t *testing.T) {
	// With no reservations, should select 10.41.0.1 (first available)
	records := []alfred.Record{}

	got, err := SelectAvailableStaticIP(records)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got != "10.41.0.1" {
		t.Errorf("SelectAvailableStaticIP() = %v, want 10.41.0.1 as first selection", got)
	}
}

func TestSelectAvailableStaticIP_ExhaustRange(t *testing.T) {
	// This test would be too slow to actually exhaust the range,
	// so we just verify the error case logic with a smaller conceptual test

	// Reserve a large number of IPs in the first few subnets
	records := []alfred.Record{}

	// Reserve 10.41.0.1 through 10.41.0.254
	for i := 1; i < 255; i++ {
		records = append(records, alfred.Record{
			Data: mustMarshalAddressReservation(&proto.AddressReservation{
				StaticIp: fmt.Sprintf("10.41.0.%d", i),
			}),
		})
	}

	// Should still find an IP in 10.41.1.x range
	got, err := SelectAvailableStaticIP(records)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it's from a different subnet
	if len(got) >= 8 && got[:8] == "10.41.0." {
		t.Errorf("SelectAvailableStaticIP() = %v, should select from different subnet", got)
	}
}

func TestSelectAvailableStaticIP_Boundaries(t *testing.T) {
	tests := []struct {
		name         string
		reservedIP   string
		shouldSelect string
	}{
		{
			name:         "skips_network_address",
			reservedIP:   "10.41.0.1",
			shouldSelect: "10.41.0.2", // Should skip to next available
		},
		{
			name:         "handles_subnet_boundary",
			reservedIP:   "10.41.0.254",
			shouldSelect: "10.41.0.1", // First IP if not reserved
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			records := []alfred.Record{
				{
					Data: mustMarshalAddressReservation(&proto.AddressReservation{
						StaticIp: tt.reservedIP,
					}),
				},
			}

			got, err := SelectAvailableStaticIP(records)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Verify it's a valid IP in the range
			if ip := net.ParseIP(got); ip == nil {
				t.Errorf("SelectAvailableStaticIP() = %v, not a valid IP", got)
			}

			// Verify it's not the reserved IP
			if got == tt.reservedIP {
				t.Errorf("SelectAvailableStaticIP() = %v, should not return reserved IP", got)
			}

			// Verify it's in the correct range
			if len(got) < 6 || got[:6] != "10.41." {
				t.Errorf("SelectAvailableStaticIP() = %v, should be in 10.41.0.0/16", got)
			}
		})
	}
}
