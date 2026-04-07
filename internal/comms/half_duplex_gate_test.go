package comms

import (
	"testing"
	"time"
)

func TestHalfDuplexGate_DefaultZeroValueInactive(t *testing.T) {
	var g HalfDuplexGate
	if g.Active() {
		t.Error("zero-value gate should not be active")
	}

	if g.LastUnixNano() != 0 {
		t.Error("zero-value gate should report zero last")
	}
}

func TestHalfDuplexGate_MarkActivatesWithinDefaultThreshold(t *testing.T) {
	var g HalfDuplexGate
	g.Mark()

	if !g.Active() {
		t.Error("gate should be active immediately after mark")
	}
}

func TestHalfDuplexGate_StaleMarkInactive(t *testing.T) {
	var g HalfDuplexGate
	g.MarkAt(time.Now().Add(-(defaultHalfDuplexThreshold + time.Second)))

	if g.Active() {
		t.Error("gate should be inactive when last mark is stale")
	}
}

func TestHalfDuplexGate_CustomThreshold(t *testing.T) {
	g := HalfDuplexGate{Threshold: 50 * time.Millisecond}
	g.MarkAt(time.Now().Add(-100 * time.Millisecond))

	if g.Active() {
		t.Error("gate with 50ms threshold should be inactive after 100ms")
	}

	g.Mark()
	if !g.Active() {
		t.Error("gate should be active immediately after mark with custom threshold")
	}
}

func TestHalfDuplexGate_Reset(t *testing.T) {
	var g HalfDuplexGate
	g.Mark()
	g.Reset()

	if g.Active() {
		t.Error("gate should be inactive after reset")
	}

	if g.LastUnixNano() != 0 {
		t.Error("reset should clear last")
	}
}
