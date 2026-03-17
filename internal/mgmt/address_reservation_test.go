package mgmt

import (
	"context"
	"errors"
	"testing"

	"github.com/digineo/go-uci/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeNetworkReader implements network.ConfigReader for address reservation tests.
// It reuses the same map-based approach as fakeOpenMANETReader / fakeWirelessReader.
type fakeNetworkReader struct {
	data        map[string]map[string]map[string][]string
	sections    map[string]map[string]string
	commitErr   error
	delErr      error
	commitCalls int
}

func newFakeNetworkReader() *fakeNetworkReader {
	return &fakeNetworkReader{
		data:     make(map[string]map[string]map[string][]string),
		sections: make(map[string]map[string]string),
	}
}

func (f *fakeNetworkReader) Get(config, section, option string) ([]string, bool) {
	if f.data[config] == nil || f.data[config][section] == nil {
		return nil, false
	}

	v, ok := f.data[config][section][option]

	return v, ok
}

func (f *fakeNetworkReader) GetSections(config, secType string) ([]string, error) {
	var out []string

	if f.sections[config] != nil {
		for s, t := range f.sections[config] {
			if t == secType {
				out = append(out, s)
			}
		}
	}

	return out, nil
}

func (f *fakeNetworkReader) SetType(config, section, option string, _ uci.OptionType, values ...string) error {
	if f.data[config] == nil {
		f.data[config] = make(map[string]map[string][]string)
	}

	if f.data[config][section] == nil {
		f.data[config][section] = make(map[string][]string)
	}

	f.data[config][section][option] = values

	return nil
}

func (f *fakeNetworkReader) Del(config, section, option string) error {
	if f.data[config] != nil && f.data[config][section] != nil {
		delete(f.data[config][section], option)
	}

	return nil
}

func (f *fakeNetworkReader) AddSection(config, section, typ string) error {
	if f.sections[config] == nil {
		f.sections[config] = make(map[string]string)
	}

	f.sections[config][section] = typ

	if f.data[config] == nil {
		f.data[config] = make(map[string]map[string][]string)
	}

	if f.data[config][section] == nil {
		f.data[config][section] = make(map[string][]string)
	}

	return nil
}

func (f *fakeNetworkReader) DelSection(config, section string) error {
	if f.delErr != nil {
		return f.delErr
	}

	if f.data[config] != nil {
		delete(f.data[config], section)
	}

	if f.sections[config] != nil {
		delete(f.sections[config], section)
	}

	return nil
}

func (f *fakeNetworkReader) Commit() error {
	f.commitCalls++

	return f.commitErr
}

func (f *fakeNetworkReader) ReloadConfig() error {
	return nil
}

// seedLanNetworkSection seeds a "lan" section so NetworkSectionExistsWithReader returns true.
func (f *fakeNetworkReader) seedLanNetworkSection() {
	_ = f.AddSection("network", "lan", "interface")
	_ = f.SetType("network", "lan", "proto", uci.TypeOption, "static")
}

// seedLanDHCPSection seeds a "lan" section so DHCPSectionExistsWithReader returns true.
func (f *fakeNetworkReader) seedLanDHCPSection() {
	_ = f.AddSection("dhcp", "lan", "dhcp")
	_ = f.SetType("dhcp", "lan", "interface", uci.TypeOption, "lan")
}

func noopReload(_ context.Context) error {
	return nil
}

func TestCleanUpInterfacesWithDeps_GatewayModeSkips(t *testing.T) {
	cfg := newTestManagementConfig()
	arw := &AddressReservationWorker{Config: cfg}

	networkReader := newFakeNetworkReader()
	dhcpReader := newFakeNetworkReader()

	err := arw.cleanUpInterfacesWithDeps(true, networkReader, dhcpReader, noopReload)
	require.NoError(t, err)

	assert.Equal(t, 0, networkReader.commitCalls, "no network commits expected in gateway mode")
	assert.Equal(t, 0, dhcpReader.commitCalls, "no DHCP commits expected in gateway mode")
}

func TestCleanUpInterfacesWithDeps_LanSectionsExist(t *testing.T) {
	cfg := newTestManagementConfig()
	arw := &AddressReservationWorker{Config: cfg}

	networkReader := newFakeNetworkReader()
	networkReader.seedLanNetworkSection()

	dhcpReader := newFakeNetworkReader()
	dhcpReader.seedLanDHCPSection()

	err := arw.cleanUpInterfacesWithDeps(false, networkReader, dhcpReader, noopReload)
	require.NoError(t, err)

	// Verify lan sections were deleted.
	_, networkExists := networkReader.Get("network", "lan", "proto")
	assert.False(t, networkExists, "lan network section should be deleted")

	_, dhcpExists := dhcpReader.Get("dhcp", "lan", "interface")
	assert.False(t, dhcpExists, "lan DHCP section should be deleted")

	// DeleteNetworkConfigWithReader calls Commit internally, plus cleanUpInterfacesWithDeps
	// calls Commit again. So we expect 2 commits per reader.
	assert.Equal(t, 2, networkReader.commitCalls)
	assert.Equal(t, 2, dhcpReader.commitCalls)
}

func TestCleanUpInterfacesWithDeps_NoLanSections(t *testing.T) {
	cfg := newTestManagementConfig()
	arw := &AddressReservationWorker{Config: cfg}

	networkReader := newFakeNetworkReader()
	dhcpReader := newFakeNetworkReader()

	err := arw.cleanUpInterfacesWithDeps(false, networkReader, dhcpReader, noopReload)
	require.NoError(t, err)

	// Commits still happen even when no sections to delete.
	assert.Equal(t, 1, networkReader.commitCalls)
	assert.Equal(t, 1, dhcpReader.commitCalls)
}

func TestCleanUpInterfacesWithDeps_NetworkCommitError(t *testing.T) {
	cfg := newTestManagementConfig()
	arw := &AddressReservationWorker{Config: cfg}

	networkReader := newFakeNetworkReader()
	networkReader.commitErr = errors.New("network commit failure")

	dhcpReader := newFakeNetworkReader()

	err := arw.cleanUpInterfacesWithDeps(false, networkReader, dhcpReader, noopReload)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error committing network config")
}

func TestCleanUpInterfacesWithDeps_DHCPCommitError(t *testing.T) {
	cfg := newTestManagementConfig()
	arw := &AddressReservationWorker{Config: cfg}

	networkReader := newFakeNetworkReader()
	dhcpReader := newFakeNetworkReader()
	dhcpReader.commitErr = errors.New("dhcp commit failure")

	err := arw.cleanUpInterfacesWithDeps(false, networkReader, dhcpReader, noopReload)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error committing DHCP config")
}

func TestCleanUpInterfacesWithDeps_NetworkDeleteError(t *testing.T) {
	cfg := newTestManagementConfig()
	arw := &AddressReservationWorker{Config: cfg}

	networkReader := newFakeNetworkReader()
	networkReader.seedLanNetworkSection()
	networkReader.delErr = errors.New("delete failed")

	dhcpReader := newFakeNetworkReader()

	err := arw.cleanUpInterfacesWithDeps(false, networkReader, dhcpReader, noopReload)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error deleting 'lan' network section")
}

func TestCleanUpInterfacesWithDeps_ReloadError(t *testing.T) {
	cfg := newTestManagementConfig()
	arw := &AddressReservationWorker{Config: cfg}

	networkReader := newFakeNetworkReader()
	dhcpReader := newFakeNetworkReader()

	reloadErr := func(_ context.Context) error {
		return errors.New("reload failed")
	}

	err := arw.cleanUpInterfacesWithDeps(false, networkReader, dhcpReader, reloadErr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error reloading network configuration")
}
