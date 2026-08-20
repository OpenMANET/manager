package talkgroup_test

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openmanet/openmanetd/internal/comms/talkgroup"
)

func TestRegistry_AddNotifyRemove(t *testing.T) {
	r := talkgroup.NewRegistry(zerolog.Nop())

	var got []talkgroup.Event

	id := r.Add(func(ev talkgroup.Event) { got = append(got, ev) })
	require.NotZero(t, id)

	ev := talkgroup.Event{
		Kind: talkgroup.KindSelected, Channel: 3, Prev: 1,
		Send: true, Receive: true, Source: talkgroup.SourceRPC, At: time.Now(),
	}
	r.Notify(ev)
	require.Len(t, got, 1)
	assert.Equal(t, ev, got[0])

	r.Remove(id)
	r.Notify(ev)
	assert.Len(t, got, 1)
}

func TestRegistry_ListenerPanicIsolated(t *testing.T) {
	r := talkgroup.NewRegistry(zerolog.Nop())

	r.Add(func(talkgroup.Event) { panic("boom") })

	var called bool

	r.Add(func(talkgroup.Event) { called = true })

	assert.NotPanics(t, func() { r.Notify(talkgroup.Event{Kind: talkgroup.KindSelected}) })
	assert.True(t, called, "panicking listener must not block later listeners")
}

func TestRegistry_DropAccounting(t *testing.T) {
	r := talkgroup.NewRegistry(zerolog.Nop())
	assert.Zero(t, r.Dropped())
	r.NoteDropped()
	r.NoteDropped()
	assert.Equal(t, uint64(2), r.Dropped())
}

func TestRegistry_NilReceiverSafe(t *testing.T) {
	var r *talkgroup.Registry

	assert.NotPanics(t, func() { r.Notify(talkgroup.Event{}) })
	assert.Zero(t, r.Dropped())
}
