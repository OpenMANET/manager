package ptt

import (
	"fmt"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

func (ptt *PTTConfig) monitorBluetoothPTT() {
	ptt.Log.Info().Msg("Starting native Bluetooth PTT backend using DBus")

	conn, err := dbus.SystemBus()
	if err != nil {
		ptt.Log.Error().Err(err).Msg("Failed to connect to system DBus; falling back to BlueALSA journal")
		ptt.monitorBluealsaXEvents()
		return
	}
	defer conn.Close()

	match := "type='signal',interface='org.freedesktop.DBus.Properties',sender='org.bluez'"
	if err := conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0, match).Store(); err != nil {
		ptt.Log.Error().Err(err).Msg("Failed to add DBus match rule")
		ptt.monitorBluealsaXEvents()
		return
	}

	signalChan := make(chan *dbus.Signal, 10)
	conn.Signal(signalChan)
	ptt.Log.Info().Msg("Listening for org.bluez PropertiesChanged signals")

	for sig := range signalChan {
		if sig.Name != "org.freedesktop.DBus.Properties.PropertiesChanged" {
			continue
		}

		if len(sig.Body) < 2 {
			continue
		}

		iface, ok := sig.Body[0].(string)
		if !ok {
			continue
		}

		changed, ok := sig.Body[1].(map[string]dbus.Variant)
		if !ok {
			continue
		}

		if iface != "org.bluez.Device1" {
			continue
		}

		if v, exists := changed["Connected"]; exists {
			state := v.Value()
			if connected, ok := state.(bool); ok {
				ptt.Log.Info().Msgf("Bluetooth device connection event: connected=%v", connected)
				continue
			}
		}

		if v, exists := changed["UUIDs"]; exists {
			strs := fmt.Sprintf("%v", v.Value())
			if strings.Contains(strs, "0000111e-0000-1000-8000-00805f9b34fb") {
				ptt.Log.Info().Msg("Detected HFP device UUID; enabling PTT path")
			}
		}

		if !ptt.runtime.broadcasting {
			// fallback continuing event watch via BlueALSA logs as this stays the safest path.
			ptt.Log.Debug().Msg("Fallback: switching to BlueALSA XEVENT monitor for key events")
			ptt.monitorBluealsaXEvents()
			return
		}

		// Keep looping; this can be expanded for more event decoding.
		time.Sleep(500 * time.Millisecond)
	}
}
