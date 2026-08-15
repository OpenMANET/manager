package handlers_test

import (
	"fmt"
	"testing"

	"buf.build/go/protovalidate"
	commsv1 "github.com/openmanet/openmanetd/internal/api/openmanet/comms/v1"
	serviceproto "github.com/openmanet/openmanetd/internal/api/openmanet/service/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newValidator creates a protovalidate.Validator and fails the test on error.
func newValidator(t *testing.T) protovalidate.Validator {
	t.Helper()

	v, err := protovalidate.New()
	require.NoError(t, err, "protovalidate.New()")

	return v
}

// ── GetNodeRequest ────────────────────────────────────────────────────────────

func TestValidation_GetNodeRequest_EmptyHostname(t *testing.T) {
	v := newValidator(t)

	err := v.Validate(&serviceproto.GetNodeRequest{Hostname: ""})
	assert.Error(t, err, "empty hostname must fail validation")
}

func TestValidation_GetNodeRequest_ValidHostname(t *testing.T) {
	v := newValidator(t)

	err := v.Validate(&serviceproto.GetNodeRequest{Hostname: "node-1"})
	assert.NoError(t, err)
}

// ── GetWirelessInterfaceRequest ───────────────────────────────────────────────

func TestValidation_GetWirelessInterfaceRequest_EmptyName(t *testing.T) {
	v := newValidator(t)

	err := v.Validate(&serviceproto.GetWirelessInterfaceRequest{Name: ""})
	assert.Error(t, err, "empty name must fail validation")
}

func TestValidation_GetWirelessInterfaceRequest_ValidName(t *testing.T) {
	v := newValidator(t)

	err := v.Validate(&serviceproto.GetWirelessInterfaceRequest{Name: "wlh0"})
	assert.NoError(t, err)
}

// ── SetSendTalkGroupRequest ───────────────────────────────────────────────────

func TestValidation_SetSendTalkGroupRequest_OutOfRange(t *testing.T) {
	v := newValidator(t)

	for _, tg := range []int32{0, -1, 33, 100} {
		tg := tg
		t.Run(fmt.Sprintf("talkgroup_%d", tg), func(t *testing.T) {
			err := v.Validate(&commsv1.SetSendTalkGroupRequest{Talkgroup: tg})
			assert.Error(t, err, "talkgroup %d is out of range [1,32] and must fail validation", tg)
		})
	}
}

func TestValidation_SetSendTalkGroupRequest_ValidTalkgroups(t *testing.T) {
	v := newValidator(t)

	for _, tg := range []int32{1, 16, 32} {
		tg := tg
		t.Run(fmt.Sprintf("talkgroup_%d", tg), func(t *testing.T) {
			err := v.Validate(&commsv1.SetSendTalkGroupRequest{Talkgroup: tg})
			assert.NoError(t, err, "talkgroup %d must pass validation", tg)
		})
	}
}

// ── SetReceiveTalkGroupRequest ────────────────────────────────────────────────

func TestValidation_SetReceiveTalkGroupRequest_OutOfRange(t *testing.T) {
	v := newValidator(t)

	for _, tg := range []int32{0, -1, 33, 100} {
		tg := tg
		t.Run(fmt.Sprintf("talkgroup_%d", tg), func(t *testing.T) {
			err := v.Validate(&commsv1.SetReceiveTalkGroupRequest{Talkgroup: tg})
			assert.Error(t, err, "talkgroup %d is out of range [1,32] and must fail validation", tg)
		})
	}
}

func TestValidation_SetReceiveTalkGroupRequest_ValidTalkgroups(t *testing.T) {
	v := newValidator(t)

	for _, tg := range []int32{1, 16, 32} {
		tg := tg
		t.Run(fmt.Sprintf("talkgroup_%d", tg), func(t *testing.T) {
			err := v.Validate(&commsv1.SetReceiveTalkGroupRequest{Talkgroup: tg})
			assert.NoError(t, err, "talkgroup %d must pass validation", tg)
		})
	}
}

// ── UpdateAudioMixerRequest ───────────────────────────────────────────────────

func TestUpdateAudioMixerRequest_Validation(t *testing.T) {
	validator := newValidator(t)

	mk := func(v int32) *commsv1.UpdateAudioMixerRequest {
		return &commsv1.UpdateAudioMixerRequest{SpeakerVolume: &v}
	}

	tests := []struct {
		name    string
		msg     *commsv1.UpdateAudioMixerRequest
		wantErr bool
	}{
		{name: "empty request valid", msg: &commsv1.UpdateAudioMixerRequest{}},
		{name: "zero valid", msg: mk(0)},
		{name: "hundred valid", msg: mk(100)},
		{name: "negative invalid", msg: mk(-1), wantErr: true},
		{name: "over hundred invalid", msg: mk(101), wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := validator.Validate(tc.msg)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
