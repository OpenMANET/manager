package control

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/openmanet/openmanetd/internal/comms/audiopool"
)

// ─── ROIP constants ───────────────────────────────────────────────────────────

const (
	// ROIPDefaultCOSMask selects GPIO1 within IR1 as the default COS input.
	ROIPDefaultCOSMask byte = 0x01

	// ROIPDefaultVOXThresh is the RMS energy level at which the VOX path
	// considers the radio active. Tuned for a typical USB audio dongle with
	// line-level input from an analog handheld radio.
	ROIPDefaultVOXThresh float32 = 0.02

	// ROIPDefaultVOXHold is the silence window after which the VOX emits PTTUp.
	ROIPDefaultVOXHold time.Duration = 500 * time.Millisecond

	// ROIPDefaultMaxTX is the safety ceiling on a single ROIP transmission.
	ROIPDefaultMaxTX time.Duration = 60 * time.Second

	// ROIPVOXOnsetFrames is the number of consecutive loud frames required to
	// trigger PTTDown. Each frame is 20 ms, so 3 frames = 60 ms onset guard.
	ROIPVOXOnsetFrames int = 3

	// ROIPMonitorBufFrames is the depth of the monitor / tap frame channel.
	ROIPMonitorBufFrames int = 8

	// ROIPVOXPollInterval is the ticker period used for isReceiving() polling
	// in the active state when no tap frames are arriving.
	ROIPVOXPollInterval time.Duration = 10 * time.Millisecond
)

// ─── ROIPSource ───────────────────────────────────────────────────────────────

// ROIPSource implements EventSource for a ROIP (Radio over IP) bridge.
//
// It uses the same OpenVLM USB audio dongle as openvlmSource but operates without
// a manual PTT button, automatically bridging an analog handheld radio into
// the multicast comms network.
//
// Detection strategy (half-duplex enforced throughout):
//
//  1. COS (Carrier-Operated Squelch): the radio squelch output is wired to an
//     OpenVLM GPIO pin. The HID report is polled; cosGPIOMask selects the IR1
//     bit. PTTDown on the HIGH→LOW squelch edge, PTTUp on LOW→HIGH.
//
//  2. VOX fallback: if the HID device is unavailable or cosGPIOMask is 0, an
//     audio energy threshold is applied to frames the always-on broadcast
//     capture stream forwards through the ROIP tap. A configurable onset
//     window (ROIPVOXOnsetFrames) prevents false triggers. The same tap
//     channel drives the ACTIVE phase silence detection — under the unified
//     capture design there is only one input device open at a time, and the
//     ROIP VOX path consumes the same frames the broadcast encoder sees.
type ROIPSource struct {
	log            zerolog.Logger
	opener         HIDOpener
	isReceiving    func() bool
	isBroadcasting func() bool
	setTap         func(chan []float32)
	clearTap       func()
	voxHoldTime    time.Duration
	maxTXDuration  time.Duration
	voxThreshold   float32
	cosGPIOMask    byte
}

// NewROIPSource constructs a production ROIPSource backed by the real HIDAPI
// library and caller-provided callbacks for half-duplex enforcement and
// broadcast-tap wiring. Under the unified capture design the VOX monitor no
// longer opens a second audio device — it subscribes to the always-on
// broadcast capture via setTap / clearTap.
func NewROIPSource(
	log zerolog.Logger,
	cosGPIOMask byte,
	voxThreshold float32,
	voxHoldTime time.Duration,
	maxTXDuration time.Duration,
	isReceiving, isBroadcasting func() bool,
	setTap func(chan []float32),
	clearTap func(),
) EventSource {
	log.Info().Msgf(
		"comms: ROIP bridge on OpenVLM (VID=0x%04X PID=0x%04X) COSmask=0x%02X VOX=%.3f hold=%s",
		OpenVLMVendorID, OpenVLMProductID, cosGPIOMask, voxThreshold, voxHoldTime,
	)

	return &ROIPSource{
		log:            log,
		opener:         DefaultHIDOpener,
		cosGPIOMask:    cosGPIOMask,
		voxThreshold:   voxThreshold,
		voxHoldTime:    voxHoldTime,
		maxTXDuration:  maxTXDuration,
		isReceiving:    isReceiving,
		isBroadcasting: isBroadcasting,
		setTap:         setTap,
		clearTap:       clearTap,
	}
}

// NewROIPSourceWithOpener is the testable COS-path constructor.
// It injects a custom HIDOpener without touching PortAudio.
func NewROIPSourceWithOpener(
	opener HIDOpener,
	cosGPIOMask byte,
	isReceiving, isBroadcasting func() bool,
	log zerolog.Logger,
) EventSource {
	return &ROIPSource{
		log:            log,
		opener:         opener,
		cosGPIOMask:    cosGPIOMask,
		voxThreshold:   0, // COS-only
		voxHoldTime:    ROIPDefaultVOXHold,
		maxTXDuration:  ROIPDefaultMaxTX,
		isReceiving:    isReceiving,
		isBroadcasting: isBroadcasting,
		setTap:         func(_ chan []float32) {},
		clearTap:       func() {},
	}
}

// TapBinding bundles the (setTap, clearTap) callback pair as a single
// value so test helpers can return one composite from one expression.
// Production code constructs it inline at the wiring layer; tests
// build it from a pre-filled frame channel via the helper in
// roip_test.go.
type TapBinding struct {
	Set   func(chan []float32)
	Clear func()
}

// NewROIPSourceWithTap is the testable VOX-path constructor. The
// supplied TapBinding replaces the production tap wiring, allowing
// tests to capture the tap channel and drive it with a pre-built
// frame sequence without real audio hardware.
func NewROIPSourceWithTap(
	tap TapBinding,
	voxHoldTime time.Duration,
	isReceiving, isBroadcasting func() bool,
	log zerolog.Logger,
) EventSource {
	return &ROIPSource{
		log:            log,
		opener:         nil, // cosGPIOMask==0; opener never called
		cosGPIOMask:    0,
		voxThreshold:   ROIPDefaultVOXThresh,
		voxHoldTime:    voxHoldTime,
		maxTXDuration:  ROIPDefaultMaxTX,
		isReceiving:    isReceiving,
		isBroadcasting: isBroadcasting,
		setTap:         tap.Set,
		clearTap:       tap.Clear,
	}
}

// ─── Events ───────────────────────────────────────────────────────────────────

// Events implements EventSource.
//
// The detection strategy is selected at startup and runs until ctx is canceled:
//   - COS mode when cosGPIOMask != 0 and the OpenVLM HID device opens successfully.
//   - VOX mode as fallback when COS is unavailable.
//   - Error (channel closed immediately) when neither is available.
func (s *ROIPSource) Events(ctx context.Context) <-chan PTTEvent {
	ch := make(chan PTTEvent, 4)

	go func() {
		defer close(ch)

		// COS path: attempt to open the OpenVLM HID device.
		if s.cosGPIOMask != 0 {
			dev, err := s.opener(OpenVLMVendorID, OpenVLMProductID)
			if err == nil {
				s.log.Info().Msgf("ROIP: COS mode active (mask=0x%02X)", s.cosGPIOMask)
				s.cosLoop(ctx, dev, ch)

				return
			}

			s.log.Warn().Err(err).Msg("ROIP: HID open failed; falling back to VOX")
		}

		// VOX fallback path.
		if s.voxThreshold > 0 {
			s.log.Info().Msgf("ROIP: VOX mode active (threshold=%.3f hold=%s)", s.voxThreshold, s.voxHoldTime)
			s.voxLoop(ctx, ch)

			return
		}

		s.log.Error().Msg("ROIP: no detection method available (COS HID unavailable and VOX disabled)")
	}()

	return ch
}

// ─── COS loop ─────────────────────────────────────────────────────────────────

// cosLoop polls the OpenVLM HID device for the COS GPIO bit (cosGPIOMask in IR1)
// and emits PTTDown on the LOW→HIGH transition and PTTUp on HIGH→LOW.
//
// Half-duplex: PTTDown is suppressed while isReceiving() returns true.  The
// COS transition is not latched in that case; the next HIGH will be re-checked.
func (s *ROIPSource) cosLoop(ctx context.Context, dev HIDDevice, ch chan<- PTTEvent) { //nolint:gocognit
	var closeOnce sync.Once

	closeDevice := func() {
		closeOnce.Do(func() {
			if err := dev.Close(); err != nil {
				s.log.Warn().Err(err).Msg("ROIP: error closing HID device")
			}
		})
	}

	stop := context.AfterFunc(ctx, closeDevice)

	defer func() {
		stop()
		closeDevice()
	}()

	buf := make([]byte, OpenVLMReportSize)
	prevCOS := false

	for {
		n, readErr := dev.Read(buf)
		if readErr != nil {
			if ctx.Err() != nil {
				return
			}

			s.log.Error().Err(readErr).Msg("ROIP: HID read error; stopping")

			return
		}

		payloadStart := 0
		if n >= OpenVLMReportSize {
			payloadStart = OpenVLMPayloadOffset
		}

		if n < payloadStart+2 {
			s.log.Debug().Msgf("ROIP: short report (%d bytes), skipping", n)
			time.Sleep(50 * time.Millisecond)

			continue
		}

		ir1 := buf[payloadStart+1]
		cos := (ir1 & s.cosGPIOMask) != 0

		s.log.Trace().Msgf("ROIP: IR1=0x%02X  COS=%v", ir1, cos)

		if cos == prevCOS {
			continue
		}

		if cos {
			// Half-duplex: suppress PTTDown while network audio is playing.
			if s.isReceiving() {
				s.log.Debug().Msg("ROIP: COS HIGH suppressed (network RX active)")
				// Do not latch — reset so the next HIGH edge is re-evaluated.
				prevCOS = false

				continue
			}

			prevCOS = true

			s.log.Debug().Msg("ROIP: COS HIGH → PTTDown")

			select {
			case ch <- PTTDown:
			case <-ctx.Done():
				return
			}
		} else {
			prevCOS = false

			s.log.Debug().Msg("ROIP: COS LOW → PTTUp")

			select {
			case ch <- PTTUp:
			case <-ctx.Done():
				return
			}
		}
	}
}

// ─── VOX loop ─────────────────────────────────────────────────────────────────

// voxLoop monitors audio energy from the broadcast capture tap. Under
// the unified design there is a single always-on capture stream; the
// ROIP VOX loop subscribes to it via setTap for the entire source
// lifetime (registered once here, unregistered on ctx.Done) and uses
// the same frame stream for both onset detection (IDLE) and silence
// detection (ACTIVE).
//
//	IDLE → accumulate onset frames from tap → PTTDown → ACTIVE
//	ACTIVE → silence for voxHoldTime → PTTUp → IDLE
//
// Half-duplex: PTTDown is suppressed when isReceiving() is true. If the
// network begins receiving during ACTIVE state, PTTUp is emitted
// immediately.
func (s *ROIPSource) voxLoop(ctx context.Context, ch chan<- PTTEvent) {
	maxTX := s.maxTXDuration
	if maxTX <= 0 {
		maxTX = ROIPDefaultMaxTX
	}

	// Single tap subscription for the lifetime of the ROIP source. The
	// always-on broadcast capture pushes every frame through this
	// channel regardless of TX gate state, so the VOX loop sees the
	// same frame cadence in both IDLE and ACTIVE phases.
	tapCh := make(chan []float32, ROIPMonitorBufFrames)
	s.setTap(tapCh)

	defer s.clearTap()

	for {
		if !s.voxIdle(ctx, tapCh) {
			return
		}

		select {
		case ch <- PTTDown:
		case <-ctx.Done():
			return
		}

		s.log.Debug().Msg("ROIP: VOX onset → PTTDown")

		if !s.voxActive(ctx, tapCh, maxTX) {
			return
		}

		select {
		case ch <- PTTUp:
		case <-ctx.Done():
			return
		}

		s.log.Debug().Msg("ROIP: VOX PTTUp emitted")

		// ── WAITING: hold off until the broadcast stream has stopped ──────

		for s.isBroadcasting() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(ROIPVOXPollInterval):
			}
		}

		// Loop back to IDLE.
	}
}

// voxIdle runs the IDLE phase of voxLoop. It reads frames from the
// shared tap channel, accumulates VOX onset frames, and returns true
// when onset is confirmed (PTTDown should fire) or false when ctx is
// canceled or the tap channel closes unexpectedly.
func (s *ROIPSource) voxIdle(ctx context.Context, tapCh <-chan []float32) bool {
	onsetCount := 0

	for {
		select {
		case <-ctx.Done():
			return false

		case frame, ok := <-tapCh:
			if !ok {
				return false
			}

			energy := audiopool.RMSEnergy(frame)
			audiopool.ReturnFloat32(frame)

			if energy >= s.voxThreshold && !s.isReceiving() {
				onsetCount++

				if onsetCount >= ROIPVOXOnsetFrames {
					return true
				}
			} else {
				onsetCount = 0
			}
		}
	}
}

// voxActive runs the ACTIVE phase of voxLoop. It monitors the broadcast tap
// channel for silence or half-duplex RX, enforcing the maxTX deadline.
// Calls clearTap() before returning.
// Returns true when PTTUp should be emitted, false when ctx is canceled.
func (s *ROIPSource) voxActive(ctx context.Context, tapCh <-chan []float32, maxTX time.Duration) bool {
	txDeadline := time.NewTimer(maxTX)
	holdTimer := time.NewTimer(s.voxHoldTime)

	for {
		select {
		case <-ctx.Done():
			txDeadline.Stop()
			holdTimer.Stop()

			return false

		case <-txDeadline.C:
			s.log.Debug().Msg("ROIP: max TX duration reached → PTTUp")
			holdTimer.Stop()

			return true

		case <-holdTimer.C:
			// voxHoldTime of silence in the broadcast tap → PTTUp.
			s.log.Debug().Msg("ROIP: VOX silence hold expired → PTTUp")
			txDeadline.Stop()

			return true

		case frame, ok := <-tapCh:
			if !ok {
				txDeadline.Stop()
				holdTimer.Stop()

				return true
			}

			energy := audiopool.RMSEnergy(frame)
			audiopool.ReturnFloat32(frame)

			if energy >= s.voxThreshold {
				// Radio still transmitting: reset the silence hold timer.
				if !holdTimer.Stop() {
					select {
					case <-holdTimer.C:
					default:
					}
				}

				holdTimer.Reset(s.voxHoldTime)
			}

		case <-time.After(ROIPVOXPollInterval):
			// Poll isReceiving() periodically even when no tap frames arrive.
			if s.isReceiving() {
				s.log.Debug().Msg("ROIP: network RX started → PTTUp (half-duplex)")
				txDeadline.Stop()
				holdTimer.Stop()

				return true
			}
		}
	}
}

// Under the unified capture design the ROIP VOX path subscribes to the
// always-on broadcast capture stream via setTap/clearTap instead of
// opening a second input device. The broadcast stream's capture
// callback forwards every frame to the registered tap channel,
// converted from int16 to float32 at the tap boundary, so the VOX
// loop sees the same sample data without a second concurrent ALSA
// open. See internal/comms/audio/encoder.go captureCallback for the
// tap producer side.
