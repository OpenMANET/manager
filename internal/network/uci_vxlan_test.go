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
					"proto":          {"vxlan"},
					"tunlink":        {"eth0"},
					"ipaddr":         {"10.0.1.1"},
					"peeraddr":       {"192.168.1.100"},
					"vid":            {"100"},
					"port":           {"4789"},
					"srcport":        {"10000-20000"},
					"macaddr":        {"00:11:22:33:44:55"},
					"rxcsum":         {"1"},
					"txcsum":         {"1"},
					"mtu":            {"1450"},
					"ttl":            {"64"},
					"tos":            {"inherit"},
					"df":             {"1"},
					"flowlabel":      {"0x12345"},
					"ageing":         {"300"},
					"maxaddress":     {"1024"},
					"learning":       {"1"},
					"rsc":            {"0"},
					"proxy":          {"1"},
					"l2miss":         {"1"},
					"l3miss":         {"1"},
					"udpcsum":        {"1"},
					"udp6zerocsumtx": {"0"},
					"udp6zerocsumrx": {"0"},
					"gbp":            {"1"},
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
				"peer0": {
					"vxlan":   {"vxlan0"},
					"lladdr":  {"00:11:22:33:44:55"},
					"dst":     {"10.0.0.2"},
					"port":    {"4789"},
					"via":     {"eth0"},
					"vni":     {"100"},
					"src_vni": {"200"},
				},
				"peer1": {
					"vxlan": {"vxlan0"},
					"dst":   {"10.0.0.3"},
				},
				"peer_multicast": {
					"vxlan": {"vxlan0"},
					"dst":   {"239.1.1.1"},
					"via":   {"br-lan"},
				},
			},
		},
	}
}

func TestGetVXLANByNameWithReader_FullConfig(t *testing.T) {
	reader := newMockVXLANReader()

	want := &UCIVXLANConfig{
		Proto:          "vxlan",
		Tunlink:        "eth0",
		IPAddr:         "10.0.1.1",
		PeerAddr:       "192.168.1.100",
		VID:            "100",
		Port:           "4789",
		SrcPort:        "10000-20000",
		MacAddr:        "00:11:22:33:44:55",
		RxCsum:         "1",
		TxCsum:         "1",
		MTU:            "1450",
		TTL:            "64",
		TOS:            "inherit",
		DF:             "1",
		FlowLabel:      "0x12345",
		Ageing:         "300",
		MaxAddress:     "1024",
		Learning:       "1",
		RSC:            "0",
		Proxy:          "1",
		L2Miss:         "1",
		L3Miss:         "1",
		UDPCsum:        "1",
		UDP6ZeroCsumTx: "0",
		UDP6ZeroCsumRx: "0",
		GBP:            "1",
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
		VID:   "500",  // Changed
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

func TestSetVXLANIPAddrWithReader(t *testing.T) {
	reader := newMockVXLANReader()

	err := SetVXLANIPAddrWithReader("vxlan0", "10.1.1.1", reader)
	if err != nil {
		t.Fatalf("SetVXLANIPAddrWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	found := false
	for _, call := range reader.setTypeCalls {
		if call.option == "ipaddr" && len(call.values) > 0 && call.values[0] == "10.1.1.1" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected ipaddr to be set to '10.1.1.1'")
	}
}

func TestSetVXLANSrcPortWithReader(t *testing.T) {
	reader := newMockVXLANReader()

	err := SetVXLANSrcPortWithReader("vxlan0", "5000-6000", reader)
	if err != nil {
		t.Fatalf("SetVXLANSrcPortWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	found := false
	for _, call := range reader.setTypeCalls {
		if call.option == "srcport" && len(call.values) > 0 && call.values[0] == "5000-6000" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected srcport to be set to '5000-6000'")
	}
}

func TestSetVXLANDFWithReader(t *testing.T) {
	reader := newMockVXLANReader()

	err := SetVXLANDFWithReader("vxlan0", "0", reader)
	if err != nil {
		t.Fatalf("SetVXLANDFWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	found := false
	for _, call := range reader.setTypeCalls {
		if call.option == "df" && len(call.values) > 0 && call.values[0] == "0" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected df to be set to '0'")
	}
}

func TestSetVXLANFlowLabelWithReader(t *testing.T) {
	reader := newMockVXLANReader()

	err := SetVXLANFlowLabelWithReader("vxlan0", "0xabcde", reader)
	if err != nil {
		t.Fatalf("SetVXLANFlowLabelWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	found := false
	for _, call := range reader.setTypeCalls {
		if call.option == "flowlabel" && len(call.values) > 0 && call.values[0] == "0xabcde" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected flowlabel to be set to '0xabcde'")
	}
}

func TestSetVXLANAgeingWithReader(t *testing.T) {
	reader := newMockVXLANReader()

	err := SetVXLANAgeingWithReader("vxlan0", "600", reader)
	if err != nil {
		t.Fatalf("SetVXLANAgeingWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	found := false
	for _, call := range reader.setTypeCalls {
		if call.option == "ageing" && len(call.values) > 0 && call.values[0] == "600" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected ageing to be set to '600'")
	}
}

func TestSetVXLANMaxAddressWithReader(t *testing.T) {
	reader := newMockVXLANReader()

	err := SetVXLANMaxAddressWithReader("vxlan0", "2048", reader)
	if err != nil {
		t.Fatalf("SetVXLANMaxAddressWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	found := false
	for _, call := range reader.setTypeCalls {
		if call.option == "maxaddress" && len(call.values) > 0 && call.values[0] == "2048" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected maxaddress to be set to '2048'")
	}
}

func TestSetVXLANLearningWithReader(t *testing.T) {
	reader := newMockVXLANReader()

	err := SetVXLANLearningWithReader("vxlan0", "0", reader)
	if err != nil {
		t.Fatalf("SetVXLANLearningWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	found := false
	for _, call := range reader.setTypeCalls {
		if call.option == "learning" && len(call.values) > 0 && call.values[0] == "0" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected learning to be set to '0'")
	}
}

func TestSetVXLANRSCWithReader(t *testing.T) {
	reader := newMockVXLANReader()

	err := SetVXLANRSCWithReader("vxlan0", "1", reader)
	if err != nil {
		t.Fatalf("SetVXLANRSCWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	found := false
	for _, call := range reader.setTypeCalls {
		if call.option == "rsc" && len(call.values) > 0 && call.values[0] == "1" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected rsc to be set to '1'")
	}
}

func TestSetVXLANProxyWithReader(t *testing.T) {
	reader := newMockVXLANReader()

	err := SetVXLANProxyWithReader("vxlan0", "0", reader)
	if err != nil {
		t.Fatalf("SetVXLANProxyWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	found := false
	for _, call := range reader.setTypeCalls {
		if call.option == "proxy" && len(call.values) > 0 && call.values[0] == "0" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected proxy to be set to '0'")
	}
}

func TestSetVXLANL2MissWithReader(t *testing.T) {
	reader := newMockVXLANReader()

	err := SetVXLANL2MissWithReader("vxlan0", "0", reader)
	if err != nil {
		t.Fatalf("SetVXLANL2MissWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	found := false
	for _, call := range reader.setTypeCalls {
		if call.option == "l2miss" && len(call.values) > 0 && call.values[0] == "0" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected l2miss to be set to '0'")
	}
}

func TestSetVXLANL3MissWithReader(t *testing.T) {
	reader := newMockVXLANReader()

	err := SetVXLANL3MissWithReader("vxlan0", "0", reader)
	if err != nil {
		t.Fatalf("SetVXLANL3MissWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	found := false
	for _, call := range reader.setTypeCalls {
		if call.option == "l3miss" && len(call.values) > 0 && call.values[0] == "0" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected l3miss to be set to '0'")
	}
}

func TestSetVXLANUDPCsumWithReader(t *testing.T) {
	reader := newMockVXLANReader()

	err := SetVXLANUDPCsumWithReader("vxlan0", "0", reader)
	if err != nil {
		t.Fatalf("SetVXLANUDPCsumWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	found := false
	for _, call := range reader.setTypeCalls {
		if call.option == "udpcsum" && len(call.values) > 0 && call.values[0] == "0" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected udpcsum to be set to '0'")
	}
}

func TestSetVXLANUDP6ZeroCsumTxWithReader(t *testing.T) {
	reader := newMockVXLANReader()

	err := SetVXLANUDP6ZeroCsumTxWithReader("vxlan0", "1", reader)
	if err != nil {
		t.Fatalf("SetVXLANUDP6ZeroCsumTxWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	found := false
	for _, call := range reader.setTypeCalls {
		if call.option == "udp6zerocsumtx" && len(call.values) > 0 && call.values[0] == "1" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected udp6zerocsumtx to be set to '1'")
	}
}

func TestSetVXLANUDP6ZeroCsumRxWithReader(t *testing.T) {
	reader := newMockVXLANReader()

	err := SetVXLANUDP6ZeroCsumRxWithReader("vxlan0", "1", reader)
	if err != nil {
		t.Fatalf("SetVXLANUDP6ZeroCsumRxWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	found := false
	for _, call := range reader.setTypeCalls {
		if call.option == "udp6zerocsumrx" && len(call.values) > 0 && call.values[0] == "1" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected udp6zerocsumrx to be set to '1'")
	}
}

func TestSetVXLANGBPWithReader(t *testing.T) {
	reader := newMockVXLANReader()

	err := SetVXLANGBPWithReader("vxlan0", "0", reader)
	if err != nil {
		t.Fatalf("SetVXLANGBPWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	found := false
	for _, call := range reader.setTypeCalls {
		if call.option == "gbp" && len(call.values) > 0 && call.values[0] == "0" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected gbp to be set to '0'")
	}
}

func TestSetVXLANConfigWithReader_AllNewFields(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {},
		},
	}

	config := &UCIVXLANConfig{
		Proto:          "vxlan",
		IPAddr:         "10.2.2.2",
		VID:            "999",
		SrcPort:        "8000-9000",
		DF:             "0",
		FlowLabel:      "0xfffff",
		Ageing:         "450",
		MaxAddress:     "512",
		Learning:       "0",
		RSC:            "1",
		Proxy:          "1",
		L2Miss:         "1",
		L3Miss:         "1",
		UDPCsum:        "0",
		UDP6ZeroCsumTx: "1",
		UDP6ZeroCsumRx: "1",
		GBP:            "1",
	}

	err := SetVXLANConfigWithReader("vxlan_new", config, reader)
	if err != nil {
		t.Fatalf("SetVXLANConfigWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	// Verify all new fields were set
	expectedCalls := map[string]string{
		"ipaddr":         "10.2.2.2",
		"srcport":        "8000-9000",
		"df":             "0",
		"flowlabel":      "0xfffff",
		"ageing":         "450",
		"maxaddress":     "512",
		"learning":       "0",
		"rsc":            "1",
		"proxy":          "1",
		"l2miss":         "1",
		"l3miss":         "1",
		"udpcsum":        "0",
		"udp6zerocsumtx": "1",
		"udp6zerocsumrx": "1",
		"gbp":            "1",
	}

	for option, expectedValue := range expectedCalls {
		found := false
		for _, call := range reader.setTypeCalls {
			if call.option == option && len(call.values) > 0 && call.values[0] == expectedValue {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected SetType call for option %q with value %q", option, expectedValue)
		}
	}
}

// VXLAN Peer Tests

func TestGetVXLANPeerByNameWithReader_FullConfig(t *testing.T) {
	reader := newMockVXLANReader()

	want := &UCIVXLANPeer{
		VXLAN:  "vxlan0",
		LLAddr: "00:11:22:33:44:55",
		Dst:    "10.0.0.2",
		Port:   "4789",
		Via:    "eth0",
		VNI:    "100",
		SrcVNI: "200",
	}

	got, err := GetVXLANPeerByNameWithReader("peer0", reader)
	if err != nil {
		t.Fatalf("GetVXLANPeerByNameWithReader failed: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetVXLANPeerByNameWithReader = %+v, want %+v", got, want)
	}
}

func TestGetVXLANPeerByNameWithReader_MinimalConfig(t *testing.T) {
	reader := newMockVXLANReader()

	want := &UCIVXLANPeer{
		VXLAN: "vxlan0",
		Dst:   "10.0.0.3",
	}

	got, err := GetVXLANPeerByNameWithReader("peer1", reader)
	if err != nil {
		t.Fatalf("GetVXLANPeerByNameWithReader failed: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetVXLANPeerByNameWithReader = %+v, want %+v", got, want)
	}
}

func TestGetVXLANPeerByNameWithReader_MulticastConfig(t *testing.T) {
	reader := newMockVXLANReader()

	want := &UCIVXLANPeer{
		VXLAN: "vxlan0",
		Dst:   "239.1.1.1",
		Via:   "br-lan",
	}

	got, err := GetVXLANPeerByNameWithReader("peer_multicast", reader)
	if err != nil {
		t.Fatalf("GetVXLANPeerByNameWithReader failed: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetVXLANPeerByNameWithReader = %+v, want %+v", got, want)
	}
}

func TestGetVXLANPeerByNameWithReader_NonExistent(t *testing.T) {
	reader := newMockVXLANReader()

	want := &UCIVXLANPeer{}

	got, err := GetVXLANPeerByNameWithReader("nonexistent", reader)
	if err != nil {
		t.Fatalf("GetVXLANPeerByNameWithReader failed: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetVXLANPeerByNameWithReader = %+v, want %+v", got, want)
	}
}

func TestAddVXLANPeerWithReader(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {},
		},
		sectionTypes: map[string]map[string]string{
			"network": {},
		},
	}

	peer := &UCIVXLANPeer{
		VXLAN:  "vxlan1",
		LLAddr: "aa:bb:cc:dd:ee:ff",
		Dst:    "192.168.1.100",
		Port:   "8472",
		Via:    "wan",
		VNI:    "999",
		SrcVNI: "888",
	}

	err := AddVXLANPeerWithReader(peer, reader)
	if err != nil {
		t.Fatalf("AddVXLANPeerWithReader failed: %v", err)
	}

	// Verify commit was called once (at the end)
	if reader.commitCount != 1 {
		t.Errorf("Expected Commit to be called 1 time, got %d", reader.commitCount)
	}

	// Verify add section was called with named section
	expectedAddSection := "network.vxlan_peer_0.vxlan_peer"
	if reader.addSectionCall != expectedAddSection {
		t.Errorf("Expected AddSection to be called with %q, got %q", expectedAddSection, reader.addSectionCall)
	}

	// Verify all fields were set with the correct section reference vxlan_peer_0
	expectedCalls := []struct {
		section string
		option  string
		value   string
	}{
		{"vxlan_peer_0", "vxlan", "vxlan1"},
		{"vxlan_peer_0", "lladdr", "aa:bb:cc:dd:ee:ff"},
		{"vxlan_peer_0", "dst", "192.168.1.100"},
		{"vxlan_peer_0", "port", "8472"},
		{"vxlan_peer_0", "via", "wan"},
		{"vxlan_peer_0", "vni", "999"},
		{"vxlan_peer_0", "src_vni", "888"},
	}

	for _, expected := range expectedCalls {
		found := false
		for _, call := range reader.setTypeCalls {
			if call.section == expected.section && call.option == expected.option && len(call.values) > 0 && call.values[0] == expected.value {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected SetType call for section %q option %q with value %q", expected.section, expected.option, expected.value)
		}
	}
}

func TestUpdateVXLANPeerWithReader(t *testing.T) {
	reader := newMockVXLANReader()

	peer := &UCIVXLANPeer{
		VXLAN: "vxlan0",
		Dst:   "10.0.0.99",
		Port:  "9999",
	}

	err := UpdateVXLANPeerWithReader("@vxlan_peer[0]", peer, reader)
	if err != nil {
		t.Fatalf("UpdateVXLANPeerWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	// Verify section was not added (update, not create)
	if reader.addSectionCall != "" {
		t.Error("Expected AddSection to NOT be called for update operation")
	}

	// Verify updated fields
	expectedCalls := []struct {
		option string
		value  string
	}{
		{"vxlan", "vxlan0"},
		{"dst", "10.0.0.99"},
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

func TestAddVXLANPeerWithReader_MinimalConfig(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {},
		},
		sectionTypes: map[string]map[string]string{
			"network": {},
		},
	}

	peer := &UCIVXLANPeer{
		VXLAN: "vxlan0",
		Dst:   "10.0.0.5",
	}

	err := AddVXLANPeerWithReader(peer, reader)
	if err != nil {
		t.Fatalf("AddVXLANPeerWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	// Verify only required fields were set with the correct section reference
	vxlanSet := false
	dstSet := false
	for _, call := range reader.setTypeCalls {
		if call.section == "vxlan_peer_0" && call.option == "vxlan" && len(call.values) > 0 && call.values[0] == "vxlan0" {
			vxlanSet = true
		}
		if call.section == "vxlan_peer_0" && call.option == "dst" && len(call.values) > 0 && call.values[0] == "10.0.0.5" {
			dstSet = true
		}
	}

	if !vxlanSet {
		t.Error("Expected vxlan to be set on section vxlan_peer_0")
	}
	if !dstSet {
		t.Error("Expected dst to be set on section vxlan_peer_0")
	}
}

func TestAddVXLANPeerWithReader_SetTypeError(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {},
		},
		sectionTypes: map[string]map[string]string{
			"network": {},
		},
		setTypeError: fmt.Errorf("settype error"),
	}

	peer := &UCIVXLANPeer{
		VXLAN: "vxlan0",
		Dst:   "10.0.0.1",
	}

	err := AddVXLANPeerWithReader(peer, reader)
	if err == nil {
		t.Fatal("Expected AddVXLANPeerWithReader to return error")
	}

	if !contains(err.Error(), "failed to set VXLAN peer") {
		t.Errorf("Expected error message to contain 'failed to set VXLAN peer', got: %v", err)
	}
}

func TestAddVXLANPeerWithReader_CommitError(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {},
		},
		sectionTypes: map[string]map[string]string{
			"network": {},
		},
		commitError: fmt.Errorf("commit error"),
	}

	peer := &UCIVXLANPeer{
		VXLAN: "vxlan0",
		Dst:   "10.0.0.1",
	}

	err := AddVXLANPeerWithReader(peer, reader)
	if err == nil {
		t.Fatal("Expected AddVXLANPeerWithReader to return error")
	}

	if !contains(err.Error(), "failed to commit VXLAN peer") {
		t.Errorf("Expected error message to contain 'failed to commit VXLAN peer', got: %v", err)
	}
}

func TestBatchAddVXLANPeersWithReader(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {},
		},
		sectionTypes: map[string]map[string]string{
			"network": {},
		},
	}

	peers := []UCIVXLANPeer{
		{
			VXLAN: "vxlan0",
			Dst:   "239.2.3.1",
			Via:   "tailscale0",
		},
		{
			VXLAN: "vxlan0",
			Dst:   "224.10.10.1",
			Via:   "tailscale0",
		},
		{
			VXLAN: "vxlan0",
			Dst:   "224.0.0.251",
			Via:   "tailscale0",
		},
	}

	err := BatchAddVXLANPeersWithReader(peers, reader)
	if err != nil {
		t.Fatalf("BatchAddVXLANPeersWithReader failed: %v", err)
	}

	// Verify commit was called exactly once (at the end)
	if reader.commitCount != 1 {
		t.Errorf("Expected Commit to be called 1 time, got %d", reader.commitCount)
	}

	// Verify all three peers had their options set
	for i, peer := range peers {
		sectionRef := fmt.Sprintf("vxlan_peer_%d", i)
		
		// Check vxlan option
		foundVxlan := false
		foundDst := false
		foundVia := false
		
		for _, call := range reader.setTypeCalls {
			if call.section == sectionRef && call.option == "vxlan" && len(call.values) > 0 && call.values[0] == peer.VXLAN {
				foundVxlan = true
			}
			if call.section == sectionRef && call.option == "dst" && len(call.values) > 0 && call.values[0] == peer.Dst {
				foundDst = true
			}
			if call.section == sectionRef && call.option == "via" && len(call.values) > 0 && call.values[0] == peer.Via {
				foundVia = true
			}
		}
		
		if !foundVxlan {
			t.Errorf("Expected SetType call for peer %d section %q option 'vxlan' with value %q", i, sectionRef, peer.VXLAN)
		}
		if !foundDst {
			t.Errorf("Expected SetType call for peer %d section %q option 'dst' with value %q", i, sectionRef, peer.Dst)
		}
		if !foundVia {
			t.Errorf("Expected SetType call for peer %d section %q option 'via' with value %q", i, sectionRef, peer.Via)
		}
	}
}

func TestBatchAddVXLANPeersWithReader_EmptySlice(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {},
		},
	}

	err := BatchAddVXLANPeersWithReader([]UCIVXLANPeer{}, reader)
	if err != nil {
		t.Fatalf("BatchAddVXLANPeersWithReader with empty slice should not error: %v", err)
	}

	// Verify no operations were performed
	if reader.commitCount != 0 {
		t.Errorf("Expected Commit to not be called for empty slice, got %d calls", reader.commitCount)
	}
}

func TestBatchAddVXLANPeersWithReader_AllFields(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {},
		},
		sectionTypes: map[string]map[string]string{
			"network": {},
		},
	}

	peers := []UCIVXLANPeer{
		{
			VXLAN:  "vxlan0",
			LLAddr: "aa:bb:cc:dd:ee:ff",
			Dst:    "192.168.1.100",
			Port:   "8472",
			Via:    "wan",
			VNI:    "999",
			SrcVNI: "888",
		},
		{
			VXLAN: "vxlan0",
			Dst:   "192.168.1.101",
			Via:   "lan",
		},
	}

	err := BatchAddVXLANPeersWithReader(peers, reader)
	if err != nil {
		t.Fatalf("BatchAddVXLANPeersWithReader failed: %v", err)
	}

	// Verify first peer has all fields set
	firstPeerSection := "vxlan_peer_0"
	expectedFirstPeerCalls := []struct {
		option string
		value  string
	}{
		{"vxlan", "vxlan0"},
		{"lladdr", "aa:bb:cc:dd:ee:ff"},
		{"dst", "192.168.1.100"},
		{"port", "8472"},
		{"via", "wan"},
		{"vni", "999"},
		{"src_vni", "888"},
	}

	for _, expected := range expectedFirstPeerCalls {
		found := false
		for _, call := range reader.setTypeCalls {
			if call.section == firstPeerSection && call.option == expected.option && len(call.values) > 0 && call.values[0] == expected.value {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected SetType call for section %q option %q with value %q", firstPeerSection, expected.option, expected.value)
		}
	}

	// Verify second peer has only required fields
	secondPeerSection := "vxlan_peer_1"
	expectedSecondPeerCalls := []struct {
		option string
		value  string
	}{
		{"vxlan", "vxlan0"},
		{"dst", "192.168.1.101"},
		{"via", "lan"},
	}

	for _, expected := range expectedSecondPeerCalls {
		found := false
		for _, call := range reader.setTypeCalls {
			if call.section == secondPeerSection && call.option == expected.option && len(call.values) > 0 && call.values[0] == expected.value {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected SetType call for section %q option %q with value %q", secondPeerSection, expected.option, expected.value)
		}
	}
}

func TestBatchAddVXLANPeersWithReader_AddSectionError(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {},
		},
		addSectionErr: fmt.Errorf("add section error"),
	}

	peers := []UCIVXLANPeer{
		{VXLAN: "vxlan0", Dst: "10.0.0.1"},
	}

	err := BatchAddVXLANPeersWithReader(peers, reader)
	if err == nil {
		t.Fatal("Expected BatchAddVXLANPeersWithReader to return error")
	}

	if !contains(err.Error(), "failed to add VXLAN peer section") {
		t.Errorf("Expected error message to contain 'failed to add VXLAN peer section', got: %v", err)
	}
}

func TestBatchAddVXLANPeersWithReader_CommitError(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {},
		},
		sectionTypes: map[string]map[string]string{
			"network": {},
		},
		commitError: fmt.Errorf("commit error"),
	}

	peers := []UCIVXLANPeer{
		{VXLAN: "vxlan0", Dst: "10.0.0.1"},
	}

	err := BatchAddVXLANPeersWithReader(peers, reader)
	if err == nil {
		t.Fatal("Expected BatchAddVXLANPeersWithReader to return error")
	}

	if !contains(err.Error(), "failed to commit VXLAN peer") {
		t.Errorf("Expected error message to contain 'failed to commit VXLAN peer', got: %v", err)
	}
}

func TestBatchAddVXLANPeersWithReader_SetTypeError(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {},
		},
		sectionTypes: map[string]map[string]string{
			"network": {},
		},
		setTypeError: fmt.Errorf("settype error"),
	}

	peers := []UCIVXLANPeer{
		{VXLAN: "vxlan0", Dst: "10.0.0.1"},
	}

	err := BatchAddVXLANPeersWithReader(peers, reader)
	if err == nil {
		t.Fatal("Expected BatchAddVXLANPeersWithReader to return error")
	}

	if !contains(err.Error(), "failed to set VXLAN peer") {
		t.Errorf("Expected error message to contain 'failed to set VXLAN peer', got: %v", err)
	}
}

func TestDeleteVXLANPeerByNameWithReader(t *testing.T) {
	reader := newMockVXLANReader()

	err := DeleteVXLANPeerByNameWithReader("@vxlan_peer[0]", reader)
	if err != nil {
		t.Fatalf("DeleteVXLANPeerByNameWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	expectedDelSection := "network.@vxlan_peer[0]"
	if reader.delSectionCall != expectedDelSection {
		t.Errorf("Expected DelSection to be called with %q, got %q", expectedDelSection, reader.delSectionCall)
	}
}

func TestDeleteVXLANPeerByNameWithReader_DelSectionError(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {},
		},
		delSectionErr: fmt.Errorf("delete section error"),
	}

	err := DeleteVXLANPeerByNameWithReader("@vxlan_peer[0]", reader)
	if err == nil {
		t.Fatal("Expected DeleteVXLANPeerByNameWithReader to return error")
	}

	if !contains(err.Error(), "failed to delete VXLAN peer section") {
		t.Errorf("Expected error message to contain 'failed to delete VXLAN peer section', got: %v", err)
	}
}

func TestDeleteVXLANPeerByNameWithReader_CommitError(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {},
		},
		commitError: fmt.Errorf("commit error"),
	}

	err := DeleteVXLANPeerByNameWithReader("@vxlan_peer[0]", reader)
	if err == nil {
		t.Fatal("Expected DeleteVXLANPeerByNameWithReader to return error")
	}

	if !contains(err.Error(), "failed to commit VXLAN peer deletion") {
		t.Errorf("Expected error message to contain 'failed to commit VXLAN peer deletion', got: %v", err)
	}
}

func TestVXLANPeerSectionExistsWithReader(t *testing.T) {
	reader := newMockVXLANReader()

	tests := []struct {
		name     string
		section  string
		expected bool
	}{
		{
			name:     "existing peer0",
			section:  "peer0",
			expected: true,
		},
		{
			name:     "existing peer1",
			section:  "peer1",
			expected: true,
		},
		{
			name:     "existing multicast peer",
			section:  "peer_multicast",
			expected: true,
		},
		{
			name:     "non-existent peer",
			section:  "peer99",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := VXLANPeerSectionExistsWithReader(tt.section, reader)
			if got != tt.expected {
				t.Errorf("VXLANPeerSectionExistsWithReader(%q) = %v, want %v", tt.section, got, tt.expected)
			}
		})
	}
}

func TestAddVXLANPeerWithReader_AllFieldsEmpty(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {},
		},
	}

	peer := &UCIVXLANPeer{
		// All fields empty
	}

	err := AddVXLANPeerWithReader(peer, reader)
	if err != nil {
		t.Fatalf("AddVXLANPeerWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	// Should have created named section but set no fields
	expectedAddSection := "network.vxlan_peer_0.vxlan_peer"
	if reader.addSectionCall != expectedAddSection {
		t.Errorf("Expected AddSection to be called with %q, got %q", expectedAddSection, reader.addSectionCall)
	}

	// No SetType calls should have been made (except through section creation)
	if len(reader.setTypeCalls) > 0 {
		t.Errorf("Expected no SetType calls for empty config, got %d calls", len(reader.setTypeCalls))
	}
}

func TestAddVXLANPeerWithReader_AddSectionError(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {},
		},
		addSectionErr: fmt.Errorf("add section error"),
	}

	peer := &UCIVXLANPeer{
		VXLAN: "vxlan0",
		Dst:   "10.0.0.1",
	}

	err := AddVXLANPeerWithReader(peer, reader)
	if err == nil {
		t.Fatal("Expected AddVXLANPeerWithReader to return error")
	}

	if !contains(err.Error(), "failed to add VXLAN peer section") {
		t.Errorf("Expected error message to contain 'failed to add VXLAN peer section', got: %v", err)
	}
}

func TestAddVXLANPeerWithReader_WithExistingPeers(t *testing.T) {
	reader := &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {
				"peer0": {
					"vxlan": {"vxlan0"},
					"dst":   {"10.0.0.1"},
				},
				"peer1": {
					"vxlan": {"vxlan0"},
					"dst":   {"10.0.0.2"},
				},
			},
		},
		sectionTypes: map[string]map[string]string{
			"network": {
				"peer0": "vxlan_peer",
				"peer1": "vxlan_peer",
			},
		},
	}

	peer := &UCIVXLANPeer{
		VXLAN: "vxlan0",
		Dst:   "10.0.0.3",
	}

	err := AddVXLANPeerWithReader(peer, reader)
	if err != nil {
		t.Fatalf("AddVXLANPeerWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	// With 2 existing named sections, the new peer should be named vxlan_peer_2
	vxlanSet := false
	dstSet := false
	for _, call := range reader.setTypeCalls {
		if call.section == "vxlan_peer_2" && call.option == "vxlan" && len(call.values) > 0 && call.values[0] == "vxlan0" {
			vxlanSet = true
		}
		if call.section == "vxlan_peer_2" && call.option == "dst" && len(call.values) > 0 && call.values[0] == "10.0.0.3" {
			dstSet = true
		}
	}

	if !vxlanSet {
		t.Error("Expected vxlan to be set on section vxlan_peer_2")
	}
	if !dstSet {
		t.Error("Expected dst to be set on section vxlan_peer_2")
	}
}

func TestGetVXLANPeerByDstWithReader_Found(t *testing.T) {
	reader := newMockVXLANReader()

	peer, section, err := GetVXLANPeerByDstWithReader("10.0.0.2", reader)
	if err != nil {
		t.Fatalf("GetVXLANPeerByDstWithReader failed: %v", err)
	}

	if section != "peer0" {
		t.Errorf("Expected section 'peer0', got %q", section)
	}

	want := &UCIVXLANPeer{
		VXLAN:  "vxlan0",
		LLAddr: "00:11:22:33:44:55",
		Dst:    "10.0.0.2",
		Port:   "4789",
		Via:    "eth0",
		VNI:    "100",
		SrcVNI: "200",
	}

	if !reflect.DeepEqual(peer, want) {
		t.Errorf("GetVXLANPeerByDstWithReader = %+v, want %+v", peer, want)
	}
}

func TestGetVXLANPeerByDstWithReader_MulticastAddress(t *testing.T) {
	reader := newMockVXLANReader()

	peer, section, err := GetVXLANPeerByDstWithReader("239.1.1.1", reader)
	if err != nil {
		t.Fatalf("GetVXLANPeerByDstWithReader failed: %v", err)
	}

	if section != "peer_multicast" {
		t.Errorf("Expected section 'peer_multicast', got %q", section)
	}

	want := &UCIVXLANPeer{
		VXLAN: "vxlan0",
		Dst:   "239.1.1.1",
		Via:   "br-lan",
	}

	if !reflect.DeepEqual(peer, want) {
		t.Errorf("GetVXLANPeerByDstWithReader = %+v, want %+v", peer, want)
	}
}

func TestGetVXLANPeerByDstWithReader_NotFound(t *testing.T) {
	reader := newMockVXLANReader()

	peer, section, err := GetVXLANPeerByDstWithReader("10.99.99.99", reader)
	if err == nil {
		t.Fatal("Expected GetVXLANPeerByDstWithReader to return error for non-existent peer")
	}

	if peer != nil {
		t.Errorf("Expected nil peer, got %+v", peer)
	}

	if section != "" {
		t.Errorf("Expected empty section, got %q", section)
	}

	if !contains(err.Error(), "not found") {
		t.Errorf("Expected error message to contain 'not found', got: %v", err)
	}
}

func TestGetVXLANPeerByDstWithReader_MultiplePeers(t *testing.T) {
	reader := newMockVXLANReader()

	// Test finding peer1 which has minimal config
	peer, section, err := GetVXLANPeerByDstWithReader("10.0.0.3", reader)
	if err != nil {
		t.Fatalf("GetVXLANPeerByDstWithReader failed: %v", err)
	}

	if section != "peer1" {
		t.Errorf("Expected section 'peer1', got %q", section)
	}

	want := &UCIVXLANPeer{
		VXLAN: "vxlan0",
		Dst:   "10.0.0.3",
	}

	if !reflect.DeepEqual(peer, want) {
		t.Errorf("GetVXLANPeerByDstWithReader = %+v, want %+v", peer, want)
	}
}

func TestVXLANPeerExistsByDstWithReader(t *testing.T) {
	reader := newMockVXLANReader()

	tests := []struct {
		name     string
		dst      string
		expected bool
	}{
		{
			name:     "existing peer with full config",
			dst:      "10.0.0.2",
			expected: true,
		},
		{
			name:     "existing peer with minimal config",
			dst:      "10.0.0.3",
			expected: true,
		},
		{
			name:     "existing multicast peer",
			dst:      "239.1.1.1",
			expected: true,
		},
		{
			name:     "non-existent peer",
			dst:      "192.168.99.99",
			expected: false,
		},
		{
			name:     "empty destination",
			dst:      "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := VXLANPeerExistsByDstWithReader(tt.dst, reader)
			if got != tt.expected {
				t.Errorf("VXLANPeerExistsByDstWithReader(%q) = %v, want %v", tt.dst, got, tt.expected)
			}
		})
	}
}

func TestVXLANPeerExistsByDst_Integration(t *testing.T) {
	reader := newMockVXLANReader()

	// Test that it finds peer0
	if !VXLANPeerExistsByDstWithReader("10.0.0.2", reader) {
		t.Error("Expected to find peer with dst 10.0.0.2")
	}

	// Test that it doesn't find non-existent peer
	if VXLANPeerExistsByDstWithReader("1.2.3.4", reader) {
		t.Error("Expected to not find peer with dst 1.2.3.4")
	}
}

func TestGetVXLANPeerByDstWithReader_EmptyDst(t *testing.T) {
	reader := newMockVXLANReader()

	peer, section, err := GetVXLANPeerByDstWithReader("", reader)
	if err == nil {
		t.Fatal("Expected GetVXLANPeerByDstWithReader to return error for empty dst")
	}

	if peer != nil {
		t.Errorf("Expected nil peer, got %+v", peer)
	}

	if section != "" {
		t.Errorf("Expected empty section, got %q", section)
	}
}

func TestDeleteVXLANPeerByDstWithReader_Success(t *testing.T) {
	reader := newMockVXLANReader()

	// Verify peer exists before deletion
	if !VXLANPeerExistsByDstWithReader("10.0.0.2", reader) {
		t.Fatal("Expected peer to exist before deletion")
	}

	err := DeleteVXLANPeerByDstWithReader("10.0.0.2", reader)
	if err != nil {
		t.Fatalf("DeleteVXLANPeerByDstWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	expectedDelSection := "network.peer0"
	if reader.delSectionCall != expectedDelSection {
		t.Errorf("Expected DelSection to be called with %q, got %q", expectedDelSection, reader.delSectionCall)
	}
}

func TestDeleteVXLANPeerByDstWithReader_MulticastPeer(t *testing.T) {
	reader := newMockVXLANReader()

	// Verify multicast peer exists before deletion
	if !VXLANPeerExistsByDstWithReader("239.1.1.1", reader) {
		t.Fatal("Expected multicast peer to exist before deletion")
	}

	err := DeleteVXLANPeerByDstWithReader("239.1.1.1", reader)
	if err != nil {
		t.Fatalf("DeleteVXLANPeerByDstWithReader failed: %v", err)
	}

	if !reader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	expectedDelSection := "network.peer_multicast"
	if reader.delSectionCall != expectedDelSection {
		t.Errorf("Expected DelSection to be called with %q, got %q", expectedDelSection, reader.delSectionCall)
	}
}

func TestDeleteVXLANPeerByDstWithReader_NotFound(t *testing.T) {
	reader := newMockVXLANReader()

	err := DeleteVXLANPeerByDstWithReader("10.99.99.99", reader)
	if err == nil {
		t.Fatal("Expected DeleteVXLANPeerByDstWithReader to return error for non-existent peer")
	}

	if !contains(err.Error(), "failed to find VXLAN peer") {
		t.Errorf("Expected error message to contain 'failed to find VXLAN peer', got: %v", err)
	}

	// Should not have called DelSection or Commit
	if reader.delSectionCall != "" {
		t.Error("Expected DelSection to NOT be called for non-existent peer")
	}
}

func TestDeleteVXLANPeerByDstWithReader_EmptyDst(t *testing.T) {
	reader := newMockVXLANReader()

	err := DeleteVXLANPeerByDstWithReader("", reader)
	if err == nil {
		t.Fatal("Expected DeleteVXLANPeerByDstWithReader to return error for empty dst")
	}

	if !contains(err.Error(), "failed to find VXLAN peer") {
		t.Errorf("Expected error message to contain 'failed to find VXLAN peer', got: %v", err)
	}
}

func TestDeleteVXLANPeerByDstWithReader_DelSectionError(t *testing.T) {
	reader := newMockVXLANReader()
	reader.delSectionErr = fmt.Errorf("delete section error")

	err := DeleteVXLANPeerByDstWithReader("10.0.0.2", reader)
	if err == nil {
		t.Fatal("Expected DeleteVXLANPeerByDstWithReader to return error")
	}

	if !contains(err.Error(), "failed to delete VXLAN peer section") {
		t.Errorf("Expected error message to contain 'failed to delete VXLAN peer section', got: %v", err)
	}
}

func TestDeleteVXLANPeerByDstWithReader_CommitError(t *testing.T) {
	reader := newMockVXLANReader()
	reader.commitError = fmt.Errorf("commit error")

	err := DeleteVXLANPeerByDstWithReader("10.0.0.2", reader)
	if err == nil {
		t.Fatal("Expected DeleteVXLANPeerByDstWithReader to return error")
	}

	if !contains(err.Error(), "failed to commit VXLAN peer deletion") {
		t.Errorf("Expected error message to contain 'failed to commit VXLAN peer deletion', got: %v", err)
	}
}

func TestDeleteVXLANPeerByDst_Integration(t *testing.T) {
	reader := newMockVXLANReader()

	// Test deleting multiple peers
	peers := []string{"10.0.0.2", "10.0.0.3", "239.1.1.1"}

	for _, dst := range peers {
		// Verify peer exists
		if !VXLANPeerExistsByDstWithReader(dst, reader) {
			t.Errorf("Expected peer with dst %s to exist", dst)
		}

		// Delete the peer
		err := DeleteVXLANPeerByDstWithReader(dst, reader)
		if err != nil {
			t.Errorf("Failed to delete peer with dst %s: %v", dst, err)
		}
	}
}
