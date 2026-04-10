package comms

import (
	"context"
	"encoding/binary"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/rs/zerolog"
)

const (
	bluezService                       = "org.bluez"
	bluezRootPath                      = dbus.ObjectPath("/")
	bluezManagedObjectsMethod          = "org.freedesktop.DBus.ObjectManager.GetManagedObjects"
	bluezInterfacesAddedRule           = "type='signal',interface='org.freedesktop.DBus.ObjectManager',sender='org.bluez',member='InterfacesAdded'"
	bluezInterfacesRemovedRule         = "type='signal',interface='org.freedesktop.DBus.ObjectManager',sender='org.bluez',member='InterfacesRemoved'"
	bluezInterfacesAddedSignal         = "org.freedesktop.DBus.ObjectManager.InterfacesAdded"
	bluezInterfacesRemovedSignal       = "org.freedesktop.DBus.ObjectManager.InterfacesRemoved"
	bluezGattServiceInterface          = "org.bluez.GattService1"
	bluezGattCharacteristicInterface   = "org.bluez.GattCharacteristic1"
	bluezGattCharacteristicStartNotify = bluezGattCharacteristicInterface + ".StartNotify"
	bluezGattCharacteristicWriteValue  = bluezGattCharacteristicInterface + ".WriteValue"
	bluezDeviceConnectMethod           = bluezDeviceInterface + ".Connect"
	bluezDevicePairMethod              = bluezDeviceInterface + ".Pair"
	bluezAdapterConnectDeviceMethod    = "org.bluez.Adapter1.ConnectDevice"
	bs22Name                           = "BS-22"
	bs22HMServiceUUID                  = "00001100-d102-11e1-9b23-00025b00a5a5"
	bs22HMWriteUUID                    = "00001101-d102-11e1-9b23-00025b00a5a5"
	bs22HMNotifyUUID                   = "00001102-d102-11e1-9b23-00025b00a5a5"
	bs22HMVendorID                     = 0x100A
	bs22HMReadSettings                 = 13
	bs22HMPlayTone                     = 10
	bs22HMSetBLEAudio                  = 15
	bs22HMKeyEventInd                  = 257
	bs22HMResponseBit                  = 0x8000
	bs22HMSetBLEAudioEnabled           = 1
	bs22HMStartTone                    = 1
	bs22HMStopTone                     = 2
	bs22BLEConnectRetryInterval        = 5 * time.Second
	bs22BLERescanInterval              = 2 * time.Second
)

type bluezManagedObjectMap map[dbus.ObjectPath]map[string]map[string]dbus.Variant

type bluezManagedLister func(*dbus.Conn) (bluezManagedObjectMap, error)
type bluezCharacteristicNotifier func(*dbus.Conn, dbus.ObjectPath) error
type bluezCharacteristicWriter func(*dbus.Conn, dbus.ObjectPath, []byte) error
type bluezDeviceConnector func(*dbus.Conn, bs22DeviceInfo) error
type bluezDevicePairer func(*dbus.Conn, bs22DeviceInfo) error

type bs22DeviceInfo struct {
	Path        dbus.ObjectPath
	Adapter     dbus.ObjectPath
	Alias       string
	Name        string
	Address     string
	AddressType string
	UUIDs       []string
	Connected   bool
	Paired      bool
}

type bs22BLEBinding struct {
	Device      bs22DeviceInfo
	ServicePath dbus.ObjectPath
	WritePath   dbus.ObjectPath
	NotifyPath  dbus.ObjectPath
}

type bs22HMPacket struct {
	VendorID uint16
	Command  uint16
	Payload  []byte
}

func (p bs22HMPacket) CommandCode() uint16 {
	return p.Command &^ bs22HMResponseBit
}

func (p bs22HMPacket) IsResponse() bool {
	return p.Command&bs22HMResponseBit != 0
}

type bs22EventSource struct {
	log            zerolog.Logger
	dial           systemBusDialer
	listManaged    bluezManagedLister
	startNotify    bluezCharacteristicNotifier
	writeValue     bluezCharacteristicWriter
	connectDevice  bluezDeviceConnector
	pairDevice     bluezDevicePairer
	xeventFactory  func(zerolog.Logger) EventSource
	rescanInterval time.Duration
	connectRetry   time.Duration
	dedupeMu       sync.Mutex
	lastEvent      PTTEvent
	lastEventAt    time.Time
	stateMu        sync.RWMutex
	toneConn       *dbus.Conn
	toneBinding    bs22BLEBinding
	tonePrimed     bool
}

// NewBS22EventSource monitors the BS-22 BLE HM control path and falls back to
// BlueALSA XEVENT events from the Classic HFP link.
func NewBS22EventSource(log zerolog.Logger) EventSource {
	return &bs22EventSource{
		log: log,
		dial: func() (*dbus.Conn, error) {
			return dbus.ConnectSystemBus()
		},
		listManaged:   listBlueZManagedObjects,
		startNotify:   startBlueZNotify,
		writeValue:    writeBlueZCharacteristicValue,
		connectDevice: connectBlueZClassicDevice,
		pairDevice:    pairBlueZDevice,
		xeventFactory: NewBlueALSAXEventSource,
	}
}

// NewBluetoothEventSource is a compatibility alias for the BS-22 specific backend.
func NewBluetoothEventSource(log zerolog.Logger) EventSource {
	return NewBS22EventSource(log)
}

func (s *bs22EventSource) Events(ctx context.Context) <-chan PTTEvent {
	out := make(chan PTTEvent, 4)
	raw := make(chan PTTEvent, 8)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		s.monitorBLE(ctx, raw)
	}()

	if s.xeventFactory != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.forwardBlueALSAFallback(ctx, raw)
		}()
	}

	go func() {
		wg.Wait()
		close(raw)
	}()

	go func() {
		defer close(out)

		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-raw:
				if !ok {
					return
				}

				if !s.emitMergedEvent(ctx, out, ev) {
					return
				}
			}
		}
	}()

	return out
}

func (s *bs22EventSource) PlayStartTone() bool {
	return s.playTone(bs22HMStartTone)
}

func (s *bs22EventSource) PlayStopTone() bool {
	return s.playTone(bs22HMStopTone)
}

func (s *bs22EventSource) emitMergedEvent(ctx context.Context, out chan<- PTTEvent, ev PTTEvent) bool {
	if s.shouldSuppressDuplicate(ev) {
		return true
	}

	return sendPTTEvent(ctx, out, ev)
}

func (s *bs22EventSource) shouldSuppressDuplicate(ev PTTEvent) bool {
	s.dedupeMu.Lock()
	defer s.dedupeMu.Unlock()

	now := time.Now()
	if s.lastEvent == ev && now.Sub(s.lastEventAt) < 200*time.Millisecond {
		return true
	}

	s.lastEvent = ev
	s.lastEventAt = now
	return false
}

func (s *bs22EventSource) forwardBlueALSAFallback(ctx context.Context, out chan<- PTTEvent) {
	src := s.xeventFactory(s.log)
	if src == nil {
		return
	}

	events := src.Events(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}

			if !sendPTTEvent(ctx, out, ev) {
				return
			}
		}
	}
}

func (s *bs22EventSource) monitorBLE(ctx context.Context, out chan<- PTTEvent) {
	if s.dial == nil || s.listManaged == nil || s.startNotify == nil || s.writeValue == nil {
		s.log.Error().Msg("BS-22 BLE: monitor is not configured")
		return
	}

	conn, err := s.dial()
	if err != nil {
		s.log.Error().Err(err).Msg("BS-22 BLE: failed to connect to system DBus")
		return
	}
	s.setToneState(conn, bs22BLEBinding{}, false)
	defer s.setToneState(nil, bs22BLEBinding{}, false)
	defer conn.Close()

	for _, rule := range []string{
		bluezPropertiesChangedRule,
		bluezInterfacesAddedRule,
		bluezInterfacesRemovedRule,
	} {
		if err := addDBusMatch(conn, rule); err != nil {
			s.log.Error().Err(err).Str("rule", rule).Msg("BS-22 BLE: failed to add DBus match rule")
			return
		}
	}

	sigCh := make(chan *dbus.Signal, 32)
	conn.Signal(sigCh)
	defer conn.RemoveSignal(sigCh)

	rescanInterval := s.rescanInterval
	if rescanInterval <= 0 {
		rescanInterval = bs22BLERescanInterval
	}
	rescanTicker := time.NewTicker(rescanInterval)
	defer rescanTicker.Stop()

	connectRetry := s.connectRetry
	if connectRetry <= 0 {
		connectRetry = bs22BLEConnectRetryInterval
	}

	var current bs22BLEBinding
	var haveCurrent bool
	var primed bool
	var lastConnectAttempt time.Time
	var adapterConnectUnsupported bool
	pairAttempted := map[dbus.ObjectPath]bool{}

	syncBinding := func() {
		managed, err := s.listManaged(conn)
		if err != nil {
			s.log.Debug().Err(err).Msg("BS-22 BLE: failed to enumerate BlueZ managed objects")
			return
		}

		devices := findBS22Devices(managed)
		if len(devices) == 0 {
			haveCurrent = false
			primed = false
			pairAttempted = map[dbus.ObjectPath]bool{}
			s.setToneState(conn, bs22BLEBinding{}, false)
			return
		}

		primary := devices[0]
		binding, ok := findBS22BLEBindingForDevices(managed, devices)
		if !ok {
			if time.Since(lastConnectAttempt) >= connectRetry {
				lastConnectAttempt = time.Now()
				s.log.Info().
					Str("device", primary.Address).
					Str("path", string(primary.Path)).
					Str("adapter", string(primary.Adapter)).
					Str("address_type", normalizeBS22AddressType(primary.AddressType)).
					Bool("classic_connected", primary.Connected).
					Bool("paired", primary.Paired).
					Int("candidates", len(devices)).
					Msg("BS-22 BLE: HM service not present, attempting attach")
				for _, device := range devices {
					if device.Connected || s.connectDevice == nil || !isBS22ClassicConnectCandidate(device) {
						continue
					}
					if err := s.connectDevice(conn, device); err != nil {
						if isIgnorableBlueZConnectError(err) {
							s.log.Info().
								Err(err).
								Str("device", string(device.Path)).
								Msg("BS-22 BLE: classic connect request already in progress")
						} else {
							s.log.Warn().
								Err(err).
								Str("device", string(device.Path)).
								Msg("BS-22 BLE: classic connect request failed")
						}
					}
				}

				if !adapterConnectUnsupported {
					if err := connectBlueZLEDevice(conn, primary); err != nil {
						if isMissingBlueZMethodError(err) {
							adapterConnectUnsupported = true
							s.log.Warn().
								Err(err).
								Str("device", primary.Address).
								Str("adapter", string(primary.Adapter)).
								Msg("BS-22 BLE: Adapter1.ConnectDevice unsupported, falling back to Device1.Pair")
						} else if isIgnorableBlueZConnectError(err) {
							s.log.Info().
								Err(err).
								Str("device", primary.Address).
								Str("adapter", string(primary.Adapter)).
								Str("address_type", normalizeBS22AddressType(primary.AddressType)).
								Msg("BS-22 BLE: LE connect request already in progress")
						} else {
							s.log.Warn().
								Err(err).
								Str("device", primary.Address).
								Str("adapter", string(primary.Adapter)).
								Str("address_type", normalizeBS22AddressType(primary.AddressType)).
								Msg("BS-22 BLE: LE connect request failed")
						}
					} else {
						s.log.Info().
							Str("device", primary.Address).
							Str("adapter", string(primary.Adapter)).
							Str("address_type", normalizeBS22AddressType(primary.AddressType)).
							Msg("BS-22 BLE: LE connect request sent")
					}
				}

				if adapterConnectUnsupported && s.pairDevice != nil {
					for _, device := range devices {
						if pairAttempted[device.Path] {
							continue
						}
						if err := s.pairDevice(conn, device); err != nil {
							if isIgnorableBlueZPairError(err) {
								pairAttempted[device.Path] = true
								s.log.Info().
									Err(err).
									Str("device", string(device.Path)).
									Bool("paired", device.Paired).
									Msg("BS-22 BLE: Pair() request already satisfied")
							} else {
								s.log.Warn().
									Err(err).
									Str("device", string(device.Path)).
									Bool("paired", device.Paired).
									Msg("BS-22 BLE: Pair() request failed")
							}
						} else {
							pairAttempted[device.Path] = true
							s.log.Info().
								Str("device", string(device.Path)).
								Bool("paired", device.Paired).
								Msg("BS-22 BLE: Pair() request sent")
						}
					}
				}
			}
			haveCurrent = false
			primed = false
			s.setToneState(conn, bs22BLEBinding{}, false)
			return
		}

		if !haveCurrent || current.NotifyPath != binding.NotifyPath {
			if err := s.startNotify(conn, binding.NotifyPath); err != nil && !isIgnorableBlueZNotifyError(err) {
				s.log.Debug().Err(err).Str("path", string(binding.NotifyPath)).Msg("BS-22 BLE: StartNotify failed")
				return
			}

			current = binding
			haveCurrent = true
			primed = false
			s.setToneState(conn, current, false)
			s.log.Info().
				Str("device", current.Device.Address).
				Str("notify", string(current.NotifyPath)).
				Str("write", string(current.WritePath)).
				Msg("BS-22 BLE: monitoring HM control channel")
		}

		if primed {
			return
		}

		if err := s.primeBLE(conn, current); err != nil {
			s.log.Debug().Err(err).Str("device", current.Device.Address).Msg("BS-22 BLE: failed to prime HM control channel")
			s.setToneState(conn, current, false)
			return
		}

		primed = true
		s.setToneState(conn, current, true)
		s.log.Info().Str("device", current.Device.Address).Msg("BS-22 BLE: primed BLE audio mode")
	}

	syncBinding()

	for {
		select {
		case <-ctx.Done():
			return
		case <-rescanTicker.C:
			syncBinding()
		case sig, ok := <-sigCh:
			if !ok {
				return
			}

			if haveCurrent {
				if data, ok := bluezCharacteristicValue(sig, current.NotifyPath); ok {
					if ev, ok := bs22PTTEventFromBytes(data); ok {
						if !sendPTTEvent(ctx, out, ev) {
							return
						}
					} else if pkt, ok := parseBS22HMPacket(data); ok {
						s.log.Debug().
							Uint16("command", pkt.CommandCode()).
							Bool("response", pkt.IsResponse()).
							Int("payload_len", len(pkt.Payload)).
							Msg("BS-22 BLE: ignored HM packet")
					}
				}
			}

			if bluezNeedsBS22Rescan(sig, current.NotifyPath) {
				syncBinding()
			}
		}
	}
}

func (s *bs22EventSource) primeBLE(conn *dbus.Conn, binding bs22BLEBinding) error {
	for _, payload := range [][]byte{
		bs22HMCommandBytes(bs22HMVendorID, bs22HMReadSettings),
		bs22HMCommandBytes(bs22HMVendorID, bs22HMSetBLEAudio, bs22HMSetBLEAudioEnabled),
	} {
		if err := s.writeValue(conn, binding.WritePath, payload); err != nil {
			return err
		}
	}

	return nil
}

func (s *bs22EventSource) playTone(toneID uint16) bool {
	s.stateMu.RLock()
	conn := s.toneConn
	binding := s.toneBinding
	primed := s.tonePrimed
	writer := s.writeValue
	s.stateMu.RUnlock()

	if conn == nil || !primed || binding.WritePath == "" || writer == nil {
		return false
	}

	payload := make([]byte, 2)
	binary.BigEndian.PutUint16(payload, toneID)
	packet := bs22HMCommandBytes(bs22HMVendorID, bs22HMPlayTone, payload...)
	if err := writer(conn, binding.WritePath, packet); err != nil {
		s.log.Debug().Err(err).Uint16("tone", toneID).Msg("BS-22 BLE: PLAY_TONE failed")
		return false
	}

	return true
}

func (s *bs22EventSource) setToneState(conn *dbus.Conn, binding bs22BLEBinding, primed bool) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()

	s.toneConn = conn
	s.toneBinding = binding
	s.tonePrimed = primed
}

func listBlueZManagedObjects(conn *dbus.Conn) (bluezManagedObjectMap, error) {
	if conn == nil {
		return nil, errors.New("nil DBus connection")
	}

	var managed bluezManagedObjectMap
	call := conn.Object(bluezService, bluezRootPath).Call(bluezManagedObjectsMethod, 0)
	if call.Err != nil {
		return nil, call.Err
	}
	if err := call.Store(&managed); err != nil {
		return nil, err
	}

	return managed, nil
}

func startBlueZNotify(conn *dbus.Conn, path dbus.ObjectPath) error {
	if conn == nil {
		return errors.New("nil DBus connection")
	}

	return conn.Object(bluezService, path).Call(bluezGattCharacteristicStartNotify, 0).Err
}

func writeBlueZCharacteristicValue(conn *dbus.Conn, path dbus.ObjectPath, data []byte) error {
	if conn == nil {
		return errors.New("nil DBus connection")
	}

	options := map[string]dbus.Variant{}
	return conn.Object(bluezService, path).Call(bluezGattCharacteristicWriteValue, 0, data, options).Err
}

func connectBlueZDevice(conn *dbus.Conn, path dbus.ObjectPath) error {
	if conn == nil {
		return errors.New("nil DBus connection")
	}

	return conn.Object(bluezService, path).Call(bluezDeviceConnectMethod, 0).Err
}

func connectBlueZClassicDevice(conn *dbus.Conn, device bs22DeviceInfo) error {
	if device.Path == "" {
		return errors.New("empty BlueZ device path")
	}

	return connectBlueZDevice(conn, device.Path)
}

func pairBlueZDevice(conn *dbus.Conn, device bs22DeviceInfo) error {
	if conn == nil {
		return errors.New("nil DBus connection")
	}
	if device.Path == "" {
		return errors.New("empty BlueZ device path")
	}

	return conn.Object(bluezService, device.Path).Call(bluezDevicePairMethod, 0).Err
}

func connectBlueZLEDevice(conn *dbus.Conn, device bs22DeviceInfo) error {
	if conn == nil {
		return errors.New("nil DBus connection")
	}
	if device.Adapter == "" {
		return errors.New("empty BlueZ adapter path")
	}
	if device.Address == "" {
		return errors.New("empty BlueZ device address")
	}

	return conn.Object(bluezService, device.Adapter).Call(bluezAdapterConnectDeviceMethod, 0, bs22LEConnectProperties(device)).Err
}

func findBS22Device(managed bluezManagedObjectMap) (bs22DeviceInfo, bool) {
	devices := findBS22Devices(managed)
	if len(devices) == 0 {
		return bs22DeviceInfo{}, false
	}
	return devices[0], true
}

func findBS22Devices(managed bluezManagedObjectMap) []bs22DeviceInfo {
	devices := make([]bs22DeviceInfo, 0)
	devicesByPath := map[dbus.ObjectPath]bs22DeviceInfo{}
	bs22Addresses := map[string]struct{}{}

	for path, ifaces := range managed {
		props, ok := ifaces[bluezDeviceInterface]
		if !ok {
			continue
		}

		info := bluezDeviceInfoFromProps(path, props)

		if !isBS22Device(info, props) {
			continue
		}

		devicesByPath[path] = info
		if info.Address != "" {
			bs22Addresses[strings.ToUpper(info.Address)] = struct{}{}
		}
	}

	if len(devicesByPath) == 0 {
		return nil
	}

	for path, ifaces := range managed {
		props, ok := ifaces[bluezDeviceInterface]
		if !ok {
			continue
		}
		if _, exists := devicesByPath[path]; exists {
			continue
		}

		info := bluezDeviceInfoFromProps(path, props)
		if _, ok := bs22Addresses[strings.ToUpper(info.Address)]; !ok {
			continue
		}
		devicesByPath[path] = info
	}

	for _, info := range devicesByPath {
		devices = append(devices, info)
	}

	if len(devices) == 0 {
		return nil
	}

	sort.Slice(devices, func(i, j int) bool {
		if devices[i].Connected != devices[j].Connected {
			return devices[i].Connected
		}
		if hasBS22Name(devices[i]) != hasBS22Name(devices[j]) {
			return hasBS22Name(devices[i])
		}
		if devices[i].Paired != devices[j].Paired {
			return devices[i].Paired
		}

		return string(devices[i].Path) < string(devices[j].Path)
	})

	return devices
}

func findBS22BLEBindingForDevice(managed bluezManagedObjectMap, device bs22DeviceInfo) (bs22BLEBinding, bool) {
	return findBS22BLEBindingForDevices(managed, []bs22DeviceInfo{device})
}

func findBS22BLEBindingForDevices(managed bluezManagedObjectMap, devices []bs22DeviceInfo) (bs22BLEBinding, bool) {
	if len(devices) == 0 {
		return bs22BLEBinding{}, false
	}

	deviceRank := map[dbus.ObjectPath]int{}
	deviceByPath := map[dbus.ObjectPath]bs22DeviceInfo{}
	for i, d := range devices {
		deviceRank[d.Path] = i
		deviceByPath[d.Path] = d
	}

	var servicePath dbus.ObjectPath
	serviceDevicePath := dbus.ObjectPath("")
	type serviceCandidate struct {
		servicePath dbus.ObjectPath
		devicePath  dbus.ObjectPath
		rank        int
	}
	candidates := make([]serviceCandidate, 0)

	for path, ifaces := range managed {
		props, ok := ifaces[bluezGattServiceInterface]
		if !ok {
			continue
		}

		if !strings.EqualFold(variantString(props, "UUID"), bs22HMServiceUUID) {
			continue
		}

		devPath := variantObjectPath(props, "Device")
		rank, ok := deviceRank[devPath]
		if !ok {
			continue
		}

		candidates = append(candidates, serviceCandidate{servicePath: path, devicePath: devPath, rank: rank})
	}

	if len(candidates) == 0 {
		return bs22BLEBinding{}, false
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].rank != candidates[j].rank {
			return candidates[i].rank < candidates[j].rank
		}
		return string(candidates[i].servicePath) < string(candidates[j].servicePath)
	})
	servicePath = candidates[0].servicePath
	serviceDevicePath = candidates[0].devicePath

	var writePath dbus.ObjectPath
	var notifyPath dbus.ObjectPath
	charPaths := make([]dbus.ObjectPath, 0)
	for path, ifaces := range managed {
		props, ok := ifaces[bluezGattCharacteristicInterface]
		if !ok {
			continue
		}

		if variantObjectPath(props, "Service") != servicePath {
			continue
		}

		charPaths = append(charPaths, path)
	}

	sort.Slice(charPaths, func(i, j int) bool {
		return string(charPaths[i]) < string(charPaths[j])
	})

	for _, path := range charPaths {
		props := managed[path][bluezGattCharacteristicInterface]
		switch {
		case strings.EqualFold(variantString(props, "UUID"), bs22HMWriteUUID):
			writePath = path
		case strings.EqualFold(variantString(props, "UUID"), bs22HMNotifyUUID):
			notifyPath = path
		}
	}

	if writePath == "" || notifyPath == "" {
		return bs22BLEBinding{}, false
	}

	device := deviceByPath[serviceDevicePath]
	if device.Path == "" {
		if ifaces, ok := managed[serviceDevicePath]; ok {
			if props, ok := ifaces[bluezDeviceInterface]; ok {
				device = bluezDeviceInfoFromProps(serviceDevicePath, props)
			}
		}
	}

	return bs22BLEBinding{
		Device:      device,
		ServicePath: servicePath,
		WritePath:   writePath,
		NotifyPath:  notifyPath,
	}, true
}

func isBS22Device(device bs22DeviceInfo, props map[string]dbus.Variant) bool {
	if containsFold(device.Alias, bs22Name) || containsFold(device.Name, bs22Name) {
		return true
	}

	for _, uuid := range variantStringSlice(props, "UUIDs") {
		if strings.EqualFold(uuid, bs22HMServiceUUID) {
			return true
		}
	}

	return false
}

func hasBS22Name(device bs22DeviceInfo) bool {
	return containsFold(device.Alias, bs22Name) || containsFold(device.Name, bs22Name)
}

func isBS22ClassicConnectCandidate(device bs22DeviceInfo) bool {
	return hasUUIDFold(device.UUIDs, "0000111e-0000-1000-8000-00805f9b34fb") ||
		hasUUIDFold(device.UUIDs, "0000110b-0000-1000-8000-00805f9b34fb")
}

func hasUUIDFold(uuids []string, target string) bool {
	for _, u := range uuids {
		if strings.EqualFold(u, target) {
			return true
		}
	}
	return false
}

func bluezDeviceInfoFromProps(path dbus.ObjectPath, props map[string]dbus.Variant) bs22DeviceInfo {
	return bs22DeviceInfo{
		Path:        path,
		Adapter:     variantObjectPath(props, "Adapter"),
		Alias:       variantString(props, "Alias"),
		Name:        variantString(props, "Name"),
		Address:     variantString(props, "Address"),
		AddressType: variantString(props, "AddressType"),
		UUIDs:       variantStringSlice(props, "UUIDs"),
		Connected:   variantBool(props, "Connected"),
		Paired:      variantBool(props, "Paired"),
	}
}

func parseBS22HMPacket(data []byte) (bs22HMPacket, bool) {
	if len(data) < 4 {
		return bs22HMPacket{}, false
	}

	packet := bs22HMPacket{
		VendorID: binary.BigEndian.Uint16(data[0:2]),
		Command:  binary.BigEndian.Uint16(data[2:4]),
		Payload:  append([]byte(nil), data[4:]...),
	}

	return packet, true
}

func bs22PTTEventFromBytes(data []byte) (PTTEvent, bool) {
	packet, ok := parseBS22HMPacket(data)
	if !ok || packet.VendorID != bs22HMVendorID {
		return 0, false
	}

	return bs22PTTEventFromPacket(packet)
}

func bs22PTTEventFromPacket(packet bs22HMPacket) (PTTEvent, bool) {
	if packet.CommandCode() != bs22HMKeyEventInd || len(packet.Payload) < 2 {
		return 0, false
	}

	switch {
	case packet.Payload[0] == 16 && packet.Payload[1] == 16:
		return PTTDown, true
	case packet.Payload[0] == 16 && packet.Payload[1] == 17:
		return PTTUp, true
	default:
		return 0, false
	}
}

func bs22HMCommandBytes(vendorID, command uint16, payload ...byte) []byte {
	packet := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint16(packet[0:2], vendorID)
	binary.BigEndian.PutUint16(packet[2:4], command)
	copy(packet[4:], payload)
	return packet
}

func bluezCharacteristicValue(sig *dbus.Signal, path dbus.ObjectPath) ([]byte, bool) {
	if sig == nil || sig.Path != path || sig.Name != bluezPropertiesChangedSignal || len(sig.Body) < 2 {
		return nil, false
	}

	iface, ok := sig.Body[0].(string)
	if !ok || iface != bluezGattCharacteristicInterface {
		return nil, false
	}

	changed, ok := sig.Body[1].(map[string]dbus.Variant)
	if !ok {
		return nil, false
	}

	value, ok := changed["Value"]
	if !ok {
		return nil, false
	}

	data, ok := value.Value().([]byte)
	if !ok {
		return nil, false
	}

	return append([]byte(nil), data...), true
}

func bluezNeedsBS22Rescan(sig *dbus.Signal, notifyPath dbus.ObjectPath) bool {
	if sig == nil {
		return false
	}

	switch sig.Name {
	case bluezInterfacesAddedSignal, bluezInterfacesRemovedSignal:
		return true
	case bluezPropertiesChangedSignal:
		if sig.Path == notifyPath {
			return false
		}

		if len(sig.Body) == 0 {
			return false
		}

		iface, ok := sig.Body[0].(string)
		if !ok {
			return false
		}

		return iface == bluezDeviceInterface || iface == bluezGattServiceInterface || iface == bluezGattCharacteristicInterface
	default:
		return false
	}
}

func isIgnorableBlueZNotifyError(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "in progress") || strings.Contains(msg, "already notifying")
}

func isIgnorableBlueZConnectError(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already connected") ||
		strings.Contains(msg, "alreadyexists") ||
		strings.Contains(msg, "already exists") ||
		strings.Contains(msg, "in progress")
}

func isIgnorableBlueZPairError(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "alreadyexists") ||
		strings.Contains(msg, "already exists") ||
		strings.Contains(msg, "already paired") ||
		strings.Contains(msg, "in progress")
}

func isMissingBlueZMethodError(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "doesn't exist") ||
		strings.Contains(msg, "does not exist") ||
		strings.Contains(msg, "unknown method")
}

func normalizeBS22AddressType(addressType string) string {
	switch strings.ToLower(strings.TrimSpace(addressType)) {
	case "random":
		return "random"
	default:
		return "public"
	}
}

func bs22LEConnectProperties(device bs22DeviceInfo) map[string]dbus.Variant {
	return map[string]dbus.Variant{
		"Address":     dbus.MakeVariant(device.Address),
		"AddressType": dbus.MakeVariant(normalizeBS22AddressType(device.AddressType)),
	}
}

func variantString(props map[string]dbus.Variant, key string) string {
	if props == nil {
		return ""
	}

	v, ok := props[key]
	if !ok {
		return ""
	}

	value, _ := v.Value().(string)
	return value
}

func variantBool(props map[string]dbus.Variant, key string) bool {
	if props == nil {
		return false
	}

	v, ok := props[key]
	if !ok {
		return false
	}

	value, _ := v.Value().(bool)
	return value
}

func variantObjectPath(props map[string]dbus.Variant, key string) dbus.ObjectPath {
	if props == nil {
		return ""
	}

	v, ok := props[key]
	if !ok {
		return ""
	}

	value, _ := v.Value().(dbus.ObjectPath)
	return value
}

func variantStringSlice(props map[string]dbus.Variant, key string) []string {
	if props == nil {
		return nil
	}

	v, ok := props[key]
	if !ok {
		return nil
	}

	value, _ := v.Value().([]string)
	return value
}

func containsFold(s, substr string) bool {
	if substr == "" {
		return true
	}

	return strings.Contains(strings.ToUpper(s), strings.ToUpper(substr))
}
