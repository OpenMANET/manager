package comms

import (
	"context"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"

	"github.com/openmanet/openmanetd/internal/comms/codec"
)

// ─── Adapter tuning constants ─────────────────────────────────────────────────
//
// These are hardcoded by design. The floor is operator-configurable via
// comms.packetLossPerc, but every other knob (dwell times, thresholds,
// EWMA alpha, tick cadence) is a code-level tuning decision that we'd
// want to be able to audit in a git blame rather than a YAML diff.
//
// All thresholds are expressed as loss ratios (0.0–1.0), not percentages.

const (
	// fecTickInterval is the controller cadence. At 2 s, the gap-run
	// histogram has had time to accumulate a statistically meaningful
	// sample without the controller feeling sluggish.
	fecTickInterval = 2 * time.Second

	// fecEWMAAlpha is the smoothing factor for the loss estimate. With a
	// 2 s tick, α = 0.2 gives a 63 % response time of roughly 10 s, which
	// is long enough to ride out a single bad 2 s window without
	// upgrading and short enough to catch a genuine burst within ~2–3
	// ticks of sustained loss.
	fecEWMAAlpha = 0.2

	// fecSilentStallLimit is the number of consecutive silent ticks
	// (zero rx_pushed delta and zero gap-run deltas) after which the
	// EWMA is reset to zero. Prevents a stale loss estimate from
	// lingering across a long inter-PTT gap.
	fecSilentStallLimit = 30

	// fecUpgradeDwell is the number of consecutive ticks the smoothed
	// loss must exceed a threshold before the controller upgrades. Two
	// ticks ≈ 4 s of sustained high loss before we touch the encoder.
	fecUpgradeDwell = 2

	// fecDowngradeDwell is the number of consecutive ticks the smoothed
	// loss must stay below a threshold before the controller downgrades.
	// Fifteen ticks ≈ 30 s, much longer than upgrade dwell so we do not
	// flap back to a lower level on a short recovery window.
	fecDowngradeDwell = 15

	// Loss-ratio thresholds for level transitions. Numbers chosen from
	// the plan — see composed-scribbling-quokka.md for the rationale.
	fecThresh20to30Up   = 0.08
	fecThresh30to40Up   = 0.20
	fecThresh40to30Down = 0.10
	fecThresh30to20Down = 0.03

	// Level values the controller may move through. The encoder knob
	// is always one of these, clamped at or above the configured floor.
	fecLevel20 = 20
	fecLevel30 = 30
	fecLevel40 = 40
)

// gapBucketMidpoints maps the six gap-run buckets on the jitter buffer
// to the approximate midpoint frame count of each bucket. The adapter
// uses these weights to estimate total missing frames from bucket
// delta counts. The over-50 midpoint is intentionally conservative
// (75 rather than, say, 100) so a single massive burst does not
// overwhelm the EWMA.
var gapBucketMidpoints = [6]int64{1, 3, 8, 15, 35, 75}

// FECAdapter is a local, damped control loop that observes the
// receive-side jitter-buffer gap-run histogram and adjusts the Opus
// encoder's packet-loss percentage in response. It runs as a single
// goroutine per comms runtime, spawned during CommsConfig.Run, and
// exits on ctx.Done().
//
// The design assumes link symmetry — every node reads its own RX loss
// and applies the result to its own TX encoder. See the Round 7 plan
// for the omnidirectional-antenna justification of that assumption.
//
// FECAdapter is safe for concurrent Snapshot calls alongside its own
// Run goroutine. All mutable state is guarded by a.mu, with four
// atomic mirrors for the Snapshot path so Snapshot never contends on
// the tick lock.
type FECAdapter struct {
	log     zerolog.Logger
	encoder codec.AudioEncoder
	rt      *CommsRuntime
	floor   int
	// now is injected for tests. Nil → time.Now.
	now func() time.Time

	mu             sync.Mutex
	lossEWMA       float64
	currentLevel   int
	upgradeTicks   int
	downgradeTicks int
	silentTicks    int
	lastChange     time.Time
	writeErrors    int64

	// prev holds the previous-tick counters per port so tick() can
	// compute deltas without scanning every window from scratch.
	prev []fecPortState

	// Atomic mirrors for lock-free Snapshot reads. Each is updated
	// under a.mu at the end of tick() so a concurrent Snapshot
	// caller sees a consistent but possibly slightly stale view.
	snapLevel       atomic.Int32
	snapLossEWMA    atomic.Uint64 // math.Float64bits
	snapLastChange  atomic.Int64  // unix nanos
	snapTransitions atomic.Int64
	snapWriteErrors atomic.Int64
}

// fecPortState caches one port's counters so tick() can compute
// deltas on the next call. Kept tiny and allocation-free.
type fecPortState struct {
	rxPushed int64
	buckets  [6]int64
}

// NewFECAdapter constructs an adapter bound to rt. The initial level
// is clamped to [fecLevel20, fecLevel40] and clamped up to the floor.
// SetPacketLossPerc(level) is called on the encoder immediately so a
// stale value from a previous enable cycle does not linger.
func NewFECAdapter(rt *CommsRuntime, encoder codec.AudioEncoder, floor int, log zerolog.Logger) *FECAdapter {
	if floor < fecLevel20 {
		floor = fecLevel20
	}

	if floor > fecLevel40 {
		floor = fecLevel40
	}

	a := &FECAdapter{
		log:          log,
		encoder:      encoder,
		rt:           rt,
		floor:        floor,
		currentLevel: floor,
		prev:         make([]fecPortState, len(rt.Ports)),
	}

	a.snapLevel.Store(int32(floor))
	a.snapLossEWMA.Store(math.Float64bits(0))

	if err := encoder.SetPacketLossPerc(floor); err != nil {
		log.Error().Err(err).Int("level", floor).Msg("comms: fec adapter initial SetPacketLossPerc failed")
		a.writeErrors++
		a.snapWriteErrors.Store(a.writeErrors)
	}

	return a
}

// nowFn honors an injected clock when set; otherwise returns time.Now.
func (a *FECAdapter) nowFn() time.Time {
	if a.now != nil {
		return a.now()
	}

	return time.Now()
}

// Run blocks until ctx is canceled, ticking every fecTickInterval.
// This is the entry point launched as a goroutine during Run().
func (a *FECAdapter) Run(ctx context.Context) {
	ticker := time.NewTicker(fecTickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.tick()
		}
	}
}

// tick is the pure state-machine step. It reads the current counters
// from every receive-enabled port on rt, computes deltas against prev,
// updates the EWMA, and applies the transition rules. Exported to the
// package (lowercase) so tests can drive it directly without real
// time. Holds a.mu for the full body.
func (a *FECAdapter) tick() {
	a.mu.Lock()
	defer a.mu.Unlock()

	var (
		pushedDelta  int64
		missingDelta int64
		anyActivity  bool
	)

	// Size the prev slice up if ports were added post-construction.
	// Rare but possible during reconfigure flows.
	if len(a.prev) < len(a.rt.Ports) {
		grown := make([]fecPortState, len(a.rt.Ports))
		copy(grown, a.prev)
		a.prev = grown
	}

	for i, pc := range a.rt.Ports {
		if pc == nil || !pc.ReceiveEnabled.Load() {
			continue
		}

		rxPushed := pc.RxPushed.Load()

		var buckets [6]int64

		if pc.Jitter != nil {
			buckets[0] = pc.Jitter.GapRuns1.Load()
			buckets[1] = pc.Jitter.GapRuns2to5.Load()
			buckets[2] = pc.Jitter.GapRuns6to10.Load()
			buckets[3] = pc.Jitter.GapRuns11to20.Load()
			buckets[4] = pc.Jitter.GapRuns21to50.Load()
			buckets[5] = pc.Jitter.GapRunsOver50.Load()
		}

		dPushed := rxPushed - a.prev[i].rxPushed
		if dPushed < 0 {
			// Counter rollback → jitter buffer was reset mid-stream.
			// Skip this port's contribution for this tick.
			dPushed = 0
		}

		var dMissing int64

		for b := range buckets {
			d := buckets[b] - a.prev[i].buckets[b]
			if d < 0 {
				d = 0
			}

			dMissing += d * gapBucketMidpoints[b]
		}

		if dPushed > 0 || dMissing > 0 {
			anyActivity = true
		}

		pushedDelta += dPushed
		missingDelta += dMissing

		a.prev[i].rxPushed = rxPushed
		a.prev[i].buckets = buckets
	}

	if !anyActivity {
		a.silentTicks++
		if a.silentTicks >= fecSilentStallLimit {
			a.lossEWMA = 0
			a.silentTicks = 0
		}

		a.syncSnapshotLocked()

		return
	}

	a.silentTicks = 0

	expected := pushedDelta + missingDelta

	var lossRaw float64
	if expected > 0 {
		lossRaw = float64(missingDelta) / float64(expected)
	}

	// EWMA update: new = α·raw + (1-α)·old.
	a.lossEWMA = fecEWMAAlpha*lossRaw + (1-fecEWMAAlpha)*a.lossEWMA

	a.log.Debug().
		Int("level", a.currentLevel).
		Float64("loss_ewma", a.lossEWMA).
		Float64("loss_raw", lossRaw).
		Int64("missing", missingDelta).
		Int64("pushed", pushedDelta).
		Int("silent_ticks", a.silentTicks).
		Int("upgrade_ticks", a.upgradeTicks).
		Int("downgrade_ticks", a.downgradeTicks).
		Msg("comms: fec adapter tick")

	a.applyStateMachineLocked()
	a.syncSnapshotLocked()
}

// applyStateMachineLocked evaluates the upgrade/downgrade transitions
// from the current level under the current EWMA. Caller must hold a.mu.
func (a *FECAdapter) applyStateMachineLocked() {
	switch a.currentLevel {
	case fecLevel20:
		if a.lossEWMA > fecThresh20to30Up {
			a.upgradeTicks++
			a.downgradeTicks = 0

			if a.upgradeTicks >= fecUpgradeDwell {
				a.transitionLocked(fecLevel30, "upgrade")
			}
		} else {
			a.upgradeTicks = 0
		}
	case fecLevel30:
		switch {
		case a.lossEWMA > fecThresh30to40Up:
			a.upgradeTicks++
			a.downgradeTicks = 0

			if a.upgradeTicks >= fecUpgradeDwell {
				a.transitionLocked(fecLevel40, "upgrade")
			}
		case a.lossEWMA < fecThresh30to20Down:
			a.downgradeTicks++
			a.upgradeTicks = 0

			if a.downgradeTicks >= fecDowngradeDwell {
				a.transitionLocked(fecLevel20, "downgrade")
			}
		default:
			a.upgradeTicks = 0
			a.downgradeTicks = 0
		}
	case fecLevel40:
		if a.lossEWMA < fecThresh40to30Down {
			a.downgradeTicks++
			a.upgradeTicks = 0

			if a.downgradeTicks >= fecDowngradeDwell {
				a.transitionLocked(fecLevel30, "downgrade")
			}
		} else {
			a.downgradeTicks = 0
		}
	}
}

// transitionLocked attempts to change the current level to target,
// clamped at the configured floor. Writes the new value to the
// encoder; on write error, logs and leaves the in-memory state
// aligned with what the encoder still holds.
func (a *FECAdapter) transitionLocked(target int, reason string) {
	if target < a.floor {
		target = a.floor
	}

	if target == a.currentLevel {
		a.upgradeTicks = 0
		a.downgradeTicks = 0

		return
	}

	if err := a.encoder.SetPacketLossPerc(target); err != nil {
		a.writeErrors++
		a.snapWriteErrors.Store(a.writeErrors)
		a.log.Error().
			Err(err).
			Int("from", a.currentLevel).
			Int("to", target).
			Msg("comms: fec adapter SetPacketLossPerc failed; keeping previous level")

		// Reset the dwell counters so we don't infinite-loop retrying
		// on every tick — the controller will re-arm if the condition
		// persists.
		a.upgradeTicks = 0
		a.downgradeTicks = 0

		return
	}

	prev := a.currentLevel
	a.currentLevel = target
	a.lastChange = a.nowFn()
	a.upgradeTicks = 0
	a.downgradeTicks = 0

	a.log.Info().
		Int("from", prev).
		Int("to", target).
		Str("reason", reason).
		Float64("loss_ewma", a.lossEWMA).
		Msg("comms: fec adapter transition")

	a.snapTransitions.Add(1)
}

// syncSnapshotLocked mirrors the current controller state into the
// atomic fields that Snapshot() reads. Caller must hold a.mu.
func (a *FECAdapter) syncSnapshotLocked() {
	a.snapLevel.Store(int32(a.currentLevel))
	a.snapLossEWMA.Store(math.Float64bits(a.lossEWMA))

	if !a.lastChange.IsZero() {
		a.snapLastChange.Store(a.lastChange.UnixNano())
	}
}

// FECAdapterSnapshot is the publicly-observable state of the adapter.
// Populated by FECAdapter.Snapshot and carried on CommsSnapshot as
// the "fec_adapter" section. See docs/instrumentation-snapshot.md.
type FECAdapterSnapshot struct {
	CurrentLevel     int     `json:"current_level"`
	LossEWMA         float64 `json:"loss_ewma"`
	LastChangeUnixNs int64   `json:"last_change_unix_nano"`
	Transitions      int64   `json:"transitions"`
	WriteErrors      int64   `json:"write_errors"`
	Floor            int     `json:"floor"`
}

// Snapshot copies the adapter's current state into dst using atomic
// loads. Nil-safe on both receiver and dst. Zero-alloc: no heap
// allocations beyond what dst already holds.
func (a *FECAdapter) Snapshot(dst *FECAdapterSnapshot) {
	if a == nil || dst == nil {
		return
	}

	dst.CurrentLevel = int(a.snapLevel.Load())
	dst.LossEWMA = math.Float64frombits(a.snapLossEWMA.Load())
	dst.LastChangeUnixNs = a.snapLastChange.Load()
	dst.Transitions = a.snapTransitions.Load()
	dst.WriteErrors = a.snapWriteErrors.Load()
	dst.Floor = a.floor
}
