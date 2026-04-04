package comms

import (
	"context"
	"errors"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/rs/zerolog"
)

const (
	bluezPropertiesChangedRule      = "type='signal',interface='org.freedesktop.DBus.Properties',sender='org.bluez'"
	bluezPropertiesChangedSignal    = "org.freedesktop.DBus.Properties.PropertiesChanged"
	bluealsaService                 = "org.bluealsa"
	bluealsaPathPrefix              = "/org/bluealsa"
	bluealsaManagerPath             = dbus.ObjectPath("/org/bluealsa")
	bluealsaManagedObjectsMethod    = "org.freedesktop.DBus.ObjectManager.GetManagedObjects"
	bluealsaInterfacesAddedRule     = "type='signal',interface='org.freedesktop.DBus.ObjectManager',path='/org/bluealsa',member='InterfacesAdded'"
	bluealsaInterfacesRemovedRule   = "type='signal',interface='org.freedesktop.DBus.ObjectManager',path='/org/bluealsa',member='InterfacesRemoved'"
	bluealsaInterfacesAddedSignal   = "org.freedesktop.DBus.ObjectManager.InterfacesAdded"
	bluealsaInterfacesRemovedSignal = "org.freedesktop.DBus.ObjectManager.InterfacesRemoved"
	bluealsaRFCOMMInterface         = "org.bluealsa.RFCOMM1"
	bluealsaRFCOMMOpenMethod        = bluealsaRFCOMMInterface + ".Open"
	bluezDeviceInterface            = "org.bluez.Device1"
	bluezHFPUUID                    = "0000111e-0000-1000-8000-00805f9b34fb"
	bluealsaJournalMarker           = "AT message: SET: command:+XEVENT, value:"
)

type systemBusDialer func() (*dbus.Conn, error)
type rfcommPathLister func(*dbus.Conn) ([]dbus.ObjectPath, error)
type rfcommOpener func(*dbus.Conn, dbus.ObjectPath) (io.ReadCloser, error)

type blueALSAXEventSource struct {
	log           zerolog.Logger
	dial          systemBusDialer
	listRFCOMM    rfcommPathLister
	openRFCOMM    rfcommOpener
	dedupeMu      sync.Mutex
	lastEventName string
	lastEventAt   time.Time
}

type blueALSARFCOMMMonitor struct {
	cancel context.CancelFunc
}

type bluetoothEventSource struct {
	log           zerolog.Logger
	dial          systemBusDialer
	xeventFactory func(zerolog.Logger) EventSource
}

// NewBlueALSAXEventSource monitors BlueALSA DBus messages and emits PTT press/release events.
func NewBlueALSAXEventSource(log zerolog.Logger) EventSource {
	return &blueALSAXEventSource{
		log: log,
		dial: func() (*dbus.Conn, error) {
			return dbus.ConnectSystemBus()
		},
		listRFCOMM: listBlueALSARFCOMMPaths,
		openRFCOMM: openBlueALSARFCOMM,
	}
}

// NewBluetoothEventSource monitors BlueZ device state and forwards BlueALSA XEVENT presses.
func NewBluetoothEventSource(log zerolog.Logger) EventSource {
	return &bluetoothEventSource{
		log: log,
		dial: func() (*dbus.Conn, error) {
			return dbus.ConnectSystemBus()
		},
		xeventFactory: NewBlueALSAXEventSource,
	}
}

func (s *blueALSAXEventSource) Events(ctx context.Context) <-chan PTTEvent {
	ch := make(chan PTTEvent, 4)

	go func() {
		defer close(ch)

		if s.dial == nil {
			return
		}

		conn, err := s.dial()
		if err != nil {
			s.log.Error().Err(err).Msg("BlueALSA: failed to connect to system DBus")
			return
		}
		defer conn.Close()

		if err := addDBusMatch(conn, bluealsaInterfacesAddedRule); err != nil {
			s.log.Error().Err(err).Msg("BlueALSA: failed to add InterfacesAdded match rule")
			return
		}

		if err := addDBusMatch(conn, bluealsaInterfacesRemovedRule); err != nil {
			s.log.Error().Err(err).Msg("BlueALSA: failed to add InterfacesRemoved match rule")
			return
		}

		if s.listRFCOMM == nil || s.openRFCOMM == nil {
			s.log.Error().Msg("BlueALSA: RFCOMM monitor is not configured")
			return
		}

		sigCh := make(chan *dbus.Signal, 16)
		conn.Signal(sigCh)
		defer conn.RemoveSignal(sigCh)

		s.log.Info().Msg("Starting BlueALSA XEVENT monitor via RFCOMM1.Open()")

		paths, err := s.listRFCOMM(conn)
		if err != nil {
			s.log.Error().Err(err).Msg("BlueALSA: failed to enumerate RFCOMM objects")
		}

		active := make(map[dbus.ObjectPath]*blueALSARFCOMMMonitor)
		var activeMu sync.Mutex

		startMonitor := func(path dbus.ObjectPath) {
			activeMu.Lock()
			if _, exists := active[path]; exists {
				activeMu.Unlock()
				return
			}
			monitorCtx, cancel := context.WithCancel(ctx)
			monitor := &blueALSARFCOMMMonitor{cancel: cancel}
			active[path] = monitor
			activeMu.Unlock()

			go func() {
				defer func() {
					activeMu.Lock()
					if current, exists := active[path]; exists && current == monitor {
						delete(active, path)
					}
					activeMu.Unlock()
					cancel()
				}()

				s.monitorRFCOMM(monitorCtx, conn, path, ch)
			}()
		}

		stopMonitor := func(path dbus.ObjectPath) {
			activeMu.Lock()
			monitor, exists := active[path]
			if exists {
				delete(active, path)
			}
			activeMu.Unlock()

			if exists {
				monitor.cancel()
			}
		}

		stopAll := func() {
			activeMu.Lock()
			cancels := make([]context.CancelFunc, 0, len(active))
			for path, monitor := range active {
				cancels = append(cancels, monitor.cancel)
				delete(active, path)
			}
			activeMu.Unlock()

			for _, cancel := range cancels {
				cancel()
			}
		}

		for _, path := range paths {
			startMonitor(path)
		}

		for {
			select {
			case <-ctx.Done():
				stopAll()
				return
			case sig, ok := <-sigCh:
				if !ok {
					stopAll()
					return
				}

				if path, ok := blueALSAInterfacesAddedRFCOMMPath(sig); ok {
					startMonitor(path)
				}
				if path, ok := blueALSAInterfacesRemovedRFCOMMPath(sig); ok {
					stopMonitor(path)
				}
			}
		}
	}()

	return ch
}

func (s *blueALSAXEventSource) monitorRFCOMM(
	ctx context.Context,
	conn *dbus.Conn,
	path dbus.ObjectPath,
	out chan<- PTTEvent,
) {
	reader, err := s.openRFCOMM(conn, path)
	if err != nil {
		s.log.Debug().Err(err).Str("path", string(path)).Msg("BlueALSA: failed to open RFCOMM channel")
		return
	}
	defer reader.Close()

	s.log.Info().Str("path", string(path)).Msg("BlueALSA: monitoring RFCOMM channel")

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = reader.Close()
		case <-done:
		}
	}()
	defer close(done)

	s.consumeRFCOMM(ctx, out, reader)
}

func (s *blueALSAXEventSource) emitEventName(ctx context.Context, out chan<- PTTEvent, eventName string) bool {
	if s.shouldSuppressDuplicate(eventName) {
		return true
	}

	s.log.Debug().Msgf("BlueALSA XEVENT: %s", eventName)

	var ev PTTEvent

	switch eventName {
	case "PTT_DOWN":
		ev = PTTDown
	case "PTT_UP":
		ev = PTTUp
	default:
		s.log.Debug().Msgf("Ignoring unsupported BlueALSA XEVENT: %s", eventName)

		return true
	}

	return sendPTTEvent(ctx, out, ev)
}

func (s *blueALSAXEventSource) shouldSuppressDuplicate(eventName string) bool {
	if eventName == "" {
		return false
	}

	s.dedupeMu.Lock()
	defer s.dedupeMu.Unlock()

	now := time.Now()
	if s.lastEventName == eventName && now.Sub(s.lastEventAt) < 200*time.Millisecond {
		return true
	}

	s.lastEventName = eventName
	s.lastEventAt = now
	return false
}

func (s *blueALSAXEventSource) consumeRFCOMM(ctx context.Context, out chan<- PTTEvent, r io.Reader) {
	buf := make([]byte, 4096)

	for {
		n, err := r.Read(buf)
		if n > 0 {
			for _, eventName := range blueALSAEventNames(string(buf[:n])) {
				if !s.emitEventName(ctx, out, eventName) {
					return
				}
			}
		}

		if err != nil {
			if ctx.Err() != nil || errors.Is(err, io.EOF) || errors.Is(err, os.ErrClosed) {
				return
			}

			s.log.Debug().Err(err).Msg("BlueALSA: RFCOMM read failed")
			return
		}
	}
}

func (s *bluetoothEventSource) Events(ctx context.Context) <-chan PTTEvent {
	ch := make(chan PTTEvent, 4)

	go func() {
		defer close(ch)

		var wg sync.WaitGroup

		wg.Add(1)
		go func() {
			defer wg.Done()
			s.monitorBlueZ(ctx)
		}()

		if s.xeventFactory != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				s.forwardBlueALSAFallback(ctx, ch)
			}()
		}

		wg.Wait()
	}()

	return ch
}

func (s *bluetoothEventSource) forwardBlueALSAFallback(ctx context.Context, out chan<- PTTEvent) {
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

func (s *bluetoothEventSource) monitorBlueZ(ctx context.Context) {
	conn, err := s.dial()
	if err != nil {
		s.log.Error().Err(err).Msg("Bluetooth: failed to connect to system DBus")

		return
	}
	defer conn.Close()

	if err := addDBusMatch(conn, bluezPropertiesChangedRule); err != nil {
		s.log.Error().Err(err).Msg("Bluetooth: failed to add BlueZ DBus match rule")

		return
	}

	sigCh := make(chan *dbus.Signal, 16)
	conn.Signal(sigCh)
	defer conn.RemoveSignal(sigCh)

	s.log.Info().Msg("Starting native Bluetooth monitor using BlueZ DBus signals")

	for {
		select {
		case <-ctx.Done():
			return
		case sig, ok := <-sigCh:
			if !ok {
				return
			}

			logBlueZSignal(s.log, sig)
		}
	}
}

func addDBusMatch(conn *dbus.Conn, rule string) error {
	return conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0, rule).Err
}

func listBlueALSARFCOMMPaths(conn *dbus.Conn) ([]dbus.ObjectPath, error) {
	if conn == nil {
		return nil, errors.New("nil DBus connection")
	}

	var managed map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	call := conn.Object(bluealsaService, bluealsaManagerPath).Call(bluealsaManagedObjectsMethod, 0)
	if call.Err != nil {
		return nil, call.Err
	}
	if err := call.Store(&managed); err != nil {
		return nil, err
	}

	return blueALSARFCOMMPaths(managed), nil
}

func openBlueALSARFCOMM(conn *dbus.Conn, path dbus.ObjectPath) (io.ReadCloser, error) {
	if conn == nil {
		return nil, errors.New("nil DBus connection")
	}

	var fd dbus.UnixFD
	call := conn.Object(bluealsaService, path).Call(bluealsaRFCOMMOpenMethod, 0)
	if call.Err != nil {
		return nil, call.Err
	}
	if err := call.Store(&fd); err != nil {
		return nil, err
	}

	file := os.NewFile(uintptr(fd), string(path))
	if file == nil {
		return nil, errors.New("RFCOMM1.Open returned invalid file descriptor")
	}

	return file, nil
}

func blueALSARFCOMMPaths(
	managed map[dbus.ObjectPath]map[string]map[string]dbus.Variant,
) []dbus.ObjectPath {
	paths := make([]dbus.ObjectPath, 0)

	for path, ifaces := range managed {
		if _, ok := ifaces[bluealsaRFCOMMInterface]; ok {
			paths = append(paths, path)
		}
	}

	sort.Slice(paths, func(i, j int) bool {
		return string(paths[i]) < string(paths[j])
	})

	return paths
}

func blueALSAInterfacesAddedRFCOMMPath(sig *dbus.Signal) (dbus.ObjectPath, bool) {
	if sig == nil || sig.Name != bluealsaInterfacesAddedSignal || len(sig.Body) < 2 {
		return "", false
	}

	path, ok := sig.Body[0].(dbus.ObjectPath)
	if !ok {
		return "", false
	}

	ifaces, ok := sig.Body[1].(map[string]map[string]dbus.Variant)
	if !ok {
		return "", false
	}

	_, ok = ifaces[bluealsaRFCOMMInterface]
	return path, ok
}

func blueALSAInterfacesRemovedRFCOMMPath(sig *dbus.Signal) (dbus.ObjectPath, bool) {
	if sig == nil || sig.Name != bluealsaInterfacesRemovedSignal || len(sig.Body) < 2 {
		return "", false
	}

	path, ok := sig.Body[0].(dbus.ObjectPath)
	if !ok {
		return "", false
	}

	ifaces, ok := sig.Body[1].([]string)
	if !ok {
		return "", false
	}

	for _, iface := range ifaces {
		if iface == bluealsaRFCOMMInterface {
			return path, true
		}
	}

	return "", false
}

func blueALSAEventName(msg string) (string, bool) {
	const marker = "+XEVENT"

	idx := strings.Index(strings.ToUpper(msg), marker)
	if idx == -1 {
		return "", false
	}

	remainder := strings.TrimSpace(msg[idx+len(marker):])
	parts := strings.FieldsFunc(remainder, func(r rune) bool {
		return r == ',' || r == ':' || r == '=' || r == ' '
	})

	if len(parts) == 0 {
		return "", false
	}

	return strings.ToUpper(parts[0]), true
}

func blueALSAEventNames(packet string) []string {
	packet = strings.TrimSpace(packet)
	if packet == "" {
		return nil
	}

	parts := strings.FieldsFunc(packet, func(r rune) bool {
		return r == '\r' || r == '\n'
	})
	if len(parts) == 0 {
		parts = []string{packet}
	}

	events := make([]string, 0, len(parts))
	for _, part := range parts {
		if eventName, ok := blueALSAEventName(strings.TrimSpace(part)); ok {
			events = append(events, eventName)
		}
	}

	return events
}

func blueALSAJournalEventName(line string) (string, bool) {
	idx := strings.Index(line, bluealsaJournalMarker)
	if idx == -1 {
		return "", false
	}

	raw := strings.TrimSpace(line[idx+len(bluealsaJournalMarker):])
	if raw == "" {
		return "", false
	}

	return strings.ToUpper(raw), true
}

func sendPTTEvent(ctx context.Context, out chan<- PTTEvent, ev PTTEvent) bool {
	select {
	case out <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}

func logBlueZSignal(log zerolog.Logger, sig *dbus.Signal) {
	if sig == nil || sig.Name != bluezPropertiesChangedSignal || len(sig.Body) < 2 {
		return
	}

	iface, ok := sig.Body[0].(string)
	if !ok || iface != bluezDeviceInterface {
		return
	}

	changed, ok := sig.Body[1].(map[string]dbus.Variant)
	if !ok {
		return
	}

	if v, exists := changed["Connected"]; exists {
		if connected, ok := v.Value().(bool); ok {
			log.Info().Msgf("Bluetooth device connection event: connected=%v", connected)
		}
	}

	if v, exists := changed["UUIDs"]; exists {
		uuids, ok := v.Value().([]string)
		if !ok {
			return
		}

		for _, uuid := range uuids {
			if strings.EqualFold(uuid, bluezHFPUUID) {
				log.Info().Msg("Detected HFP device UUID; BlueALSA XEVENT fallback remains active")

				return
			}
		}
	}
}
