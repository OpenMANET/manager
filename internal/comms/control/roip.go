package control

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gordonklaus/portaudio"
	"github.com/rs/zerolog"

	"github.com/openmanet/openmanetd/internal/comms/audiopool"
	"github.com/openmanet/openmanetd/internal/comms/device"
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
//     OpenVLM GPIO pin.  The HID report is polled; cosGPIOMask selects the IR1
//     bit.  PTTDown on the HIGH→LOW squelch edge, PTTUp on LOW→HIGH.
//
//  2. VOX fallback: if the HID device is unavailable or cosGPIOMask is 0, an
//     audio energy threshold is applied to the OpenVLM input stream.  A
//     configurable onset window (ROIPVOXOnsetFrames) prevents false triggers.
//     During active transmission the broadcast stream feeds frames into a tap
//     channel so silence can be detected and PTTUp emitted after voxHoldTime.
type ROIPSource struct {
	log            zerolog.Logger
	opener         HIDOpener
	openMonitor    func() (<-chan []float32, func(), error)
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
// library, real PortAudio, and caller-provided callbacks for half-duplex
// enforcement and broadcast-tap wiring. Step 4 of the comms refactor flattened
// the constructor to take primitives + callbacks directly so the control
// sub-package no longer depends on parent comms types.
func NewROIPSource(
	log zerolog.Logger,
	cosGPIOMask byte,
	voxThreshold float32,
	voxHoldTime time.Duration,
	maxTXDuration time.Duration,
	inputDevice string,
	isReceiving, isBroadcasting func() bool,
	setTap func(chan []float32),
	clearTap func(),
	openMonitor func() (<-chan []float32, func(), error),
) EventSource {
	log.Info().Msgf(
		"comms: ROIP bridge on OpenVLM (VID=0x%04X PID=0x%04X) COSmask=0x%02X VOX=%.3f hold=%s",
		OpenVLMVendorID, OpenVLMProductID, cosGPIOMask, voxThreshold, voxHoldTime,
	)

	s := &ROIPSource{
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

	if openMonitor != nil {
		s.openMonitor = openMonitor
	} else {
		s.openMonitor = makeROIPMonitorOpener(inputDevice, log)
	}

	return s
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
		openMonitor:    noopMonitorOpener,
	}
}

// NewROIPSourceWithMonitor is the testable VOX-path constructor.
// openMonitorFn replaces the PortAudio monitor stream so tests can inject a
// pre-filled frame channel without real hardware.
func NewROIPSourceWithMonitor(
	openMonitorFn func() (<-chan []float32, func(), error),
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
		setTap:         func(_ chan []float32) {},
		clearTap:       func() {},
		openMonitor:    openMonitorFn,
	}
}

// noopMonitorOpener returns an immediately-closed channel. Used in COS-only
// tests where openMonitor is wired but never reached.
func noopMonitorOpener() (<-chan []float32, func(), error) {
	ch := make(chan []float32)
	close(ch)

	return ch, func() {}, nil
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

// voxLoop monitors audio energy from the OpenVLM input.  It operates as a
// two-phase state machine:
//
//	IDLE → open monitor stream, accumulate onset frames → PTTDown → ACTIVE
//	ACTIVE → broadcast tap provides frames; silence for voxHoldTime → PTTUp → IDLE
//
// Half-duplex: PTTDown is suppressed when isReceiving() is true.  If the
// network begins receiving during ACTIVE state, PTTUp is emitted immediately.
func (s *ROIPSource) voxLoop(ctx context.Context, ch chan<- PTTEvent) {
	maxTX := s.maxTXDuration
	if maxTX <= 0 {
		maxTX = ROIPDefaultMaxTX
	}

	for {
		if !s.voxIdle(ctx) {
			return
		}

		// ── Transition: open broadcast tap, emit PTTDown ───────────────────

		tapCh := make(chan []float32, ROIPMonitorBufFrames)
		s.setTap(tapCh)

		select {
		case ch <- PTTDown:
		case <-ctx.Done():
			s.clearTap()

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

// voxIdle runs the IDLE phase of voxLoop. It opens the monitor stream,
// accumulates VOX onset frames, and closes the monitor before returning.
// Returns true when onset is confirmed (PTTDown should fire),
// or false when ctx is canceled or the monitor channel closes unexpectedly.
func (s *ROIPSource) voxIdle(ctx context.Context) bool {
	for {
		monitorCh, closeMonitor, err := s.openMonitor()
		if err != nil {
			s.log.Error().Err(err).Msg("ROIP: failed to open VOX monitor stream; retrying")

			select {
			case <-ctx.Done():
				return false
			case <-time.After(2 * time.Second):
				continue
			}
		}

		onsetCount := 0

		for {
			select {
			case <-ctx.Done():
				closeMonitor()

				return false

			case frame, ok := <-monitorCh:
				if !ok {
					closeMonitor()

					return false
				}

				energy := audiopool.RMSEnergy(frame)
				audiopool.ReturnFloat32(frame)

				if energy >= s.voxThreshold && !s.isReceiving() {
					onsetCount++

					if onsetCount >= ROIPVOXOnsetFrames {
						closeMonitor()

						return true
					}
				} else {
					onsetCount = 0
				}
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

	defer s.clearTap()

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

// ─── Production monitor opener ────────────────────────────────────────────────

// makeROIPMonitorOpener returns a factory that opens a PortAudio input stream
// on inputDevice and pushes raw float32 frames into a channel. The returned
// closer stops and closes the stream and drains any buffered frames.
func makeROIPMonitorOpener(inputDevice string, log zerolog.Logger) func() (<-chan []float32, func(), error) {
	return func() (<-chan []float32, func(), error) {
		inDev, err := device.ResolveAudio(inputDevice, true)
		if err != nil {
			return nil, nil, fmt.Errorf("ROIP: resolve audio device: %w", err)
		}

		frameCh := make(chan []float32, ROIPMonitorBufFrames)

		params := portaudio.StreamParameters{
			Input: portaudio.StreamDeviceParameters{
				Device:   inDev,
				Channels: audiopool.Channels,
			},
			SampleRate:      float64(audiopool.SampleRate),
			FramesPerBuffer: audiopool.FrameSize,
		}

		stream, openErr := portaudio.OpenStream(params, func(in []float32) {
			fp := audiopool.Float32Pool.Get().(*[]float32) //nolint:forcetypeassert
			f := (*fp)[:audiopool.FrameSize]
			copy(f, in)

			select {
			case frameCh <- f:
			default:
				audiopool.ReturnFloat32(f)
			}
		})
		if openErr != nil {
			return nil, nil, fmt.Errorf("ROIP: open monitor stream: %w", openErr)
		}

		if startErr := stream.Start(); startErr != nil {
			_ = stream.Close()

			return nil, nil, fmt.Errorf("ROIP: start monitor stream: %w", startErr)
		}

		log.Debug().Str("device", inDev.Name).Msg("ROIP: VOX monitor stream opened")

		closer := func() {
			_ = stream.Stop()
			_ = stream.Close()

			// Drain any buffered frames to return pool allocations.
		drain:
			for {
				select {
				case f := <-frameCh:
					audiopool.ReturnFloat32(f)
				default:
					break drain
				}
			}

			log.Debug().Msg("ROIP: VOX monitor stream closed")
		}

		return frameCh, closer, nil
	}
}
