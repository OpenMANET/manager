package rtp_test

import (
	"testing"

	"github.com/openmanet/openmanetd/internal/comms/rtp"
	"github.com/stretchr/testify/assert"
)

func TestJitterBuffer_Snapshot_NilSafe(t *testing.T) {
	t.Parallel()

	var jb *rtp.JitterBuffer

	var dst rtp.JitterBufferSnapshot

	jb.Snapshot(&dst)
	jb.Snapshot(nil)

	assert.Zero(t, dst)
}

func TestJitterBuffer_Snapshot_ReadsCounters(t *testing.T) {
	t.Parallel()

	jb := rtp.NewJitterBuffer(rtp.PrebufferPackets, rtp.MaxDepth)
	jb.Overflows.Store(3)
	jb.SSRCResets.Store(5)
	jb.IdleResets.Store(7)

	var dst rtp.JitterBufferSnapshot

	jb.Snapshot(&dst)

	assert.Equal(t, int64(3), dst.Overflows)
	assert.Equal(t, int64(5), dst.SSRCResets)
	assert.Equal(t, int64(7), dst.IdleResets)
}

func TestJitterBuffer_Snapshot_ZeroAlloc(t *testing.T) {
	jb := rtp.NewJitterBuffer(rtp.PrebufferPackets, rtp.MaxDepth)
	jb.Overflows.Store(1)

	var dst rtp.JitterBufferSnapshot

	allocs := testing.AllocsPerRun(100, func() {
		jb.Snapshot(&dst)
	})

	assert.Equal(t, 0.0, allocs, "JitterBuffer.Snapshot must not allocate")
}
