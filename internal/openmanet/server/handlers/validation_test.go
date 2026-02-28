package handlers_test

import (
	"testing"

	"buf.build/go/protovalidate"
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

	err := v.Validate(&serviceproto.GetWirelessInterfaceRequest{Name: "wlan0"})
	assert.NoError(t, err)
}

// ── JoinTalkGroupRequest ──────────────────────────────────────────────────────

func TestValidation_JoinTalkGroupRequest_NonMulticastAddress(t *testing.T) {
	v := newValidator(t)

	cases := []struct {
		name    string
		address string
	}{
		{"unicast", "10.0.0.1"},
		{"broadcast", "255.255.255.255"},
		{"ipv6 multicast", "ff02::1"},
		{"empty", ""},
		{"non-ip string", "not-an-ip"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := v.Validate(&serviceproto.JoinTalkGroupRequest{Address: tc.address})
			assert.Error(t, err, "address %q must fail validation", tc.address)
		})
	}
}

func TestValidation_JoinTalkGroupRequest_ValidMulticastAddresses(t *testing.T) {
	v := newValidator(t)

	cases := []string{
		"224.0.0.1",
		"224.5.23.1",
		"239.255.255.255",
		"239.1.2.3",
	}

	for _, addr := range cases {
		t.Run(addr, func(t *testing.T) {
			err := v.Validate(&serviceproto.JoinTalkGroupRequest{Address: addr})
			assert.NoError(t, err, "address %q must pass validation", addr)
		})
	}
}

// ── TalkGroup ─────────────────────────────────────────────────────────────────

func TestValidation_TalkGroup_InvalidAddress(t *testing.T) {
	v := newValidator(t)

	err := v.Validate(&serviceproto.TalkGroup{Address: "192.168.1.1", Port: 5007})
	assert.Error(t, err, "unicast address in TalkGroup must fail validation")
}

func TestValidation_TalkGroup_ValidMulticastAddress(t *testing.T) {
	v := newValidator(t)

	err := v.Validate(&serviceproto.TalkGroup{Address: "239.0.0.1", Port: 5007})
	assert.NoError(t, err)
}
