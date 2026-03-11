package comms

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/gordonklaus/portaudio"
	"github.com/rs/zerolog"
)

// ─── ROIP constants ───────────────────────────────────────────────────────────

const (
	// roipDefaultCOSMask selects GPIO1 within IR1 as the default COS input.
	roipDefaultCOSMask byte = 0x01

	// roipDefaultVOXThresh is the RMS energy level at which the VOX path
	// considers the radio active. Tuned for a typical USB audio dongle with
	// line-level input from an analog handheld radio.
	roipDefaultVOXThresh float32 = 0.02

	// roipDefaultVOXHold is the silence window after which the VOX emits PTTUp.
	roipDefaultVOXHold time.Duration = 500 * time.Millisecond

	// roipDefaultMaxTX is the safety ceiling on a single ROIP transmission.
	roipDefaultMaxTX time.Duration = 60 * time.Second

	// roipVOXOnsetFrames is the number of consecutive loud frames required to
	// trigger PTTDown. Each frame is 20 ms, so 3 frames = 60 ms onset guard.
	roipVOXOnsetFrames int = 3

	// roipMonitorBufFrames is the depth of the monitor / tap frame channel.
	roipMonitorBufFrames int = 8

	// roipVOXPollInterval is the ticker period used for isReceiving() polling
	// in the active state when no tap frames are arriving.
	roipVOXPollInterval time.Duration = 10 * time.Millisecond
)

// ─── roipSource ───────────────────────────────────────────────────────────────

// roipSource implements EventSource for a ROIP (Radio over IP) bridge.
//
// It uses the same CM108 USB audio dongle as cm108Source but operates without
// a manual PTT button, automatically bridging an analog handheld radio into
// the multicast comms network.
//
// Detection strategy (half-duplex enforced throughout):
//
//  1. COS (Carrier-Operated Squelch): the radio squelch output is wired to a
//     CM108 GPIO pin.  The HID report is polled; cosGPIOMask selects the IR1
//     bit.  PTTDown on the HIGH→LOW squelch edge, PTTUp on LOW→HIGH.
//
//  2. VOX fallback: if the HID device is unavailable or cosGPIOMask is 0, an
//     audio energy threshold is applied to the CM108 input stream.  A
//     configurable onset window (roipVOXOnsetFrames) prevents false triggers.
//     During active transmission the broadcast stream feeds frames into a tap
//     channel so silence can be detected and PTTUp emitted after voxHoldTime.
type roipSource struct {
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

// NewROIPSource constructs a production roipSource backed by the real HIDAPI
// library, real PortAudio, and CommsRuntime callbacks for half-duplex
// enforcement and broadcast-tap wiring.
func NewROIPSource(
	cfg *CommsConfig,
	isReceiving, isBroadcasting func() bool,
	setTap func(chan []float32),
	clearTap func(),
	log zerolog.Logger,
) EventSource {
	s := &roipSource{
		log:            log,
		opener:         defaultHIDOpener,
		cosGPIOMask:    cfg.ROIPCOSGPIOMask,
		voxThreshold:   cfg.ROIPVOXThreshold,
		voxHoldTime:    cfg.ROIPVOXHoldTime,
		maxTXDuration:  cfg.ROIPMaxTXDuration,
		isReceiving:    isReceiving,
		isBroadcasting: isBroadcasting,
		setTap:         setTap,
		clearTap:       clearTap,
	}
	s.openMonitor = makeROIPMonitorOpener(cfg.ROIPInputDevice, log)

	return s
}

// newROIPSourceWithOpener is the testable COS-path constructor.
// It injects a custom HIDOpener without touching PortAudio.
func newROIPSourceWithOpener(
	opener HIDOpener,
	cosGPIOMask byte,
	isReceiving, isBroadcasting func() bool,
	log zerolog.Logger,
) EventSource {
	return &roipSource{
		log:            log,
		opener:         opener,
		cosGPIOMask:    cosGPIOMask,
		voxThreshold:   0, // COS-only
		voxHoldTime:    roipDefaultVOXHold,
		maxTXDuration:  roipDefaultMaxTX,
		isReceiving:    isReceiving,
		isBroadcasting: isBroadcasting,
		setTap:         func(_ chan []float32) {},
		clearTap:       func() {},
		openMonitor:    noopMonitorOpener,
	}
}

// newROIPSourceWithMonitor is the testable VOX-path constructor.
// openMonitorFn replaces the PortAudio monitor stream so tests can inject a
// pre-filled frame channel without real hardware.
func newROIPSourceWithMonitor(
	openMonitorFn func() (<-chan []float32, func(), error),
	voxHoldTime time.Duration,
	isReceiving, isBroadcasting func() bool,
	log zerolog.Logger,
) EventSource {
	return &roipSource{
		log:            log,
		opener:         nil, // cosGPIOMask==0; opener never called
		cosGPIOMask:    0,
		voxThreshold:   roipDefaultVOXThresh,
		voxHoldTime:    voxHoldTime,
		maxTXDuration:  roipDefaultMaxTX,
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
//   - COS mode when cosGPIOMask != 0 and the CM108 HID device opens successfully.
//   - VOX mode as fallback when COS is unavailable.
//   - Error (channel closed immediately) when neither is available.
func (s *roipSource) Events(ctx context.Context) <-chan PTTEvent {
	ch := make(chan PTTEvent, 4)

	go func() {
		defer close(ch)

		// COS path: attempt to open the CM108 HID device.
		if s.cosGPIOMask != 0 {
			dev, err := s.opener(cm108VendorID, cm108ProductID)
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

// cosLoop polls the CM108 HID device for the COS GPIO bit (cosGPIOMask in IR1)
// and emits PTTDown on the LOW→HIGH transition and PTTUp on HIGH→LOW.
//
// Half-duplex: PTTDown is suppressed while isReceiving() returns true.  The
// COS transition is not latched in that case; the next HIGH will be re-checked.
func (s *roipSource) cosLoop(ctx context.Context, dev HIDDevice, ch chan<- PTTEvent) { //nolint:gocognit
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

	buf := make([]byte, cm108ReportSize)
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
		if n >= cm108ReportSize {
			payloadStart = cm108PayloadOffset
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

// voxLoop monitors audio energy from the CM108 input.  It operates as a
// two-phase state machine:
//
//	IDLE → open monitor stream, accumulate onset frames → PTTDown → ACTIVE
//	ACTIVE → broadcast tap provides frames; silence for voxHoldTime → PTTUp → IDLE
//
// Half-duplex: PTTDown is suppressed when isReceiving() is true.  If the
// network begins receiving during ACTIVE state, PTTUp is emitted immediately.
func (s *roipSource) voxLoop(ctx context.Context, ch chan<- PTTEvent) {
	maxTX := s.maxTXDuration
	if maxTX <= 0 {
		maxTX = roipDefaultMaxTX
	}

	for {
		if !s.voxIdle(ctx) {
			return
		}

		// ── Transition: open broadcast tap, emit PTTDown ───────────────────

		tapCh := make(chan []float32, roipMonitorBufFrames)
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
			case <-time.After(roipVOXPollInterval):
			}
		}

		// Loop back to IDLE.
	}
}

// voxIdle runs the IDLE phase of voxLoop. It opens the monitor stream,
// accumulates VOX onset frames, and closes the monitor before returning.
// Returns true when onset is confirmed (PTTDown should fire),
// or false when ctx is canceled or the monitor channel closes unexpectedly.
func (s *roipSource) voxIdle(ctx context.Context) bool {
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

				energy := rmsEnergy(frame)
				returnFloat32(frame)

				if energy >= s.voxThreshold && !s.isReceiving() {
					onsetCount++

					if onsetCount >= roipVOXOnsetFrames {
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
func (s *roipSource) voxActive(ctx context.Context, tapCh <-chan []float32, maxTX time.Duration) bool {
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

			energy := rmsEnergy(frame)
			returnFloat32(frame)

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

		case <-time.After(roipVOXPollInterval):
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

// ─── Audio helpers ────────────────────────────────────────────────────────────

// rmsEnergy computes the root-mean-square energy of a float32 PCM frame.
func rmsEnergy(frame []float32) float32 {
	if len(frame) == 0 {
		return 0
	}

	var sum float64

	for _, v := range frame {
		sum += float64(v) * float64(v)
	}

	return float32(math.Sqrt(sum / float64(len(frame))))
}

// ─── Production monitor opener ────────────────────────────────────────────────

// makeROIPMonitorOpener returns a factory that opens a PortAudio input stream
// on inputDevice and pushes raw float32 frames into a channel. The returned
// closer stops and closes the stream and drains any buffered frames.
func makeROIPMonitorOpener(inputDevice string, log zerolog.Logger) func() (<-chan []float32, func(), error) {
	return func() (<-chan []float32, func(), error) {
		inDev, err := resolveAudioDevice(inputDevice, true)
		if err != nil {
			return nil, nil, fmt.Errorf("ROIP: resolve audio device: %w", err)
		}

		frameCh := make(chan []float32, roipMonitorBufFrames)

		params := portaudio.StreamParameters{
			Input: portaudio.StreamDeviceParameters{
				Device:   inDev,
				Channels: channels,
			},
			SampleRate:      float64(sampleRate),
			FramesPerBuffer: frameSize,
		}

		stream, openErr := portaudio.OpenStream(params, func(in []float32) {
			fp := float32Pool.Get().(*[]float32) //nolint:forcetypeassert
			f := (*fp)[:frameSize]
			copy(f, in)

			select {
			case frameCh <- f:
			default:
				returnFloat32(f)
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
					returnFloat32(f)
				default:
					break drain
				}
			}

			log.Debug().Msg("ROIP: VOX monitor stream closed")
		}

		return frameCh, closer, nil
	}
}
