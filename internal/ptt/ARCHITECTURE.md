# PTT Package — Developer Architecture Guide

This document explains how the `ptt` package works internally, how each component
relates to the others, and how to extend it.  It is intended as a companion to the
higher-level [README](README.md).

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
9. [PTT Event System](#9-ptt-event-system)
10. [Interface Reference](#10-interface-reference)
11. [Extending the Package](#11-extending-the-package)
12. [Testing Strategy](#12-testing-strategy)

---

## 1. Package Layout

```
ptt/
├── ptt.go          PTTConfig + PTTRuntime structs; startup pipeline (Start/Run)
│                   replaceNetwork(); UpdateMulticastEndpoint()
├── comms.go        Receive loop, playout loop, beginTransmission, endTransmission
├── rtp.go          RTP framing helpers (wrap/unwrap/parse, SSRC, seq)
├── jitter.go       RTP jitter buffer with PLC gap detection
├── event.go        PTTEvent type; EventSource interface; evdevSource backend
├── device.go       Audio device resolution; network helpers; evdev enumeration
├── codec.go        AudioEncoder/AudioDecoder interfaces; Opus constructors
├── stream.go       AudioStream interface; portaudioStream wrapper
├── transport.go    PacketWriter/PacketReader interfaces;
│                   swappableSender / swappableReceiver (atomic-swap wrappers)
└── xevent.go       BlueALSA xevent backend (placeholder — not yet implemented)
```

---

## 2. Component Overview

```
  ┌─────────────────────────────────────────────────────────────────────┐
  │                          PTTConfig                                  │
  │  (static config: iface, mcast addr/port, protocol, device names…)  │
  └──────────────────────────────┬──────────────────────────────────────┘
                                 │ Start() assembles all components
                                 │ and stores them in PTTRuntime
                                 ▼
  ┌─────────────────────────────────────────────────────────────────────┐
  │                          PTTRuntime                                 │
  │                                                                     │
  │  ┌──────────────┐   ┌──────────────┐   ┌───────────────────────┐  │
  │  │ AudioEncoder │   │ AudioDecoder │   │ broadcastStream       │  │
  │  │ (Opus)       │   │ (Opus+PLC)   │   │ (AudioStream / mic)   │  │
  │  └──────┬───────┘   └──────┬───────┘   └───────────┬───────────┘  │
  │         │                  │                       │               │
  │  ┌──────▼───────┐   ┌──────▼───────┐   ┌──────────▼──────────┐   │
  │  │ PacketWriter │   │  chan[]float32│   │ PacketReader        │   │
  │  │ (UDP sender) │   │ playbackBuf  │   │ (UDP receiver)      │   │
  │  └──────────────┘   └──────────────┘   └─────────────────────┘   │
  └─────────────────────────────────────────────────────────────────────┘
         │  (write)             │  (drain)               │  (read)
         ▼                      ▼                        ▼
  UDP Multicast           PortAudio output         UDP Multicast
  (send path)             (speaker)                (receive path)
```

`PTTConfig` owns the configuration and all methods.  `PTTRuntime` is an opaque
resource container whose fields are all interfaces — making every hardware
dependency injectable for testing.

---

## 3. Key Types

### PTTConfig

The single public struct callers interact with.  Create with `NewPTT`, populate
fields, call `Start`.

```
PTTConfig
├── Log             zerolog.Logger
├── Interrupt       chan os.Signal       (external shutdown hook)
├── Enable          bool
│
├── Iface           string              e.g. "br-ahwlan"
├── McastAddr       string              e.g. "224.0.0.1"
├── McastPort       int                 e.g. 5007
├── Protocol        string              "udp" | "rtp"
├── RtpID           string              → SSRC seed
│
├── InputDevice     string              PortAudio device name/index
├── OutputDevice    string
├── AudioDeviceHint string              fills both if neither set
├── PlaybackDepth   int                 channel buffer depth (default 2)
│
├── PTTKey          string              "any" | decimal EV_KEY code
├── PTTDeviceGlob   string              evdev glob, e.g. "/dev/hidraw0/*"
├── PTTDeviceName   string              exact device name to match
├── ControlSource   string              "evdev" | "bluealsa_xevent"
│
├── Debug, Loopback, Trace  bool
│
└── runtime         *PTTRuntime         nil until Start() runs
```

### PTTRuntime

Private.  Wires together the live interfaces.  All fields are interfaces so
tests can swap implementations without real hardware.

```
PTTRuntime
├── encoder         AudioEncoder          Opus encoder
├── decoder         AudioDecoder          Opus decoder (nil arg → PLC)
│
├── sender          *swappableSender      atomic-swappable UDP send conn
├── receiver        *swappableReceiver    atomic-swappable UDP recv conn
├── localIP         atomic.Value (string) source IP for loopback filter
├── rtpSeq          uint16                per-packet sequence counter
├── rtpSSRC         uint32                FNV-1a hash of RtpID/hostname/IP
│
├── broadcastStream AudioStream           mic capture (start/stop per tx)
├── playbackBuffer  chan []float32         decoded PCM frames → speaker
├── beepBufferStart []float32             1000 Hz tone (tx start)
├── beepBufferStop  []float32             600 Hz tone (tx end)
│
├── recordMutex     sync.Mutex            guards broadcasting
├── broadcasting    bool
└── reopenBroadcast func() error          closure → reopenBroadcastStream
```

`sender` and `receiver` are not plain interfaces — they are concrete
`*swappableSender` / `*swappableReceiver` wrappers (see §10) that still satisfy
`PacketWriter` / `PacketReader`.  This lets `UpdateMulticastEndpoint` atomically
replace the underlying connections while the send/receive path continues to
operate without locking.  `localIP` is an `atomic.Value` for the same reason:
it is read on every received packet by `receiveLoop` and written only by
`replaceNetwork`.

---

## 4. Startup Sequence

`Start()` runs in the caller's goroutine and is intentionally sequential so
failures surface as `log.Fatal` with clear stage labels.

```
Start()
  │
  ├─ 1. Guard:  if !Enable → return
  │
  ├─ 2. applyDefaults()
  │     fills empty fields with package-level constants
  │     AudioDeviceHint propagates to Input/OutputDevice when both empty
  │
  ├─ 3. buildCodec()
  │     newOpusEncoder() — 48 kHz, mono, VoIP, 32 kbps, complexity=10
  │     newOpusDecoder() — 48 kHz, mono
  │
  ├─ 4. Build playback channel + beep buffers
  │     chan []float32 (depth = PlaybackDepth, default 2)
  │     beepStart: 1000 Hz sine × 0.2 amplitude, frameSize samples
  │     beepStop:   600 Hz sine × 0.2 amplitude, frameSize samples
  │
  ├─ 5. buildNetwork()
  │     getIfaceIPv4(Iface)         → localIP, *net.Interface
  │     net.DialUDP(localIP → mcast) → PacketWriter (raw sender)
  │     net.ListenUDP(0.0.0.0:port)  → PacketReader (raw receiver)
  │     joinMulticastGroup(ifi, conn, group)
  │
  ├─ 6. Assemble PTTRuntime (enc, dec, sender, receiver, buffers)
  │     sender   = newSwappableSender(rawSender)
  │     receiver = newSwappableReceiver(rawReceiver)
  │     rt.localIP.Store(localIP)       ← atomic.Value
  │     If Protocol==rtp: derive rtpSSRC, randomise rtpSeq
  │
  ├─ 7. portaudio.Initialize()
  │     Registers SIGTERM goroutine → Terminate() + os.Exit(0)
  │
  ├─ 8. buildAudio(rt)
  │     resolveAudioDevice(OutputDevice) → *portaudio.DeviceInfo
  │     resolveAudioDevice(InputDevice)  → *portaudio.DeviceInfo
  │     portaudio.OpenStream(output, playback callback) → playbackStream
  │     openBroadcastStreamOn(inDev, rt) → broadcastStream
  │     Wire rt.reopenBroadcast = func(){ reopenBroadcastStream(rt, inDev) }
  │
  ├─ 9. playbackStream.Start()
  │
  ├─10. buildEventSource()
  │     switch ControlSource:
  │       "evdev" → findPTTDevice() → NewEvdevSource(dev, key, log)
  │       others  → (placeholder, returns error)
  │
  └─11. Run(ctx, rt, src)   ← blocks until ctx/signal
```

---

## 5. Goroutine Topology

Once `Start` reaches step 11, the following goroutines are live:

```
  main goroutine
  └── Start() → Run()
        │
        ├── go receiveLoop(ctx, rt)
        │     │
        │     └── [if protocol==rtp]
        │           go rtpPlayoutLoop(ctx, jitter, rt)
        │
        └── [event loop — blocks in Run()]
              for ev := range src.Events(ctx):
                PTTToggle → beginTransmission / endTransmission

  [background, wired in Start()]
  └── go func() { <-ptt.Interrupt → portaudio.Terminate(); os.Exit(0) }
  └── go func() { <-c (SIGTERM)  → cancel() }

  [PortAudio-internal, managed by the audio library]
  └── playback callback  — runs on PortAudio thread, drains playbackBuffer
  └── broadcast callback — runs on PortAudio thread, encodes + sends UDP
```

Context cancellation causes `receiveLoop`, `rtpPlayoutLoop`, and `Run` to exit
in order.  PortAudio streams are stopped/closed via `defer` in `Start`.

---

## 6. Transmission Path

The capture side is driven by a PortAudio callback (not a goroutine) that fires
every 20 ms when the broadcast stream is running.

```
PortAudio mic callback (every 20 ms while broadcastStream.Start() active)
  │
  │  []float32  (in, frameSize=960 samples)
  │
  ▼
  convert float32 → int16 (multiply by 32767)
  │
  ▼
  AudioEncoder.Encode(pcm []int16, buf []byte) → n bytes
  │
  ├─ [if protocol == "rtp"]
  │    wrapRTP(payload, rt)
  │       prepend 12-byte header:
  │         version=0x80, PT=0, seq++, ts=Unix(), SSRC=rtpSSRC
  │
  ▼
  PacketWriter.Write(packet)   → UDP multicast datagram
```

### SSRC derivation

```
ptt.RtpID set? ──yes──► FNV-1a hash → rtpSSRC
     │
     no
     ▼
hostname available? ──yes──► FNV-1a hash → rtpSSRC
     │
     no
     ▼
localIP string ──────────► FNV-1a hash → rtpSSRC
```

### beginTransmission state machine

```
PTTToggle arrives (currently idle)
  │
  ▼
[lock] broadcasting = true [unlock]
  │
  ▼
drainPlaybackBuffer()
  │
  ▼
playbackBuffer ← beepBufferStart  (1000 Hz, 20 ms)
  │
  ▼
time.Sleep(200 ms)  ← lets beep play before mic opens
  │
  ▼
broadcastStream == nil?
  ├─ yes → reopenBroadcast()
  │           ├─ error → broadcasting=false, return
  │           └─ ok    → broadcastStream = new stream
  └─ no  → continue
  │
  ▼
broadcastStream.Start()
  ├─ error → reopenBroadcast() → Start() again
  │              ├─ still error → broadcasting=false, return
  │              └─ ok          → continue
  └─ ok   → "Mic stream started"
```

### endTransmission state machine

```
PTTToggle arrives (currently broadcasting)
  │
  ▼
[check] broadcasting == false? → return (idempotent)
  │
  ▼
broadcastStream.Stop()
  │
  ▼
drainPlaybackBuffer()
  │
  ▼
playbackBuffer ← beepBufferStop  (600 Hz, 20 ms)
  │
  ▼
[lock] broadcasting = false [unlock]
```

---

## 7. Receive Path

`receiveLoop` runs in its own goroutine.  Its behaviour varies by protocol.

### UDP mode

```
receiveLoop goroutine
  │
  │  ReadFromUDP(buf[1500])
  ▼
  loopback check
  │  src.IP.IsLoopback() || src.IP == localIP
  │  └─ Loopback==false → drop, continue
  │
  ▼
  unwrapRTP(frame) → (payload, ok)
  │  ok==true  → frame = payload  (auto-detect RTP-framed packets)
  │  ok==false → frame unchanged  (raw Opus)
  │
  ▼
  decodeAndQueue(rt, frame)
       │
       ▼
       AudioDecoder.Decode(frame, pcm[960])
       │
       ▼
       int16 → float32 (÷ 32768)
       │
       ▼
       playbackBuffer <- []float32     (non-blocking; drop if full)
```

### RTP mode

```
receiveLoop goroutine
  │
  │  ReadFromUDP(buf[1500])
  ▼
  loopback check (same as UDP)
  │
  ▼
  parseRTPHeader(frame) → seq, ts, ssrc, ok
  │  ok==false → drop ("invalid RTP header")
  │
  ▼
  unwrapRTP(frame) → payload
  │
  ▼
  jitter.push(seq, payload)
  │  false → drop (stale / duplicate / buffer full)
  └─ true  → stored in jitter buffer map[uint16][]byte
             (receiveLoop does NOT decode; that is rtpPlayoutLoop's job)

  ┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄ separate goroutine ┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄

rtpPlayoutLoop goroutine    (ticker: 20 ms)
  │
  ├─ jitter.popReady()
  │    case ready==true   → decodeAndQueue(rt, payload)
  │    case skipped==true → decodeAndQueuePLC(rt)   (gap: skip seq)
  │    case neither       → jitter.shouldConceal(100ms)?
  │                           true  → decodeAndQueuePLC(rt)
  │                           false → nothing (stream idle/not started)
```

### Playback callback (PortAudio thread — not a goroutine)

```
PortAudio output callback  (every 20 ms)
  │
  select {
  case data := <-playbackBuffer:  copy(out, data)
  default:                        zero-fill out  (silence)
  }
```

---

## 8. RTP Jitter Buffer

Located in `jitter.go`.  Provides sequence-ordered buffering with Packet Loss
Concealment gap detection.

### Internal state

```
rtpJitterBuffer
├── frames    map[uint16][]byte   payload keyed by RTP sequence number
├── expected  uint16              next sequence number to pop
├── init      bool                false until first push
├── started   bool                false until prebuffer fills
├── prebuffer int                 min frames before playout begins (3)
├── maxDepth  int                 max buffered frames before drops (24)
├── lastPush  time.Time           timestamp of most-recent push (for PLC)
└── mu        sync.Mutex
```

### push() logic

```
push(seq, payload)
  │
  ├─ !init → expected = seq, init = true
  │
  ├─ seqLess(seq, expected)?  → return false  (stale/late)
  │
  ├─ frames[seq] exists?      → return false  (duplicate)
  │
  ├─ len(frames) >= maxDepth? → return false  (buffer full)
  │
  └─ frames[seq] = copy(payload), lastPush = now → return true
```

### popReady() logic

```
popReady()
  │
  ├─ !init → return nil, false, false
  │
  ├─ !started && len(frames) < prebuffer → return nil, false, false
  ├─ !started && len(frames) >= prebuffer → started = true
  │
  ├─ frames[expected] exists?
  │    yes → delete, expected++, return payload, true, false
  │
  └─ len(frames) >= maxDepth/2?
       yes → expected++, return nil, false, true  (SKIP → caller does PLC)
       no  → return nil, false, false             (wait for next tick)
```

### Sequence number wrap-around

uint16 arithmetic wraps at 65535→0.  `seqLess(a, b)` casts `(a-b)` to
`int16` — a negative result means `a < b` in sequence space:

```
seqLess(a, b) = int16(a-b) < 0

Examples:
  seqLess(65535, 0) = int16(65535) = -1 → true  (65535 is "before" 0)
  seqLess(0, 65535) = int16(1)     =  1 → false
  seqLess(100, 100) = int16(0)     =  0 → false
```

---

## 9. PTT Event System

### PTTEvent type

```go
type PTTEvent uint8

const (
    PTTDown   PTTEvent = iota  // 0: hold-to-talk press
    PTTUp                      // 1: hold-to-talk release
    PTTToggle                  // 2: press-to-toggle
)
```

### EventSource interface

```go
type EventSource interface {
    Events(ctx context.Context) <-chan PTTEvent
}
```

Implementations return a channel that must be closed when `ctx` is cancelled,
causing `Run` to exit its `range` loop cleanly.

### evdevSource (current default)

Emits only `PTTToggle` — there is no hold-to-talk in the current implementation.

```
evdevSource.Events(ctx) goroutine
  │
  │  dev.ReadOne() → InputEvent
  │
  ├─ ev.Type != EV_KEY  → skip
  │
  ├─ key match?
  │    pttKey == "any"                    → match
  │    pttKey is decimal code == ev.Code  → match
  │    else                               → skip
  │
  └─ ev.Value == 1 (press) → ch <- PTTToggle
     ev.Value == 0 (release) → (no action, toggle model has no PTTUp)
```

### Run() — event dispatch

```
Run(ctx, rt, src)
  │
  ├─ go receiveLoop(ctx, rt)
  │
  └─ for {
       select {
       case <-ctx.Done()     → return
       case ev, ok := <-events:
           !ok               → return  (EventSource closed channel)
           PTTDown           → beginTransmission(rt)
           PTTUp             → endTransmission(rt)
           PTTToggle         → isBroadcasting(rt)?
                                 yes → endTransmission(rt)
                                 no  → beginTransmission(rt)
       }
     }
```

---

## 10. Interface Reference

All hardware-touching operations are hidden behind these interfaces.  Every
interface has an in-package mock in `mocks_test.go`.

### AudioEncoder (`codec.go`)

```go
type AudioEncoder interface {
    Encode(pcm []int16, data []byte) (int, error)
}
```

- Production: `opusEncoder` wrapping `*opus.Encoder`
- Test: `mockEncoder` — stores last PCM, returns canned bytes

### AudioDecoder (`codec.go`)

```go
type AudioDecoder interface {
    Decode(data []byte, pcm []int16) (int, error)
}
```

- `Decode(nil, pcm)` triggers Opus Packet Loss Concealment
- Production: `opusDecoder` wrapping `*opus.Decoder`
- Test: `mockDecoder` — fills PCM with a fixed int16 value; supports error injection

### AudioStream (`stream.go`)

```go
type AudioStream interface {
    Start() error
    Stop() error
    Close() error
}
```

- Production: `portaudioStream` wrapping `*portaudio.Stream`
- Test: `mockStream` — counts calls, returns injected errors

### PacketWriter (`transport.go`)

```go
type PacketWriter interface {
    Write(b []byte) (int, error)
}
```

- Production: `*swappableSender` wrapping a `*net.UDPConn`
- Test: `mockWriter` — accumulates packets in `Packets [][]byte`

### PacketReader (`transport.go`)

```go
type PacketReader interface {
    ReadFromUDP(b []byte) (int, *net.UDPAddr, error)
    Close() error
}
```

- Production: `*swappableReceiver` wrapping a `*net.UDPConn`
- Test: `mockReader` (wrapped with `newSwappableReceiver`) — pre-seeded packet queue; blocks then errors on `Close()`

### swappableSender / swappableReceiver (`transport.go`)

Concrete wrappers added to support runtime multicast endpoint changes.
Both satisfy the `PacketWriter` / `PacketReader` interfaces respectively.

```go
// swappableSender
func (s *swappableSender) Write(b []byte) (int, error)           // PacketWriter
func (s *swappableSender) swap(newW PacketWriter) PacketWriter   // returns old

// swappableReceiver
func (r *swappableReceiver) ReadFromUDP(b []byte) (int, *net.UDPAddr, error)  // PacketReader
func (r *swappableReceiver) Close() error                                     // PacketReader
func (r *swappableReceiver) swap(newR PacketReader) PacketReader              // returns old
```

**Hot-path locking discipline** — neither wrapper holds a lock during I/O:

```
Write / ReadFromUDP / Close:
  RLock → snapshot impl pointer → RUnlock → call impl (no lock held during I/O)

swap:
  Lock → update impl pointer → Unlock → return old (caller closes it)
```

This means `swap` can execute concurrently with any number of `Write` or
`ReadFromUDP` calls without ever blocking them for longer than a pointer
read.  Closing the old socket after the swap unblocks any in-flight
`ReadFromUDP` on the old connection, causing `receiveLoop` to loop and
immediately read from the new socket.

### EventSource (`event.go`)

```go
type EventSource interface {
    Events(ctx context.Context) <-chan PTTEvent
}
```

- Production: `evdevSource`; placeholder `bluealsa_xevent`
- Test: `mockEventSource` — pre-seeded `chan PTTEvent` returned directly

---

## 11. Extending the Package

### Adding a new PTT control source

1. Create a new file (e.g. `xevent.go`) and implement `EventSource`:

```go
type mySource struct { /* fields */ }

func (s *mySource) Events(ctx context.Context) <-chan PTTEvent {
    ch := make(chan PTTEvent, 4)
    go func() {
        defer close(ch)
        for {
            select {
            case <-ctx.Done():
                return
            default:
            }
            // read hardware event...
            // emit PTTDown, PTTUp, or PTTToggle
            ch <- PTTDown
        }
    }()
    return ch
}
```

2. Register it in `buildEventSource()` in `ptt.go`:

```go
func (ptt *PTTConfig) buildEventSource() (EventSource, error) {
    switch normalizeControlSource(ptt.ControlSource) {
    case "my_source":
        return newMySource(ptt), nil
    default: // evdev
        dev := ptt.findPTTDevice()
        if dev == nil {
            return nil, errors.New("PTT device not found")
        }
        return NewEvdevSource(dev, ptt.PTTKey, ptt.Log), nil
    }
}
```

3. Add the new value to `normalizeControlSource()` in `device.go`:

```go
func normalizeControlSource(src string) string {
    switch strings.ToLower(strings.TrimSpace(src)) {
    case "my_source":
        return "my_source"
    case "bluealsa_xevent":
        return "bluealsa_xevent"
    default:
        return "evdev"
    }
}
```

4. Add a `PTTConfig` field if the source needs configuration, and copy it in
   `NewPTT`.

5. Write unit tests using `mockEventSource` as a reference.

### Switching to hold-to-talk semantics

The `Run` loop already handles `PTTDown` and `PTTUp` separately.  To add
hold-to-talk, emit `PTTDown` on press and `PTTUp` on release from your
`EventSource` instead of `PTTToggle`.  No changes to `Run`, `beginTransmission`,
or `endTransmission` are needed.

### Replacing the codec

Implement `AudioEncoder` and `AudioDecoder`, then inject them via the
`PTTRuntime` — for example by adding a `buildCodec` override or by wiring
alternate constructors in `Start`.  The rest of the pipeline is codec-agnostic.

### Changing the wire format

The framing decision lives entirely in `rtp.go` and in the send callback inside
`openBroadcastStreamOn` (`ptt.go`).  Add a new protocol constant, handle it in
`normalizeProtocol`, add a wrap call in the send path, and a matching unwrap in
`receiveLoop`.

### Changing the multicast endpoint at runtime

Use the public `UpdateMulticastEndpoint(ptt, addr, port)` function from anywhere
in the application:

```go
if err := ptt.UpdateMulticastEndpoint(cfg, "239.255.0.1", 5010); err != nil {
    log.Error().Err(err).Msg("failed to change PTT multicast endpoint")
}
```

The function validates inputs, allocates new UDP sockets, and delegates to the
internal `replaceNetwork` helper:

```
UpdateMulticastEndpoint(ptt, addr, port)
  │
  ├─ guard: runtime == nil?  → error "not running"
  ├─ validate: IPv4 multicast address?
  ├─ validate: port in [1, 65535]?
  │
  ├─ temporarily update McastAddr / McastPort on PTTConfig
  ├─ buildNetwork()  →  newSender, newReceiver, newLocalIP
  │    error → roll back McastAddr/McastPort, return error
  │
  └─ replaceNetwork(rt, newSender, newReceiver, newLocalIP)
       │
       ├─ rt.receiver.swap(newReceiver)  ← atomic; receiveLoop
       │                                   now reads from new socket
       ├─ rt.sender.swap(newSender)      ← atomic; PortAudio callback
       │                                   now writes to new socket
       ├─ rt.localIP.Store(newLocalIP)   ← atomic.Value
       │
       ├─ oldReceiver.Close()   ← unblocks in-flight ReadFromUDP;
       │                          receiveLoop skips the error and
       │                          immediately reads from the new receiver
       └─ oldSender.Close()    ← if the concrete type has Close()
```

The swap is always applied even if `buildNetwork` returns an error mid-way —
but in practice the error path returns before `replaceNetwork` is called, so
the runtime sockets are never left in a partial state.

No changes to `comms.go`, the PortAudio callbacks, or `Run` are required; they
already dereference the swappable wrappers transparently.

---

## 12. Testing Strategy

The package is fully testable without real hardware because every external
dependency is hidden behind one of the five interfaces above.

### Test file map

| File | Tests |
|---|---|
| `comms_test.go` | `decodeAndQueue`, `decodeAndQueuePLC`, `receiveLoop`, `rtpPlayoutLoop` |
| `device_test.go` | `normalizeControlSource`, `getIfaceIPv4`, `logInputDeviceList`, `joinMulticastGroup` |
| `endpoint_test.go` | `swappableSender`, `swappableReceiver`, `replaceNetwork`, `UpdateMulticastEndpoint` |
| `event_test.go` | `applyDefaults`, `NewPTT`, `Run` event dispatch |
| `jitter_test.go` | `push`, `popReady`, `shouldConceal`, `seqLess` |
| `rtp_test.go` | `normalizeProtocol`, `wrapRTP`, `unwrapRTP`, `parseRTPHeader`, `rtpSSRCFromID` |
| `transmission_test.go` | `beginTransmission`, `endTransmission`, `isBroadcasting`, `drainPlaybackBuffer` |
| `mocks_test.go` | All mock implementations (shared across test files) |

### Mock injection pattern

Tests assemble a `PTTRuntime` directly with mock implementations rather than
calling `Start`:

```go
rt := &PTTRuntime{
    encoder:         &mockEncoder{cannedBytes: []byte{0xde, 0xad}},
    decoder:         &mockDecoder{fillValue: 8192},
    sender:          newSwappableSender(&mockWriter{}),
    receiver:        newSwappableReceiver(newMockReader(mockPacket{data: ..., src: ...})),
    broadcastStream: &mockStream{},
    playbackBuffer:  make(chan []float32, 8),
    beepBufferStart: make([]float32, frameSize),
    beepBufferStop:  make([]float32, frameSize),
}
rt.localIP.Store("10.0.0.1")
ptt := &PTTConfig{Log: zerolog.Nop(), Protocol: protocolUDP, Loopback: true}
```

`sender` and `receiver` are never set as plain `PacketWriter`/`PacketReader`
fields in tests anymore; they are always wrapped so `replaceNetwork` (and
any code that reads `rt.localIP`) works correctly.

For `replaceNetwork`/`UpdateMulticastEndpoint` tests that need no real sockets,
a minimal runtime is assembled with `newReplaceNetworkRuntime`:

```go
rt := newReplaceNetworkRuntime(&mockWriter{}, &trackingReader{})
// rt.sender and rt.receiver are swappable; rt.localIP is initialised
```

### Concurrency tests

Tests that fire many goroutines concurrently use `safeMockWriter` (a
mutex-protected variant of `mockWriter`) instead of the plain `mockWriter`.
This ensures the race detector reports genuine races in the production
code rather than races inside an unsynchronised mock:

```go
wOld := &safeMockWriter{}
wNew := &safeMockWriter{}
s    := newSwappableSender(wOld)
// ... launch goroutines calling s.Write() ...
s.swap(wNew)
// total writes across wOld + wNew must equal goroutine count
```

### Testing the receive loop

`mockReader` pre-loads packets and blocks (then errors on `Close`) when
exhausted.  `cancelAfterDrain` polls the reader's queue in a goroutine and
cancels the context once it is empty, allowing `receiveLoop` to exit cleanly:

```
test goroutine        cancelAfterDrain goroutine     receiveLoop goroutine
     │                       │                              │
     │ go receiveLoop(ctx)   │                              │
     │──────────────────────────────────────────────────►  │
     │                       │ poll len(reader.packets)     │
     │                       │──────────────────────────►  │ ReadFromUDP → pkt1
     │               empty   │                              │ (process pkt1)
     │                       │ cancel(); reader.Close()     │
     │                       │                              │ ReadFromUDP → error
     │                       │                              │ ctx.Done() → return
     │◄─────────────────────────────────────────────────── │ (exited)
```

### Testing transmission timing

`beginTransmission` calls `time.Sleep(200ms)` internally.  Tests that need to
observe the post-sleep state use:

```go
go ptt.beginTransmission(rt)
time.Sleep(400 * time.Millisecond)   // > 200 ms to ensure completion
```

This is intentional: the sleep is a deliberate audio UX delay, not a
synchronisation primitive.  Tests accept the wall-clock cost.

### Testing Run() dispatch

`mockEventSource` returns a pre-seeded Go channel:

```go
evCh := make(chan PTTEvent, 1)
evCh <- PTTDown
src := &mockEventSource{ch: evCh}

ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
defer cancel()
go ptt.Run(ctx, rt, src)
time.Sleep(450 * time.Millisecond)
// assert rt broadcasting state / mock stream call counts
```

Closing `evCh` causes `Run` to return immediately (the `!ok` branch), which
is used to test the "event channel closed" exit path without waiting for a
timeout.
