package comms

import (
	"context"
	"strings"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/rs/zerolog"
)

const (
	bluezPropertiesChangedRule   = "type='signal',interface='org.freedesktop.DBus.Properties',sender='org.bluez'"
	bluezPropertiesChangedSignal = "org.freedesktop.DBus.Properties.PropertiesChanged"
	bluealsaATMessageRule        = "type='signal',interface='org.bluealsa.Device1',member='ATMessage'"
	bluealsaATMessageSignal      = "org.bluealsa.Device1.ATMessage"
	bluealsaPropertiesRule       = "type='signal',interface='org.freedesktop.DBus.Properties',path_namespace='/org/bluealsa'"
	bluealsaPathPrefix           = "/org/bluealsa"
	bluezDeviceInterface         = "org.bluez.Device1"
	bluezHFPUUID                 = "0000111e-0000-1000-8000-00805f9b34fb"
)

type systemBusDialer func() (*dbus.Conn, error)

type blueALSAXEventSource struct {
	log  zerolog.Logger
	dial systemBusDialer
}

type bluetoothEventSource struct {
	log           zerolog.Logger
	dial          systemBusDialer
	xeventFactory func(zerolog.Logger) EventSource
}

// NewBlueALSAXEventSource monitors BlueALSA DBus messages and emits PTT press/release events.
func NewBlueALSAXEventSource(log zerolog.Logger) EventSource {
	return &blueALSAXEventSource{
		log:  log,
		dial: func() (*dbus.Conn, error) {
			return dbus.ConnectSystemBus()
		},
	}
}

// NewBluetoothEventSource monitors BlueZ device state and forwards BlueALSA XEVENT presses.
func NewBluetoothEventSource(log zerolog.Logger) EventSource {
	return &bluetoothEventSource{
		log:           log,
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

		conn, err := s.dial()
		if err != nil {
			s.log.Error().Err(err).Msg("BlueALSA: failed to connect to system DBus")

			return
		}
		defer conn.Close()

		matched := false

		if err := addDBusMatch(conn, bluealsaATMessageRule); err != nil {
			s.log.Debug().Err(err).Msg("BlueALSA: ATMessage match unavailable")
		} else {
			matched = true
		}

		if err := addDBusMatch(conn, bluealsaPropertiesRule); err != nil {
			s.log.Debug().Err(err).Msg("BlueALSA: property match unavailable")
		} else {
			matched = true
		}

		if !matched {
			s.log.Warn().Msg("BlueALSA: no usable DBus match rules; XEVENT monitoring unavailable")

			return
		}

		sigCh := make(chan *dbus.Signal, 16)
		conn.Signal(sigCh)
		defer conn.RemoveSignal(sigCh)

		s.log.Info().Msg("Starting BlueALSA XEVENT monitor via native DBus signals")

		for {
			select {
			case <-ctx.Done():
				return
			case sig, ok := <-sigCh:
				if !ok {
					return
				}

				if !s.handleSignal(ctx, ch, sig) {
					return
				}
			}
		}
	}()

	return ch
}

func (s *blueALSAXEventSource) handleSignal(ctx context.Context, out chan<- PTTEvent, sig *dbus.Signal) bool {
	if sig == nil {
		return true
	}

	switch sig.Name {
	case bluealsaATMessageSignal:
		msg, ok := signalStringBody(sig)
		if !ok {
			return true
		}

		return s.emitATMessage(ctx, out, msg)
	case bluezPropertiesChangedSignal:
		if !strings.HasPrefix(string(sig.Path), bluealsaPathPrefix) {
			return true
		}

		for _, msg := range blueALSAPropertyMessages(sig) {
			if !s.emitATMessage(ctx, out, msg) {
				return false
			}
		}
	}

	return true
}

func (s *blueALSAXEventSource) emitATMessage(ctx context.Context, out chan<- PTTEvent, msg string) bool {
	eventName, ok := blueALSAEventName(msg)
	if !ok {
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

func signalStringBody(sig *dbus.Signal) (string, bool) {
	if sig == nil || len(sig.Body) == 0 {
		return "", false
	}

	msg, ok := sig.Body[0].(string)

	return msg, ok
}

func blueALSAPropertyMessages(sig *dbus.Signal) []string {
	if sig == nil || len(sig.Body) < 2 {
		return nil
	}

	changed, ok := sig.Body[1].(map[string]dbus.Variant)
	if !ok {
		return nil
	}

	var msgs []string

	for key, val := range changed {
		upperKey := strings.ToUpper(key)
		if !strings.Contains(upperKey, "AT") && !strings.Contains(upperKey, "COMMAND") {
			continue
		}

		msg, ok := val.Value().(string)
		if ok {
			msgs = append(msgs, msg)
		}
	}

	return msgs
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
