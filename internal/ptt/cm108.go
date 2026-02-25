package ptt

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	hid "github.com/sstallion/go-hid"

	"github.com/rs/zerolog"
)

// ─── CM108 constants ──────────────────────────────────────────────────────────

const (
	// cm108VendorID is the C-Media Electronics USB vendor identifier.
	cm108VendorID uint16 = 0x0D8C
	// cm108ProductID identifies the CM108 / CM108B all-in-one USB audio dongle.
	cm108ProductID uint16 = 0x013C

	// cm108ReportSize is the total HID input report buffer size.
	// The OS prepends a Report ID byte followed by the 4 CM108 data bytes
	// (IR0..IR3), giving 5 bytes total.
	cm108ReportSize = 5

	// cm108PayloadOffset is the byte index at which the CM108 data payload
	// begins when the OS has prepended the one-byte Report ID.
	cm108PayloadOffset = 1

	// cm108GPIO3Mask selects GPIO3 within IR1.
	// IR1 carries GPIO4..GPIO1 in its lower nibble (bits 3..0):
	//   bit 3 = GPIO4, bit 2 = GPIO3, bit 1 = GPIO2, bit 0 = GPIO1.
	cm108GPIO3Mask byte = 0x04
)

// ─── HIDDevice / HIDOpener abstractions ──────────────────────────────────────

// HIDDevice is a minimal interface over a USB HID device.
// Only the two methods used by cm108Source are declared, which allows unit
// tests to inject a mock without reaching the real hardware.
type HIDDevice interface {
	// Read fills b with the next HID input report and returns the number of
	// bytes written.  It blocks until a report arrives or an error occurs.
	Read(b []byte) (int, error)
	// Close releases the HID device handle.
	Close() error
}

// HIDOpener opens a HID device identified by its Vendor/Product ID pair.
// The production implementation calls hid.Open; tests provide a factory that
// returns a mock.
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

// defaultHIDOpener is the production HIDOpener.  It initializes HIDAPI,
// opens the device, and wraps it so that Close() performs cleanup.
func defaultHIDOpener(vendorID, productID uint16) (HIDDevice, error) {
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

// ─── cm108Source ──────────────────────────────────────────────────────────────

// cm108Source reads GPIO3 state from a CM108/CM108B USB HID audio dongle and
// emits PTTDown when the button is pressed and PTTUp when it is released.
//
// The CM108B input data format is four bytes IR0..IR3.  GPIO3 lives in bit 2
// of IR1 (the GPIO4..GPIO1 nibble).  When the OS prepends a Report ID byte the
// payload offset shifts by one, and the total report length is 5 bytes.
type cm108Source struct {
	log    zerolog.Logger
	opener HIDOpener
}

// NewCM108Source constructs a cm108Source backed by the real HIDAPI library.
// Exported so callers can wire it up directly when bypassing buildEventSource.
func NewCM108Source(log zerolog.Logger) EventSource {
	return &cm108Source{log: log, opener: defaultHIDOpener}
}

// newCM108SourceWithOpener constructs a cm108Source with an injectable opener.
// Intended for unit tests only.
func newCM108SourceWithOpener(opener HIDOpener, log zerolog.Logger) EventSource {
	return &cm108Source{log: log, opener: opener}
}

// Events implements EventSource.
//
// A goroutine opens the CM108 device, polls for HID input reports, and emits
// PTTDown when GPIO3 transitions LOW→HIGH and PTTUp when it transitions
// HIGH→LOW.  The channel is closed when ctx is canceled or the device becomes
// unreadable.
func (s *cm108Source) Events(ctx context.Context) <-chan PTTEvent {
	ch := make(chan PTTEvent, 4)

	go func() {
		defer close(ch)

		dev, err := s.opener(cm108VendorID, cm108ProductID)
		if err != nil {
			s.log.Error().Err(err).
				Msgf("CM108: failed to open HID device VID=0x%04X PID=0x%04X",
					cm108VendorID, cm108ProductID)

			return
		}

		defer func() {
			if cerr := dev.Close(); cerr != nil {
				s.log.Warn().Err(cerr).Msg("CM108: error closing HID device")
			}
		}()

		s.log.Info().Msgf("CM108: opened HID device VID=0x%04X PID=0x%04X",
			cm108VendorID, cm108ProductID)

		buf := make([]byte, cm108ReportSize)
		prevGPIO3 := false

		for {
			// Honor context cancellation before blocking on Read.
			select {
			case <-ctx.Done():
				return
			default:
			}

			n, readErr := dev.Read(buf)
			if readErr != nil {
				s.log.Error().Err(readErr).Msg("CM108: HID read error; stopping")

				return
			}

			// Determine where in the buffer the data payload begins.
			// When the OS prepends a Report ID byte the total length is ≥5
			// and the payload starts at offset 1.
			payloadStart := 0
			if n >= cm108ReportSize {
				payloadStart = cm108PayloadOffset
			}

			// We need at least IR0 and IR1 after the payload offset.
			if n < payloadStart+2 {
				s.log.Debug().Msgf("CM108: short report (%d bytes), skipping", n)
				time.Sleep(50 * time.Millisecond)

				continue
			}

			// IR1 is the byte immediately after IR0; GPIO3 is bit 2.
			ir1 := buf[payloadStart+1]
			gpio3 := (ir1 & cm108GPIO3Mask) != 0

			s.log.Trace().Msgf("CM108: IR1=0x%02X  GPIO3=%v", ir1, gpio3)

			if gpio3 == prevGPIO3 {
				continue
			}

			prevGPIO3 = gpio3

			var ev PTTEvent

			if gpio3 {
				ev = PTTDown

				s.log.Debug().Msg("CM108: GPIO3 HIGH → PTTDown")
			} else {
				ev = PTTUp

				s.log.Debug().Msg("CM108: GPIO3 LOW → PTTUp")
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

// ─── CM108 ALSA card detection ────────────────────────────────────────────────

// detectAndSetALSACard probes /proc/asound/card*/usbid to locate the CM108
// by its VID:PID and sets the ALSA_CARD environment variable to its card
// number.  This must be called before portaudio.Initialize() so that PortAudio
// and ALSA select the correct card.  On OpenWRT the kernel exposes a usbid
// file under /proc/asound/card<N>/ for every registered USB audio device.
//
// If ALSA_CARD is already present in the environment it is left unchanged.
func detectAndSetALSACard(log zerolog.Logger) {
	detectAndSetALSACardFromRoot("/proc/asound", log)
}

// detectAndSetALSACardFromRoot is the testable core of detectAndSetALSACard.
// root replaces /proc/asound so tests can supply a temporary directory tree.
func detectAndSetALSACardFromRoot(root string, log zerolog.Logger) {
	if v := os.Getenv("ALSA_CARD"); v != "" {
		log.Debug().Str("ALSA_CARD", v).Msg("CM108: ALSA_CARD already set, skipping auto-detection")

		return
	}

	pattern := filepath.Join(root, "card*", "usbid")

	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		log.Warn().Msgf("CM108: no USB audio cards found at %s; ALSA_CARD not set", pattern)

		return
	}

	target := fmt.Sprintf("%04x:%04x", cm108VendorID, cm108ProductID)

	for _, path := range matches {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			log.Debug().Err(readErr).Str("path", path).Msg("CM108: could not read usbid")

			continue
		}

		if strings.TrimSpace(strings.ToLower(string(data))) != target {
			continue
		}

		// Path shape: <root>/card<N>/usbid — extract the numeric suffix.
		cardDir := filepath.Base(filepath.Dir(path)) // "card<N>"
		cardNum := strings.TrimPrefix(cardDir, "card")

		if cardNum == cardDir {
			log.Warn().Str("path", path).Msg("CM108: unexpected usbid path format")

			continue
		}

		if setErr := os.Setenv("ALSA_CARD", cardNum); setErr != nil {
			log.Error().Err(setErr).Msg("CM108: failed to set ALSA_CARD")

			return
		}

		log.Info().
			Str("ALSA_CARD", cardNum).
			Msgf("CM108: auto-detected card %s for VID=0x%04X PID=0x%04X",
				cardNum, cm108VendorID, cm108ProductID)

		return
	}

	log.Warn().Msgf("CM108: no card matching VID=0x%04X PID=0x%04X found in %s; ALSA_CARD not set",
		cm108VendorID, cm108ProductID, root)
}
