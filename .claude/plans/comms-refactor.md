# Comms Package Refactor Plan

## Context

`internal/comms` has grown into a flat package where `comms.go` alone is 1216 lines and concerns are heavily intermingled: lifecycle, config, audio I/O, codec, RTP, multiport transport, control sources, device discovery and global handler accessors all live side by side. A `buildEventSource()` string-switch (`comms.go:865`), three separate Bluetooth device fields, and duplicated stream-reopen logic in `transmit.go:77-111` make adding a new control source or onboarding a new audio device painful. CM108-based OpenVLM is currently discovered twice — once via HID scan in `openvlm.go` and once via `/proc/asound` in `comms.go:1116` — and the float32 audio pipeline is expensive on mipsle softfloat targets. The refactor consolidates these concerns into focused sub-packages, makes control sources pluggable via a registry, unifies CM108 discovery behind a single descriptor, and prepares an int16-native pipeline for resource-constrained targets — all without losing features, test coverage, or low-latency characteristics.

## Target Layout

```
internal/comms/
  comms.go           // thin façade: Service, Start/Stop (~200 lines)
  config.go          // CommsConfig, CommsRuntime
  manager.go         // lifecycle (idempotent Enable/Disable)
  audio/             // capture.go, playout.go, portaudio.go, alsa.go
  codec/             // opus.go (Encoder/Decoder interfaces over libopus)
  rtp/               // session.go, jitter.go, transport.go
  control/           // source.go (registry), openvlm.go, roip.go, nanoptt.go, web.go
  device/            // cm108.go, evdev.go, discovery.go
```

## Key Changes

### 1. Control source registry (extensibility)
Replace the switch in `comms.go:865-903`:
```go
// internal/comms/control/source.go
type Factory func(ControlDeps) (EventSource, error)
var registry = map[string]Factory{}
func Register(name string, f Factory)
```
Each backend self-registers in `init()`. Adding a new source = new file, no central edits. `CommsConfig.Validate()` checks `ControlSource` against `registry` keys. Replaces stringly-typed enum scattered across `device.go:23`, `handlers/comms.go:28`, `comms.go:865`.

### 2. Unified CM108 device descriptor
New `device.CM108Descriptor{HIDPath, ALSACardIdx, PADeviceIdx, VID, PID, Serial}` produced by a single `/sys/bus/usb/devices` walk that resolves hidraw, ALSA card, and PortAudio index together. Both `openvlm.go` and `openBroadcastStreamOn` consume the same descriptor, eliminating the dual scan. Optional udev netlink hotplug watcher allows the OpenVLM source to react to unplug without polling.

### 3. Remove globals → `*Service`
`activeConfig`, `webEvtSrc`, `webBridge` (`comms.go:92,1035,1047`) become fields on `*comms.Service` returned by `Start`. Provide a temporary `Default()` shim so `internal/openmanet/server/handlers/comms.go` can migrate in a follow-up change.

### 4. Pipeline simplification

**TX before:** PA callback → captureCallback → encCh(8) → encodeLoop → opus → sendToAllPorts → pionRTPSession → swappableSender → UDP

**TX after:** PA int16 callback → SPSC ringbuffer (3 frames) → encodeWorker → `Encoder.EncodeS16` → `rtp.Session.Broadcast`.

Pion is **kept**: `pionRTPSession`, the packetizer, the interceptor chain (RTCP SR every 5 s), and the SSRC/sequence/timestamp bookkeeping all remain — they are needed for future functionality (RTCP RR ingest, congestion control, encryption interceptors, additional senders/receivers). What changes is:

- The hot-path mutex on `pionRTPSession.mu` (`rtp.go:~80`) is replaced with finer-grained state: per-port seq/ts counters maintained inside the pion packetizer are accessed via a per-port `Session` so the per-frame `Broadcast` call no longer serializes across all ports through one lock.
- `swappableSender`'s RWLock-per-write (`transport.go:36-40`) is replaced with an `atomic.Pointer[*net.UDPConn]` snapshot so writes are lock-free; endpoint swaps publish a new pointer.
- The pre-allocated `rtpBufSize` pool (`rtp.go:34`) is reused end-to-end so the encoded payload is written into a buffer that pion then packetizes in place via `Packetizer.Packetize` with a writer that targets the same buffer (`net.Buffers{header, payload}` via `WriteMsgUDP`), removing one intermediate copy per frame per port.
- Encode/decode become independent of pion through the new `codec.Encoder`/`codec.Decoder` interfaces, so pion only handles RTP framing — not audio bytes.

Net effect on TX hot path: one shared mutex removed, one copy per frame per port removed, RWLock per write removed. Pion's packetizer, interceptor chain, and RTCP SR emission are unchanged and continue to back the session — preserving the hooks future features will plug into.

**RX before:** UDP → pion parse → jitter → DecodeFloat32 (PLC) → PA float out

**RX after:** UDP into pooled `[]byte` → `jitter.Push` (raw payload retained) → `Decoder.DecodeS16(dst []int16)` → PA int16 out. Eliminates float↔int16 conversions on mipsle.

`beginTransmission`'s duplicated reopen fallback (`transmit.go:77-111`) collapses into `audio.Capture.Ensure()` — idempotent open-or-reopen.

### 5. Codec
- Stay on `github.com/hraban/opus` (libopus via CGO) — works on both arm64 (float/NEON) and mipsle (libopus is built `--enable-fixed-point` on OpenWRT 24.10).
- Switch the hot path to the int16 entry points (`opus_encode` / `opus_decode`) so mipsle avoids softfloat conversions and arm64 still benefits from one fewer copy.
- `layeh.com/gopus` and `pion/opus` evaluated and rejected (abandoned / encode-immature).

Encoder/Decoder interfaces:
```go
type Encoder interface { EncodeS16(pcm []int16, out []byte) (int, error); Close() error }
type Decoder interface { DecodeS16(payload []byte, dst []int16) (int, error); Close() error }
```

### 6. Half-duplex gate
Extract `lastRemoteRx` atomic + 400ms threshold (`receive.go:38-44`) into a `halfDuplexGate` type shared by TX and RX, with the threshold exposed as a config knob.

## Critical Files

- `internal/comms/comms.go` (1216 lines — split + slim down)
- `internal/comms/transmit.go` → `audio/capture.go`
- `internal/comms/receive.go` → `audio/playout.go`
- `internal/comms/broadcast_encoder.go` → `audio/capture.go`
- `internal/comms/openvlm.go` → `control/openvlm.go` (HID-only; device discovery moves out)
- `internal/comms/device.go` → `device/evdev.go` + `device/discovery.go`
- `internal/comms/codec.go` → `codec/opus*.go`
- `internal/comms/rtp.go`, `transport.go`, `jitter.go` → `rtp/`
- `internal/openmanet/server/handlers/comms.go` (consume `*Service` instead of globals)

## Migration Phases (each phase green on `make test-race`)

1. **Mechanical extract** — `git mv` files into sub-packages, fix imports/package decls. Zero behavior change. `mocks_test.go` splits per package.
2. **Registry** alongside switch; switch delegates. `comms_test.go` unchanged.
3. **CM108 descriptor** behind old call sites. New `device/cm108_test.go` uses an `fs.FS` fake for `/sys`.
4. **Globals → `*Service`** with `Default()` shim; tests construct `newTestService(t)`.
5. **int16 codec interface** swap; update `codec_test.go`, `broadcast_encoder_test.go`, `receive_test.go` in lockstep.
6. **rtp.Session hot-path tightening**: replace `swappableSender` RWLock with atomic pointer; reuse the pooled buffer end-to-end. Pion packetizer, interceptors, and RTCP SR remain. `transport_test.go`/`rtp_test.go` updated for atomic-snapshot semantics; `rtp_fuzz_test.go` unchanged.

Tests preserved as-is: `jitter_test.go`, `rtp_fuzz_test.go`, `event_test.go`, `codec_test.go` (body), `roip_test.go`, `nanoptt` behavior tests, `integration_test.go` (re-wired through `Service`).

## Verification

1. `make test`, `make test-race`, `make integration-test` after each phase.
2. Cross-compile: `GOOS=linux GOARCH=arm64`, `GOARCH=mipsle GOMIPS=softfloat`, with/without CGO.
3. `go test -bench=. ./internal/comms/...` — encode/decode/jitter benches pre vs post; ensure no regression.
4. `make lint-go` (`go vet`, `staticcheck`, `golangci-lint`).
5. On-device smoke on arm64 router, then mipsle: TX/RX loopback over 802.11s mesh between two nodes; measure glass-to-glass latency via RTP timestamps; confirm ≤ baseline.
6. PESQ A/B on recorded samples if pion/opus decode is enabled on any target.

## Risks & Mitigations

| Risk | Mitigation |
|---|---|
| pion/opus decode quality differs from libopus | A/B PESQ; gate behind build tag; libopus stays default |
| Removing globals breaks external handler call sites | Temporary `Default()` shim; grep before deletion |
| Hot-path tightening accidentally drops pion features | Pion packetizer + interceptor chain + RTCP SR are explicitly preserved; only locking/copies around them change. Existing `rtp_test.go` SR assertions are kept and extended. |
| int16 conversion shifts ROIP VOX threshold semantics | Convert threshold at config load; regression test in `roip_test.go` |
| CM108 descriptor misidentifies non-OpenVLM CM108 audio | Require VID+PID+iSerial triple match; fallback to legacy scan with warn |
| Package split loses git blame history | One `git mv` per file per commit |
| Registry `init()` order vs. config validation | `CommsConfig.Validate()` checks against `registry` keys after all imports |
| PortAudio Go-side conversions still hit softfloat on mipsle | Open PA stream with `paInt16` directly, bypass conversion |
