package audio

import (
	"github.com/openmanet/openmanetd/internal/comms/device"
)

// PortSlot is the per-port surface that audio.Init.StartHardware needs in
// order to open a malgo playback stream for one multicast endpoint.
// The parent comms package builds one PortSlot per receive-capable port,
// baking the *PortChannel reference into closures so the audio package
// never imports the parent.
//
// Field semantics:
//   - HasReceiver: true when the port has an open receive socket and a
//     playback stream should be opened. False ports are skipped entirely.
//   - Port: the UDP port number, used in log lines.
//   - BeepBuf: the one-shot beep channel that the playback callback drains
//     ahead of falling through to PlayoutFrame. The parent allocates and
//     owns this channel; the playback callback only reads from it.
//   - SetStream: the parent's setter that stores the freshly opened
//     device.AudioStream back into its *PortChannel.
//   - PlayoutFrame: the per-frame playback callback. The parent's closure
//     captures (cfg, rt, pc, pc.Jitter) and exposes only the int16 output
//     buffer to the audio package.
type PortSlot struct {
	BeepBuf      chan []int16
	SetStream    func(s device.AudioStream)
	PlayoutFrame func(out []int16)
	Port         int
	HasReceiver  bool
}
