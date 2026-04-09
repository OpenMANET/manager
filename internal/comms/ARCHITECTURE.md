# Comms Package — Developer Architecture Guide

This document explains how the `comms` package and its sub-packages work
internally, how each component relates to the others, and how to extend it.
It is the companion to the higher-level [README](README.md).

The package was reorganized from a flat 40+ file layout into orchestration
code at the top level plus seven leaf sub-packages. References below use the
current names; if you're reading older history, the old single-port,
`pionRTPSession` / `rtpJitterBuffer` / `swappableSender` / `broadcastEncoder`
shape has been retired.

---

## Table of Contents

1. [Package Layout](#1-package-layout)
2. [Component Overview](#2-component-overview)
3. [Key Types](#3-key-types)
4. [Startup Sequence](#4-startup-sequence)
5. [Goroutine Topology](#5-goroutine-topology)
6. [Transmission Path](#6-transmission-path)
7. [Receive Path](#7-receive-path)
8. [RTP Jitter Buffer](#8-rtp-jitter-buffer)
9. [Comm Event System](#9-comm-event-system)
10. [Interface Reference](#10-interface-reference)
11. [Extending the Package](#11-extending-the-package)
12. [Testing Strategy](#12-testing-strategy)

---

## 1. Package Layout

```
internal/comms/
├── doc.go                Package doc + omd_omit_comms build stub
│
├── comms.go              buildCodec, sendToAllPorts, buildEventSource,
│                         package-level constants
├── config.go             CommsConfig, CommsRuntime, NewComms, applyDefaults
├── lifecycle.go          Start, startHardwareAudio (audio.Init wiring)
├── manager.go            CommsManager (Enable / Disable / IsRunning)
├── service.go            Service + Default()/SetDefault() singleton +
│                         Service.WebAudioBridge / WebEventSource /
│                         TalkGroupStates / EnableTalkGroupSend / Receive
├── transmit.go           Run, beginTransmission, endTransmission,
│                         drainPlaybackBuffer, isBroadcasting, pttStartDelay
├── receive.go            receiveLoop, playoutOneFrame, webPlayoutLoop,
│                         halfDuplexDecayLoop, isReceivingRemote
├── network.go            buildNetwork, buildSinglePortChannel,
│                         replaceNetwork, listenRTPReceiver
├── port_channel.go       McastPortConfig, McastPortState, PortChannel,
│                         MarkRemoteRx, closePartial
├── control_register.go   init() registers the four backends with
│                         control.Register; buildControlDeps; Validate
├── device.go             normalizeControlSource, findCommDevice,
│                         logInputDeviceList
│
├── audio/                Hardware-bound capture + per-port playback
│   ├── encoder.go            BroadcastEncoder + Deps + SendFn
│   ├── init.go               Init.BuildAudio / OpenBroadcastStream /
│   │                         ReopenBroadcast / StartHardware
│   └── port_slot.go          PortSlot (per-port playback wiring)
│
├── audiopool/            Buffer pools + audio constants (leaf)
│   └── audiopool.go          FrameSize, SampleRate, Channels, EncBufSize,
│                             Float32Pool, Int16Pool, EncBufPool, RMSEnergy
│
├── codec/                Opus encoder/decoder (leaf)
│   └── opus.go               AudioEncoder, AudioDecoder, NewOpusEncoder,
│                             NewOpusDecoder, EncodeS16, DecodeS16,
│                             DecodeFloat32
│
├── control/              PTT event sources, registry, half-duplex gate
│   ├── event.go              PTTEvent, EventSource interface
│   ├── source.go             ControlDeps, Factory, Register, Lookup, Names
│   ├── half_duplex_gate.go   HalfDuplexGate, DefaultHalfDuplexThreshold
│   ├── openvlm.go            OpenVLMSource + HIDDevice/HIDOpener +
│   │                         DetectAndSetALSACard / FromSys / FromRoot
│   ├── roip.go               ROIPSource (COS + VOX bridge)
│   ├── nanoptt.go            NanoPTTSource (evdev key press → PTTToggle)
│   └── web_event_source.go   WebEventSource (RPC Push)
│
├── device/               Hardware discovery + AudioStream wrapper
│   ├── stream.go             AudioStream interface, NewPortAudioStream
│   ├── alsa_silence.go       CGo: SilenceALSAProbeNoise / Restore
│   ├── cm108.go              CM108 sysfs walk (DiscoverCM108)
│   ├── evdev.go              FindEvdev
│   ├── network.go            IfaceIPv4, JoinMulticastGroup
│   └── portaudio.go          ResolveAudio, LogPortAudioDevices
│
├── rtp/                  RTP/RTCP transport + jitter buffer
│   ├── session.go            Session, Sender, SSRCFromID, ParseIncoming
│   ├── jitter.go             JitterBuffer (ring buffer + SSRC tracking +
│   │                         EnableNotify)
│   └── transport.go          PacketWriter, PacketReader, SwappableSender
│                             (lock-free + SwapAndDeferClose),
│                             SwappableReceiver, SwapCloseGrace
│
└── webaudio/             Browser audio bridge (web control source only)
    └── bridge.go             Bridge, NewBridge, SendFn, InjectTxFrame,
                              PushRxFrame, RxFrames
```

The dependency direction is strict: every sub-package is a leaf or imports
only sibling sub-packages, never the parent `comms` package. The parent
imports all sub-packages and supplies callbacks (`audio.SendFn`,
`webaudio.SendFn`, `roipBackend.SetTap`, …) so each sub-package stays
ignorant of `*PortChannel` and the rest of the parent runtime types.

---

## 2. Component Overview

```
                       ┌──────────────────────────────┐
                       │         CommsManager         │
                       │   Enable() / Disable()       │
                       └──────────────┬───────────────┘
                                      │ owns lifetime
                                      ▼
  ┌─────────────────────────────────────────────────────────────────────┐
  │                            CommsConfig                              │
  │  (immutable: iface, McastPorts, control source, ROIP, latencies…) │
  └──────────────────────────────┬──────────────────────────────────────┘
                                 │ Start() builds runtime
                                 ▼
  ┌─────────────────────────────────────────────────────────────────────┐
  │                           CommsRuntime                              │
  │                                                                     │
  │   Encoder / Decoder (codec)         BroadcastStream                 │
  │                                     (device.AudioStream =           │
  │                                      *audio.BroadcastEncoder        │
  │                                      OR *webaudio.Bridge surrogate) │
  │                                                                     │
  │   Ports []*PortChannel              WebBridge   *webaudio.Bridge    │
  │   ┌─────────────────────────┐       WebEvtSrc  *control.WebEvent…   │
  │   │ PortChannel [0]         │       BroadcastTap (atomic ptr        │
  │   │   Sender  / RTCPSend    │                    chan []float32)    │
  │   │   Receiver              │       Broadcasting   (atomic.Bool)    │
  │   │   RTPSess (rtp.Session) │       RemoteRxActive (atomic.Bool)    │
  │   │   Jitter                │       FECAdapter *FECAdapter          │
  │   │   PlaybackStream        │                                       │
  │   │   PlaybackBuffer (beep) │                                       │
  │   │   RxGate (HalfDuplex)   │                                       │
  │   │   SendEnabled / RecvEn  │                                       │
  │   └─────────────────────────┘                                       │
  │   ┌─────────────────────────┐                                       │
  │   │ PortChannel [1] … [N-1] │  one entry per McastPortConfig        │
  │   └─────────────────────────┘                                       │
  └──────────┬─────────────────────────────────┬────────────────────────┘
             │                                 │
             ▼                                 ▼
       UDP multicast            ┌─────────────────────────────┐
       (per port:               │           Service           │
        RTP+RTCP)                │  Cfg + Rt; published via    │
                                │  SetDefault → Default()     │
                                │  used by HTTP handlers      │
                                └─────────────────────────────┘
```

`CommsConfig` owns the static configuration. `CommsRuntime` owns the live
resources, all behind interfaces so unit tests can inject fakes. The shared
`BroadcastEncoder` captures mic audio once and `sendToAllPorts` fans the
encoded frame out to every send-enabled `PortChannel`. Each `PortChannel`
has its own RTP receive socket, jitter buffer, PortAudio playback stream,
and `HalfDuplexGate` so multiple talk groups operate independently.

`Service` is the public handle handlers reach for. It is published via
`SetDefault` once `Start` finishes building the runtime, and cleared on
shutdown so handlers can defensively call `Default()` before comms is
enabled. `CommsManager` sits one layer above and owns the start/stop
lifetime — `Enable()` calls `Validate` synchronously, then runs `Start`
in a background goroutine; `Disable()` cancels the context and waits.

`FECAdapter` is a damped control loop that observes the per-port
jitter-buffer gap-run histogram every 2 s and adjusts the Opus
encoder's `packetLossPerc` setting in response to observed channel
loss. It runs as a single goroutine spawned from `Run()` alongside
`halfDuplexDecayLoop`, writes the encoder knob via
`codec.AudioEncoder.SetPacketLossPerc`, and exposes its state via
`FECAdapter.Snapshot`. See §3 for the state machine and §7 for how
the receive pipeline feeds it.

---

## 3. Key Types

### CommsConfig

The single struct callers populate. Treat it as immutable after `Start`.

```
CommsConfig
├── Log                       zerolog.Logger
├── Interrupt                 chan os.Signal
├── Enable                    bool
│
├── Iface                     string                e.g. "br-ahwlan"
├── McastPorts                []McastPortConfig     one entry per talk group
├── RtpID                     string                → SSRC seed
│
├── ControlSource             string  "openvlm" | "roip" | "web" | "nanoptt"
│
├── BluetoothInputDevice      string                PortAudio device spec
├── BluetoothOutputDevice     string
├── BluetoothAudioDeviceHint  string                fallback for both above
│
├── EnableNanoPTT             bool
├── NanoPTTDevicePath         string                evdev glob
├── NanoPTTDeviceName         string                evdev device name
├── CommKey                   string                "any" or decimal EV_KEY
│
├── ROIPCOSGPIOMask           byte                  IR1 bit selecting COS
├── ROIPVOXThreshold          float32               RMS energy threshold
├── ROIPVOXHoldTime           time.Duration         silence → PTTUp
├── ROIPMaxTXDuration         time.Duration         max single TX
├── ROIPInputDevice           string
│
├── HalfDuplexThreshold       time.Duration         per-port RxGate window
├── PlaybackLatencyMs         int                   PortAudio playback hint
├── CaptureLatencyMs          int                   PortAudio capture hint
├── EncoderComplexity         int                   1..10; defaults to 10
├── PttStartDelayMs           int                   mic warmup floor; the
│                                                   actual settle is
│                                                   max(this, playback
│                                                   output latency +
│                                                   beep + margin) so
│                                                   the start tone
│                                                   cannot leak via
│                                                   speaker→mic coupling
├── MicGain                   float32               int16 gain w/ clipping
│
└── Debug, Loopback, Trace    bool
```

`NewComms(cfg CommsConfig) *CommsConfig` deep-copies `McastPorts` and
returns a pointer ready for `Start`. `applyDefaults` fills empty fields,
expands `BluetoothAudioDeviceHint` into the input/output specs, and
applies ROIP defaults (COS-primary, VOX fallback) once `ControlSource`
has been normalized.

### CommsRuntime

All exported fields. Lifetime is one `Start()` call.

```
CommsRuntime
├── Encoder           codec.AudioEncoder
├── Decoder           codec.AudioDecoder
│
├── BroadcastStream   device.AudioStream         *audio.BroadcastEncoder
├── ReopenBroadcast   func() error               wraps audio.Init.ReopenBroadcast
│
├── WebBridge         *webaudio.Bridge           non-nil only in web mode
├── WebEvtSrc         *control.WebEventSource    non-nil only in web mode
│
├── BroadcastTap      atomic.Pointer[chan []float32]   ROIP VOX tap
│
├── LocalIP           atomic.Pointer[string]     atomically swappable
│
├── Ports             []*PortChannel             one per McastPortConfig
│
├── BeepBufferStart   []int16                    1000 Hz, frame-sized
├── BeepBufferStop    []int16                    600 Hz, frame-sized
│
├── Broadcasting      atomic.Bool                TX state
└── RemoteRxActive    atomic.Bool                cached half-duplex flag
```

`BroadcastTap` is a lock-free atomic pointer the ROIP control source
publishes when it begins VOX monitoring. `audio.BroadcastEncoder` reads
it on every capture callback and, when set, copies the frame into a
pooled `[]float32` and non-blockingly sends it to the tap channel. Other
control sources never enter that branch.

`BeepBufferStart` / `BeepBufferStop` are int16-native — the playback
PortAudio callback drains them with `copy(out, beep)` directly, no
float32 conversion.

`RemoteRxActive` is a cache populated by `MarkRemoteRx` (called from
every receive loop on a successful RTP parse) and cleared by
`halfDuplexDecayLoop` once every send-enabled port's `RxGate.Active()`
returns false. The PTT TX path reads it via `isReceivingRemote(rt)` in
O(1) without walking ports.

### PortChannel

One per `McastPortConfig` entry. Built by `buildSinglePortChannel`.

```
PortChannel
├── cfg              McastPortConfig            (private)
│
├── Sender           *rtp.SwappableSender       wraps DialUDP → mcast:port
├── RTCPSend         *rtp.SwappableSender       wraps DialUDP → mcast:port+1
├── Receiver         *rtp.SwappableReceiver     wraps ListenUDP(0.0.0.0:port)
├── RTPSess          rtp.Sender                 *rtp.Session in production
│
├── Jitter           *rtp.JitterBuffer
├── PlaybackStream   device.AudioStream         per-port PortAudio output
├── PlaybackBuffer   chan []int16               TX-beep side channel
│
├── RxGate           control.HalfDuplexGate     embedded by value
├── ConsecutivePLC   int                        owned by playback callback
├── PlaybackUnderruns atomic.Int64
│
├── SendEnabled      atomic.Bool                runtime toggle
└── ReceiveEnabled   atomic.Bool                runtime toggle
```

`MarkRemoteRx(rt)` is the canonical "remote packet arrived" call: it
stamps `RxGate` and, when this port is currently send-enabled, primes
`rt.RemoteRxActive` so the next PTT attempt sees a busy channel without
waiting for the next decay tick.

`closePartial()` is the safe rollback path used by both the
`buildSinglePortChannel` defer and the bulk cleanup in `buildNetwork`
when a later port fails. It is nil-receiver-safe and tolerates any
combination of unset fields.

`SendEnabled` / `ReceiveEnabled` start from
`McastPortConfig.InitSendEnabled` / `InitReceiveEnabled` (falling back
to `Send` / `Receive` when those are nil); only port 0 is active on
first startup so `EnableTalkGroupSend` / `EnableTalkGroupReceive` can
activate any port at runtime without restarting goroutines or sockets.

### Service

The public handle the HTTP/RPC layer reaches for.

```go
type Service struct {
    Cfg *CommsConfig
    Rt  *CommsRuntime
}

func Default() *Service          // most-recently-published instance
func SetDefault(svc *Service)    // Start publishes; shutdown clears
```

Methods all tolerate a nil receiver and a nil `Service.Rt`:

- `ActiveMulticastAddr()` / `ActiveMulticastPort()` — first port's address.
- `EnableTalkGroupSend(idx, bool)` / `EnableTalkGroupReceive(idx, bool)` —
  toggle the per-port atomics.
- `TalkGroupStates() ([]McastPortState, error)` — snapshot of every port.
- `WebEventSource() *control.WebEventSource` — non-nil only in web mode.
- `WebAudioBridge() *webaudio.Bridge` — non-nil only in web mode.

> **Public API delta from the refactor**: `Service.WebAudioBridge()`
> returns `*webaudio.Bridge`. The old return type was a parent-package
> `*comms.WebAudioBridge` that no longer exists. The single internal
> handler that calls it uses Go's type inference and compiles unchanged.

### CommsManager

Owns the start/stop lifecycle.

```go
type CommsManager struct { … }

func NewCommsManager(cfg *config.Config, log zerolog.Logger) *CommsManager
func (m *CommsManager) Enable() error  // synchronous Validate; bg Start
func (m *CommsManager) Disable()       // cancels ctx and waits
func (m *CommsManager) IsRunning() bool
```

`Enable()` calls `cfg.Validate()` on the same goroutine so an unknown
`ControlSource` surfaces as an immediate error to the caller; only after
`Validate` passes does it spawn the background `Start` goroutine. The
manager is constructed unconditionally at process startup so the HTTP
handler can call `IsRunning()` even when comms is disabled.

### FECAdapter

Damped control loop over the Opus encoder's `packetLossPerc` knob.
Implemented in `fec_adapter.go`. One instance per `CommsRuntime`,
constructed inside `Start()` after the encoder and ports are built,
stored as `CommsRuntime.FECAdapter`, and run as a single goroutine
spawned from `Run()` alongside `halfDuplexDecayLoop`.

```go
type FECAdapter struct {
    log     zerolog.Logger
    encoder codec.AudioEncoder
    rt      *CommsRuntime
    floor   int              // operator-configured lower bound
    now     func() time.Time // injected for tests

    mu             sync.Mutex
    lossEWMA       float64
    currentLevel   int
    upgradeTicks   int
    downgradeTicks int
    silentTicks    int
    prev           []fecPortState // per-port counter memory

    // Atomic mirrors for lock-free Snapshot reads.
    snapLevel       atomic.Int32
    snapLossEWMA    atomic.Uint64 // math.Float64bits
    snapLastChange  atomic.Int64  // unix nanos
    snapTransitions atomic.Int64
    snapWriteErrors atomic.Int64
}
```

**Inputs** (read every 2 s from every `ReceiveEnabled` port):
`PortChannel.RxPushed` and the six `JitterBuffer.GapRuns{1, 2to5,
6to10, 11to20, 21to50, Over50}` atomic counters. The adapter
computes deltas against its own `prev` slice, estimates missing
frames by weighting each bucket by its midpoint (1, 3, 8, 15, 35,
75), derives a raw loss ratio, and updates an EWMA with α = 0.2.

**Output**: `codec.AudioEncoder.SetPacketLossPerc(level)`. The knob
moves through three hardcoded levels — 20, 30, 40 — clamped at or
above `floor`. The state machine uses asymmetric dwell (fast
upgrades, slow downgrades) and hysteresis bands to prevent
flapping:

```
   level=floor ── loss_ewma > 0.08 for ≥ 4s ──► level=30
       ▲                                          │
       │                 loss_ewma < 0.03 for ≥ 30s│
       │◄─────────────────────────────────────────┘
                                │
                                │ loss_ewma > 0.20 for ≥ 4s
                                ▼
                            level=40
                                │
                                │ loss_ewma < 0.10 for ≥ 30s
                                ▼
                            level=30
```

**Concurrency**: all mutable state is under `a.mu`. The `Snapshot`
path reads four atomic mirrors written at the end of every
`tick()` so snapshot callers never contend on the tick lock.
`Run` terminates on `ctx.Done()` and is safe to cancel from any
goroutine. See `fec_adapter_test.go` for the full state-machine
coverage matrix and the race-safety tests.

**Rationale for sender-side inference**: the adapter uses this
node's own RX loss as a proxy for its TX loss to peers. On
omnidirectional mesh links that assumption holds because link
quality is effectively reciprocal. See the Round 7 plan file in
`.claude/plans/` for the full analysis and the reasons we did not
implement RTCP RR ingestion.

### rtp.Session

Wraps a pion `Packetizer` and an interceptor chain. One session represents
one local SSRC (the node running this software). The RTCP path is one-way
outbound — `report.SenderInterceptor` fires every 5 s and writes Sender
Reports to a dedicated RTCP UDP socket. Inbound RTCP is not processed.

```
rtp.Session
├── log         zerolog.Logger
├── packetizer  pionrtp.Packetizer
├── rtpWriter   interceptor.RTPWriter
├── intercept   interceptor.Interceptor
└── ssrc        uint32
```

Concurrency: `Session.Send` is single-writer per session — each
`PortChannel` owns one session, and the only caller is the
`audio.BroadcastEncoder.encodeLoop` goroutine via `sendToAllPorts`.
Adding a second concurrent `Send` caller without external
synchronization is a bug; the call invariant is enforced by code review,
not a mutex.

---

## 4. Startup Sequence

`Start()` runs sequentially in the caller's goroutine so failures surface
with a clear error and the deferred cleanup is straightforward.

```
Start(ctx)
  │
  ├─ 1. Guard:  !Enable → log and return nil
  │
  ├─ 2. applyDefaults()
  │       fills McastPorts from config.GetMulticastTalkGroups,
  │       expands BluetoothAudioDeviceHint, applies ROIP defaults
  │
  ├─ 3. control.DetectAndSetALSACard(cfg.Log)         (openvlm/roip only)
  │       walks /sys via device.DiscoverCM108 → ALSA_CARD
  │       falls back to /proc/asound/card*/usbid scan
  │
  ├─ 4. Set log level (Trace → TraceLevel; Debug → DebugLevel)
  │       cfg.logInputDeviceList()                    (Debug, non-web)
  │
  ├─ 5. cfg.buildCodec()
  │       codec.NewOpusEncoder(48 kHz, mono, 32 kbps,
  │                            EncoderComplexity, packetLossPerc=20)
  │       codec.NewOpusDecoder(48 kHz, mono)
  │
  ├─ 6. Allocate beep buffers (int16-native)
  │       []int16 of audiopool.FrameSize each
  │       amplitude 0.2 * 32767 ≈ 6553
  │       beepStart: 1000 Hz; beepStop: 600 Hz
  │
  ├─ 7. cfg.buildNetwork()
  │       device.IfaceIPv4(Iface) → localIP, *net.Interface
  │       ssrc = rtp.SSRCFromID(RtpID || localIP)
  │       for each McastPortConfig:
  │         buildSinglePortChannel(mpc, localIP, ifi, ssrc)
  │           ├─ if Send:    DialUDP → Sender, RTCP +1, rtp.NewSession
  │           ├─ if Receive: ListenConfig (SO_REUSEPORT) →
  │           │              SetReadBuffer(1 MiB) → JoinMulticastGroup →
  │           │              NewJitterBuffer
  │           └─ on any error: pc.closePartial(), unwind
  │
  ├─ 8. Assemble CommsRuntime { Encoder, Decoder, Ports,
  │       BeepBufferStart, BeepBufferStop }; rt.LocalIP.Store(&localIP)
  │
  ├─ 8a. rt.FECAdapter = NewFECAdapter(rt, enc, cfg.PacketLossPerc, …)
  │       constructed after ports are populated so the adapter's
  │       prev slice is sized to len(rt.Ports). The constructor
  │       makes an initial SetPacketLossPerc(floor) call to scrub
  │       any stale value from a previous enable cycle.
  │
  ├─ 9. SetDefault(&Service{Cfg: cfg, Rt: rt})
  │       publishes the live instance for HTTP handlers
  │
  ├─10. cfg.buildEventSource(rt)
  │       factory := controlLookup(ControlSource)     (control.Lookup)
  │       deps   := cfg.buildControlDeps(rt)          (per-backend payload)
  │       return factory(deps)                        // *control.X
  │
  ├─11. Branch on ControlSource:
  │       web → rt.WebBridge = webaudio.NewBridge(cfg.Log,
  │                func(p []byte) { cfg.sendToAllPorts(rt, p) })
  │       else → cleanup, _ := cfg.startHardwareAudio(rt)
  │              builds audio.Init{Deps: …}, []audio.PortSlot, calls
  │              audioInit.StartHardware(slots) →
  │                device.SilenceALSAProbeNoise +
  │                portaudio.Initialize +
  │                Init.BuildAudio (per-port playback + broadcast capture) +
  │                stream Start; returns cleanup func
  │              rt.BroadcastStream = broadcast
  │              rt.ReopenBroadcast = audioInit.ReopenBroadcast wrapper
  │              defer cleanup()
  │
  └─12. cfg.Run(ctx, rt, src)   ← blocks until ctx canceled
```

The SetDefault call sits at step 9, before the event source is built, so
RPC handlers that race the start (e.g. an HTTP request hitting in the
window between Enable and Run) can still observe the live `Service` and
either succeed against the now-running ports or fail cleanly via the
nil-Rt branch.

---

## 5. Goroutine Topology

Once `Start` reaches step 12 the following goroutines are live:

```
  main goroutine
  └── Start() → Run()
        │
        ├── for each Receive-capable PortChannel:
        │     go cfg.receiveLoop(ctx, pc, rt)
        │           └── web mode only:
        │                 go cfg.webPlayoutLoop(ctx, pc.Jitter, rt)
        │
        ├── go cfg.halfDuplexDecayLoop(ctx, rt)
        │
        ├── go rt.FECAdapter.Run(ctx)
        │     └── 2 s ticker. Reads every ReceiveEnabled port's
        │         RxPushed + jitter.GapRuns_* atomics, updates an
        │         EWMA loss estimate, applies the state machine,
        │         and writes rt.Encoder.SetPacketLossPerc on a
        │         transition. Exits on ctx.Done.
        │
        └── for ev := range src.Events(ctx):
              PTTDown   → beginTransmission(rt)
              PTTUp     → endTransmission(rt)
              PTTToggle → toggle by current Broadcasting state

  [audio.BroadcastEncoder, spawned by NewBroadcastEncoder]
  └── encodeLoop goroutine — drains encCh, applies gain, EncodeS16,
      deps.Send(buf[:n])  →  cfg.sendToAllPorts → walks rt.Ports

  [rtp.Session-internal, started by interceptor chain]
  └── per session: report.SenderInterceptor RTCP SR timer (5 s)

  [control.*Source-internal, started by Events(ctx)]
  └── one of:
        OpenVLMSource   — blocks on hid.Device.Read
        ROIPSource      — HID poll + VOX state machine
        NanoPTTSource   — blocks on evdev.Device.ReadOne
        WebEventSource  — closes channel on ctx.Done

  [PortAudio-internal, managed by the audio library]
  ├── per port: playback callback  (int16) — drains PlaybackBuffer beep
  │             then calls playoutOneFrame(pc, rt, pc.Jitter, out)
  └── one shared: broadcast capture callback (int16) — non-blocking
                  hand-off into BroadcastEncoder.encCh
```

Context cancellation drives every Go goroutine to return.
`SwappableReceiver.Close()` (called from the parent's deferred cleanup
via `pc.closePartial`) unblocks any in-flight `ReadFromUDP` so the
receive loop can exit. PortAudio streams are stopped/closed by the
`cleanup` returned from `audio.Init.StartHardware`. The pion interceptor
chain is shut down by `rtp.Session.Close` (called from `closePartial`).

---

## 6. Transmission Path

Capture is split across two stages decoupled by a bounded channel inside
`audio.BroadcastEncoder`:

1. **PortAudio mic callback** runs every 20 ms on the audio callback
   thread (not a Go goroutine). It does no cgo work, no Opus encoding,
   no UDP I/O. Its only jobs are: optional VOX tap, copy the captured
   `[]int16` frame into a pooled slice, and non-blockingly enqueue it.
2. **encodeLoop goroutine** drains the channel and runs gain → Opus
   `EncodeS16` → `sendToAllPorts`. This stage has its own scheduling
   slack absorbed by the channel, so encoder spikes / GC pauses / UDP
   backpressure cannot starve the audio thread and cause ADC overruns.

```
PortAudio mic callback (audio thread, every 20 ms while
                        BroadcastStream.Start() is active)
  │
  │  in []int16  (frameSize = audiopool.FrameSize = 960)
  ▼
  optional VOX tap (deps.Tap is non-nil and *Tap is non-nil):
    fp := audiopool.Float32Pool.Get()
    convert int16 → float32 (÷ 32768)
    select { case *tapPtr <- fp: | default: audiopool.ReturnFloat32(fp) }
  │
  ▼
  framesCaptured.Add(1)
  fp := audiopool.Int16Pool.Get(); copy(*fp, in)
  │
  ▼
  select {
    case encCh <- fp:                       ← non-blocking
    default: framesDropped.Add(1)            ← consumer >60 ms behind
             audiopool.Int16Pool.Put(fp)
  }
  return  — audio callback exits.

──── goroutine boundary (encCh, cap = broadcastEncoderChanDepth = 3) ────

encodeLoop goroutine
  │
  ▼
  for fp := range encCh:
    apply MicGain in int16 space:
      scaled = float32(v)*gain
      clamp to [-32768, 32767]
      pcm[i] = int16(scaled)
    │
    ▼
    bufPtr := audiopool.EncBufPool.Get()    (defer Put)
    n, err := deps.Encoder.EncodeS16(pcm, *bufPtr)
      err  → encodeErrors.Add(1); Debug log; drop
      ok   → framesEncoded.Add(1)
    │
    ▼
    deps.Send(buf[:n])  →  cfg.sendToAllPorts(rt, payload)
      for _, pc := range rt.Ports:
        if !pc.SendEnabled.Load() || pc.RTPSess == nil { continue }
        pc.RTPSess.Send(payload)
          │
          ├─ pion Packetizer → adds RTP header (V=2, PT=111, seq++,
          │   ts += FrameSamples=960, SSRC)
          └─ rtpWriter.Write(header, payload, nil) → interceptor chain →
             baseRTPWriter → SwappableSender.Write → UDP socket
```

`BroadcastEncoder.Stop()` logs cumulative per-cycle counters at Debug
level: `captured`, `encoded`, `dropped`, `encode_errors`. See
[README.md](README.md#per-cycle-cycle-stats) for the diagnostic decision
tree those values drive.

### SSRC derivation

```
RtpID set?  ──yes──► rtp.SSRCFromID(RtpID)
     │
     no
     ▼
hostname?   ──yes──► RtpID = hostname (in applyDefaults), then SSRCFromID
     │
     no
     ▼
fallback ──────────► rtp.SSRCFromID(localIP)
```

`rtp.SSRCFromID` is FNV-1a 32-bit.

### beginTransmission state machine

```
PTTDown / (PTTToggle when not broadcasting)
  │
  ▼
[atomic] rt.Broadcasting.Load() == true ?  → log "already broadcasting"; return
  │
  ▼
isReceivingRemote(rt)?  → log "channel busy"; return  ← half-duplex
  │
  ▼
[atomic] rt.Broadcasting.Store(true)
  │
  ▼
rt.WebBridge != nil?  → log "Begin web transmission"; return
  │                     (browser owns audio I/O in web mode)
  ▼
drainPlaybackBuffer(rt)
  │
  ▼
for each pc in rt.Ports with PlaybackBuffer != nil:
  pc.PlaybackBuffer <- rt.BeepBufferStart    ← []int16 beep into output cb
  │
  ▼
sleep cfg.transmitSettleWait(rt)             ← max(pttStartDelay,
                                                rt.PlaybackOutputLatency
                                                + 20 ms beep + 20 ms
                                                margin); holds mic
                                                closed until beep has
                                                cleared the speaker
  │
  ▼
rt.BroadcastStream == nil?
  ├─ yes → rt.ReopenBroadcast() (if non-nil)
  │           ├─ error → Broadcasting=false, return
  │           └─ ok    → continue
  └─ no  → continue
  │
  ▼
rt.BroadcastStream.Start()
  ├─ error → ReopenBroadcast() → Start() again
  │            ├─ still error → Broadcasting=false, return
  │            └─ ok          → continue
  └─ ok   → "Mic stream started"
```

### endTransmission state machine

```
PTTUp / (PTTToggle when broadcasting)
  │
  ▼
[atomic] rt.Broadcasting.Load() == false ?  → log "already idle"; return
  │
  ▼
rt.WebBridge != nil?  → Broadcasting=false; log "End web transmission"; return
  │
  ▼
rt.BroadcastStream.Stop()                    (best-effort)
  │
  ▼
drainPlaybackBuffer(rt)
  │
  ▼
for each pc in rt.Ports with PlaybackBuffer != nil:
  pc.PlaybackBuffer <- rt.BeepBufferStop     ← []int16 stop tone
  │
  ▼
[atomic] rt.Broadcasting.Store(false)
```

---

## 7. Receive Path

Each receive-capable `PortChannel` owns its own `receiveLoop` goroutine.
A `halfDuplexDecayLoop` runs alongside them and clears
`rt.RemoteRxActive` when no port is within its half-duplex window.
Playout is driven by the per-port PortAudio output callback (one Go-side
loop per port is unnecessary because the audio hardware clock drives the
consumer side).

The RX pipeline also feeds a closed-loop feedback path into the TX
encoder: `FECAdapter.Run` wakes every 2 s, reads the per-port
`PortChannel.RxPushed` and `JitterBuffer.GapRuns_*` atomic counters,
and writes the Opus encoder's `packetLossPerc` knob in response to
observed channel loss. See §3 for the state machine and
`docs/instrumentation-snapshot.md` for the `comms.fec_adapter`
snapshot field semantics.

```
cfg.receiveLoop(ctx, pc, rt)            (one per receive-capable port)
  │
  │  buf [1500]byte                     ← per-loop, reused across iterations
  │  cachedLocalIPStr / cachedLocalIP   ← parsed only when LocalIP changes
  │
  ▼
  pc.Jitter == nil ? allocate one (test self-sufficiency)
  │
  ▼
  rt.WebBridge != nil ?
    yes → go cfg.webPlayoutLoop(ctx, pc.Jitter, rt)
  │
  ▼
  for {
      select { case <-ctx.Done(): return; default: }
      n, src, err := pc.Receiver.ReadFromUDP(buf)
      err != nil ?
        ctx.Done       → return
        net.ErrClosed  → log Debug "recv socket swapped"; jitter.Reset()
        otherwise      → log Error; continue
      │
      ▼
      refresh cachedLocalIP if rt.LocalIP changed
      │
      ▼
      loopback drop:
        !cfg.Loopback && (src.IsLoopback || src.IP.Equal(cachedLocalIP))
        → continue
      │
      ▼
      pkt, err := rtp.ParseIncoming(buf[:n])
        err → log Debug "dropping non-RTP datagram"; continue
      │
      ▼
      [trace] log src/seq/ts/ssrc/payload_bytes
      │
      ▼
      !pc.ReceiveEnabled.Load() ? continue   ← runtime mute
      │
      ▼
      pc.MarkRemoteRx(rt)                    ← stamps RxGate, primes
                                                 RemoteRxActive cache
      │
      ▼
      jitter.PushWithSSRC(pkt.SSRC, pkt.SequenceNumber, pkt.Payload,
        func(oldSSRC, newSSRC uint32) {
          log "RTP SSRC changed; jitter buffer reset"
        })
        false → if Overflows mod 50 == 0, log Warn "jitter overflow"
  }
```

### playoutOneFrame (called from per-port PortAudio output callback)

The playback callback runs every audio period (~20 ms) per port. It is
the consumer of decoded PCM and is single-threaded with respect to its
own `pc.ConsecutivePLC` field — the production callback and tests must
not invoke `playoutOneFrame` concurrently for the same port.

```
playoutOneFrame(pc, rt, jitter, out []int16)
  │
  ├─ cfg.isBroadcasting(rt) && pc.SendEnabled.Load() ?
  │     → zeroInt16(out); return    (half-duplex local echo silence)
  │
  ├─ !pc.ReceiveEnabled.Load() ?    → zeroInt16(out); return
  ├─ jitter == nil ?                → zeroInt16(out); return
  │
  ├─ payload, conceal := jitter.PopOrConceal(concealRecentWindow=200ms)
  │
  ├─ payload != nil:
  │     n, err := rt.Decoder.DecodeS16(payload, out)
  │     jitter.ReleasePayload(payload)        ← MUST release back to pool
  │     err → DecodeS16(nil, out) PLC fallback
  │            err again → zeroInt16(out); PlaybackUnderruns++; return
  │     n < len(out) → zero-fill remainder
  │     pc.ConsecutivePLC = 0; return
  │
  ├─ conceal:
  │     pc.ConsecutivePLC++
  │     if pc.ConsecutivePLC <= maxConsecutivePLC=10:
  │         DecodeS16(nil, out) PLC; return
  │     else:
  │         zeroInt16(out); return  (sustained loss → clean silence)
  │
  └─ default: zeroInt16(out)         (idle stream / not started)
```

The PortAudio output callback drains `pc.PlaybackBuffer` (the beep side
channel) ahead of falling through to `playoutOneFrame`, so a TX
start/stop tone preempts exactly one frame of jitter-buffered audio.

### webPlayoutLoop (web mode only)

Web mode skips PortAudio entirely. The browser drives audio I/O via RPC
streams to/from `webaudio.Bridge`.

```
cfg.webPlayoutLoop(ctx, jitter, rt)
  │
  notify := jitter.EnableNotify()             ← edge-triggered wakeup
  ticker := time.NewTicker(100ms)             ← safety poll
  │
  drain := func() {
    for {
      payload, _ := jitter.PopOrConceal(200ms)
      if payload == nil { return }
      cp := copy of payload
      rt.WebBridge.PushRxFrame(cp)
      jitter.ReleasePayload(payload)
    }
  }
  │
  for {
    select {
    case <-ctx.Done(): return
    case <-notify:     drain()                ← steady-state wakeup
    case <-ticker.C:   drain()                ← safety net only
    }
  }
```

### halfDuplexDecayLoop

```
cfg.halfDuplexDecayLoop(ctx, rt)
  │
  ticker := time.NewTicker(100ms)
  │
  for {
    <-ticker.C
    anyActive := false
    for _, pc := range rt.Ports:
      if !pc.SendEnabled.Load() { continue }
      if pc.RxGate.Active() { anyActive = true; break }
    if !anyActive { rt.RemoteRxActive.Store(false) }
  }
```

The loop only ever **clears** the cache. The set side is `MarkRemoteRx`
in `receiveLoop`, called the moment a packet arrives — so the start of
an incoming stream cannot be missed by a TX attempt that races the next
decay tick.

---

## 8. RTP Jitter Buffer

`internal/comms/rtp/jitter.go` provides a sequence-number-ordered buffer
backed by a fixed-size ring of slots. Allocations on the hot path are
zero (payload buffers come from a `sync.Pool`); SSRC tracking handles
talker rotations cleanly.

### Internal state

```
JitterBuffer
├── slots         [MaxDepth=24]jitterSlot     fixed-size ring
├── count         int                         live slot count
├── prebuffer     int                         min frames before playout
├── maxDepth      int                         drop threshold
├── expected      uint16                      next sequence to pop
├── ssrc          uint32                      tracked SSRC; reset on change
├── haveSSRC      bool
├── init, started bool
├── lastPush      time.Time                   for jitterIdleResetThreshold=2s
├── now           func() time.Time            test-injectable clock
├── notifyCh      chan struct{}               EnableNotify() wakeup
├── payloadPool   sync.Pool                   []byte cap MaxOpusPayloadSize=1275
├── Overflows     atomic.Int64                count of full-buffer drops
├── SSRCResets    atomic.Int64
├── IdleResets    atomic.Int64
└── mu            sync.Mutex
```

Slots are indexed by `seq % maxDepth`, eliminating the per-packet map
allocation the original implementation paid. The payload pool is sized
to RFC 6716 §3.2.1 maximum (1275 B) so an Opus frame copy never causes a
heap allocation.

### push semantics

`Push(seq, payload)` is a thin SSRC-less wrapper retained for tests.
Production callers use `PushWithSSRC(ssrc, seq, payload, onChange)`:

```
PushWithSSRC(ssrc, seq, payload, onSSRCChange)
  │
  jb.mu.Lock()
  │
  ├─ haveSSRC && init && ssrc != jb.ssrc:
  │     resetLocked()
  │     SSRCResets.Add(1)
  │     ssrcChanged = true
  │
  ├─ pushLocked(seq, payload):
  │     ├─ idle reset:
  │     │     if init && now-lastPush > 2s:
  │     │       resetLocked(); IdleResets.Add(1)
  │     ├─ !init → expected = seq, init = true
  │     ├─ seqLess(seq, expected) → return false  (stale)
  │     ├─ duplicate slot? → return false
  │     ├─ count >= maxDepth && slot empty → return false
  │     ├─ stale slot at idx → ReleasePayload, count--
  │     ├─ payloadPool.Get → buf := buf[:len(payload)]; copy(buf, payload)
  │     └─ slot.{seq, payload, valid} = …; count++; lastPush = now; → true
  │
  ├─ !ok && count >= maxDepth → Overflows.Add(1)
  ├─ ok                       → ssrc = ssrc; haveSSRC = true
  │
  jb.mu.Unlock()
  │
  ├─ ok && notifyCh != nil → non-blocking signal (coalesced)
  └─ ssrcChanged && onSSRCChange != nil → invoke callback
```

The callback is invoked **after** the lock is released so it cannot
deadlock against `EnableNotify` or any other jitter buffer call.

### Pop / conceal

`PopOrConceal(window)` is the production caller's entry point. It
returns `(payload, false)` for an in-order frame, `(nil, true)` when the
buffer wants the consumer to apply PLC (gap concealment), or `(nil,
false)` for genuine silence (idle stream or below prebuffer). It is a
thin wrapper around `popReadyLocked` + `shouldConcealLocked`.

`PopReady()` — returns the next in-order payload, or `(nil, false,
skippedMissing=true)` when the buffer has filled to half its depth and
is forced to advance past the missing slot.

`AdvancePast()` — discards the slot at `expected` and increments it,
maintaining the invariant of exactly one frame produced per playout
tick when the consumer applied a PLC frame externally.

### Edge-triggered notify

```
ch := jb.EnableNotify()  // returns a chan struct{} with depth 1
```

Each successful push fires a non-blocking signal on `notifyCh`. Coalesced
signals lose no data because the consumer drains every available payload
after each wake. Used by `webPlayoutLoop`; PortAudio-driven consumers do
not call `EnableNotify` and pay nothing on the push hot path beyond the
nil check.

### Sequence-number wrap

```
seqLess(a, b) = int16(a-b) < 0      // "a < b" in modulo-65536 space
```

---

## 9. Comm Event System

### PTTEvent

```go
type PTTEvent uint8

const (
    PTTDown   PTTEvent = iota  // hold-to-talk press   (openvlm GPIO HIGH, ROIP COS, web Push)
    PTTUp                       // hold-to-talk release (openvlm GPIO LOW,  ROIP silence, web Push)
    PTTToggle                   // press-to-toggle      (nanoptt key press)
)
```

### EventSource

```go
type EventSource interface {
    Events(ctx context.Context) <-chan PTTEvent
}
```

Implementations close the channel when `ctx` is canceled, causing `Run`
to exit its `select` loop cleanly.

### Backends

#### `control.OpenVLMSource` (default)

Reads HID input reports from an OpenVLM USB dongle. GPIO3 (IR1 bit 2)
maps to PTT state: HIGH → `PTTDown`, LOW → `PTTUp`. Unchanged from the
pre-refactor design except for the package move and the addition of a
`device.DiscoverCM108` Debug log on open.

```
opener := control.DefaultHIDOpener         (initializes hidapi + opens VID/PID)
src    := control.NewOpenVLMSource(log)
```

#### `control.ROIPSource` (Radio-over-IP bridge)

Same OpenVLM USB dongle, but operates **without** a manual PTT button —
it bridges an analog handheld radio into the multicast comms network.
Detection strategy:

1. **COS** (Carrier-Operated Squelch): the radio squelch output is
   wired to an OpenVLM GPIO pin. The HID report is polled; `cosGPIOMask`
   selects the IR1 bit. PTTDown on the HIGH→LOW squelch edge, PTTUp on
   LOW→HIGH.
2. **VOX fallback**: if the HID device is unavailable or `cosGPIOMask`
   is 0, an audio energy threshold (`audiopool.RMSEnergy`) is applied
   to the OpenVLM input stream. `ROIPVOXOnsetFrames=3` (60 ms) prevents
   false triggers. During active TX the broadcast tap (`rt.BroadcastTap`)
   feeds float32 frames into a channel so silence can be detected and
   `PTTUp` emitted after `voxHoldTime`.

Half-duplex is enforced throughout via the injected `isReceiving` /
`isBroadcasting` callbacks. `maxTXDuration` caps a single transmission.
The constructor takes primitives + callbacks instead of `*CommsConfig`
so the `control` package never imports the parent.

```go
src := control.NewROIPSource(
    log,
    cosMask, voxThresh, voxHold, maxTX, inputDevice,
    isReceiving, isBroadcasting, setTap, clearTap, nil,
)
```

#### `control.NanoPTTSource` (evdev key)

```
nanoPTTSource.Events(ctx)
  │
  for {
    ev := dev.ReadOne()
    if ev.Type != EV_KEY: continue
    match := pttKey == "any" || (parse decimal && ev.Code == kc)
    if !match: continue
    switch ev.Value:
      case 1: log "key press"; ch <- PTTToggle
      case 0: log "key release"  (no event emitted)
  }
```

Press-to-toggle only — there is no hold-to-talk variant. The `Run` loop
inspects current `Broadcasting` state and calls `beginTransmission` /
`endTransmission` accordingly.

#### `control.WebEventSource` (RPC)

```go
type WebEventSource struct {
    ch  chan PTTEvent  // depth 4, buffered
    log zerolog.Logger
}

func NewWebEventSource(log zerolog.Logger) *WebEventSource
func (w *WebEventSource) Events(ctx context.Context) <-chan PTTEvent
func (w *WebEventSource) Push(ev PTTEvent)              // RPC handler entry
```

`Push` is non-blocking: a full channel drops the event with a Warn log.
The constructed `*WebEventSource` is written into `rt.WebEvtSrc` via the
backend `Sink` callback so `Service.WebEventSource()` can return it to
HTTP handlers.

### Registry

`internal/comms/control/source.go` exposes a name → factory map:

```go
type ControlDeps struct {
    Log     zerolog.Logger
    Backend any                  // backend-specific payload
}

type Factory func(ControlDeps) (EventSource, error)

func Register(name string, f Factory)
func Lookup(name string) (Factory, bool)
func Names() []string
```

The map is written only from `init()` functions (Go runtime serializes
init before any user goroutines start), so no mutex is needed.

`internal/comms/control_register.go` is the parent's `init()` that
populates the registry:

```go
control.Register("openvlm",  factory_openvlm)
control.Register("roip",     factory_roip)
control.Register("web",      factory_web)
control.Register("nanoptt",  factory_nanoptt)
```

Each factory casts `ControlDeps.Backend` to its expected concrete type.
The parent's `buildControlDeps(rt)` populates the right payload struct
per `ControlSource`:

| Source | Backend struct | Carries |
|---|---|---|
| `openvlm` | `*openvlmBackend` | nothing (zero-size sentinel) |
| `roip`    | `*roipBackend`    | COS mask, VOX thresh/hold, maxTX, input device, `isReceiving`/`isBroadcasting`/`setTap`/`clearTap` closures bound to `rt` |
| `web`     | `*webBackend`     | `Sink func(*control.WebEventSource)` that writes the constructed instance into `rt.WebEvtSrc` |
| `nanoptt` | `*nanopttBackend` | `Cfg *CommsConfig` so the factory can call `cfg.findCommDevice()` |

`cfg.Validate()` runs `control.Lookup(normalizeControlSource(cfg.ControlSource))`
synchronously from `CommsManager.Enable` so an unknown source name
fails fast at the API caller, not asynchronously inside the background
`Start` goroutine.

### Run dispatch

```
Run(ctx, rt, src)
  │
  for each receive-capable PortChannel: go receiveLoop(ctx, pc, rt)
  go halfDuplexDecayLoop(ctx, rt)
  events := src.Events(ctx)
  │
  for {
    select {
    case <-ctx.Done():        return
    case ev, ok := <-events:
      !ok                  → return
      PTTDown              → beginTransmission(rt)
      PTTUp                → endTransmission(rt)
      PTTToggle            → cfg.isBroadcasting(rt) ? endTransmission : beginTransmission
    }
  }
```

---

## 10. Interface Reference

Every hardware-touching operation is hidden behind one of these
interfaces. Each sub-package owns its own `mocks_test.go` (or similar)
with hand-written fakes — there is no shared mock package per the
codebase's "hand-written fakes only" rule.

### `codec.AudioEncoder` / `codec.AudioDecoder` (codec/opus.go)

```go
type AudioEncoder interface {
    EncodeS16(pcm []int16, out []byte) (int, error)
    Encode(pcm []int16, data []byte) (int, error)        // deprecated alias
    SetPacketLossPerc(perc int) error
    Close() error
}

type AudioDecoder interface {
    DecodeS16(data []byte, dst []int16) (int, error)
    Decode(data []byte, pcm []int16) (int, error)        // deprecated alias
    DecodeFloat32(data []byte, pcm []float32) (int, error)
    Close() error
}
```

- Production: `*opusEncoder` / `*opusDecoder` wrap `*opus.Encoder` /
  `*opus.Decoder` from `github.com/hraban/opus`.
- `DecodeS16(nil, dst)` triggers Opus Packet Loss Concealment.
- Hot path is int16-native; the float32 method is retained only for
  consumer-boundary callers (e.g. legacy web bridge code). New code
  should call `EncodeS16` / `DecodeS16`.
- `SetPacketLossPerc` is called at runtime by `FECAdapter` to move
  the encoder's LBRR bitrate allocation in response to observed
  channel loss. The `opusEncoder` implementation guards both
  `EncodeS16` and `SetPacketLossPerc` under a single mutex because
  the hraban/opus binding is not documented thread-safe between
  `Encode` and `opus_encoder_ctl`. Contention is effectively zero
  in production (one 50 Hz encode goroutine vs one ≤ 0.5 Hz
  adapter caller) and benchstat confirms no measurable impact on
  `EncodeS16` throughput.

### `device.AudioStream` (device/stream.go)

```go
type AudioStream interface {
    Start() error
    Stop() error
    Close() error
}

func NewPortAudioStream(s *portaudio.Stream) AudioStream
```

- Production: unexported `*portaudioStream` returned by `NewPortAudioStream`.
- `*audio.BroadcastEncoder` also satisfies `device.AudioStream` via a
  compile-time `var _ device.AudioStream = (*BroadcastEncoder)(nil)`.

### `rtp.Sender` / `rtp.PacketWriter` / `rtp.PacketReader` (rtp/transport.go + rtp/session.go)

```go
type Sender interface {
    Send(payload []byte) error
}

type PacketWriter interface {
    Write(b []byte) (int, error)
}

type PacketReader interface {
    ReadFromUDP(b []byte) (int, *net.UDPAddr, error)
    Close() error
}
```

- Production `Sender`: `*rtp.Session` (one per `PortChannel`).
- Production `PacketWriter`: `*net.UDPConn` wrapped in
  `*rtp.SwappableSender`.
- Production `PacketReader`: `*net.UDPConn` wrapped in
  `*rtp.SwappableReceiver`.

### `rtp.SwappableSender` / `rtp.SwappableReceiver` (rtp/transport.go)

The atomic-swap wrappers. Both support runtime endpoint changes without
blocking the hot I/O path:

```go
// SwappableSender — fully lock-free Write
func (s *SwappableSender) Write(b []byte) (int, error)
func (s *SwappableSender) Swap(newW PacketWriter) PacketWriter
func (s *SwappableSender) SwapAndDeferClose(newW PacketWriter)
func (s *SwappableSender) Close() error

// SwappableReceiver — sync.RWMutex snapshot
func (r *SwappableReceiver) ReadFromUDP(b []byte) (int, *net.UDPAddr, error)
func (r *SwappableReceiver) Close() error
func (r *SwappableReceiver) Swap(newR PacketReader) PacketReader
```

`SwappableSender.Write` is fully lock-free — a single
`atomic.Pointer.Load` followed by the underlying `Write`. Because no
lock is held during I/O, the swapping side cannot prove all writers are
done with the previous pointer before it returns; instead,
`SwapAndDeferClose` schedules the underlying close after
`SwapCloseGrace = 50ms`. That window is far longer than the single
non-blocking `sendto(2)` on the hot path.

`SwappableReceiver` does take a brief `RLock` to snapshot the
implementation pointer, then releases it before blocking in
`ReadFromUDP`. Closing the previous reader after a `Swap` unblocks
any in-flight `ReadFromUDP` so `receiveLoop` can immediately read from
the new socket.

### `control.EventSource` (control/event.go)

```go
type EventSource interface {
    Events(ctx context.Context) <-chan PTTEvent
}
```

Implementations: `OpenVLMSource`, `ROIPSource`, `NanoPTTSource`,
`*WebEventSource`.

### `control.HIDDevice` / `control.HIDOpener` (control/openvlm.go)

```go
type HIDDevice interface {
    Read(b []byte) (int, error)
    Close() error
}

type HIDOpener func(vendorID, productID uint16) (HIDDevice, error)
```

- Production: `DefaultHIDOpener` initializes hidapi, opens the device,
  and wraps it in an unexported `hidDeviceWrapper` whose `Close` also
  calls `hid.Exit` so init/teardown stay balanced.
- Test: `NewOpenVLMSourceWithOpener(opener, log)` injects a mock that
  pre-loads HID report sequences.

### `control.Factory` / `control.ControlDeps` / Register / Lookup (control/source.go)

See [§9 Comm Event System](#9-comm-event-system) for the registry
discussion.

### `webaudio.SendFn` (webaudio/bridge.go)

```go
type SendFn func(opusData []byte)

func NewBridge(log zerolog.Logger, send SendFn) *Bridge
func (b *Bridge) InjectTxFrame(opusData []byte)        // RPC TX path
func (b *Bridge) PushRxFrame(opusData []byte)          // webPlayoutLoop RX
func (b *Bridge) RxFrames() <-chan []byte              // RPC RX channel
```

The parent binds `SendFn` to `cfg.sendToAllPorts(rt, …)` at construction
so `webaudio` never imports the parent and never sees a `*PortChannel`.

---

## 11. Extending the Package

### Adding a new control source

1. **Implement `control.EventSource`** in a new file under
   `internal/comms/control/`. Follow the existing patterns:

   ```go
   type MySource struct { /* … */ }

   func NewMySource(log zerolog.Logger /* + cfg */) control.EventSource {
       return &MySource{ /* … */ }
   }

   func (s *MySource) Events(ctx context.Context) <-chan control.PTTEvent {
       ch := make(chan control.PTTEvent, 4)
       go func() {
           defer close(ch)
           for {
               select {
               case <-ctx.Done():
                   return
               default:
               }
               // read your hardware event…
               select {
               case ch <- control.PTTDown:  // or PTTUp / PTTToggle
               case <-ctx.Done():
                   return
               }
           }
       }()
       return ch
   }
   ```

2. **Add a backend payload struct** in [control_register.go](control_register.go)
   if your factory needs anything from the parent runtime (port list,
   atomic flags, etc.). Populate it inside `buildControlDeps`:

   ```go
   type mySourceBackend struct {
       // whatever closures / config the factory needs
   }

   case "my_source":
       deps.Backend = &mySourceBackend{ /* … */ }
   ```

3. **Register the factory** in the same file's `init()`:

   ```go
   control.Register("my_source", func(deps control.ControlDeps) (control.EventSource, error) {
       b, ok := deps.Backend.(*mySourceBackend)
       if !ok || b == nil {
           return nil, errors.New("comms: my_source missing backend deps")
       }
       return control.NewMySource(deps.Log /* , b.X */), nil
   })
   ```

4. **Add the canonical name** to `normalizeControlSource` in
   [device.go](device.go) so unknown casing/whitespace folds into it.

5. **Add any new `CommsConfig` fields** required by the source and copy
   them in `NewComms`.

6. **Write tests** under `internal/comms/control/my_source_test.go` —
   white-box (`package control`) so it can reach unexported state.

`cfg.Validate()` will refuse to enable a source whose name is not in the
registry, so the `Enable` API surface fails fast for typos.

### Switching to hold-to-talk semantics with a new source

The `Run` loop already handles `PTTDown` and `PTTUp` separately. To add
hold-to-talk, emit `PTTDown` on the press edge and `PTTUp` on the
release edge instead of `PTTToggle`. No changes to `Run`,
`beginTransmission`, or `endTransmission` are required.

### Replacing the codec

Implement `codec.AudioEncoder` / `codec.AudioDecoder`, then wire the
constructor through `buildCodec` in [comms.go](comms.go). The rest of
the pipeline — capture/playback callbacks, RTP session, jitter buffer,
and playout — is codec-agnostic.

### Toggling talk-group direction at runtime

```go
svc := comms.Default()
if err := svc.EnableTalkGroupSend(idx, true); err != nil { /* … */ }
if err := svc.EnableTalkGroupReceive(idx, false); err != nil { /* … */ }
states, _ := svc.TalkGroupStates()
```

The atomic toggles are checked by `sendToAllPorts` (TX side),
`receiveLoop` (RX side, via `pc.ReceiveEnabled`), and
`halfDuplexDecayLoop` (which only watches send-enabled ports for
half-duplex purposes). No goroutines or sockets are restarted.

> **Endpoint swap plumbing** (`replaceNetwork` + `SwapAndDeferClose`)
> still exists in [network.go](network.go) for future use. The previous
> public `UpdateMulticastEndpoint` API was removed during the refactor;
> if you need to reintroduce it, the lock-free swap path is intact.

---

## 12. Testing Strategy

The package and every sub-package are fully testable without real
hardware because every external dependency is hidden behind one of the
interfaces in [§10](#10-interface-reference). Each sub-package keeps its
own hand-written fakes in a sibling `_test.go` file (no shared mock
package, no mock framework).

### Test file map

| Area | Package | Main test files |
|---|---|---|
| Top-level orchestration | `comms` | [comms_test.go](comms_test.go), [transmit_test.go](transmit_test.go), [receive_test.go](receive_test.go), [manager_test.go](manager_test.go), [service_test.go](service_test.go), [register_close_test.go](register_close_test.go), [control_register_test.go](control_register_test.go), [multiport_test.go](multiport_test.go), [integration_test.go](integration_test.go), [event_test.go](event_test.go), [bench_test.go](bench_test.go), [mocks_test.go](mocks_test.go) |
| Broadcast encoder + Init | `audio` | [audio/encoder_test.go](audio/encoder_test.go), [audio/mocks_test.go](audio/mocks_test.go) |
| Pools + RMS energy | `audiopool` | [audiopool/audiopool_test.go](audiopool/audiopool_test.go) |
| Opus wrapper | `codec` | [codec/opus_test.go](codec/opus_test.go) |
| PTT backends + gate | `control` | [control/openvlm_test.go](control/openvlm_test.go), [control/roip_test.go](control/roip_test.go), [control/web_event_source_test.go](control/web_event_source_test.go), [control/source_test.go](control/source_test.go), [control/half_duplex_gate_test.go](control/half_duplex_gate_test.go) |
| Device discovery | `device` | [device/cm108_test.go](device/cm108_test.go), [device/alsa_test.go](device/alsa_test.go) |
| RTP session, jitter, transport | `rtp` | [rtp/session_test.go](rtp/session_test.go), [rtp/jitter_test.go](rtp/jitter_test.go), [rtp/transport_test.go](rtp/transport_test.go), [rtp/fuzz_test.go](rtp/fuzz_test.go), [rtp/mocks_test.go](rtp/mocks_test.go) |
| Web audio bridge | `webaudio` | [webaudio/bridge_test.go](webaudio/bridge_test.go) |

### Mock injection pattern

Tests assemble a `CommsRuntime` directly from the exported struct fields
rather than calling `Start`:

```go
mpc := comms.McastPortConfig{Address: "224.0.0.1", Port: 5007, Send: true, Receive: true}
pc  := &comms.PortChannel{
    cfg:            mpc,
    Sender:         rtp.NewSwappableSender(&mockWriter{}),
    Receiver:       rtp.NewSwappableReceiver(newMockReader(/* … */)),
    RTPSess:        &mockRTPSender{},
    Jitter:         rtp.NewJitterBuffer(rtp.PrebufferPackets, rtp.MaxDepth),
    PlaybackBuffer: make(chan []int16, 4),
}
pc.SendEnabled.Store(true)
pc.ReceiveEnabled.Store(true)

rt := &comms.CommsRuntime{
    Decoder:         &mockDecoder{fillValue: 8192},
    Encoder:         &mockEncoder{},
    BroadcastStream: &mockStream{},
    Ports:           []*comms.PortChannel{pc},
    BeepBufferStart: make([]int16, audiopool.FrameSize),
    BeepBufferStop:  make([]int16, audiopool.FrameSize),
}
localIP := "10.0.0.1"
rt.LocalIP.Store(&localIP)

cfg := &comms.CommsConfig{Log: zerolog.Nop(), Loopback: true}
```

`mockWriter`, `mockReader`, `mockEncoder`, `mockDecoder`, `mockStream`,
and `mockRTPSender` live in [mocks_test.go](mocks_test.go) and are
mutex-protected for race-detector cleanliness. Helper variants
(`safeMockWriter`, `trackingReader`, `errClosingReader`) handle the
concurrency-heavy tests in [register_close_test.go](register_close_test.go)
and the swap tests in [rtp/transport_test.go](rtp/transport_test.go).

### cancelAfterDrain pattern for receiveLoop tests

`mockReader` pre-loads packets and blocks (then errors on `Close`) when
exhausted. `cancelAfterDrain` polls the reader's queue in a goroutine
and cancels the context once it is empty, allowing `receiveLoop` to exit
cleanly:

```
test goroutine        cancelAfterDrain goroutine     receiveLoop goroutine
     │                       │                              │
     │ go receiveLoop(ctx)   │                              │
     │──────────────────────────────────────────────────►   │
     │                       │ poll len(reader.packets)     │
     │                       │──────────────────────────►   │ ReadFromUDP → pkt
     │               empty   │                              │ (push to jitter)
     │                       │ cancel(); reader.Close()     │
     │                       │                              │ ReadFromUDP → error
     │                       │                              │ ctx.Done() → return
     │◄───────────────────────────────────────────────────  │ (exited)
```

### Concurrency tests

Tests that fire many goroutines use `safeMockWriter` (mutex-protected
counters) so the race detector reports genuine races in production code
rather than races inside an unsynchronized fake. The
[rtp/transport_test.go](rtp/transport_test.go) swap tests fire ~1000
concurrent writers against a `SwappableSender`, swap mid-flight, and
assert the sum of writes seen by both impls equals the goroutine count.

### Testing OpenVLM without hardware

`OpenVLMSource` accepts a `HIDOpener` function via
`NewOpenVLMSourceWithOpener`. Tests provide a factory returning a mock
`HIDDevice` pre-loaded with deterministic GPIO3 sequences:

```go
opener := func(_, _ uint16) (control.HIDDevice, error) {
    return &mockHIDDevice{reports: [][]byte{
        {0, 0x00, 0x04, 0, 0},  // IR1 bit 2 HIGH → PTTDown
        {0, 0x00, 0x00, 0, 0},  // IR1 bit 2 LOW  → PTTUp
    }}, nil
}
src := control.NewOpenVLMSourceWithOpener(opener, zerolog.Nop())
```

`mockHIDDevice` lives in [control/openvlm_test.go](control/openvlm_test.go)
and is reused by [control/roip_test.go](control/roip_test.go) (which
exercises both the COS and VOX paths against the same mock).

### Testing transmission timing

`beginTransmission` calls `time.Sleep(cfg.transmitSettleWait(rt))`
once. The settle wait is the greater of `cfg.pttStartDelay()` (default
50 ms; configurable via `PttStartDelayMs`; set to a negative value to
skip entirely) and `rt.PlaybackOutputLatency + 20 ms beep + 20 ms
margin`. The second term keeps the start tone from leaking into the
transmitted RTP stream via acoustic or device sidetone coupling between
the speaker and the mic. Tests that need to observe the post-sleep
state either set `PttStartDelayMs = -1` (which short-circuits both
contributions) or wait long enough to step over the configured floor.
The sleep is a deliberate hardware-settle window, not a synchronization
primitive.

### Testing Run() dispatch

A pre-seeded `PTTEvent` channel is closed after injection so `Run`
returns immediately via the `!ok` branch:

```go
evCh := make(chan control.PTTEvent, 1)
evCh <- control.PTTDown
close(evCh)
src := &mockEventSource{ch: evCh}

ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
defer cancel()
go cfg.Run(ctx, rt, src)
// assert rt.Broadcasting and mock stream call counts
```
