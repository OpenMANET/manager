package network

import (
	"fmt"
	"reflect"
	"testing"
)

func newMockVXLANReader() *mockConfigReader {
	return &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {
				"vxlan0": {
					"proto":    {"vxlan"},
					"tunlink":  {"eth0"},
					"peeraddr": {"192.168.1.100"},
					"vid":      {"100"},
					"port":     {"4789"},
					"macaddr":  {"00:11:22:33:44:55"},
					"rxcsum":   {"1"},
					"txcsum":   {"1"},
					"mtu":      {"1450"},
					"ttl":      {"64"},
					"tos":      {"inherit"},
				},
				"vxlan1": {
					"proto":    {"vxlan"},
					"peeraddr": {"10.0.0.1"},
					"vid":      {"200"},
				},
				"vxlan_minimal": {
					"proto": {"vxlan"},
					"vid":   {"300"},
				},
			},
		},
	}
}

func TestGetVXLANByNameWithReader_FullConfig(t *testing.T) {
	reader := newMockVXLANReader()

	want := &UCIVXLANConfig{
		Proto:    "vxlan",
		Tunlink:  "eth0",
		PeerAddr: "192.168.1.100",
		VID:      "100",
		Port:     "4789",
		MacAddr:  "00:11:22:33:44:55",
		RxCsum:   "1",
		TxCsum:   "1",
		MTU:      "1450",
		TTL:      "64",
		TOS:      "inherit",
	}

	got, err := GetVXLANByNameWithReader("vxlan0", reader)
	if err != nil {
		t.Fatalf("GetVXLANByNameWithReader failed: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetVXLANByNameWithReader = %+v, want %+v", got, want)
	}
}

func TestGetVXLANByNameWithReader_PartialConfig(t *testing.T) {
	reader := newMockVXLANReader()

	want := &UCIVXLANConfig{
		Proto:    "vxlan",
		PeerAddr: "10.0.0.1",
		VID:      "200",
	}

	got, err := GetVXLANByNameWithReader("vxlan1", reader)
	if err != nil {
		t.Fatalf("GetVXLANByNameWithReader failed: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetVXLANByNameWithReader = %+v, want %+v", got, want)
	}
}

func TestGetVXLANByNameWithReader_MinimalConfig(t *testing.T) {
	reader := newMockVXLANReader()

	want := &UCIVXLANConfig{
		Proto: "vxlan",
		VID:   "300",
	}

	got, err := GetVXLANByNameWithReader("vxlan_minimal", reader)
	if err != nil {
		t.Fatalf("GetVXLANByNameWithReader failed: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetVXLANByNameWithReader = %+v, want %+v", got, want)
	}
}

func TestGetVXLANByNameWithReader_NonExistent(t *testing.T) {
	reader := newMockVXLANReader()

	want := &UCIVXLANConfig{}

	got, err := GetVXLANByNameWithReader("nonexistent", reader)
	if err != nil {
		t.Fatalf("GetVXLANByNameWithReader failed: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetVXLANByNameWithReader = %+v, want %+v", got, want)
	}
}

func TestSetVXLANConfigWithReader_CreateNew(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {},
		},
	}

	config := &UCIVXLANConfig{
		Proto:    "vxlan",
		Tunlink:  "br-lan",
		PeerAddr: "172.16.0.1",
		VID:      "500",
		Port:     "8472",
		MacAddr:  "aa:bb:cc:dd:ee:ff",
		RxCsum:   "0",
		TxCsum:   "0",
		MTU:      "1400",
		TTL:      "128",
		TOS:      "cs1",
	}

	err := SetVXLANConfigWithReader("vxlan2", config, reader)
	if err != nil {
		t.Fatalf("SetVXLANConfigWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	// Verify add section was called
	expectedAddSection := "network.vxlan2.interface"
	if reader.addSectionCall != expectedAddSection {
		t.Errorf("Expected AddSection to be called with %q, got %q", expectedAddSection, reader.addSectionCall)
	}

	// Verify all fields were set
	expectedCalls := []struct {
		option string
		value  string
	}{
		{"proto", "vxlan"},
		{"tunlink", "br-lan"},
		{"peeraddr", "172.16.0.1"},
		{"vid", "500"},
		{"port", "8472"},
		{"macaddr", "aa:bb:cc:dd:ee:ff"},
		{"rxcsum", "0"},
		{"txcsum", "0"},
		{"mtu", "1400"},
		{"ttl", "128"},
		{"tos", "cs1"},
	}

	for _, expected := range expectedCalls {
		found := false
		for _, call := range reader.setTypeCalls {
			if call.option == expected.option && len(call.values) > 0 && call.values[0] == expected.value {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected SetType call for option %q with value %q", expected.option, expected.value)
		}
	}
}

func TestSetVXLANConfigWithReader_UpdateExisting(t *testing.T) {
	reader := newMockVXLANReader()

	config := &UCIVXLANConfig{
		Proto:    "vxlan",
		PeerAddr: "192.168.2.200",
		VID:      "999",
		Port:     "9999",
	}

	err := SetVXLANConfigWithReader("vxlan0", config, reader)
	if err != nil {
		t.Fatalf("SetVXLANConfigWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	// Verify section was not added (already exists)
	if reader.addSectionCall != "" {
		t.Error("Expected AddSection to NOT be called for existing section")
	}

	// Verify updated fields
	expectedCalls := []struct {
		option string
		value  string
	}{
		{"proto", "vxlan"},
		{"peeraddr", "192.168.2.200"},
		{"vid", "999"},
		{"port", "9999"},
	}

	for _, expected := range expectedCalls {
		found := false
		for _, call := range reader.setTypeCalls {
			if call.option == expected.option && len(call.values) > 0 && call.values[0] == expected.value {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected SetType call for option %q with value %q", expected.option, expected.value)
		}
	}
}

func TestSetVXLANConfigWithReader_MinimalConfig(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {},
		},
	}

	config := &UCIVXLANConfig{
		Proto: "vxlan",
		VID:   "100",
	}

	err := SetVXLANConfigWithReader("vxlan_new", config, reader)
	if err != nil {
		t.Fatalf("SetVXLANConfigWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	// Verify only required fields were set
	protoSet := false
	vidSet := false
	for _, call := range reader.setTypeCalls {
		if call.option == "proto" && len(call.values) > 0 && call.values[0] == "vxlan" {
			protoSet = true
		}
		if call.option == "vid" && len(call.values) > 0 && call.values[0] == "100" {
			vidSet = true
		}
	}

	if !protoSet {
		t.Error("Expected proto to be set")
	}
	if !vidSet {
		t.Error("Expected vid to be set")
	}
}

func TestSetVXLANConfigWithReader_SetTypeError(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {},
		},
		setTypeError: fmt.Errorf("settype error"),
	}

	config := &UCIVXLANConfig{
		Proto: "vxlan",
		VID:   "100",
	}

	err := SetVXLANConfigWithReader("vxlan_error", config, reader)
	if err == nil {
		t.Fatal("Expected SetVXLANConfigWithReader to return error")
	}

	if !contains(err.Error(), "failed to set VXLAN") {
		t.Errorf("Expected error message to contain 'failed to set VXLAN', got: %v", err)
	}
}

func TestSetVXLANConfigWithReader_CommitError(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {},
		},
		commitError: fmt.Errorf("commit error"),
	}

	config := &UCIVXLANConfig{
		Proto: "vxlan",
		VID:   "100",
	}

	err := SetVXLANConfigWithReader("vxlan_commit_error", config, reader)
	if err == nil {
		t.Fatal("Expected SetVXLANConfigWithReader to return error")
	}

	if !contains(err.Error(), "failed to commit VXLAN config") {
		t.Errorf("Expected error message to contain 'failed to commit VXLAN config', got: %v", err)
	}
}

func TestDeleteVXLANConfigWithReader(t *testing.T) {
	reader := newMockVXLANReader()

	err := DeleteVXLANConfigWithReader("vxlan0", reader)
	if err != nil {
		t.Fatalf("DeleteVXLANConfigWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	expectedDelSection := "network.vxlan0"
	if reader.delSectionCall != expectedDelSection {
		t.Errorf("Expected DelSection to be called with %q, got %q", expectedDelSection, reader.delSectionCall)
	}
}

func TestDeleteVXLANConfigWithReader_DelSectionError(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {},
		},
		delSectionErr: fmt.Errorf("delete section error"),
	}

	err := DeleteVXLANConfigWithReader("vxlan0", reader)
	if err == nil {
		t.Fatal("Expected DeleteVXLANConfigWithReader to return error")
	}

	if !contains(err.Error(), "failed to delete VXLAN section") {
		t.Errorf("Expected error message to contain 'failed to delete VXLAN section', got: %v", err)
	}
}

func TestDeleteVXLANConfigWithReader_CommitError(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {},
		},
		commitError: fmt.Errorf("commit error"),
	}

	err := DeleteVXLANConfigWithReader("vxlan0", reader)
	if err == nil {
		t.Fatal("Expected DeleteVXLANConfigWithReader to return error")
	}

	if !contains(err.Error(), "failed to commit VXLAN deletion") {
		t.Errorf("Expected error message to contain 'failed to commit VXLAN deletion', got: %v", err)
	}
}

func TestVXLANSectionExistsWithReader(t *testing.T) {
	reader := newMockVXLANReader()

	tests := []struct {
		name     string
		section  string
		expected bool
	}{
		{
			name:     "existing vxlan0",
			section:  "vxlan0",
			expected: true,
		},
		{
			name:     "existing vxlan1",
			section:  "vxlan1",
			expected: true,
		},
		{
			name:     "non-existent vxlan99",
			section:  "vxlan99",
			expected: false,
		},
		{
			name:     "existing minimal config",
			section:  "vxlan_minimal",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := VXLANSectionExistsWithReader(tt.section, reader)
			if got != tt.expected {
				t.Errorf("VXLANSectionExistsWithReader(%q) = %v, want %v", tt.section, got, tt.expected)
			}
		})
	}
}

func TestSetVXLANProtoWithReader(t *testing.T) {
	reader := newMockVXLANReader()

	err := SetVXLANProtoWithReader("vxlan0", reader)
	if err != nil {
		t.Fatalf("SetVXLANProtoWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	// Verify proto was set
	found := false
	for _, call := range reader.setTypeCalls {
		if call.config == "network" && call.section == "vxlan0" && call.option == "proto" {
			if len(call.values) > 0 && call.values[0] == DefaultVXLANProto {
				found = true
				break
			}
		}
	}

	if !found {
		t.Error("Expected proto to be set to 'vxlan'")
	}
}

func TestSetVXLANProtoWithReader_CommitError(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {},
		},
		commitError: fmt.Errorf("commit error"),
	}

	err := SetVXLANProtoWithReader("vxlan0", reader)
	if err == nil {
		t.Fatal("Expected SetVXLANProtoWithReader to return error")
	}

	if !contains(err.Error(), "failed to commit VXLAN proto") {
		t.Errorf("Expected error message to contain 'failed to commit VXLAN proto', got: %v", err)
	}
}

func TestSetVXLANTunlinkWithReader(t *testing.T) {
	reader := newMockVXLANReader()

	err := SetVXLANTunlinkWithReader("vxlan0", "br-wan", reader)
	if err != nil {
		t.Fatalf("SetVXLANTunlinkWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	// Verify tunlink was set
	found := false
	for _, call := range reader.setTypeCalls {
		if call.option == "tunlink" && len(call.values) > 0 && call.values[0] == "br-wan" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected tunlink to be set to 'br-wan'")
	}
}

func TestSetVXLANTunlinkWithReader_SetTypeError(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {},
		},
		setTypeError: fmt.Errorf("settype error"),
	}

	err := SetVXLANTunlinkWithReader("vxlan0", "eth0", reader)
	if err == nil {
		t.Fatal("Expected SetVXLANTunlinkWithReader to return error")
	}

	if !contains(err.Error(), "failed to set VXLAN tunlink") {
		t.Errorf("Expected error message to contain 'failed to set VXLAN tunlink', got: %v", err)
	}
}

func TestSetVXLANPeerAddrWithReader(t *testing.T) {
	reader := newMockVXLANReader()

	err := SetVXLANPeerAddrWithReader("vxlan0", "10.20.30.40", reader)
	if err != nil {
		t.Fatalf("SetVXLANPeerAddrWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	// Verify peeraddr was set
	found := false
	for _, call := range reader.setTypeCalls {
		if call.option == "peeraddr" && len(call.values) > 0 && call.values[0] == "10.20.30.40" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected peeraddr to be set to '10.20.30.40'")
	}
}

func TestSetVXLANPeerAddrWithReader_CommitError(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {},
		},
		commitError: fmt.Errorf("commit error"),
	}

	err := SetVXLANPeerAddrWithReader("vxlan0", "10.0.0.1", reader)
	if err == nil {
		t.Fatal("Expected SetVXLANPeerAddrWithReader to return error")
	}

	if !contains(err.Error(), "failed to commit VXLAN peeraddr") {
		t.Errorf("Expected error message to contain 'failed to commit VXLAN peeraddr', got: %v", err)
	}
}

func TestSetVXLANVIDWithReader(t *testing.T) {
	reader := newMockVXLANReader()

	err := SetVXLANVIDWithReader("vxlan0", "12345", reader)
	if err != nil {
		t.Fatalf("SetVXLANVIDWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	// Verify vid was set
	found := false
	for _, call := range reader.setTypeCalls {
		if call.option == "vid" && len(call.values) > 0 && call.values[0] == "12345" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected vid to be set to '12345'")
	}
}

func TestSetVXLANVIDWithReader_SetTypeError(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {},
		},
		setTypeError: fmt.Errorf("settype error"),
	}

	err := SetVXLANVIDWithReader("vxlan0", "100", reader)
	if err == nil {
		t.Fatal("Expected SetVXLANVIDWithReader to return error")
	}

	if !contains(err.Error(), "failed to set VXLAN vid") {
		t.Errorf("Expected error message to contain 'failed to set VXLAN vid', got: %v", err)
	}
}

func TestSetVXLANPortWithReader(t *testing.T) {
	reader := newMockVXLANReader()

	err := SetVXLANPortWithReader("vxlan0", "8888", reader)
	if err != nil {
		t.Fatalf("SetVXLANPortWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	// Verify port was set
	found := false
	for _, call := range reader.setTypeCalls {
		if call.option == "port" && len(call.values) > 0 && call.values[0] == "8888" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected port to be set to '8888'")
	}
}

func TestSetVXLANPortWithReader_CommitError(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {},
		},
		commitError: fmt.Errorf("commit error"),
	}

	err := SetVXLANPortWithReader("vxlan0", "4789", reader)
	if err == nil {
		t.Fatal("Expected SetVXLANPortWithReader to return error")
	}

	if !contains(err.Error(), "failed to commit VXLAN port") {
		t.Errorf("Expected error message to contain 'failed to commit VXLAN port', got: %v", err)
	}
}

func TestSetVXLANMacAddrWithReader(t *testing.T) {
	reader := newMockVXLANReader()

	err := SetVXLANMacAddrWithReader("vxlan0", "11:22:33:44:55:66", reader)
	if err != nil {
		t.Fatalf("SetVXLANMacAddrWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	// Verify macaddr was set
	found := false
	for _, call := range reader.setTypeCalls {
		if call.option == "macaddr" && len(call.values) > 0 && call.values[0] == "11:22:33:44:55:66" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected macaddr to be set to '11:22:33:44:55:66'")
	}
}

func TestSetVXLANMacAddrWithReader_SetTypeError(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {},
		},
		setTypeError: fmt.Errorf("settype error"),
	}

	err := SetVXLANMacAddrWithReader("vxlan0", "00:11:22:33:44:55", reader)
	if err == nil {
		t.Fatal("Expected SetVXLANMacAddrWithReader to return error")
	}

	if !contains(err.Error(), "failed to set VXLAN macaddr") {
		t.Errorf("Expected error message to contain 'failed to set VXLAN macaddr', got: %v", err)
	}
}

func TestSetVXLANRxCsumWithReader(t *testing.T) {
	reader := newMockVXLANReader()

	err := SetVXLANRxCsumWithReader("vxlan0", "0", reader)
	if err != nil {
		t.Fatalf("SetVXLANRxCsumWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	// Verify rxcsum was set
	found := false
	for _, call := range reader.setTypeCalls {
		if call.option == "rxcsum" && len(call.values) > 0 && call.values[0] == "0" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected rxcsum to be set to '0'")
	}
}

func TestSetVXLANRxCsumWithReader_CommitError(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {},
		},
		commitError: fmt.Errorf("commit error"),
	}

	err := SetVXLANRxCsumWithReader("vxlan0", "1", reader)
	if err == nil {
		t.Fatal("Expected SetVXLANRxCsumWithReader to return error")
	}

	if !contains(err.Error(), "failed to commit VXLAN rxcsum") {
		t.Errorf("Expected error message to contain 'failed to commit VXLAN rxcsum', got: %v", err)
	}
}

func TestSetVXLANTxCsumWithReader(t *testing.T) {
	reader := newMockVXLANReader()

	err := SetVXLANTxCsumWithReader("vxlan0", "0", reader)
	if err != nil {
		t.Fatalf("SetVXLANTxCsumWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	// Verify txcsum was set
	found := false
	for _, call := range reader.setTypeCalls {
		if call.option == "txcsum" && len(call.values) > 0 && call.values[0] == "0" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected txcsum to be set to '0'")
	}
}

func TestSetVXLANTxCsumWithReader_SetTypeError(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {},
		},
		setTypeError: fmt.Errorf("settype error"),
	}

	err := SetVXLANTxCsumWithReader("vxlan0", "1", reader)
	if err == nil {
		t.Fatal("Expected SetVXLANTxCsumWithReader to return error")
	}

	if !contains(err.Error(), "failed to set VXLAN txcsum") {
		t.Errorf("Expected error message to contain 'failed to set VXLAN txcsum', got: %v", err)
	}
}

func TestSetVXLANMTUWithReader(t *testing.T) {
	reader := newMockVXLANReader()

	err := SetVXLANMTUWithReader("vxlan0", "1350", reader)
	if err != nil {
		t.Fatalf("SetVXLANMTUWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	// Verify mtu was set
	found := false
	for _, call := range reader.setTypeCalls {
		if call.option == "mtu" && len(call.values) > 0 && call.values[0] == "1350" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected mtu to be set to '1350'")
	}
}

func TestSetVXLANMTUWithReader_CommitError(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {},
		},
		commitError: fmt.Errorf("commit error"),
	}

	err := SetVXLANMTUWithReader("vxlan0", "1400", reader)
	if err == nil {
		t.Fatal("Expected SetVXLANMTUWithReader to return error")
	}

	if !contains(err.Error(), "failed to commit VXLAN mtu") {
		t.Errorf("Expected error message to contain 'failed to commit VXLAN mtu', got: %v", err)
	}
}

func TestSetVXLANTTLWithReader(t *testing.T) {
	reader := newMockVXLANReader()

	err := SetVXLANTTLWithReader("vxlan0", "128", reader)
	if err != nil {
		t.Fatalf("SetVXLANTTLWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	// Verify ttl was set
	found := false
	for _, call := range reader.setTypeCalls {
		if call.option == "ttl" && len(call.values) > 0 && call.values[0] == "128" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected ttl to be set to '128'")
	}
}

func TestSetVXLANTTLWithReader_SetTypeError(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {},
		},
		setTypeError: fmt.Errorf("settype error"),
	}

	err := SetVXLANTTLWithReader("vxlan0", "64", reader)
	if err == nil {
		t.Fatal("Expected SetVXLANTTLWithReader to return error")
	}

	if !contains(err.Error(), "failed to set VXLAN ttl") {
		t.Errorf("Expected error message to contain 'failed to set VXLAN ttl', got: %v", err)
	}
}

func TestSetVXLANTOSWithReader(t *testing.T) {
	reader := newMockVXLANReader()

	err := SetVXLANTOSWithReader("vxlan0", "cs2", reader)
	if err != nil {
		t.Fatalf("SetVXLANTOSWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	// Verify tos was set
	found := false
	for _, call := range reader.setTypeCalls {
		if call.option == "tos" && len(call.values) > 0 && call.values[0] == "cs2" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected tos to be set to 'cs2'")
	}
}

func TestSetVXLANTOSWithReader_CommitError(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {},
		},
		commitError: fmt.Errorf("commit error"),
	}

	err := SetVXLANTOSWithReader("vxlan0", "inherit", reader)
	if err == nil {
		t.Fatal("Expected SetVXLANTOSWithReader to return error")
	}

	if !contains(err.Error(), "failed to commit VXLAN tos") {
		t.Errorf("Expected error message to contain 'failed to commit VXLAN tos', got: %v", err)
	}
}

func TestSetVXLANConfigWithReader_AllFieldsEmpty(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {},
		},
	}

	config := &UCIVXLANConfig{
		// All fields empty
	}

	err := SetVXLANConfigWithReader("vxlan_empty", config, reader)
	if err != nil {
		t.Fatalf("SetVXLANConfigWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	// Should have created section but set no fields
	expectedAddSection := "network.vxlan_empty.interface"
	if reader.addSectionCall != expectedAddSection {
		t.Errorf("Expected AddSection to be called with %q, got %q", expectedAddSection, reader.addSectionCall)
	}

	// No SetType calls should have been made (except through section creation)
	if len(reader.setTypeCalls) > 0 {
		t.Errorf("Expected no SetType calls for empty config, got %d calls", len(reader.setTypeCalls))
	}
}

func TestVXLANConstants(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected string
	}{
		{"DefaultVXLANPort", DefaultVXLANPort, "4789"},
		{"DefaultVXLANProto", DefaultVXLANProto, "vxlan"},
		{"DefaultVXLANRxCsum", DefaultVXLANRxCsum, "1"},
		{"DefaultVXLANTxCsum", DefaultVXLANTxCsum, "1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value != tt.expected {
				t.Errorf("%s = %q, want %q", tt.name, tt.value, tt.expected)
			}
		})
	}
}

func TestSetVXLANConfigWithReader_AddSectionError(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {},
		},
		addSectionErr: fmt.Errorf("add section error"),
	}

	config := &UCIVXLANConfig{
		Proto: "vxlan",
		VID:   "100",
	}

	err := SetVXLANConfigWithReader("vxlan_new", config, reader)
	if err == nil {
		t.Fatal("Expected SetVXLANConfigWithReader to return error")
	}

	if !contains(err.Error(), "failed to add VXLAN section") {
		t.Errorf("Expected error message to contain 'failed to add VXLAN section', got: %v", err)
	}
}

func TestGetVXLANByNameWithReader_AllFieldsPopulated(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {
				"vxlan_full": {
					"proto":    {"vxlan"},
					"tunlink":  {"wan"},
					"peeraddr": {"203.0.113.1"},
					"vid":      {"16777215"}, // Max 24-bit value
					"port":     {"9999"},
					"macaddr":  {"ff:ee:dd:cc:bb:aa"},
					"rxcsum":   {"0"},
					"txcsum":   {"0"},
					"mtu":      {"1200"},
					"ttl":      {"255"},
					"tos":      {"cs7"},
				},
			},
		},
	}

	want := &UCIVXLANConfig{
		Proto:    "vxlan",
		Tunlink:  "wan",
		PeerAddr: "203.0.113.1",
		VID:      "16777215",
		Port:     "9999",
		MacAddr:  "ff:ee:dd:cc:bb:aa",
		RxCsum:   "0",
		TxCsum:   "0",
		MTU:      "1200",
		TTL:      "255",
		TOS:      "cs7",
	}

	got, err := GetVXLANByNameWithReader("vxlan_full", reader)
	if err != nil {
		t.Fatalf("GetVXLANByNameWithReader failed: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetVXLANByNameWithReader = %+v, want %+v", got, want)
	}
}

func TestSetVXLANConfigWithReader_SelectiveUpdate(t *testing.T) {
	reader := newMockVXLANReader()

	// Update only specific fields of existing config
	config := &UCIVXLANConfig{
		Proto: "vxlan",
		VID:   "500", // Changed
		MTU:   "1300", // New field
	}

	err := SetVXLANConfigWithReader("vxlan0", config, reader)
	if err != nil {
		t.Fatalf("SetVXLANConfigWithReader failed: %v", err)
	}

	// Verify only the specified fields were set
	vidFound := false
	mtuFound := false

	for _, call := range reader.setTypeCalls {
		if call.option == "vid" && len(call.values) > 0 && call.values[0] == "500" {
			vidFound = true
		}
		if call.option == "mtu" && len(call.values) > 0 && call.values[0] == "1300" {
			mtuFound = true
		}
	}

	if !vidFound {
		t.Error("Expected vid to be updated to '500'")
	}
	if !mtuFound {
		t.Error("Expected mtu to be set to '1300'")
	}
}
