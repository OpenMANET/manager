package firewall

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
	delError       error
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
	if m.delError != nil {
		return m.delError
	}
	// Delete the option from data
	if configData, ok := m.data[config]; ok {
		if sectionData, ok := configData[section]; ok {
			delete(sectionData, option)
		}
	}
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
			"firewall": {
				"defaults": {
					"input":            {"REJECT"},
					"output":           {"ACCEPT"},
					"forward":          {"REJECT"},
					"synflood_protect": {"1"},
				},
				"lan": {
					"name":    {"lan"},
					"input":   {"ACCEPT"},
					"output":  {"ACCEPT"},
					"forward": {"ACCEPT"},
					"network": {"lan"},
					"masq":    {"1"},
					"mtu_fix": {"1"},
				},
				"wan": {
					"name":    {"wan"},
					"input":   {"ACCEPT"},
					"output":  {"ACCEPT"},
					"forward": {"ACCEPT"},
					"network": {"wan", "wan6"},
				},
				"ahwlan": {
					"name":    {"ahwlan"},
					"input":   {"ACCEPT"},
					"output":  {"ACCEPT"},
					"forward": {"ACCEPT"},
					"network": {"ahwlan", "tailscale0"},
				},
				"mmrouter": {
					"src":     {"ahwlan"},
					"dest":    {"lan"},
					"enabled": {"1"},
				},
				"lan_to_wan": {
					"src":     {"lan"},
					"dest":    {"wan"},
					"enabled": {"0"},
				},
				"Allow-Ping": {
					"name":      {"Allow-Ping"},
					"src":       {"wan"},
					"proto":     {"icmp"},
					"icmp_type": {"echo-request"},
					"family":    {"ipv4"},
					"target":    {"ACCEPT"},
				},
				"Allow-DHCP-Renew": {
					"name":      {"Allow-DHCP-Renew"},
					"src":       {"wan"},
					"proto":     {"udp"},
					"dest_port": {"68"},
					"target":    {"ACCEPT"},
					"family":    {"ipv4"},
				},
				"Allow-ICMPv6-Input": {
					"name":      {"Allow-ICMPv6-Input"},
					"src":       {"wan"},
					"proto":     {"icmp"},
					"icmp_type": {"echo-request", "echo-reply", "destination-unreachable"},
					"limit":     {"1000/sec"},
					"family":    {"ipv6"},
					"target":    {"ACCEPT"},
				},
			},
		},
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || 
		(len(s) > 0 && len(substr) > 0 && containsHelper(s, substr)))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Tests for GetFirewallDefaultsWithReader
func TestGetFirewallDefaultsWithReader(t *testing.T) {
	reader := newMockReader()

	want := &UCIFirewallDefaults{
		Input:           "REJECT",
		Output:          "ACCEPT",
		Forward:         "REJECT",
		SynfloodProtect: "1",
	}

	got, err := GetFirewallDefaultsWithReader(reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestGetFirewallDefaultsWithReader_Empty(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"firewall": {},
		},
	}

	want := &UCIFirewallDefaults{}

	got, err := GetFirewallDefaultsWithReader(reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// Tests for SetFirewallDefaultsWithReader
func TestSetFirewallDefaultsWithReader(t *testing.T) {
	tests := []struct {
		name        string
		config      *UCIFirewallDefaults
		wantErr     bool
		errContains string
	}{
		{
			name: "set_complete_config",
			config: &UCIFirewallDefaults{
				Input:           "DROP",
				Output:          "ACCEPT",
				Forward:         "DROP",
				SynfloodProtect: "1",
				DisableIPV6:     "0",
				DropInvalid:     "1",
			},
			wantErr: false,
		},
		{
			name: "set_minimal_config",
			config: &UCIFirewallDefaults{
				Input: "ACCEPT",
			},
			wantErr: false,
		},
		{
			name:        "nil_config",
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

			err := SetFirewallDefaultsWithReader(tt.config, reader)

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
			if tt.config.Input != "" {
				found := false
				for _, call := range reader.setTypeCalls {
					if call.option == "input" && call.values[0] == tt.config.Input {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("input not set correctly")
				}
			}
		})
	}
}

func TestSetFirewallDefaultsWithReader_SetTypeError(t *testing.T) {
	reader := &mockConfigReader{
		data:         make(map[string]map[string]map[string][]string),
		setTypeError: fmt.Errorf("mock settype error"),
	}

	config := &UCIFirewallDefaults{
		Input: "ACCEPT",
	}

	err := SetFirewallDefaultsWithReader(config, reader)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !contains(err.Error(), "failed to set input") {
		t.Errorf("expected error about input, got: %v", err)
	}
}

func TestSetFirewallDefaultsWithReader_CommitError(t *testing.T) {
	reader := &mockConfigReader{
		data:        make(map[string]map[string]map[string][]string),
		commitError: fmt.Errorf("mock commit error"),
	}

	config := &UCIFirewallDefaults{
		Input: "ACCEPT",
	}

	err := SetFirewallDefaultsWithReader(config, reader)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !contains(err.Error(), "failed to commit firewall config") {
		t.Errorf("expected error about commit, got: %v", err)
	}
}

// Tests for GetFirewallZoneWithReader
func TestGetFirewallZoneWithReader_LAN(t *testing.T) {
	reader := newMockReader()

	want := &UCIFirewallZone{
		Name:    "lan",
		Input:   "ACCEPT",
		Output:  "ACCEPT",
		Forward: "ACCEPT",
		Network: []string{"lan"},
		Masq:    "1",
		MtuFix:  "1",
	}

	got, err := GetFirewallZoneWithReader("lan", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestGetFirewallZoneWithReader_WAN(t *testing.T) {
	reader := newMockReader()

	want := &UCIFirewallZone{
		Name:    "wan",
		Input:   "ACCEPT",
		Output:  "ACCEPT",
		Forward: "ACCEPT",
		Network: []string{"wan", "wan6"},
	}

	got, err := GetFirewallZoneWithReader("wan", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestGetFirewallZoneWithReader_AHWLAN(t *testing.T) {
	reader := newMockReader()

	want := &UCIFirewallZone{
		Name:    "ahwlan",
		Input:   "ACCEPT",
		Output:  "ACCEPT",
		Forward: "ACCEPT",
		Network: []string{"ahwlan", "tailscale0"},
	}

	got, err := GetFirewallZoneWithReader("ahwlan", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestGetFirewallZoneWithReader_NonExistent(t *testing.T) {
	reader := newMockReader()

	want := &UCIFirewallZone{}

	got, err := GetFirewallZoneWithReader("nonexistent", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// Tests for SetFirewallZoneWithReader
func TestSetFirewallZoneWithReader(t *testing.T) {
	tests := []struct {
		name        string
		section     string
		config      *UCIFirewallZone
		wantErr     bool
		errContains string
	}{
		{
			name:    "set_complete_config",
			section: "guest",
			config: &UCIFirewallZone{
				Name:    "guest",
				Input:   "ACCEPT",
				Output:  "ACCEPT",
				Forward: "REJECT",
				Network: []string{"guest"},
				Masq:    "1",
				MtuFix:  "1",
			},
			wantErr: false,
		},
		{
			name:    "set_minimal_config",
			section: "dmz",
			config: &UCIFirewallZone{
				Name: "dmz",
			},
			wantErr: false,
		},
		{
			name:    "set_with_multiple_networks",
			section: "wan",
			config: &UCIFirewallZone{
				Name:    "wan",
				Input:   "REJECT",
				Output:  "ACCEPT",
				Forward: "REJECT",
				Network: []string{"wan", "wan6"},
			},
			wantErr: false,
		},
		{
			name:        "nil_config",
			section:     "guest",
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

			err := SetFirewallZoneWithReader(tt.section, tt.config, reader)

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
			if tt.config.Name != "" {
				found := false
				for _, call := range reader.setTypeCalls {
					if call.option == "name" && call.values[0] == tt.config.Name {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("name not set correctly")
				}
			}
		})
	}
}

func TestSetFirewallZoneWithReader_SetTypeError(t *testing.T) {
	reader := &mockConfigReader{
		data:         make(map[string]map[string]map[string][]string),
		setTypeError: fmt.Errorf("mock settype error"),
	}

	config := &UCIFirewallZone{
		Name: "guest",
	}

	err := SetFirewallZoneWithReader("guest", config, reader)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !contains(err.Error(), "failed to set name") {
		t.Errorf("expected error about name, got: %v", err)
	}
}

func TestSetFirewallZoneWithReader_CommitError(t *testing.T) {
	reader := &mockConfigReader{
		data:        make(map[string]map[string]map[string][]string),
		commitError: fmt.Errorf("mock commit error"),
	}

	config := &UCIFirewallZone{
		Name: "guest",
	}

	err := SetFirewallZoneWithReader("guest", config, reader)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !contains(err.Error(), "failed to commit firewall config") {
		t.Errorf("expected error about commit, got: %v", err)
	}
}

// Tests for DeleteFirewallZoneWithReader
func TestDeleteFirewallZoneWithReader(t *testing.T) {
	tests := []struct {
		name    string
		section string
		wantErr bool
	}{
		{
			name:    "delete_existing_zone",
			section: "guest",
			wantErr: false,
		},
		{
			name:    "delete_another_zone",
			section: "dmz",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &mockConfigReader{
				data: make(map[string]map[string]map[string][]string),
			}

			err := DeleteFirewallZoneWithReader(tt.section, reader)

			if (err != nil) != tt.wantErr {
				t.Errorf("expected error: %v, got: %v", tt.wantErr, err)
			}

			if !tt.wantErr {
				if !reader.commitCalled {
					t.Error("expected Commit to be called")
				}
				expectedCall := fmt.Sprintf("firewall.%s", tt.section)
				if reader.delSectionCall != expectedCall {
					t.Errorf("expected DelSection call %q, got %q", expectedCall, reader.delSectionCall)
				}
			}
		})
	}
}

func TestDeleteFirewallZoneWithReader_DelSectionError(t *testing.T) {
	reader := &mockConfigReader{
		data:          make(map[string]map[string]map[string][]string),
		delSectionErr: fmt.Errorf("mock delsection error"),
	}

	err := DeleteFirewallZoneWithReader("guest", reader)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !contains(err.Error(), "failed to delete zone section") {
		t.Errorf("expected error about delete zone section, got: %v", err)
	}
}

func TestDeleteFirewallZoneWithReader_CommitError(t *testing.T) {
	reader := &mockConfigReader{
		data:        make(map[string]map[string]map[string][]string),
		commitError: fmt.Errorf("mock commit error"),
	}

	err := DeleteFirewallZoneWithReader("guest", reader)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !contains(err.Error(), "failed to commit firewall config") {
		t.Errorf("expected error about commit, got: %v", err)
	}
}

// Tests for FirewallZoneExistsWithReader
func TestFirewallZoneExistsWithReader(t *testing.T) {
	tests := []struct {
		name    string
		section string
		data    map[string]map[string]map[string][]string
		want    bool
	}{
		{
			name:    "zone_exists",
			section: "lan",
			data: map[string]map[string]map[string][]string{
				"firewall": {
					"lan": {
						"name": []string{"lan"},
					},
				},
			},
			want: true,
		},
		{
			name:    "zone_does_not_exist",
			section: "guest",
			data: map[string]map[string]map[string][]string{
				"firewall": {
					"lan": {
						"name": []string{"lan"},
					},
				},
			},
			want: false,
		},
		{
			name:    "empty_config",
			section: "lan",
			data:    map[string]map[string]map[string][]string{},
			want:    false,
		},
		{
			name:    "zone_exists_with_name",
			section: "ahwlan",
			data: map[string]map[string]map[string][]string{
				"firewall": {
					"ahwlan": {
						"name":    []string{"ahwlan"},
						"network": []string{"ahwlan"},
					},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &mockConfigReader{
				data: tt.data,
			}

			got := FirewallZoneExistsWithReader(tt.section, reader)
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// Tests for GetFirewallForwardingWithReader
func TestGetFirewallForwardingWithReader(t *testing.T) {
	reader := newMockReader()

	want := &UCIFirewallForwarding{
		Src:     "ahwlan",
		Dest:    "lan",
		Enabled: "1",
	}

	got, err := GetFirewallForwardingWithReader("mmrouter", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestGetFirewallForwardingWithReader_Disabled(t *testing.T) {
	reader := newMockReader()

	want := &UCIFirewallForwarding{
		Src:     "lan",
		Dest:    "wan",
		Enabled: "0",
	}

	got, err := GetFirewallForwardingWithReader("lan_to_wan", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestGetFirewallForwardingWithReader_NonExistent(t *testing.T) {
	reader := newMockReader()

	want := &UCIFirewallForwarding{}

	got, err := GetFirewallForwardingWithReader("nonexistent", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// Tests for SetFirewallForwardingWithReader
func TestSetFirewallForwardingWithReader(t *testing.T) {
	tests := []struct {
		name        string
		section     string
		config      *UCIFirewallForwarding
		wantErr     bool
		errContains string
	}{
		{
			name:    "set_complete_config",
			section: "guest_to_wan",
			config: &UCIFirewallForwarding{
				Src:     "guest",
				Dest:    "wan",
				Enabled: "1",
			},
			wantErr: false,
		},
		{
			name:    "set_minimal_config",
			section: "dmz_fwd",
			config: &UCIFirewallForwarding{
				Src:  "dmz",
				Dest: "lan",
			},
			wantErr: false,
		},
		{
			name:        "nil_config",
			section:     "test",
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

			err := SetFirewallForwardingWithReader(tt.section, tt.config, reader)

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
			if tt.config.Src != "" {
				found := false
				for _, call := range reader.setTypeCalls {
					if call.option == "src" && call.values[0] == tt.config.Src {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("src not set correctly")
				}
			}
		})
	}
}

func TestSetFirewallForwardingWithReader_SetTypeError(t *testing.T) {
	reader := &mockConfigReader{
		data:         make(map[string]map[string]map[string][]string),
		setTypeError: fmt.Errorf("mock settype error"),
	}

	config := &UCIFirewallForwarding{
		Src:  "lan",
		Dest: "wan",
	}

	err := SetFirewallForwardingWithReader("test", config, reader)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !contains(err.Error(), "failed to set src") {
		t.Errorf("expected error about src, got: %v", err)
	}
}

func TestSetFirewallForwardingWithReader_CommitError(t *testing.T) {
	reader := &mockConfigReader{
		data:        make(map[string]map[string]map[string][]string),
		commitError: fmt.Errorf("mock commit error"),
	}

	config := &UCIFirewallForwarding{
		Src:  "lan",
		Dest: "wan",
	}

	err := SetFirewallForwardingWithReader("test", config, reader)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !contains(err.Error(), "failed to commit firewall config") {
		t.Errorf("expected error about commit, got: %v", err)
	}
}

// Tests for DeleteFirewallForwardingWithReader
func TestDeleteFirewallForwardingWithReader(t *testing.T) {
	tests := []struct {
		name    string
		section string
		wantErr bool
	}{
		{
			name:    "delete_existing_forwarding",
			section: "guest_fwd",
			wantErr: false,
		},
		{
			name:    "delete_another_forwarding",
			section: "dmz_fwd",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &mockConfigReader{
				data: make(map[string]map[string]map[string][]string),
			}

			err := DeleteFirewallForwardingWithReader(tt.section, reader)

			if (err != nil) != tt.wantErr {
				t.Errorf("expected error: %v, got: %v", tt.wantErr, err)
			}

			if !tt.wantErr {
				if !reader.commitCalled {
					t.Error("expected Commit to be called")
				}
				expectedCall := fmt.Sprintf("firewall.%s", tt.section)
				if reader.delSectionCall != expectedCall {
					t.Errorf("expected DelSection call %q, got %q", expectedCall, reader.delSectionCall)
				}
			}
		})
	}
}

func TestDeleteFirewallForwardingWithReader_DelSectionError(t *testing.T) {
	reader := &mockConfigReader{
		data:          make(map[string]map[string]map[string][]string),
		delSectionErr: fmt.Errorf("mock delsection error"),
	}

	err := DeleteFirewallForwardingWithReader("test", reader)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !contains(err.Error(), "failed to delete forwarding section") {
		t.Errorf("expected error about delete forwarding section, got: %v", err)
	}
}

func TestDeleteFirewallForwardingWithReader_CommitError(t *testing.T) {
	reader := &mockConfigReader{
		data:        make(map[string]map[string]map[string][]string),
		commitError: fmt.Errorf("mock commit error"),
	}

	err := DeleteFirewallForwardingWithReader("test", reader)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !contains(err.Error(), "failed to commit firewall config") {
		t.Errorf("expected error about commit, got: %v", err)
	}
}

// Tests for FirewallForwardingExistsWithReader
func TestFirewallForwardingExistsWithReader(t *testing.T) {
	tests := []struct {
		name    string
		section string
		data    map[string]map[string]map[string][]string
		want    bool
	}{
		{
			name:    "forwarding_exists",
			section: "mmrouter",
			data: map[string]map[string]map[string][]string{
				"firewall": {
					"mmrouter": {
						"src":  []string{"ahwlan"},
						"dest": []string{"lan"},
					},
				},
			},
			want: true,
		},
		{
			name:    "forwarding_does_not_exist",
			section: "guest_fwd",
			data: map[string]map[string]map[string][]string{
				"firewall": {
					"mmrouter": {
						"src": []string{"ahwlan"},
					},
				},
			},
			want: false,
		},
		{
			name:    "empty_config",
			section: "test",
			data:    map[string]map[string]map[string][]string{},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &mockConfigReader{
				data: tt.data,
			}

			got := FirewallForwardingExistsWithReader(tt.section, reader)
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// Tests for GetFirewallRuleWithReader
func TestGetFirewallRuleWithReader_Ping(t *testing.T) {
	reader := newMockReader()

	want := &UCIFirewallRule{
		Name:     "Allow-Ping",
		Src:      "wan",
		Proto:    "icmp",
		IcmpType: []string{"echo-request"},
		Family:   "ipv4",
		Target:   "ACCEPT",
	}

	got, err := GetFirewallRuleWithReader("Allow-Ping", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestGetFirewallRuleWithReader_DHCP(t *testing.T) {
	reader := newMockReader()

	want := &UCIFirewallRule{
		Name:     "Allow-DHCP-Renew",
		Src:      "wan",
		Proto:    "udp",
		DestPort: "68",
		Target:   "ACCEPT",
		Family:   "ipv4",
	}

	got, err := GetFirewallRuleWithReader("Allow-DHCP-Renew", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestGetFirewallRuleWithReader_ICMPv6(t *testing.T) {
	reader := newMockReader()

	want := &UCIFirewallRule{
		Name:     "Allow-ICMPv6-Input",
		Src:      "wan",
		Proto:    "icmp",
		IcmpType: []string{"echo-request", "echo-reply", "destination-unreachable"},
		Limit:    "1000/sec",
		Family:   "ipv6",
		Target:   "ACCEPT",
	}

	got, err := GetFirewallRuleWithReader("Allow-ICMPv6-Input", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestGetFirewallRuleWithReader_NonExistent(t *testing.T) {
	reader := newMockReader()

	want := &UCIFirewallRule{}

	got, err := GetFirewallRuleWithReader("nonexistent", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// Tests for SetFirewallRuleWithReader
func TestSetFirewallRuleWithReader(t *testing.T) {
	tests := []struct {
		name        string
		section     string
		config      *UCIFirewallRule
		wantErr     bool
		errContains string
	}{
		{
			name:    "set_complete_config",
			section: "Custom-Rule",
			config: &UCIFirewallRule{
				Name:     "Custom-Rule",
				Src:      "lan",
				Dest:     "wan",
				Proto:    "tcp",
				DestPort: "443",
				Target:   "ACCEPT",
				Family:   "ipv4",
			},
			wantErr: false,
		},
		{
			name:    "set_icmp_rule",
			section: "Allow-Ping-Custom",
			config: &UCIFirewallRule{
				Name:     "Allow-Ping-Custom",
				Src:      "wan",
				Proto:    "icmp",
				IcmpType: []string{"echo-request", "echo-reply"},
				Family:   "ipv4",
				Target:   "ACCEPT",
			},
			wantErr: false,
		},
		{
			name:    "set_minimal_config",
			section: "Simple-Rule",
			config: &UCIFirewallRule{
				Name:   "Simple-Rule",
				Target: "DROP",
			},
			wantErr: false,
		},
		{
			name:    "set_rule_with_limit",
			section: "Rate-Limited",
			config: &UCIFirewallRule{
				Name:   "Rate-Limited",
				Src:    "wan",
				Proto:  "tcp",
				Limit:  "10/sec",
				Target: "ACCEPT",
			},
			wantErr: false,
		},
		{
			name:        "nil_config",
			section:     "test",
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

			err := SetFirewallRuleWithReader(tt.section, tt.config, reader)

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
			if tt.config.Name != "" {
				found := false
				for _, call := range reader.setTypeCalls {
					if call.option == "name" && call.values[0] == tt.config.Name {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("name not set correctly")
				}
			}
		})
	}
}

func TestSetFirewallRuleWithReader_SetTypeError(t *testing.T) {
	reader := &mockConfigReader{
		data:         make(map[string]map[string]map[string][]string),
		setTypeError: fmt.Errorf("mock settype error"),
	}

	config := &UCIFirewallRule{
		Name: "Test-Rule",
	}

	err := SetFirewallRuleWithReader("test", config, reader)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !contains(err.Error(), "failed to set name") {
		t.Errorf("expected error about name, got: %v", err)
	}
}

func TestSetFirewallRuleWithReader_CommitError(t *testing.T) {
	reader := &mockConfigReader{
		data:        make(map[string]map[string]map[string][]string),
		commitError: fmt.Errorf("mock commit error"),
	}

	config := &UCIFirewallRule{
		Name: "Test-Rule",
	}

	err := SetFirewallRuleWithReader("test", config, reader)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !contains(err.Error(), "failed to commit firewall config") {
		t.Errorf("expected error about commit, got: %v", err)
	}
}

// Tests for DeleteFirewallRuleWithReader
func TestDeleteFirewallRuleWithReader(t *testing.T) {
	tests := []struct {
		name    string
		section string
		wantErr bool
	}{
		{
			name:    "delete_existing_rule",
			section: "old_rule",
			wantErr: false,
		},
		{
			name:    "delete_another_rule",
			section: "unused_rule",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &mockConfigReader{
				data: make(map[string]map[string]map[string][]string),
			}

			err := DeleteFirewallRuleWithReader(tt.section, reader)

			if (err != nil) != tt.wantErr {
				t.Errorf("expected error: %v, got: %v", tt.wantErr, err)
			}

			if !tt.wantErr {
				if !reader.commitCalled {
					t.Error("expected Commit to be called")
				}
				expectedCall := fmt.Sprintf("firewall.%s", tt.section)
				if reader.delSectionCall != expectedCall {
					t.Errorf("expected DelSection call %q, got %q", expectedCall, reader.delSectionCall)
				}
			}
		})
	}
}

func TestDeleteFirewallRuleWithReader_DelSectionError(t *testing.T) {
	reader := &mockConfigReader{
		data:          make(map[string]map[string]map[string][]string),
		delSectionErr: fmt.Errorf("mock delsection error"),
	}

	err := DeleteFirewallRuleWithReader("test", reader)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !contains(err.Error(), "failed to delete rule section") {
		t.Errorf("expected error about delete rule section, got: %v", err)
	}
}

func TestDeleteFirewallRuleWithReader_CommitError(t *testing.T) {
	reader := &mockConfigReader{
		data:        make(map[string]map[string]map[string][]string),
		commitError: fmt.Errorf("mock commit error"),
	}

	err := DeleteFirewallRuleWithReader("test", reader)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !contains(err.Error(), "failed to commit firewall config") {
		t.Errorf("expected error about commit, got: %v", err)
	}
}

// Tests for FirewallRuleExistsWithReader
func TestFirewallRuleExistsWithReader(t *testing.T) {
	tests := []struct {
		name    string
		section string
		data    map[string]map[string]map[string][]string
		want    bool
	}{
		{
			name:    "rule_exists",
			section: "Allow-Ping",
			data: map[string]map[string]map[string][]string{
				"firewall": {
					"Allow-Ping": {
						"name":   []string{"Allow-Ping"},
						"target": []string{"ACCEPT"},
					},
				},
			},
			want: true,
		},
		{
			name:    "rule_does_not_exist",
			section: "Custom-Rule",
			data: map[string]map[string]map[string][]string{
				"firewall": {
					"Allow-Ping": {
						"name": []string{"Allow-Ping"},
					},
				},
			},
			want: false,
		},
		{
			name:    "empty_config",
			section: "test",
			data:    map[string]map[string]map[string][]string{},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &mockConfigReader{
				data: tt.data,
			}

			got := FirewallRuleExistsWithReader(tt.section, reader)
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// Tests for AddNetworkToZoneWithReader
func TestAddNetworkToZoneWithReader(t *testing.T) {
	tests := []struct {
		name        string
		zone        string
		network     string
		initialData map[string]map[string]map[string][]string
		wantErr     bool
		errContains string
		wantNetwork []string
	}{
		{
			name:    "add_network_to_existing_zone",
			zone:    "ahwlan",
			network: "tailscale0",
			initialData: map[string]map[string]map[string][]string{
				"firewall": {
					"ahwlan": {
						"name":    []string{"ahwlan"},
						"input":   []string{"ACCEPT"},
						"output":  []string{"ACCEPT"},
						"forward": []string{"ACCEPT"},
						"network": []string{"ahwlan"},
					},
				},
			},
			wantErr:     false,
			wantNetwork: []string{"ahwlan", "tailscale0"},
		},
		{
			name:    "add_network_already_present",
			zone:    "ahwlan",
			network: "ahwlan",
			initialData: map[string]map[string]map[string][]string{
				"firewall": {
					"ahwlan": {
						"name":    []string{"ahwlan"},
						"input":   []string{"ACCEPT"},
						"network": []string{"ahwlan"},
					},
				},
			},
			wantErr:     false,
			wantNetwork: []string{"ahwlan"},
		},
		{
			name:    "add_network_to_zone_with_empty_network_list",
			zone:    "guest",
			network: "guest",
			initialData: map[string]map[string]map[string][]string{
				"firewall": {
					"guest": {
						"name":  []string{"guest"},
						"input": []string{"ACCEPT"},
					},
				},
			},
			wantErr:     false,
			wantNetwork: []string{"guest"},
		},
		{
			name:    "add_network_to_zone_with_multiple_existing_networks",
			zone:    "wan",
			network: "wan7",
			initialData: map[string]map[string]map[string][]string{
				"firewall": {
					"wan": {
						"name":    []string{"wan"},
						"network": []string{"wan", "wan6"},
					},
				},
			},
			wantErr:     false,
			wantNetwork: []string{"wan", "wan6", "wan7"},
		},
		{
			name:    "zone_does_not_exist",
			zone:    "nonexistent",
			network: "test",
			initialData: map[string]map[string]map[string][]string{
				"firewall": {
					"lan": {
						"name": []string{"lan"},
					},
				},
			},
			wantErr:     true,
			errContains: "does not exist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &mockConfigReader{
				data: tt.initialData,
			}

			err := AddNetworkToZoneWithReader(tt.zone, tt.network, reader)

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

			// For successful operations, verify the network list was updated correctly
			if tt.wantNetwork != nil {
				gotNetwork, ok := reader.Get(firewallConfigName, tt.zone, "network")
				if !ok {
					t.Error("expected network list to be set")
				}
				if !reflect.DeepEqual(gotNetwork, tt.wantNetwork) {
					t.Errorf("got network list %v, want %v", gotNetwork, tt.wantNetwork)
				}
			}

			// Verify commit was called for successful operations
			if !reader.commitCalled {
				// Only check commit if network was actually added (not already present)
				networkAlreadyPresent := false
				if initialNet, ok := tt.initialData["firewall"][tt.zone]["network"]; ok {
					for _, net := range initialNet {
						if net == tt.network {
							networkAlreadyPresent = true
							break
						}
					}
				}
				if !networkAlreadyPresent {
					t.Error("expected Commit to be called")
				}
			}
		})
	}
}

func TestAddNetworkToZoneWithReader_SetTypeError(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"firewall": {
				"lan": {
					"name":    []string{"lan"},
					"network": []string{"lan"},
				},
			},
		},
		setTypeError: fmt.Errorf("mock settype error"),
	}

	err := AddNetworkToZoneWithReader("lan", "guest", reader)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !contains(err.Error(), "failed to add network to zone") {
		t.Errorf("expected error about adding network to zone, got: %v", err)
	}
}

func TestAddNetworkToZoneWithReader_CommitError(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"firewall": {
				"lan": {
					"name":    []string{"lan"},
					"network": []string{"lan"},
				},
			},
		},
		commitError: fmt.Errorf("mock commit error"),
	}

	err := AddNetworkToZoneWithReader("lan", "guest", reader)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !contains(err.Error(), "failed to commit firewall config") {
		t.Errorf("expected error about commit, got: %v", err)
	}
}
