package roip

import (
	"fmt"
	"net/netip"
	"testing"
	"time"

	"github.com/digineo/go-uci/v2"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/network"
	"github.com/rs/zerolog"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/types/key"
)

// MockConfigReader for testing VXLAN operations
type MockVXLANConfigReader struct {
	data                 map[string]map[string]map[string][]string
	addedPeers           []network.UCIVXLANPeer
	updatedPeers         map[string]network.UCIVXLANPeer
	deletedDsts          []string
	commitCalled         bool
	shouldFailCommit     bool
	shouldFailAdd        bool
	shouldFailUpdate     bool
	shouldFailDelete     bool
	lastAnonymousSection string // Track last anonymous section created
}

func newMockVXLANConfigReader() *MockVXLANConfigReader {
	return &MockVXLANConfigReader{
		data: map[string]map[string]map[string][]string{
			"network": {},
		},
		addedPeers:   []network.UCIVXLANPeer{},
		updatedPeers: make(map[string]network.UCIVXLANPeer),
		deletedDsts:  []string{},
	}
}

func (m *MockVXLANConfigReader) Get(config, section, option string) ([]string, bool) {
	if configData, ok := m.data[config]; ok {
		if sectionData, ok := configData[section]; ok {
			if values, ok := sectionData[option]; ok {
				return values, true
			}
		}
	}
	return nil, false
}

func (m *MockVXLANConfigReader) SetType(config, section, option string, typ uci.OptionType, values ...string) error {
	if m.data[config] == nil {
		m.data[config] = make(map[string]map[string][]string)
	}

	// Handle empty section name (anonymous section) - use the last one we created
	if section == "" {
		section = m.lastAnonymousSection
	}

	if m.data[config][section] == nil {
		m.data[config][section] = make(map[string][]string)
	}
	m.data[config][section][option] = values
	return nil
}

func (m *MockVXLANConfigReader) Del(config, section, option string) error {
	if configData, ok := m.data[config]; ok {
		if sectionData, ok := configData[section]; ok {
			delete(sectionData, option)
		}
	}
	return nil
}

func (m *MockVXLANConfigReader) AddSection(config, section, typ string) error {
	if m.shouldFailAdd {
		return fmt.Errorf("mock add error")
	}
	if m.data[config] == nil {
		m.data[config] = make(map[string]map[string][]string)
	}

	// Handle anonymous sections (empty string section name)
	if section == "" {
		// Find the next available numeric peer name
		peerNum := 0
		for {
			testSection := fmt.Sprintf("peer%d", peerNum)
			if _, exists := m.data[config][testSection]; !exists {
				section = testSection
				m.lastAnonymousSection = section
				break
			}
			peerNum++
		}
	}

	if m.data[config][section] == nil {
		m.data[config][section] = make(map[string][]string)
	}
	return nil
}

func (m *MockVXLANConfigReader) DelSection(config, section string) error {
	if m.shouldFailDelete {
		return fmt.Errorf("mock delete error")
	}
	if configData, ok := m.data[config]; ok {
		delete(configData, section)
	}
	return nil
}

func (m *MockVXLANConfigReader) Commit() error {
	m.commitCalled = true
	if m.shouldFailCommit {
		return fmt.Errorf("mock commit error")
	}
	return nil
}

func (m *MockVXLANConfigReader) ReloadConfig() error {
	return nil
}

func (m *MockVXLANConfigReader) addPeer(dst, via, vxlan string) {
	// Use numeric peer names like the real implementation
	peerNum := len(m.data["network"])
	section := fmt.Sprintf("peer%d", peerNum)
	if m.data["network"][section] == nil {
		m.data["network"][section] = make(map[string][]string)
	}
	m.data["network"][section]["dst"] = []string{dst}
	m.data["network"][section]["via"] = []string{via}
	m.data["network"][section]["vxlan"] = []string{vxlan}
}

func createTestROIP() *ROIP {
	cfg := &config.Config{}
	logger := zerolog.Nop()

	return &ROIP{
		Config:           cfg,
		Logger:           logger,
		uciNetworkConfig: newMockVXLANConfigReader(),
	}
}

func TestCreateVXMulticastPeers(t *testing.T) {
	r := createTestROIP()

	err := r.createVXMulticastPeers()
	if err != nil {
		t.Fatalf("createVXMulticastPeers failed: %v", err)
	}

	mockReader := r.uciNetworkConfig.(*MockVXLANConfigReader)
	if !mockReader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	// Verify multicast addresses were added
	for _, addr := range multicastGroupAddresses {
		found := false
		for section := range mockReader.data["network"] {
			if values, ok := mockReader.data["network"][section]["dst"]; ok && len(values) > 0 {
				if values[0] == addr {
					found = true
					break
				}
			}
		}
		if !found {
			t.Errorf("Expected multicast address %s to be added", addr)
		}
	}
}

func TestCreateVxlanPeer_New(t *testing.T) {
	r := createTestROIP()
	peerIP := "100.64.1.2"

	err := r.createVxlanPeer(peerIP)
	if err != nil {
		t.Fatalf("createVxlanPeer failed: %v", err)
	}

	mockReader := r.uciNetworkConfig.(*MockVXLANConfigReader)
	if !mockReader.commitCalled {
		t.Error("Expected Commit to be called")
	}

	// Verify peer was added
	found := false
	for section := range mockReader.data["network"] {
		if values, ok := mockReader.data["network"][section]["dst"]; ok && len(values) > 0 {
			if values[0] == peerIP {
				found = true
				// Verify other fields
				if via, ok := mockReader.data["network"][section]["via"]; !ok || len(via) == 0 || via[0] != defaultTunnelDeviceName {
					t.Error("Expected via field to be set correctly")
				}
				if vxlan, ok := mockReader.data["network"][section]["vxlan"]; !ok || len(vxlan) == 0 || vxlan[0] != defaultVxLanDeviceName {
					t.Error("Expected vxlan field to be set correctly")
				}
				break
			}
		}
	}
	if !found {
		t.Errorf("Expected peer %s to be added", peerIP)
	}
}

func TestCreateVxlanPeer_Update(t *testing.T) {
	r := createTestROIP()
	mockReader := r.uciNetworkConfig.(*MockVXLANConfigReader)

	peerIP := "100.64.1.2"

	// Add an existing peer
	mockReader.addPeer(peerIP, "old_tunnel", "old_vxlan")

	err := r.createVxlanPeer(peerIP)
	if err != nil {
		t.Fatalf("createVxlanPeer failed: %v", err)
	}

	// Verify peer was updated - find the section with this dst
	found := false
	for section := range mockReader.data["network"] {
		if values, ok := mockReader.data["network"][section]["dst"]; ok && len(values) > 0 && values[0] == peerIP {
			found = true
			if via, ok := mockReader.data["network"][section]["via"]; !ok || len(via) == 0 || via[0] != defaultTunnelDeviceName {
				t.Error("Expected via field to be updated")
			}
			if vxlan, ok := mockReader.data["network"][section]["vxlan"]; !ok || len(vxlan) == 0 || vxlan[0] != defaultVxLanDeviceName {
				t.Error("Expected vxlan field to be updated")
			}
			break
		}
	}
	if !found {
		t.Error("Expected to find updated peer")
	}
}

func TestSyncVXLANPeersWithTailscale_NoPeers(t *testing.T) {
	r := createTestROIP()

	// No status worker or peers
	err := r.syncVXLANPeersWithTailscale()
	if err != nil {
		t.Fatalf("syncVXLANPeersWithTailscale failed: %v", err)
	}
}

func TestSyncVXLANPeersWithTailscale_AddPeers(t *testing.T) {
	r := createTestROIP()

	// Create mock peers
	nodeKey1 := key.NewNode()
	ip1, _ := netip.ParseAddr("100.64.1.2")
	peer1 := &ipnstate.PeerStatus{
		HostName:     "peer1",
		TailscaleIPs: []netip.Addr{ip1},
	}

	nodeKey2 := key.NewNode()
	ip2, _ := netip.ParseAddr("100.64.1.3")
	peer2 := &ipnstate.PeerStatus{
		HostName:     "peer2",
		TailscaleIPs: []netip.Addr{ip2},
	}

	mockStatus := &ipnstate.Status{
		Peer: map[key.NodePublic]*ipnstate.PeerStatus{
			nodeKey1.Public(): peer1,
			nodeKey2.Public(): peer2,
		},
	}

	mockClient := &MockStatusClient{}
	mockClient.SetStatus(mockStatus)

	// interval removed
	r.statusWorker = NewStatusWorker(mockClient, 1*time.Second, r.Logger)
	r.statusWorker.fetchAndStoreStatus()

	err := r.syncVXLANPeersWithTailscale()
	if err != nil {
		t.Fatalf("syncVXLANPeersWithTailscale failed: %v", err)
	}

	mockReader := r.uciNetworkConfig.(*MockVXLANConfigReader)

	// Verify both peers were added
	foundPeers := 0
	for section := range mockReader.data["network"] {
		if values, ok := mockReader.data["network"][section]["dst"]; ok && len(values) > 0 {
			dst := values[0]
			if dst == ip1.String() || dst == ip2.String() {
				foundPeers++
			}
		}
	}

	if foundPeers != 2 {
		t.Errorf("Expected 2 peers to be added, found %d", foundPeers)
	}
}

func TestSyncVXLANPeersWithTailscale_RemoveInactivePeers(t *testing.T) {
	r := createTestROIP()
	mockReader := r.uciNetworkConfig.(*MockVXLANConfigReader)

	// Add some existing peers
	mockReader.addPeer("100.64.1.2", defaultTunnelDeviceName, defaultVxLanDeviceName)
	mockReader.addPeer("100.64.1.3", defaultTunnelDeviceName, defaultVxLanDeviceName)
	mockReader.addPeer("100.64.1.4", defaultTunnelDeviceName, defaultVxLanDeviceName)

	// Create mock peers with only one active peer
	nodeKey1 := key.NewNode()
	ip1, _ := netip.ParseAddr("100.64.1.2")
	peer1 := &ipnstate.PeerStatus{
		HostName:     "peer1",
		TailscaleIPs: []netip.Addr{ip1},
	}

	mockStatus := &ipnstate.Status{
		Peer: map[key.NodePublic]*ipnstate.PeerStatus{
			nodeKey1.Public(): peer1,
		},
	}

	mockClient := &MockStatusClient{}
	mockClient.SetStatus(mockStatus)

	// interval removed
	r.statusWorker = NewStatusWorker(mockClient, 1*time.Second, r.Logger)
	r.statusWorker.fetchAndStoreStatus()

	err := r.syncVXLANPeersWithTailscale()
	if err != nil {
		t.Fatalf("syncVXLANPeersWithTailscale failed: %v", err)
	}

	// Verify only the active peer remains
	foundPeers := 0
	for section := range mockReader.data["network"] {
		if values, ok := mockReader.data["network"][section]["dst"]; ok && len(values) > 0 {
			dst := values[0]
			if dst == "100.64.1.2" {
				foundPeers++
			}
			if dst == "100.64.1.3" || dst == "100.64.1.4" {
				t.Errorf("Inactive peer %s should have been removed", dst)
			}
		}
	}

	if foundPeers != 1 {
		t.Errorf("Expected 1 active peer to remain, found %d", foundPeers)
	}
}

func TestSyncVXLANPeersWithTailscale_PreserveMulticast(t *testing.T) {
	r := createTestROIP()
	mockReader := r.uciNetworkConfig.(*MockVXLANConfigReader)

	// Add multicast peers
	for _, addr := range multicastGroupAddresses {
		mockReader.addPeer(addr, defaultTunnelDeviceName, defaultVxLanDeviceName)
	}

	// Add a unicast peer
	mockReader.addPeer("100.64.1.2", defaultTunnelDeviceName, defaultVxLanDeviceName)

	// Create status with no active peers
	mockStatus := &ipnstate.Status{
		Peer: map[key.NodePublic]*ipnstate.PeerStatus{},
	}

	mockClient := &MockStatusClient{}
	mockClient.SetStatus(mockStatus)

	r.statusWorker = NewStatusWorker(mockClient, 1*time.Second, r.Logger)
	r.statusWorker.fetchAndStoreStatus()

	err := r.syncVXLANPeersWithTailscale()
	if err != nil {
		t.Fatalf("syncVXLANPeersWithTailscale failed: %v", err)
	}

	// Verify multicast peers are preserved
	for _, addr := range multicastGroupAddresses {
		found := false
		for section := range mockReader.data["network"] {
			if values, ok := mockReader.data["network"][section]["dst"]; ok && len(values) > 0 {
				if values[0] == addr {
					found = true
					break
				}
			}
		}
		if !found {
			t.Errorf("Multicast peer %s should have been preserved", addr)
		}
	}

	// Verify unicast peer was removed
	for section := range mockReader.data["network"] {
		if values, ok := mockReader.data["network"][section]["dst"]; ok && len(values) > 0 {
			if values[0] == "100.64.1.2" {
				t.Error("Inactive unicast peer should have been removed")
			}
		}
	}
}

func TestSyncVXLANPeersWithTailscale_PeerWithoutIP(t *testing.T) {
	r := createTestROIP()

	// Create a peer without IPs
	nodeKey1 := key.NewNode()
	peer1 := &ipnstate.PeerStatus{
		HostName:     "peer1",
		TailscaleIPs: []netip.Addr{}, // No IPs
	}

	mockStatus := &ipnstate.Status{
		Peer: map[key.NodePublic]*ipnstate.PeerStatus{
			nodeKey1.Public(): peer1,
		},
	}

	mockClient := &MockStatusClient{}
	mockClient.SetStatus(mockStatus)

	// interval removed
	r.statusWorker = NewStatusWorker(mockClient, 1*time.Second, r.Logger)
	r.statusWorker.fetchAndStoreStatus()

	err := r.syncVXLANPeersWithTailscale()
	if err != nil {
		t.Fatalf("syncVXLANPeersWithTailscale failed: %v", err)
	}

	mockReader := r.uciNetworkConfig.(*MockVXLANConfigReader)

	// Verify no peers were added (peer has no IP)
	peerCount := 0
	for section := range mockReader.data["network"] {
		if _, ok := mockReader.data["network"][section]["dst"]; ok {
			peerCount++
		}
	}

	if peerCount != 0 {
		t.Errorf("Expected 0 peers to be added, found %d", peerCount)
	}
}

func TestRemoveInactiveVXLANPeers(t *testing.T) {
	r := createTestROIP()
	mockReader := r.uciNetworkConfig.(*MockVXLANConfigReader)

	// Add various peers
	mockReader.addPeer("100.64.1.2", defaultTunnelDeviceName, defaultVxLanDeviceName)
	mockReader.addPeer("100.64.1.3", defaultTunnelDeviceName, defaultVxLanDeviceName)
	mockReader.addPeer("239.2.3.1", defaultTunnelDeviceName, defaultVxLanDeviceName) // multicast

	activePeerIPs := map[string]bool{
		"100.64.1.2": true, // Keep this one
	}

	err := r.removeInactiveVXLANPeers(activePeerIPs)
	if err != nil {
		t.Fatalf("removeInactiveVXLANPeers failed: %v", err)
	}

	// Verify active peer remains
	found := false
	for section := range mockReader.data["network"] {
		if values, ok := mockReader.data["network"][section]["dst"]; ok && len(values) > 0 {
			if values[0] == "100.64.1.2" {
				found = true
			}
			// Inactive unicast should be removed
			if values[0] == "100.64.1.3" {
				t.Error("Inactive peer 100.64.1.3 should have been removed")
			}
		}
	}
	if !found {
		t.Error("Active peer 100.64.1.2 should remain")
	}

	// Verify multicast is preserved
	found = false
	for section := range mockReader.data["network"] {
		if values, ok := mockReader.data["network"][section]["dst"]; ok && len(values) > 0 {
			if values[0] == "239.2.3.1" {
				found = true
			}
		}
	}
	if !found {
		t.Error("Multicast peer should have been preserved")
	}
}
