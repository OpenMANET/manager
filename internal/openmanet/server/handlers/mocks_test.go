package handlers_test

import (
	"context"
	"database/sql"
	"net"
	"sync"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mdlayher/wifi"
	"github.com/openmanet/openmanetd/internal/database/models"
	"github.com/openmanet/openmanetd/internal/gpsd"
)

// ── fakeBLOSManager ────────────────────────────────────────────────────────────

// fakeBLOSManager implements blos.BLOSLifecycle for handler tests.
type fakeBLOSManager struct {
	mu                      sync.Mutex
	running                 bool
	enableErr               error
	enableCalls             int
	configureAndEnableErr   error
	configureAndEnableCalls int
	disableCalls            int
}

func (f *fakeBLOSManager) ConfigureAndEnable(_ context.Context, _ string, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.configureAndEnableCalls++

	if f.configureAndEnableErr != nil {
		return f.configureAndEnableErr
	}

	f.running = true

	return nil
}

func (f *fakeBLOSManager) Enable(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.enableCalls++

	if f.enableErr != nil {
		return f.enableErr
	}

	f.running = true

	return nil
}

func (f *fakeBLOSManager) Disable() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.disableCalls++
	f.running = false
}

func (f *fakeBLOSManager) IsRunning() bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.running
}

func (f *fakeBLOSManager) getConfigureAndEnableCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.configureAndEnableCalls
}

func (f *fakeBLOSManager) getEnableCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.enableCalls
}

func (f *fakeBLOSManager) getDisableCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.disableCalls
}

// ── fakeCommsManager ───────────────────────────────────────────────────────────

// fakeCommsManager implements comms.CommsLifecycle for handler tests.
type fakeCommsManager struct {
	mu           sync.Mutex
	running      bool
	enableErr    error
	enableCalls  int
	disableCalls int
}

func (f *fakeCommsManager) Enable() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.enableCalls++

	if f.enableErr != nil {
		return f.enableErr
	}

	f.running = true

	return nil
}

func (f *fakeCommsManager) Disable() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.disableCalls++
	f.running = false
}

func (f *fakeCommsManager) IsRunning() bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.running
}

func (f *fakeCommsManager) getEnableCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.enableCalls
}

func (f *fakeCommsManager) getDisableCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.disableCalls
}

// ── fakeWireless ────────────────────────────────────────────────────────────

// fakeWireless implements mgmt.WirelessProvider for use in tests.
type fakeWireless struct {
	interfaces    []*wifi.Interface
	interfacesErr error

	meshInterfaces    []*wifi.Interface
	meshInterfacesErr error

	stationInfo        []*wifi.StationInfo
	stationInfoByIface map[string][]*wifi.StationInfo // per-interface station info
	stationInfoErr     error
}

func (f *fakeWireless) Interfaces() ([]*wifi.Interface, error) {
	return f.interfaces, f.interfacesErr
}

func (f *fakeWireless) GetMeshInterfaces() ([]*wifi.Interface, error) {
	return f.meshInterfaces, f.meshInterfacesErr
}

func (f *fakeWireless) StationInfo(iface *wifi.Interface) ([]*wifi.StationInfo, error) {
	if f.stationInfoByIface != nil {
		if stations, ok := f.stationInfoByIface[iface.Name]; ok {
			return stations, f.stationInfoErr
		}
	}

	return f.stationInfo, f.stationInfoErr
}

// ── helper constructors ──────────────────────────────────────────────────────

// makeInterface creates a wifi.Interface with fields useful in tests.
func makeInterface(name string, ifType wifi.InterfaceType) *wifi.Interface {
	mac, _ := net.ParseMAC("aa:bb:cc:dd:ee:ff")

	return &wifi.Interface{
		Index:        1,
		Name:         name,
		HardwareAddr: mac,
		PHY:          0,
		Device:       1,
		Type:         ifType,
		Frequency:    2412,
		ChannelWidth: 20,
	}
}

// makeStation creates a wifi.StationInfo for use in tests.
func makeStation(macStr string, signal int) *wifi.StationInfo {
	mac, _ := net.ParseMAC(macStr)

	return &wifi.StationInfo{
		HardwareAddr:    mac,
		Signal:          signal,
		SignalAverage:   signal,
		TransmitBitrate: 54000,
	}
}

// ── fakeGNSSProvider ────────────────────────────────────────────────────────

type fakeGNSSProvider struct {
	mu              sync.Mutex
	position        gpsd.PositionReport
	satelliteReport gpsd.SatelliteReport
}

func (f *fakeGNSSProvider) GetPosition() gpsd.PositionReport {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.position
}

func (f *fakeGNSSProvider) GetSatelliteReport() gpsd.SatelliteReport {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.satelliteReport
}

// ── in-memory SQLite DB helper ───────────────────────────────────────────────

const schemaSQL = `
CREATE TABLE IF NOT EXISTS mesh_nodes (
  mac_addr       text PRIMARY KEY NOT NULL,
  hostname       text NOT NULL,
  ip_addr        text NOT NULL,
  latitude       real,
  longitude      real,
  altitude       real,
  uci_dhcp_start integer,
  uci_dhcp_limit integer,
  created_at     timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at     timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP
);`

// newTestDB opens an in-memory SQLite database, applies the schema, and returns
// a *models.Queries ready for use. The database is closed when the test ends.
func newTestDB(t *testing.T) *models.Queries {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}

	t.Cleanup(func() { db.Close() })

	if _, err = db.Exec(schemaSQL); err != nil {
		t.Fatalf("apply schema: %v", err)
	}

	return models.New(db)
}
