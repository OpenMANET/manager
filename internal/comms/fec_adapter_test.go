package comms

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/openmanet/openmanetd/internal/comms/codec"
	"github.com/openmanet/openmanetd/internal/comms/rtp"
)

// ─── Test fake encoder ───────────────────────────────────────────────────────

// fakeAdapterEncoder is a minimal codec.AudioEncoder fake that records
// every SetPacketLossPerc call and can be primed to return an error.
type fakeAdapterEncoder struct {
	mu           sync.Mutex
	lastPerc     int
	calls        int
	setPercCalls []int
	setPercErr   error
}

func (f *fakeAdapterEncoder) EncodeS16(_ []int16, _ []byte) (int, error) {
	return 0, nil
}

func (f *fakeAdapterEncoder) Encode(_ []int16, _ []byte) (int, error) {
	return 0, nil
}

func (f *fakeAdapterEncoder) SetPacketLossPerc(perc int) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++

	if f.setPercErr != nil {
		return f.setPercErr
	}

	f.lastPerc = perc
	f.setPercCalls = append(f.setPercCalls, perc)

	return nil
}

func (f *fakeAdapterEncoder) Close() error { return nil }

func (f *fakeAdapterEncoder) last() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.lastPerc
}

func (f *fakeAdapterEncoder) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.calls
}

func (f *fakeAdapterEncoder) history() []int {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]int, len(f.setPercCalls))
	copy(out, f.setPercCalls)

	return out
}

// ─── Test harness ────────────────────────────────────────────────────────────

// newTestAdapter constructs a FECAdapter bound to a synthetic runtime
// containing `numPorts` receive-enabled ports. Each port gets its own
// real JitterBuffer so the adapter reads the actual atomic fields.
func newTestAdapter(t *testing.T, floor int, numPorts int) (*FECAdapter, *fakeAdapterEncoder, *CommsRuntime) {
	t.Helper()

	rt := &CommsRuntime{
		Ports: make([]*PortChannel, 0, numPorts),
	}

	for range numPorts {
		pc := &PortChannel{
			Jitter: rtp.NewJitterBuffer(rtp.PrebufferPackets, rtp.MaxDepth),
		}
		pc.ReceiveEnabled.Store(true)
		rt.Ports = append(rt.Ports, pc)
	}

	enc := &fakeAdapterEncoder{}

	a := NewFECAdapter(rt, enc, floor, zerolog.Nop())

	return a, enc, rt
}

// seedPort bumps the counters on port index `idx` as if `pushed` frames
// were received and the six buckets accumulated the values in `buckets`.
// Seeding is absolute, not delta — the adapter will compute deltas
// against its own prev state.
func seedPort(t *testing.T, rt *CommsRuntime, idx int, pushed int64, buckets [6]int64) {
	t.Helper()

	pc := rt.Ports[idx]
	pc.RxPushed.Store(pushed)
	pc.Jitter.GapRuns1.Store(buckets[0])
	pc.Jitter.GapRuns2to5.Store(buckets[1])
	pc.Jitter.GapRuns6to10.Store(buckets[2])
	pc.Jitter.GapRuns11to20.Store(buckets[3])
	pc.Jitter.GapRuns21to50.Store(buckets[4])
	pc.Jitter.GapRunsOver50.Store(buckets[5])
}

// addToPort increments the absolute counters on port idx — used to
// feed a tick's worth of fresh deltas between two tick() calls.
func addToPort(t *testing.T, rt *CommsRuntime, idx int, pushed int64, buckets [6]int64) {
	t.Helper()

	pc := rt.Ports[idx]
	pc.RxPushed.Add(pushed)
	pc.Jitter.GapRuns1.Add(buckets[0])
	pc.Jitter.GapRuns2to5.Add(buckets[1])
	pc.Jitter.GapRuns6to10.Add(buckets[2])
	pc.Jitter.GapRuns11to20.Add(buckets[3])
	pc.Jitter.GapRuns21to50.Add(buckets[4])
	pc.Jitter.GapRunsOver50.Add(buckets[5])
}

// feedLossTicks runs `n` ticks where each tick adds `pushed` frames and
// a crafted `buckets` delta to port 0. Used for sustained-loss scenarios.
func feedLossTicks(t *testing.T, a *FECAdapter, rt *CommsRuntime, n int, pushed int64, buckets [6]int64) {
	t.Helper()

	for range n {
		addToPort(t, rt, 0, pushed, buckets)
		a.tick()
	}
}

// ─── State machine tests ─────────────────────────────────────────────────────

// TestFECAdapter_IdleStartup verifies the adapter writes the floor to
// the encoder at construction and stays at the floor when no ports are
// active.
func TestFECAdapter_IdleStartup(t *testing.T) {
	a, enc, _ := newTestAdapter(t, 20, 0)

	if got := enc.last(); got != 20 {
		t.Errorf("initial SetPacketLossPerc = %d, want 20", got)
	}

	if a.currentLevel != 20 {
		t.Errorf("initial currentLevel = %d, want 20", a.currentLevel)
	}

	// Run a few ticks; nothing should change.
	for range 10 {
		a.tick()
	}

	if a.currentLevel != 20 {
		t.Errorf("after idle ticks: currentLevel = %d, want 20", a.currentLevel)
	}

	if a.snapTransitions.Load() != 0 {
		t.Errorf("snapTransitions = %d, want 0", a.snapTransitions.Load())
	}
}

// TestFECAdapter_CleanTrafficAtFloor verifies sustained clean traffic
// holds the adapter at the floor.
func TestFECAdapter_CleanTrafficAtFloor(t *testing.T) {
	a, _, rt := newTestAdapter(t, 20, 1)

	for range 50 {
		addToPort(t, rt, 0, 100, [6]int64{0, 0, 0, 0, 0, 0})
		a.tick()
	}

	if a.currentLevel != 20 {
		t.Errorf("currentLevel = %d, want 20", a.currentLevel)
	}

	if a.lossEWMA > 0.01 {
		t.Errorf("lossEWMA = %f, want near 0", a.lossEWMA)
	}

	if a.snapTransitions.Load() != 0 {
		t.Errorf("transitions = %d, want 0", a.snapTransitions.Load())
	}
}

// TestFECAdapter_TransientSpikeNoUpgrade verifies a single high-loss
// tick does not trigger an upgrade (dwell requires 2 consecutive).
func TestFECAdapter_TransientSpikeNoUpgrade(t *testing.T) {
	a, _, rt := newTestAdapter(t, 20, 1)

	// One bad tick: 15% loss → raw 0.15, but EWMA at tick 1 = 0.2*0.15 = 0.03.
	// EWMA is well under 0.08 upgrade threshold after just one tick — so
	// the upgradeTicks counter never increments.
	addToPort(t, rt, 0, 85, [6]int64{0, 5, 0, 0, 0, 0}) // missing = 5*3 = 15
	a.tick()

	// Feed clean traffic for 10 more ticks.
	for range 10 {
		addToPort(t, rt, 0, 100, [6]int64{0, 0, 0, 0, 0, 0})
		a.tick()
	}

	if a.currentLevel != 20 {
		t.Errorf("currentLevel = %d, want 20 (transient spike must not upgrade)", a.currentLevel)
	}
}

// TestFECAdapter_SustainedUpgradeTo30 verifies sustained ~15% loss
// upgrades from level 20 to level 30.
func TestFECAdapter_SustainedUpgradeTo30(t *testing.T) {
	a, _, rt := newTestAdapter(t, 20, 1)

	// 85 pushed, 5 gap runs of ~3 frames each → missing=15, loss≈15/100=0.15.
	// EWMA converges toward 0.15; crosses 0.08 upgrade threshold after ~3
	// ticks. Dwell of 2 ticks means upgrade fires around tick 5 at latest.
	feedLossTicks(t, a, rt, 20, 85, [6]int64{0, 5, 0, 0, 0, 0})

	if a.currentLevel != 30 {
		t.Errorf("currentLevel = %d, want 30", a.currentLevel)
	}

	if a.snapTransitions.Load() < 1 {
		t.Errorf("transitions = %d, want ≥ 1", a.snapTransitions.Load())
	}
}

// TestFECAdapter_SustainedUpgradeTo40 verifies sustained ~30% loss
// upgrades through 30 to 40.
func TestFECAdapter_SustainedUpgradeTo40(t *testing.T) {
	a, enc, rt := newTestAdapter(t, 20, 1)

	// 70 pushed, 10 gap runs of 3 frames → missing=30, loss≈30/100=0.30.
	// Converges past both 0.08 and 0.20 thresholds.
	feedLossTicks(t, a, rt, 30, 70, [6]int64{0, 10, 0, 0, 0, 0})

	if a.currentLevel != 40 {
		t.Errorf("currentLevel = %d, want 40", a.currentLevel)
	}

	if a.snapTransitions.Load() != 2 {
		t.Errorf("transitions = %d, want 2 (20→30→40)", a.snapTransitions.Load())
	}

	history := enc.history()
	if len(history) < 3 {
		t.Fatalf("encoder history len = %d, want ≥ 3", len(history))
	}

	// First call is the initial floor from construction; then 30, then 40.
	if history[0] != 20 {
		t.Errorf("history[0] = %d, want 20", history[0])
	}

	if history[len(history)-1] != 40 {
		t.Errorf("history[last] = %d, want 40", history[len(history)-1])
	}
}

// TestFECAdapter_NeverBelowFloor verifies the adapter cannot drop below
// its configured floor under any amount of clean traffic.
func TestFECAdapter_NeverBelowFloor(t *testing.T) {
	a, _, rt := newTestAdapter(t, 25, 1)

	// Start pinned at floor 25; feed 50 ticks of clean traffic.
	for range 50 {
		addToPort(t, rt, 0, 100, [6]int64{0, 0, 0, 0, 0, 0})
		a.tick()
	}

	if a.currentLevel != 25 {
		t.Errorf("currentLevel = %d, want 25 (floor)", a.currentLevel)
	}
}

// TestFECAdapter_FloorAt40 verifies a floor of 40 pins the adapter at
// 40 regardless of the loss pattern.
func TestFECAdapter_FloorAt40(t *testing.T) {
	a, _, rt := newTestAdapter(t, 40, 1)

	// Mix clean, lossy, very lossy windows.
	for range 20 {
		addToPort(t, rt, 0, 100, [6]int64{0, 0, 0, 0, 0, 0})
		a.tick()
	}

	for range 20 {
		addToPort(t, rt, 0, 50, [6]int64{0, 0, 0, 0, 0, 1}) // 75 missing
		a.tick()
	}

	if a.currentLevel != 40 {
		t.Errorf("currentLevel = %d, want 40", a.currentLevel)
	}

	if a.snapTransitions.Load() != 0 {
		t.Errorf("transitions = %d, want 0 (pinned at ceiling floor)", a.snapTransitions.Load())
	}
}

// TestFECAdapter_DowngradeFromPeak verifies full recovery from 40 back
// to floor over enough ticks to satisfy both downgrade dwells.
func TestFECAdapter_DowngradeFromPeak(t *testing.T) {
	a, _, rt := newTestAdapter(t, 20, 1)

	// Drive up to 40.
	feedLossTicks(t, a, rt, 20, 70, [6]int64{0, 10, 0, 0, 0, 0})
	if a.currentLevel != 40 {
		t.Fatalf("precondition: currentLevel = %d, want 40", a.currentLevel)
	}

	// Feed clean traffic; should drop 40→30 after 15 ticks, then 30→20
	// after another 15 once EWMA falls below 0.03.
	// 60 ticks is plenty of headroom for both dwells + EWMA decay.
	for range 60 {
		addToPort(t, rt, 0, 100, [6]int64{0, 0, 0, 0, 0, 0})
		a.tick()
	}

	if a.currentLevel != 20 {
		t.Errorf("currentLevel = %d, want 20 (floor)", a.currentLevel)
	}

	// Expect at least the 20→30, 30→40, 40→30, 30→20 transitions.
	if a.snapTransitions.Load() < 4 {
		t.Errorf("transitions = %d, want ≥ 4", a.snapTransitions.Load())
	}
}

// TestFECAdapter_NoFastFlap verifies that immediately after an upgrade
// the downgrade dwell holds level 40 even if loss collapses to zero.
func TestFECAdapter_NoFastFlap(t *testing.T) {
	a, _, rt := newTestAdapter(t, 20, 1)

	// Drive up to 40 fast.
	feedLossTicks(t, a, rt, 10, 70, [6]int64{0, 10, 0, 0, 0, 0})
	if a.currentLevel != 40 {
		t.Fatalf("precondition: currentLevel = %d, want 40", a.currentLevel)
	}

	transitionsBefore := a.snapTransitions.Load()

	// Immediately feed 4 clean ticks — well below the 15-tick downgrade
	// dwell. Level must stay at 40.
	for range 4 {
		addToPort(t, rt, 0, 100, [6]int64{0, 0, 0, 0, 0, 0})
		a.tick()
	}

	if a.currentLevel != 40 {
		t.Errorf("currentLevel = %d, want 40 (dwell hold)", a.currentLevel)
	}

	if a.snapTransitions.Load() != transitionsBefore {
		t.Errorf("transitions changed during dwell: before=%d after=%d",
			transitionsBefore, a.snapTransitions.Load())
	}
}

// TestFECAdapter_SilentWindowFreeze verifies silent ticks (no RX
// activity) do not change the EWMA or the level.
func TestFECAdapter_SilentWindowFreeze(t *testing.T) {
	a, _, rt := newTestAdapter(t, 20, 1)

	// Warm up a non-zero EWMA.
	feedLossTicks(t, a, rt, 5, 90, [6]int64{5, 0, 0, 0, 0, 0}) // missing=5

	ewmaBefore := a.lossEWMA
	levelBefore := a.currentLevel

	// 10 silent ticks.
	for range 10 {
		a.tick()
	}

	if a.lossEWMA != ewmaBefore {
		t.Errorf("EWMA changed during silence: before=%f after=%f", ewmaBefore, a.lossEWMA)
	}

	if a.currentLevel != levelBefore {
		t.Errorf("level changed during silence: before=%d after=%d", levelBefore, a.currentLevel)
	}

	if a.silentTicks != 10 {
		t.Errorf("silentTicks = %d, want 10", a.silentTicks)
	}
}

// TestFECAdapter_SilentStallResetsEWMA verifies that after the silent
// stall limit the EWMA is reset to zero.
func TestFECAdapter_SilentStallResetsEWMA(t *testing.T) {
	a, _, rt := newTestAdapter(t, 20, 1)

	feedLossTicks(t, a, rt, 5, 90, [6]int64{5, 0, 0, 0, 0, 0})
	if a.lossEWMA <= 0 {
		t.Fatal("precondition: expected nonzero EWMA")
	}

	for range fecSilentStallLimit + 1 {
		a.tick()
	}

	if a.lossEWMA != 0 {
		t.Errorf("EWMA after silent stall = %f, want 0", a.lossEWMA)
	}
}

// TestFECAdapter_EncoderWriteFailure verifies that when the encoder
// rejects SetPacketLossPerc, the adapter does not update its own
// currentLevel (the encoder is the source of truth).
func TestFECAdapter_EncoderWriteFailure(t *testing.T) {
	rt := &CommsRuntime{
		Ports: []*PortChannel{{Jitter: rtp.NewJitterBuffer(rtp.PrebufferPackets, rtp.MaxDepth)}},
	}
	rt.Ports[0].ReceiveEnabled.Store(true)

	enc := &fakeAdapterEncoder{}
	a := NewFECAdapter(rt, enc, 20, zerolog.Nop())

	// Arm the error AFTER construction so the initial SetPacketLossPerc
	// at construction is allowed to succeed.
	enc.mu.Lock()
	enc.setPercErr = errors.New("simulated opus ctl failure")
	enc.mu.Unlock()

	// Drive sustained loss that would normally upgrade.
	feedLossTicks(t, a, rt, 20, 70, [6]int64{0, 10, 0, 0, 0, 0})

	if a.currentLevel != 20 {
		t.Errorf("currentLevel = %d, want 20 (encoder write failed, level must not change)", a.currentLevel)
	}

	if a.writeErrors == 0 {
		t.Error("expected writeErrors > 0 after failed Set calls")
	}

	if a.snapWriteErrors.Load() == 0 {
		t.Error("expected snapWriteErrors > 0")
	}
}

// TestFECAdapter_MultiplePortSum verifies bucket counters sum across
// every receive-enabled port before the adapter computes loss.
func TestFECAdapter_MultiplePortSum(t *testing.T) {
	a, _, rt := newTestAdapter(t, 20, 3)

	// Each port contributes 1/3 of the total load. Sum: 210 pushed,
	// 30 gap2to5 runs → missing=90, loss≈90/300=0.30.
	for range 20 {
		addToPort(t, rt, 0, 70, [6]int64{0, 10, 0, 0, 0, 0})
		addToPort(t, rt, 1, 70, [6]int64{0, 10, 0, 0, 0, 0})
		addToPort(t, rt, 2, 70, [6]int64{0, 10, 0, 0, 0, 0})
		a.tick()
	}

	if a.currentLevel != 40 {
		t.Errorf("currentLevel = %d, want 40 (summed 30%% loss across 3 ports)", a.currentLevel)
	}
}

// TestFECAdapter_DisabledPortIgnored verifies that a ReceiveEnabled=false
// port is entirely skipped by the adapter even if its counters are nonzero.
func TestFECAdapter_DisabledPortIgnored(t *testing.T) {
	a, _, rt := newTestAdapter(t, 20, 2)

	// Disable port 1 entirely.
	rt.Ports[1].ReceiveEnabled.Store(false)

	// Feed port 0 with clean traffic.
	// Pre-seed port 1 with a catastrophic "loss" pattern that would upgrade
	// the adapter if it were read.
	seedPort(t, rt, 1, 0, [6]int64{1000, 1000, 1000, 1000, 1000, 1000})

	for range 20 {
		addToPort(t, rt, 0, 100, [6]int64{0, 0, 0, 0, 0, 0})
		a.tick()
	}

	if a.currentLevel != 20 {
		t.Errorf("currentLevel = %d, want 20 (disabled port must be ignored)", a.currentLevel)
	}
}

// TestFECAdapter_BucketMidpointWeighting verifies the over-50 bucket
// weight (75 frames/run) drives the loss estimate, not 1 frame/run.
func TestFECAdapter_BucketMidpointWeighting(t *testing.T) {
	a, _, rt := newTestAdapter(t, 20, 1)

	// 25 pushed + 1 bucket-over-50 run × 75 midpoint = 75 missing,
	// loss = 75/100 = 0.75. Should drive the adapter straight to 40.
	feedLossTicks(t, a, rt, 10, 25, [6]int64{0, 0, 0, 0, 0, 1})

	if a.currentLevel != 40 {
		t.Errorf("currentLevel = %d, want 40 (over-50 bucket must apply 75× weight)", a.currentLevel)
	}
}

// ─── Snapshot test ───────────────────────────────────────────────────────────

// TestFECAdapter_Snapshot_RoundTrip verifies the adapter snapshot copies
// the current state into the destination struct via atomic loads.
func TestFECAdapter_Snapshot_RoundTrip(t *testing.T) {
	a, _, rt := newTestAdapter(t, 20, 1)

	// Drive some state change.
	feedLossTicks(t, a, rt, 10, 70, [6]int64{0, 10, 0, 0, 0, 0})

	var snap FECAdapterSnapshot

	a.Snapshot(&snap)

	if snap.CurrentLevel != a.currentLevel {
		t.Errorf("snap.CurrentLevel = %d, adapter = %d", snap.CurrentLevel, a.currentLevel)
	}

	if snap.Floor != 20 {
		t.Errorf("snap.Floor = %d, want 20", snap.Floor)
	}

	if snap.LossEWMA < 0 || snap.LossEWMA > 1 {
		t.Errorf("snap.LossEWMA = %f, want in [0,1]", snap.LossEWMA)
	}

	if snap.Transitions < 1 {
		t.Errorf("snap.Transitions = %d, want ≥ 1", snap.Transitions)
	}

	if snap.LastChangeUnixNs == 0 {
		t.Error("snap.LastChangeUnixNs = 0, want nonzero after transition")
	}
}

// TestFECAdapter_Snapshot_NilSafe verifies Snapshot handles nil cleanly.
func TestFECAdapter_Snapshot_NilSafe(t *testing.T) {
	var a *FECAdapter

	var dst FECAdapterSnapshot

	a.Snapshot(&dst)
	a.Snapshot(nil)

	if dst.CurrentLevel != 0 {
		t.Errorf("nil Snapshot mutated dst: %+v", dst)
	}
}

// ─── Concurrency tests ───────────────────────────────────────────────────────

// TestFECAdapter_Run_ContextCancelExits verifies Run terminates
// promptly when ctx is canceled.
func TestFECAdapter_Run_ContextCancelExits(t *testing.T) {
	a, _, _ := newTestAdapter(t, 20, 1)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})

	go func() {
		a.Run(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run did not exit within 500ms of cancel")
	}
}

// TestFECAdapter_Snapshot_Concurrent verifies Snapshot is race-safe
// against an active Run loop feeding synthetic loss.
func TestFECAdapter_Snapshot_Concurrent(t *testing.T) {
	a, _, rt := newTestAdapter(t, 20, 1)

	var (
		wg         sync.WaitGroup
		stop       atomic.Bool
		snapCount  atomic.Int64
		tickCount  atomic.Int64
		lastSeqErr atomic.Int64
	)

	// Tick loop: feed loss and call tick() directly (faster than Run).
	wg.Add(1)

	go func() {
		defer wg.Done()

		for !stop.Load() {
			addToPort(t, rt, 0, 100, [6]int64{1, 0, 0, 0, 0, 0})
			a.tick()
			tickCount.Add(1)
		}
	}()

	// Four snapshot-reader goroutines.
	for range 4 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			var snap FECAdapterSnapshot

			for !stop.Load() {
				a.Snapshot(&snap)
				// Coherence check: level must be one of the valid values.
				if snap.CurrentLevel != 20 && snap.CurrentLevel != 30 && snap.CurrentLevel != 40 {
					lastSeqErr.Store(int64(snap.CurrentLevel))
				}

				snapCount.Add(1)
			}
		}()
	}

	time.Sleep(100 * time.Millisecond)
	stop.Store(true)
	wg.Wait()

	if snapCount.Load() == 0 {
		t.Error("no Snapshot calls executed")
	}

	if tickCount.Load() == 0 {
		t.Error("no tick() calls executed")
	}

	if v := lastSeqErr.Load(); v != 0 {
		t.Errorf("observed invalid snapshot level: %d", v)
	}
}

// ─── Interface conformance ───────────────────────────────────────────────────

// Compile-time assertion that fakeAdapterEncoder satisfies codec.AudioEncoder.
var _ codec.AudioEncoder = (*fakeAdapterEncoder)(nil)

// ─── Benchmarks ──────────────────────────────────────────────────────────────

// BenchmarkFECAdapter_Tick measures the per-tick cost with 5 ports
// carrying non-zero but stable counters (the common steady-state case).
// Target: under 1 µs/op and zero allocs/op.
func BenchmarkFECAdapter_Tick(b *testing.B) {
	rt := &CommsRuntime{Ports: make([]*PortChannel, 0, 5)}

	for range 5 {
		pc := &PortChannel{Jitter: rtp.NewJitterBuffer(rtp.PrebufferPackets, rtp.MaxDepth)}
		pc.ReceiveEnabled.Store(true)
		pc.RxPushed.Store(1000)
		pc.Jitter.GapRuns1.Store(2)
		pc.Jitter.GapRuns2to5.Store(1)
		rt.Ports = append(rt.Ports, pc)
	}

	a := NewFECAdapter(rt, &fakeAdapterEncoder{}, 20, zerolog.Nop())

	// Warm: run one tick so prev is populated.
	a.tick()

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		// Bump counters so each iteration has a non-zero delta and we
		// exercise the active-state branch, not the silent-stall branch.
		for _, pc := range rt.Ports {
			pc.RxPushed.Add(100)
		}

		a.tick()
	}
}

// BenchmarkFECAdapter_Snapshot measures the per-call snapshot cost.
// Target: under 100 ns/op and zero allocs/op.
func BenchmarkFECAdapter_Snapshot(b *testing.B) {
	rt := &CommsRuntime{
		Ports: []*PortChannel{{Jitter: rtp.NewJitterBuffer(rtp.PrebufferPackets, rtp.MaxDepth)}},
	}
	rt.Ports[0].ReceiveEnabled.Store(true)

	a := NewFECAdapter(rt, &fakeAdapterEncoder{}, 20, zerolog.Nop())

	var dst FECAdapterSnapshot

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		a.Snapshot(&dst)
	}
}
