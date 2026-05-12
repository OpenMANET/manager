// Package alsa provides an AuxEventHandler that adjusts the system ALSA
// playback volume in response to volume up/down aux events. It is wired
// into CommsConfig in the production daemon and can be attached to any
// control source that implements control.AuxEventSource.
//
// The controller talks to ALSA via the pure-Go github.com/gen2brain/alsa
// binding (no CGO, cross-compiles cleanly to linux/amd64, linux/arm64,
// and linux/mipsle). It does not depend on alsa-utils being installed
// on the target.
package alsa

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/gen2brain/alsa"
	"github.com/rs/zerolog"

	"github.com/openmanet/openmanetd/internal/comms/control"
)

// DefaultControlName is the ALSA simple-control name the controller adjusts
// when Controller.ControlName is empty. "Master" is the conventional name
// for the primary playback volume on USB sound cards including the CM108B.
const DefaultControlName = "Master"

// DefaultStep is the per-press change applied to the raw mixer-control
// value when Controller.Step is zero. On a CM108B Master control (38
// steps from -37 dB to 0 dB) one raw step is approximately one dB; this
// approximation does not generalize to all cards.
const DefaultStep = 1

// Mixer is the minimal subset of github.com/gen2brain/alsa.Mixer that the
// controller uses, exposed as an interface so tests can inject a fake
// without requiring a real /dev/snd/controlC* device.
type Mixer interface {
	CtlByName(name string) (Ctl, error)
	Close() error
}

// Ctl is the minimal subset of github.com/gen2brain/alsa.MixerCtl used by
// the controller.
type Ctl interface {
	NumValues() uint32
	Value(index uint) (int, error)
	SetValue(index uint, value int) error
	RangeMin() (int, error)
	RangeMax() (int, error)
}

// Opener opens an ALSA mixer for the given card index. Production callers
// use DefaultOpener; tests inject a fake.
type Opener func(card uint) (Mixer, error)

// Controller is an AuxEventHandler that increments or decrements the raw
// value of a named ALSA mixer control on volume up/down press events.
// Release events are ignored. The zero value is usable: the default opener
// uses gen2brain/alsa, the default control name is "Master", and the
// default step is 1.
type Controller struct {
	Log         zerolog.Logger
	Open        Opener
	ControlName string
	Step        int
}

// Handle implements control.AuxEventHandler. It dispatches on event kind;
// only press events trigger an ALSA write.
func (c *Controller) Handle(ctx context.Context, ev control.AuxEvent) {
	switch ev {
	case control.VolumeUpPressed:
		c.adjust(ctx, +1)
	case control.VolumeDownPressed:
		c.adjust(ctx, -1)
	case control.VolumeUpReleased, control.VolumeDownReleased:
		// Releases are intentionally ignored in v1; held buttons do not
		// auto-repeat.
	}
}

func (c *Controller) controlName() string {
	if c.ControlName != "" {
		return c.ControlName
	}

	return DefaultControlName
}

func (c *Controller) step() int {
	if c.Step > 0 {
		return c.Step
	}

	return DefaultStep
}

func (c *Controller) opener() Opener {
	if c.Open != nil {
		return c.Open
	}

	return DefaultOpener
}

// adjust applies dir * Step to every channel of the named mixer control,
// clamped to [RangeMin, RangeMax]. Errors at any step are logged and
// swallowed so a transient ALSA failure cannot crash the daemon.
func (c *Controller) adjust(_ context.Context, dir int) {
	cardStr := os.Getenv("ALSA_CARD")
	if cardStr == "" {
		c.Log.Debug().Msg("alsa-vol: ALSA_CARD not set; volume event ignored")

		return
	}

	cardNum, err := strconv.Atoi(cardStr)
	if err != nil || cardNum < 0 {
		c.Log.Warn().Err(err).Str("ALSA_CARD", cardStr).
			Msg("alsa-vol: ALSA_CARD is not a non-negative integer; volume event ignored")

		return
	}

	m, err := c.opener()(uint(cardNum))
	if err != nil {
		c.Log.Warn().Err(err).Int("card", cardNum).Msg("alsa-vol: failed to open mixer")

		return
	}

	defer func() {
		if cerr := m.Close(); cerr != nil {
			c.Log.Debug().Err(cerr).Msg("alsa-vol: error closing mixer")
		}
	}()

	name := c.controlName()

	ctl, err := m.CtlByName(name)
	if err != nil {
		c.Log.Warn().Err(err).Str("control", name).Msg("alsa-vol: control not found")

		return
	}

	if ctl == nil {
		c.Log.Warn().Str("control", name).Msg("alsa-vol: nil control")

		return
	}

	minVal, err := ctl.RangeMin()
	if err != nil {
		c.Log.Warn().Err(err).Str("control", name).Msg("alsa-vol: RangeMin failed")

		return
	}

	maxVal, err := ctl.RangeMax()
	if err != nil {
		c.Log.Warn().Err(err).Str("control", name).Msg("alsa-vol: RangeMax failed")

		return
	}

	if maxVal < minVal {
		c.Log.Warn().Int("min", minVal).Int("max", maxVal).Str("control", name).
			Msg("alsa-vol: control reports inverted range; ignoring")

		return
	}

	delta := dir * c.step()
	n := ctl.NumValues()

	if n == 0 {
		c.Log.Warn().Str("control", name).Msg("alsa-vol: control has no values")

		return
	}

	for i := uint(0); i < uint(n); i++ {
		cur, vErr := ctl.Value(i)
		if vErr != nil {
			c.Log.Warn().Err(vErr).Str("control", name).Uint("channel", i).
				Msg("alsa-vol: read value failed")

			return
		}

		next := clamp(cur+delta, minVal, maxVal)
		if next == cur {
			c.Log.Debug().Str("control", name).Uint("channel", i).
				Int("value", cur).Int("min", minVal).Int("max", maxVal).
				Msg("alsa-vol: at range bound; no change")

			continue
		}

		if sErr := ctl.SetValue(i, next); sErr != nil {
			c.Log.Warn().Err(sErr).Str("control", name).Uint("channel", i).
				Int("value", next).Msg("alsa-vol: set value failed")

			return
		}

		c.Log.Debug().Str("control", name).Uint("channel", i).
			Int("from", cur).Int("to", next).Msg("alsa-vol: adjusted")
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}

	if v > hi {
		return hi
	}

	return v
}

// ─── gen2brain/alsa adapter ──────────────────────────────────────────────────

// DefaultOpener is the production Opener: it opens the kernel mixer for the
// given card via gen2brain/alsa and wraps the result so the controller's
// Mixer/Ctl interfaces are satisfied.
func DefaultOpener(card uint) (Mixer, error) {
	m, err := alsa.MixerOpen(card)
	if err != nil {
		return nil, fmt.Errorf("alsa.MixerOpen card=%d: %w", card, err)
	}

	return &mixerWrap{m: m}, nil
}

type mixerWrap struct {
	m *alsa.Mixer
}

func (w *mixerWrap) CtlByName(name string) (Ctl, error) {
	c, err := w.m.CtlByName(name)
	if err != nil {
		return nil, fmt.Errorf("CtlByName %q: %w", name, err)
	}

	if c == nil {
		return nil, errors.New("CtlByName returned nil")
	}

	return &ctlWrap{c: c}, nil
}

func (w *mixerWrap) Close() error {
	if err := w.m.Close(); err != nil {
		return fmt.Errorf("mixer close: %w", err)
	}

	return nil
}

type ctlWrap struct {
	c *alsa.MixerCtl
}

func (w *ctlWrap) NumValues() uint32 { return w.c.NumValues() }

func (w *ctlWrap) Value(index uint) (int, error) {
	v, err := w.c.Value(index)
	if err != nil {
		return 0, fmt.Errorf("ctl value: %w", err)
	}

	return v, nil
}

func (w *ctlWrap) SetValue(index uint, value int) error {
	if err := w.c.SetValue(index, value); err != nil {
		return fmt.Errorf("ctl set value: %w", err)
	}

	return nil
}

func (w *ctlWrap) RangeMin() (int, error) {
	v, err := w.c.RangeMin()
	if err != nil {
		return 0, fmt.Errorf("ctl range min: %w", err)
	}

	return v, nil
}

func (w *ctlWrap) RangeMax() (int, error) {
	v, err := w.c.RangeMax()
	if err != nil {
		return 0, fmt.Errorf("ctl range max: %w", err)
	}

	return v, nil
}
