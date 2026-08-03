package mgmt

import (
	"context"
	"sync"
	"testing"

	"github.com/openmanet/openmanetd/internal/network"
)

type fakeWirelessStatusProvider struct {
	mu sync.RWMutex // protects the fields below

	Status map[string]*network.WirelessRadioStatus
	Err    error
}

func (f *fakeWirelessStatusProvider) GetWirelessStatus(_ context.Context) (map[string]*network.WirelessRadioStatus, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	return f.Status, f.Err
}

func wirelessStatusForRadio(t *testing.T, radio, ifname string) *fakeWirelessStatusProvider {
	t.Helper()

	return &fakeWirelessStatusProvider{Status: map[string]*network.WirelessRadioStatus{
		radio: {Interfaces: []network.WirelessRadioInterface{{Ifname: ifname}}},
	}}
}
