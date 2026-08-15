package alsa_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openmanet/openmanetd/internal/comms/control/alsa"
)

// fullCardMixer builds a fakeMixer resembling a CM108B: Master playback
// (0..38), Mic Capture Volume (0..7), boolean Auto Gain Control.
func fullCardMixer() (*fakeMixer, *fakeCtl, *fakeCtl, *fakeCtl) {
	speaker := newFakeCtl([]int{19, 19}, 0, 38)
	mic := newFakeCtl([]int{4}, 0, 7)
	agc := newFakeCtl([]int{1}, 0, 1)
	agc.isBool = true

	mx := &fakeMixer{ctls: map[string]alsa.Ctl{
		"Master":             speaker,
		"Mic Capture Volume": mic,
		"Auto Gain Control":  agc,
	}}

	return mx, speaker, mic, agc
}

func TestVolume_State_FullCard(t *testing.T) {
	withCard(t, "0")

	mx, _, _, _ := fullCardMixer()
	op := &fakeOpener{mixer: mx}
	v := &alsa.Volume{Log: zerolog.Nop(), Open: op.opener()}

	st, err := v.State(context.Background())
	require.NoError(t, err)

	assert.True(t, st.Available)
	assert.Equal(t, 50, st.SpeakerPct, "raw 19 of 0..38 is 50%")
	assert.Equal(t, "Master", st.SpeakerControl)
	assert.Equal(t, 57, st.MicPct, "raw 4 of 0..7 is 57%")
	assert.Equal(t, "Mic Capture Volume", st.MicControl)
	assert.True(t, st.AGCPresent)
	assert.True(t, st.AGCEnabled)
	assert.Equal(t, "Auto Gain Control", st.AGCControl)
}

func TestVolume_State_MissingControlsReportAbsent(t *testing.T) {
	withCard(t, "0")

	speaker := newFakeCtl([]int{19}, 0, 38)
	mx := &fakeMixer{ctls: map[string]alsa.Ctl{"Master": speaker}}
	op := &fakeOpener{mixer: mx}
	v := &alsa.Volume{Log: zerolog.Nop(), Open: op.opener()}

	st, err := v.State(context.Background())
	require.NoError(t, err)

	assert.True(t, st.Available)
	assert.Equal(t, 50, st.SpeakerPct)
	assert.Equal(t, -1, st.MicPct, "absent capture control reads -1")
	assert.Empty(t, st.MicControl)
	assert.False(t, st.AGCPresent)
}

func TestVolume_State_NoCard_DetectSeamRuns(t *testing.T) {
	t.Cleanup(func() { _ = os.Unsetenv("ALSA_CARD") })
	require.NoError(t, os.Unsetenv("ALSA_CARD"))

	detectCalls := 0
	op := &fakeOpener{mixer: &fakeMixer{}}
	v := &alsa.Volume{
		Log:        zerolog.Nop(),
		Open:       op.opener(),
		DetectCard: func(zerolog.Logger) { detectCalls++ },
	}

	_, err := v.State(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, alsa.ErrNoCard)
	assert.Equal(t, 1, detectCalls, "detection must be retried once when ALSA_CARD is unset")
	assert.Equal(t, 0, op.calls(), "mixer must not open without a card")
}

func TestVolume_State_DetectSeamRecovers(t *testing.T) {
	t.Cleanup(func() { _ = os.Unsetenv("ALSA_CARD") })
	require.NoError(t, os.Unsetenv("ALSA_CARD"))

	mx, _, _, _ := fullCardMixer()
	op := &fakeOpener{mixer: mx}
	v := &alsa.Volume{
		Log:        zerolog.Nop(),
		Open:       op.opener(),
		DetectCard: func(zerolog.Logger) { _ = os.Setenv("ALSA_CARD", "2") },
	}

	st, err := v.State(context.Background())
	require.NoError(t, err)
	assert.True(t, st.Available)
	assert.Equal(t, uint(2), op.openCard, "opener must use the detected card")
}

func TestVolume_Apply_SpeakerPercentMapsIntoRange(t *testing.T) {
	withCard(t, "0")

	// Asymmetric range like the CM108B's -37..0 dB Master.
	speaker := newFakeCtl([]int{-20, -20}, -37, 0)
	mx := &fakeMixer{ctls: map[string]alsa.Ctl{"Master": speaker}}
	op := &fakeOpener{mixer: mx}
	v := &alsa.Volume{Log: zerolog.Nop(), Open: op.opener()}

	pct := 80
	st, err := v.Apply(context.Background(), alsa.Update{SpeakerPct: &pct})
	require.NoError(t, err)

	assert.Equal(t, []int{-8, -8}, speaker.snapshotValues(),
		"-37 + 37*80/100 = -8, written to every channel")
	assert.Equal(t, 78, st.SpeakerPct, "read-back of raw -8 over -37..0 is 78%")
}

func TestVolume_Apply_Boundaries(t *testing.T) {
	tests := []struct {
		name string
		pct  int
		want int
	}{
		{name: "zero maps to range min", pct: 0, want: 0},
		{name: "hundred maps to range max", pct: 100, want: 38},
		{name: "negative clamps to min", pct: -5, want: 0},
		{name: "over 100 clamps to max", pct: 150, want: 38},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			withCard(t, "0")

			speaker := newFakeCtl([]int{10}, 0, 38)
			mx := &fakeMixer{ctls: map[string]alsa.Ctl{"Master": speaker}}
			op := &fakeOpener{mixer: mx}
			v := &alsa.Volume{Log: zerolog.Nop(), Open: op.opener()}

			pct := tc.pct
			_, err := v.Apply(context.Background(), alsa.Update{SpeakerPct: &pct})
			require.NoError(t, err)
			assert.Equal(t, []int{tc.want}, speaker.snapshotValues())
		})
	}
}

func TestVolume_Apply_AGCToggle(t *testing.T) {
	withCard(t, "0")

	mx, _, _, agc := fullCardMixer()
	op := &fakeOpener{mixer: mx}
	v := &alsa.Volume{Log: zerolog.Nop(), Open: op.opener()}

	off := false
	st, err := v.Apply(context.Background(), alsa.Update{AGC: &off})
	require.NoError(t, err)

	assert.Equal(t, []int{0}, agc.snapshotValues())
	assert.True(t, st.AGCPresent)
	assert.False(t, st.AGCEnabled)
}

func TestVolume_Apply_AGCOnNonBoolControl_Errors(t *testing.T) {
	withCard(t, "0")

	notBool := newFakeCtl([]int{1}, 0, 1) // isBool left false
	mx := &fakeMixer{ctls: map[string]alsa.Ctl{"Auto Gain Control": notBool}}
	op := &fakeOpener{mixer: mx}
	v := &alsa.Volume{Log: zerolog.Nop(), Open: op.opener()}

	on := true
	_, err := v.Apply(context.Background(), alsa.Update{AGC: &on})
	require.Error(t, err)
	assert.ErrorIs(t, err, alsa.ErrControlNotFound)
}

func TestVolume_Apply_MissingControl_Errors(t *testing.T) {
	withCard(t, "0")

	mx := &fakeMixer{ctls: map[string]alsa.Ctl{}}
	op := &fakeOpener{mixer: mx}
	v := &alsa.Volume{Log: zerolog.Nop(), Open: op.opener()}

	pct := 50
	_, err := v.Apply(context.Background(), alsa.Update{MicPct: &pct})
	require.Error(t, err)
	assert.ErrorIs(t, err, alsa.ErrControlNotFound)
}

func TestVolume_Apply_EmptyUpdateIsReadOnly(t *testing.T) {
	withCard(t, "0")

	mx, speaker, _, _ := fullCardMixer()
	op := &fakeOpener{mixer: mx}
	v := &alsa.Volume{Log: zerolog.Nop(), Open: op.opener()}

	st, err := v.Apply(context.Background(), alsa.Update{})
	require.NoError(t, err)
	assert.Equal(t, 0, speaker.setCallCount(), "no writes for an empty update")
	assert.Equal(t, 50, st.SpeakerPct)
}

func TestVolume_NameOverride_PinsControl(t *testing.T) {
	withCard(t, "0")

	custom := newFakeCtl([]int{5}, 0, 10)
	mx := &fakeMixer{ctls: map[string]alsa.Ctl{
		"Master":     newFakeCtl([]int{9}, 0, 10),
		"My Speaker": custom,
	}}
	op := &fakeOpener{mixer: mx}
	v := &alsa.Volume{
		Log:   zerolog.Nop(),
		Open:  op.opener(),
		Names: alsa.NamesFromOverrides("My Speaker", "", ""),
	}

	pct := 100
	_, err := v.Apply(context.Background(), alsa.Update{SpeakerPct: &pct})
	require.NoError(t, err)
	assert.Equal(t, []int{10}, custom.snapshotValues(), "override must win over Master")
}

func TestVolume_OpenError_Propagates(t *testing.T) {
	withCard(t, "0")

	op := &fakeOpener{openErr: errors.New("device busy")}
	v := &alsa.Volume{Log: zerolog.Nop(), Open: op.opener()}

	_, err := v.State(context.Background())
	require.Error(t, err)
	assert.NotErrorIs(t, err, alsa.ErrNoCard, "I/O failure is not a missing-card condition")
}
