package comms

import (
	"bufio"
	"context"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

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
	bluealsaLogreadCmd           = "logread -f"
	bluealsaJournalMarker        = "AT message: SET: command:+XEVENT, value:"
)

type systemBusDialer func() (*dbus.Conn, error)
type logTailSpawner func(context.Context) (*exec.Cmd, io.ReadCloser, io.ReadCloser, error)

type blueALSAXEventSource struct {
	log           zerolog.Logger
	dial          systemBusDialer
	spawnLogTail  logTailSpawner
	dedupeMu      sync.Mutex
	lastEventName string
	lastEventAt   time.Time
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
		spawnLogTail: func(ctx context.Context) (*exec.Cmd, io.ReadCloser, io.ReadCloser, error) {
			cmd := exec.CommandContext(ctx, "sh", "-c", bluealsaLogreadCmd)

			stdout, err := cmd.StdoutPipe()
			if err != nil {
				return nil, nil, nil, err
			}

			stderr, err := cmd.StderrPipe()
			if err != nil {
				_ = stdout.Close()
				return nil, nil, nil, err
			}

			if err := cmd.Start(); err != nil {
				_ = stdout.Close()
				_ = stderr.Close()
				return nil, nil, nil, err
			}

			return cmd, stdout, stderr, nil
		},
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

		var wg sync.WaitGroup

		if s.spawnLogTail != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				s.monitorBlueALSALogread(ctx, ch)
			}()
		}

		if s.dial == nil {
			wg.Wait()
			return
		}

		conn, err := s.dial()
		if err != nil {
			s.log.Error().Err(err).Msg("BlueALSA: failed to connect to system DBus")
			wg.Wait()
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
			s.log.Warn().Msg("BlueALSA: no usable DBus match rules; using logread fallback only")
			wg.Wait()
			return
		}

		sigCh := make(chan *dbus.Signal, 16)
		conn.Signal(sigCh)
		defer conn.RemoveSignal(sigCh)

		s.log.Info().Msg("Starting BlueALSA XEVENT monitor via native DBus signals")

		for {
			select {
			case <-ctx.Done():
				wg.Wait()
				return
			case sig, ok := <-sigCh:
				if !ok {
					wg.Wait()
					return
				}

				if !s.handleSignal(ctx, ch, sig) {
					wg.Wait()
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

	return s.emitEventName(ctx, out, eventName)
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

func (s *blueALSAXEventSource) monitorBlueALSALogread(ctx context.Context, out chan<- PTTEvent) {
	cmd, stdout, stderr, err := s.spawnLogTail(ctx)
	if err != nil {
		s.log.Debug().Err(err).Msg("BlueALSA: logread fallback unavailable")
		return
	}
	defer func() {
		_ = stdout.Close()
		_ = stderr.Close()
		_ = cmd.Wait()
	}()

	s.log.Info().Msg("Starting BlueALSA XEVENT monitor via logread fallback")

	go s.drainBlueALSALogreadStderr(stderr)
	s.consumeBlueALSALogread(ctx, out, stdout)
}

func (s *blueALSAXEventSource) drainBlueALSALogreadStderr(r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			s.log.Debug().Msgf("BlueALSA logread stderr: %s", line)
		}
	}
}

func (s *blueALSAXEventSource) consumeBlueALSALogread(ctx context.Context, out chan<- PTTEvent, r io.Reader) {
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		eventName, ok := blueALSAJournalEventName(scanner.Text())
		if !ok {
			continue
		}

		if !s.emitEventName(ctx, out, eventName) {
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
