package ptt

import (
	"bufio"
	"io"
	"os/exec"
	"strings"
	"time"
)

const bluealsaJournalCmd = "journalctl -u bluealsa -f -n 0 --output=cat"

func (ptt *PTTConfig) monitorBluealsaXEvents() {
	for {
		cmd := exec.Command("sh", "-c", bluealsaJournalCmd)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			ptt.Log.Error().Err(err).Msg("Failed to create BlueALSA journal stdout pipe")
			time.Sleep(2 * time.Second)
			continue
		}

		stderr, err := cmd.StderrPipe()
		if err != nil {
			ptt.Log.Error().Err(err).Msg("Failed to create BlueALSA journal stderr pipe")
			time.Sleep(2 * time.Second)
			continue
		}

		if err := cmd.Start(); err != nil {
			ptt.Log.Error().Err(err).Msg("Failed to start BlueALSA journal monitor")
			time.Sleep(2 * time.Second)
			continue
		}

		ptt.Log.Debug().Msg("BlueALSA XEVENT monitor started")
		go ptt.drainBluealsaStderr(stderr)
		ptt.consumeBluealsaXEvents(stdout)

		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		ptt.Log.Warn().Msg("BlueALSA XEVENT monitor exited; restarting")
		time.Sleep(2 * time.Second)
	}
}

func (ptt *PTTConfig) drainBluealsaStderr(r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			ptt.Log.Debug().Msgf("bluealsa journal stderr: %s", line)
		}
	}
}

func (ptt *PTTConfig) consumeBluealsaXEvents(r io.Reader) {
	scanner := bufio.NewScanner(r)
	const marker = "AT message: SET: command:+XEVENT, value:"
	for scanner.Scan() {
		line := scanner.Text()
		idx := strings.Index(line, marker)
		if idx == -1 {
			continue
		}

		raw := strings.TrimSpace(line[idx+len(marker):])
		if raw == "" {
			continue
		}

		event := strings.ToUpper(raw)
		ptt.Log.Debug().Msgf("BlueALSA XEVENT: %s", event)
		switch event {
		case "PTT_DOWN":
			ptt.beginTransmission()
		case "PTT_UP":
			ptt.endTransmission()
		case "PREV_CH", "NEXT_CH", "BLE":
			ptt.Log.Info().Msgf("BlueALSA XEVENT received: %s", event)
		default:
			ptt.Log.Debug().Msgf("Ignoring unsupported BlueALSA XEVENT: %s", event)
		}
	}
}
