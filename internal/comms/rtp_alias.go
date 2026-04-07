package comms

import (
	"github.com/openmanet/openmanetd/internal/comms/rtp"
)

// This file provides parent-package aliases for symbols that moved into
// internal/comms/rtp so that files remaining in the parent (comms.go,
// receive.go, tests) keep compiling without call-site churn beyond the
// `rtp.`-qualified constructor invocations.

// ─── Type aliases ───────────────────────────────────────────────────────────

type (
	pionRTPSession    = rtp.Session
	RTPSession        = rtp.Session      //nolint:revive // parent alias from 2A rename
	RTPJitterBuffer   = rtp.JitterBuffer //nolint:revive // internal ergonomics
	SwappableSender   = rtp.SwappableSender
	SwappableReceiver = rtp.SwappableReceiver
	RTPSender         = rtp.Sender //nolint:revive // internal ergonomics
	PacketWriter      = rtp.PacketWriter
	PacketReader      = rtp.PacketReader
)
