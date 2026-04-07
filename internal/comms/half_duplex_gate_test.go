package comms

import (
	"testing"
	"time"
)

func TestHalfDuplexGate_DefaultZeroValueInactive(t *testing.T) {
	var g halfDuplexGate
	if g.active() {
		t.Error("zero-value gate should not be active")
	}

	if g.lastUnixNano() != 0 {
		t.Error("zero-value gate should report zero last")
	}
}

func TestHalfDuplexGate_MarkActivatesWithinDefaultThreshold(t *testing.T) {
	var g halfDuplexGate
	g.mark()

	if !g.active() {
		t.Error("gate should be active immediately after mark")
	}
}

func TestHalfDuplexGate_StaleMarkInactive(t *testing.T) {
	var g halfDuplexGate
	g.markAt(time.Now().Add(-(defaultHalfDuplexThreshold + time.Second)))

	if g.active() {
		t.Error("gate should be inactive when last mark is stale")
	}
}

func TestHalfDuplexGate_CustomThreshold(t *testing.T) {
	g := halfDuplexGate{threshold: 50 * time.Millisecond}
	g.markAt(time.Now().Add(-100 * time.Millisecond))

	if g.active() {
		t.Error("gate with 50ms threshold should be inactive after 100ms")
	}

	g.mark()

	if !g.active() {
		t.Error("gate should be active immediately after mark with custom threshold")
	}
}

func TestHalfDuplexGate_Reset(t *testing.T) {
	var g halfDuplexGate
	g.mark()
	g.reset()

	if g.active() {
		t.Error("gate should be inactive after reset")
	}

	if g.lastUnixNano() != 0 {
		t.Error("reset should clear last")
	}
}
