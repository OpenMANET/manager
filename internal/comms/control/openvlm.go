package control

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	hid "github.com/sstallion/go-hid"

	"github.com/openmanet/openmanetd/internal/comms/device"
)

// ─── OpenVLM constants ────────────────────────────────────────────────────────

const (
	// OpenVLMVendorID is the C-Media Electronics USB vendor identifier.
	OpenVLMVendorID uint16 = 0x0D8C
	// OpenVLMProductID identifies the OpenVLM (Open Voice Link Module) USB audio dongle.
	OpenVLMProductID uint16 = 0x0012

	// OpenVLMReportSize is the total HID input report buffer size.
	// The OS prepends a Report ID byte followed by the 4 OpenVLM data bytes
	// (IR0..IR3), giving 5 bytes total.
	OpenVLMReportSize = 5

	// OpenVLMPayloadOffset is the byte index at which the OpenVLM data payload
	// begins when the OS has prepended the one-byte Report ID.
	OpenVLMPayloadOffset = 1

	// OpenVLMGPIO3Mask selects GPIO3 within IR1.
	OpenVLMGPIO3Mask byte = 0x04
)

// ─── HIDDevice / HIDOpener abstractions ──────────────────────────────────────

// HIDDevice is a minimal interface over a USB HID device.
type HIDDevice interface {
	Read(b []byte) (int, error)
	Close() error
}

// HIDOpener opens a HID device identified by its Vendor/Product ID pair.
type HIDOpener func(vendorID, productID uint16) (HIDDevice, error)

// hidDeviceWrapper decorates a *hid.Device so that Close also calls hid.Exit,
// keeping HIDAPI initialisation and teardown balanced.
type hidDeviceWrapper struct {
	inner *hid.Device
}

func (w *hidDeviceWrapper) Read(b []byte) (int, error) {
	n, err := w.inner.Read(b)
	if err != nil {
		return 0, fmt.Errorf("hid read: %w", err)
	}

	return n, nil
}

func (w *hidDeviceWrapper) Close() error {
	err := w.inner.Close()
	_ = hid.Exit()

	return err
}

// DefaultHIDOpener is the production HIDOpener. It initializes HIDAPI,
// opens the device, and wraps it so that Close() performs cleanup.
func DefaultHIDOpener(vendorID, productID uint16) (HIDDevice, error) {
	if err := hid.Init(); err != nil {
		return nil, fmt.Errorf("hid.Init: %w", err)
	}

	dev, err := hid.Open(vendorID, productID, "")
	if err != nil {
		_ = hid.Exit()

		return nil, fmt.Errorf("hid.Open VID=0x%04X PID=0x%04X: %w", vendorID, productID, err)
	}

	return &hidDeviceWrapper{inner: dev}, nil
}

// ─── OpenVLMSource ────────────────────────────────────────────────────────────

// OpenVLMSource reads GPIO3 state from an OpenVLM (Open Voice Link Module) USB
// HID audio dongle and emits PTTDown when the button is pressed and PTTUp when
// it is released.
type OpenVLMSource struct {
	log    zerolog.Logger
	opener HIDOpener
}

// NewOpenVLMSource constructs an OpenVLMSource backed by the real HIDAPI library.
func NewOpenVLMSource(log zerolog.Logger) EventSource {
	log.Info().Msgf("comms: PTT on OpenVLM HID dongle (VID=0x%04X PID=0x%04X)",
		OpenVLMVendorID, OpenVLMProductID)

	return &OpenVLMSource{log: log, opener: DefaultHIDOpener}
}

// NewOpenVLMSourceWithOpener constructs an OpenVLMSource with an injectable opener.
// Intended for unit tests only.
func NewOpenVLMSourceWithOpener(opener HIDOpener, log zerolog.Logger) EventSource {
	return &OpenVLMSource{log: log, opener: opener}
}

// Events implements EventSource.
func (s *OpenVLMSource) Events(ctx context.Context) <-chan PTTEvent { //nolint:gocognit
	ch := make(chan PTTEvent, 4)

	go func() {
		defer close(ch)

		if descs, dErr := device.DiscoverCM108(os.DirFS("/sys"), nil); dErr == nil {
			s.log.Debug().
				Int("cm108_count", len(descs)).
				Msg("OpenVLM: unified CM108 descriptor scan")
		}

		dev, err := s.opener(OpenVLMVendorID, OpenVLMProductID)
		if err != nil {
			s.log.Error().Err(err).
				Msgf("OpenVLM: failed to open HID device VID=0x%04X PID=0x%04X",
					OpenVLMVendorID, OpenVLMProductID)

			return
		}

		var closeOnce sync.Once

		closeDevice := func() {
			closeOnce.Do(func() {
				if cerr := dev.Close(); cerr != nil {
					s.log.Warn().Err(cerr).Msg("OpenVLM: error closing HID device")
				}
			})
		}

		stop := context.AfterFunc(ctx, closeDevice)

		defer func() {
			stop()
			closeDevice()
		}()

		s.log.Info().Msgf("OpenVLM: opened HID device VID=0x%04X PID=0x%04X",
			OpenVLMVendorID, OpenVLMProductID)

		buf := make([]byte, OpenVLMReportSize)
		prevGPIO3 := false

		for {
			n, readErr := dev.Read(buf)
			if readErr != nil {
				if ctx.Err() != nil {
					return
				}

				s.log.Error().Err(readErr).Msg("OpenVLM: HID read error; stopping")

				return
			}

			payloadStart := 0
			if n >= OpenVLMReportSize {
				payloadStart = OpenVLMPayloadOffset
			}

			if n < payloadStart+2 {
				s.log.Debug().Msgf("OpenVLM: short report (%d bytes), skipping", n)
				time.Sleep(50 * time.Millisecond)

				continue
			}

			ir1 := buf[payloadStart+1]
			gpio3 := (ir1 & OpenVLMGPIO3Mask) != 0

			s.log.Trace().Msgf("OpenVLM: IR1=0x%02X  GPIO3=%v", ir1, gpio3)

			if gpio3 == prevGPIO3 {
				continue
			}

			prevGPIO3 = gpio3

			var ev PTTEvent

			if gpio3 {
				ev = PTTDown

				s.log.Debug().Msg("OpenVLM: GPIO3 HIGH → PTTDown")
			} else {
				ev = PTTUp

				s.log.Debug().Msg("OpenVLM: GPIO3 LOW → PTTUp")
			}

			select {
			case ch <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch
}

// ─── OpenVLM ALSA card detection ──────────────────────────────────────────────

// DetectAndSetALSACard probes /proc/asound/card*/usbid to locate the OpenVLM
// by its VID:PID and sets the ALSA_CARD environment variable to its card
// number. If ALSA_CARD is already present it is left unchanged.
func DetectAndSetALSACard(log zerolog.Logger) {
	if DetectAndSetALSACardFromSys(os.DirFS("/sys"), log) {
		return
	}

	DetectAndSetALSACardFromRoot("/proc/asound", log)
}

// DetectAndSetALSACardFromSys attempts to set ALSA_CARD from the unified
// device.DiscoverCM108 walk.
func DetectAndSetALSACardFromSys(fsys fs.FS, log zerolog.Logger) bool {
	if v := os.Getenv("ALSA_CARD"); v != "" {
		log.Debug().Str("ALSA_CARD", v).Msg("OpenVLM: ALSA_CARD already set, skipping auto-detection")

		return true
	}

	descs, err := device.DiscoverCM108(fsys, nil)
	if err != nil {
		log.Debug().Err(err).Msg("OpenVLM: sysfs CM108 discovery failed; falling back")

		return false
	}

	log.Debug().Int("cm108_count", len(descs)).Msg("OpenVLM: sysfs CM108 discovery")

	for _, d := range descs {
		if d.ALSACardIdx < 0 {
			continue
		}

		cardNum := strconv.Itoa(d.ALSACardIdx)
		if setErr := os.Setenv("ALSA_CARD", cardNum); setErr != nil {
			log.Error().Err(setErr).Msg("OpenVLM: failed to set ALSA_CARD")

			return false
		}

		log.Info().
			Str("ALSA_CARD", cardNum).
			Str("hid_path", d.HIDPath).
			Str("sys_path", d.SysPath).
			Msgf("OpenVLM: auto-detected card %s via sysfs (VID=0x%04X PID=0x%04X)",
				cardNum, d.VID, d.PID)

		return true
	}

	return false
}

// DetectAndSetALSACardFromRoot is the testable core of DetectAndSetALSACard.
// root replaces /proc/asound so tests can supply a temporary directory tree.
func DetectAndSetALSACardFromRoot(root string, log zerolog.Logger) {
	if v := os.Getenv("ALSA_CARD"); v != "" {
		log.Debug().Str("ALSA_CARD", v).Msg("OpenVLM: ALSA_CARD already set, skipping auto-detection")

		return
	}

	pattern := filepath.Join(root, "card*", "usbid")

	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		log.Warn().Msgf("OpenVLM: no USB audio cards found at %s; ALSA_CARD not set", pattern)

		return
	}

	target := fmt.Sprintf("%04x:%04x", OpenVLMVendorID, OpenVLMProductID)

	for _, path := range matches {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			log.Debug().Err(readErr).Str("path", path).Msg("OpenVLM: could not read usbid")

			continue
		}

		if strings.TrimSpace(strings.ToLower(string(data))) != target {
			continue
		}

		cardDir := filepath.Base(filepath.Dir(path))
		cardNum := strings.TrimPrefix(cardDir, "card")

		if cardNum == cardDir {
			log.Warn().Str("path", path).Msg("OpenVLM: unexpected usbid path format")

			continue
		}

		if setErr := os.Setenv("ALSA_CARD", cardNum); setErr != nil {
			log.Error().Err(setErr).Msg("OpenVLM: failed to set ALSA_CARD")

			return
		}

		log.Info().
			Str("ALSA_CARD", cardNum).
			Msgf("OpenVLM: auto-detected card %s for VID=0x%04X PID=0x%04X",
				cardNum, OpenVLMVendorID, OpenVLMProductID)

		return
	}

	log.Warn().Msgf("OpenVLM: no card matching VID=0x%04X PID=0x%04X found in %s; ALSA_CARD not set",
		OpenVLMVendorID, OpenVLMProductID, root)
}
