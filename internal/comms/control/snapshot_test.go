package control_test

import (
	"testing"
	"time"

	"github.com/openmanet/openmanetd/internal/comms/control"
	"github.com/stretchr/testify/assert"
)

func TestHalfDuplexGate_Snapshot_NilSafe(t *testing.T) {
	t.Parallel()

	var g *control.HalfDuplexGate

	var dst control.HalfDuplexGateSnapshot

	g.Snapshot(&dst)
	g.Snapshot(nil)
}

func TestHalfDuplexGate_Snapshot_Unmarked(t *testing.T) {
	t.Parallel()

	var g control.HalfDuplexGate

	var dst control.HalfDuplexGateSnapshot

	g.Snapshot(&dst)

	assert.Zero(t, dst.LastMarkUnixNano)
	assert.False(t, dst.Active)
	assert.Equal(t, control.DefaultHalfDuplexThreshold.Nanoseconds(), dst.ThresholdNs)
}

func TestHalfDuplexGate_Snapshot_ActiveAfterMark(t *testing.T) {
	t.Parallel()

	var g control.HalfDuplexGate

	g.Mark()

	var dst control.HalfDuplexGateSnapshot

	g.Snapshot(&dst)

	assert.NotZero(t, dst.LastMarkUnixNano)
	assert.True(t, dst.Active)
}

func TestHalfDuplexGate_Snapshot_CustomThreshold(t *testing.T) {
	t.Parallel()

	g := control.HalfDuplexGate{Threshold: 250 * time.Millisecond}

	var dst control.HalfDuplexGateSnapshot

	g.Snapshot(&dst)

	assert.Equal(t, int64(250_000_000), dst.ThresholdNs)
}

func TestHalfDuplexGate_Snapshot_ZeroAlloc(t *testing.T) {
	var g control.HalfDuplexGate

	g.Mark()

	var dst control.HalfDuplexGateSnapshot

	allocs := testing.AllocsPerRun(100, func() {
		g.Snapshot(&dst)
	})

	assert.Equal(t, 0.0, allocs, "HalfDuplexGate.Snapshot must not allocate")
}
