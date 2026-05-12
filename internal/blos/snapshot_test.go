package blos_test

import (
	"testing"

	"github.com/openmanet/openmanetd/internal/blos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBLOSManager_Snapshot_NilSafe(t *testing.T) {
	t.Parallel()

	var m *blos.BLOSManager

	var dst blos.BLOSSnapshot

	m.Snapshot(&dst)
	m.Snapshot(nil)
}

func TestBLOSSnapshotter_NilManager(t *testing.T) {
	t.Parallel()

	var a blos.BLOSSnapshotter

	a.Refresh()

	data, ok := a.Data().(*blos.BLOSSnapshot)
	require.True(t, ok)
	assert.False(t, data.Running)
}

func TestBLOSSnapshotter_DataPointerStable(t *testing.T) {
	t.Parallel()

	var a blos.BLOSSnapshotter

	first := a.Data()
	a.Refresh()
	second := a.Data()

	assert.Same(t, first, second)
}
