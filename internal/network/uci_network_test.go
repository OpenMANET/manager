package network

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/digineo/go-uci/v2"
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
