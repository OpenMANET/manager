package ptt

import (
	"strings"

	"github.com/godbus/dbus/v5"
)

// monitorBluealsaXEvents listens for BlueALSA AT command signals via DBus.
// This is a native OpenWrt approach that doesn't require journalctl/systemd.
func (ptt *PTTConfig) monitorBluealsaXEvents() {
	ptt.Log.Info().Msg("Starting BlueALSA XEVENT monitor via native DBus signals")

	conn, err := dbus.SystemBus()
	if err != nil {
		ptt.Log.Error().Err(err).Msg("Failed to connect to system DBus for BlueALSA monitoring")
		return
	}
	defer conn.Close()

	// Listen for BlueALSA's signal interface or property changes.
	// BlueALSA typically broadcasts AT commands as DBus method calls or signals.
	match := "type='signal',interface='org.bluealsa.Device1',member='ATMessage'"
	if err := conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0, match).Store(); err != nil {
		ptt.Log.Debug().Err(err).Msg("BlueALSA ATMessage signal not available, trying property-based monitoring")
		// Fallback: monitor property changes on BlueALSA objects
		ptt.monitorBluealsaProperties(conn)
		return
	}

	signalChan := make(chan *dbus.Signal, 10)
	conn.Signal(signalChan)
	ptt.Log.Info().Msg("Listening for BlueALSA ATMessage signals")

	for sig := range signalChan {
		if sig.Name != "org.bluealsa.Device1.ATMessage" {
			continue
		}

		if len(sig.Body) == 0 {
			continue
		}

		msg, ok := sig.Body[0].(string)
		if !ok {
			continue
		}

		ptt.parseAndHandleATMessage(msg)
	}
}

// monitorBluealsaProperties monitors BlueALSA properties for AT command indicators.
func (ptt *PTTConfig) monitorBluealsaProperties(conn *dbus.Conn) {
	// Match property changes on BlueALSA device objects
	match := "type='signal',interface='org.freedesktop.DBus.Properties',path_namespace='/org/bluealsa'"
	if err := conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0, match).Store(); err != nil {
		ptt.Log.Warn().Err(err).Msg("Could not add BlueALSA property match; XEVENT monitoring unavailable")
		return
	}

	signalChan := make(chan *dbus.Signal, 10)
	conn.Signal(signalChan)
	ptt.Log.Info().Msg("Listening for BlueALSA property changes")

	for sig := range signalChan {
		if sig.Name != "org.freedesktop.DBus.Properties.PropertiesChanged" {
			continue
		}

		if len(sig.Body) < 2 {
			continue
		}

		_, ok := sig.Body[0].(string)
		if !ok {
			continue
		}

		changed, ok := sig.Body[1].(map[string]dbus.Variant)
		if !ok {
			continue
		}

		// Look for ATCommand or similar property on BlueALSA objects
		for key, val := range changed {
			if strings.Contains(key, "AT") || strings.Contains(key, "Command") {
				// Attempt to extract string value
				str, ok := val.Value().(string)
				if ok {
					ptt.parseAndHandleATMessage(str)
				}
			}
		}
	}
}

// parseAndHandleATMessage parses an AT command message and dispatches PTT events.
func (ptt *PTTConfig) parseAndHandleATMessage(msg string) {
	const marker = "+XEVENT"
	idx := strings.Index(msg, marker)
	if idx == -1 {
		return
	}

	// Extract value after marker (typically "PTT_DOWN", "PTT_UP", etc.)
	remainder := strings.TrimSpace(msg[idx+len(marker):])
	// Remove common delimiters: comma, colon, equals
	lines := strings.FieldsFunc(remainder, func(r rune) bool {
		return r == ',' || r == ':' || r == '=' || r == ' '
	})

	if len(lines) == 0 {
		return
	}

	event := strings.ToUpper(lines[0])
	ptt.Log.Debug().Msgf("BlueALSA XEVENT: %s", event)

	switch event {
	case "PTT_DOWN":
		ptt.beginTransmission(ptt.runtime.broadcastStream)
	case "PTT_UP":
		ptt.endTransmission(ptt.runtime.broadcastStream)
	case "PREV_CH", "NEXT_CH", "BLE":
		ptt.Log.Info().Msgf("BlueALSA XEVENT received: %s", event)
	default:
		ptt.Log.Debug().Msgf("Ignoring unsupported BlueALSA XEVENT: %s", event)
	}
}
