# Comms Package — Developer Architecture Guide

This document explains how the `comms` package works internally, how each
component relates to the others, and how to extend it.  It is intended as a
companion to the higher-level [README](README.md).

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
comms/
├── comms.go        CommsConfig + CommsRuntime structs; startup pipeline
│                   (NewComms, applyDefaults, buildCodec, buildNetwork,
│                    buildAudio, buildEventSource, Start)
│                   replaceNetwork(); UpdateMulticastEndpoint()
├── receive.go      receiveLoop, playoutLoop, decodeAndQueue, decodeAndQueuePLC
├── transmit.go     isBroadcasting, drainPlaybackBuffer,
│                   beginTransmission, endTransmission, Run
├── rtp.go          pionRTPSession; ssrcFromID; parseIncomingRTP
├── jitter.go       rtpJitterBuffer — sequence-ordered playout buffer with PLC
├── event.go        PTTEvent type; EventSource interface; evdevSource backend
├── openvlm.go      openvlmSource; HIDDevice / HIDOpener abstractions;
│                   detectAndSetALSACard / detectAndSetALSACardFromRoot
├── device.go       normalizeControlSource; resolveAudioDevice;
│                   findCommDevice; getIfaceIPv4; joinMulticastGroup
├── codec.go        AudioEncoder/AudioDecoder interfaces; Opus constructors
├── stream.go       AudioStream interface; portaudioStream wrapper
├── transport.go    PacketWriter/PacketReader interfaces;
│                   swappableSender / swappableReceiver (atomic-swap wrappers)
├── alsa_silence.go CGo helpers — silence ALSA probe noise during PA init
└── doc.go          Package doc comment; omd_omit_comms build stub
```

---

## 2. Component Overview

```
  ┌──────────────────────────────────────────────────────────────────────┐
  │                          CommsConfig                                 │
  │  (static config: iface, mcast addr/port, control source, devices…)  │
  └─────────────────────────────┬────────────────────────────────────────┘
                                │ Start() assembles all components
                                │ and stores them in CommsRuntime
                                ▼
  ┌──────────────────────────────────────────────────────────────────────┐
  │                          CommsRuntime                                │
  │                                                                      │
  │  ┌──────────────┐   ┌──────────────┐   ┌───────────────────────┐   │
  │  │ AudioEncoder │   │ AudioDecoder │   │ broadcastStream       │   │
  │  │ (Opus)       │   │ (Opus + PLC) │   │ (AudioStream / mic)   │   │
  │  └──────┬───────┘   └──────┬───────┘   └───────────┬───────────┘   │
  │         │                  │                        │               │
  │  ┌──────▼───────┐   ┌──────▼───────┐   ┌───────────▼─────────┐    │
  │  │ pionRTPSess  │   │  chan[]float32│   │ *swappableReceiver  │    │
  │  │ (Packetizer  │   │ playbackBuf  │   │ (UDP recv)          │    │
  │  │  + RTCP SR)  │   └──────────────┘   └─────────────────────┘    │
  │  └──────┬───────┘                                                   │
  │         │ via *swappableSender                                       │
  └─────────┼──────────────────────────────────────────────────────────-┘
            │  (write)             │  (drain)               │  (read)
            ▼                      ▼                        ▼
     UDP Multicast           PortAudio output         UDP Multicast
     RTP (port N)            (speaker)                RTP (port N)
     RTCP (port N+1)
```

`CommsConfig` owns the configuration and all methods.  `CommsRuntime` is an
opaque resource container whose fields are all interfaces — making every
hardware dependency injectable for testing.  `pionRTPSession` sits between the
encoder and the UDP socket, adding RTP packetization and RTCP Sender Reports
via the pion interceptor chain.

---

## 3. Key Types

### CommsConfig

The single public struct callers interact with.  Create with `NewComms`,
populate fields, call `Start`.

```
CommsConfig
├── Log             zerolog.Logger
├── Interrupt       chan os.Signal       (external shutdown hook)
├── Enable          bool
│
├── Iface           string              e.g. "br-ahwlan"
├── McastAddr       string              e.g. "224.0.0.1"
├── McastPort       int                 e.g. 5007
├── RtpID           string              → SSRC seed (default: hostname)
│
├── BluetoothInputDevice     string              PortAudio device name/index
├── BluetoothOutputDevice    string
├── BluetoothAudioDeviceHint string              fills both if neither is set
├── PlaybackDepth   int                 channel buffer depth (default 10)
├── MicGain         float32             per-sample gain (default 1.0)
│
├── CommKey         string              "any" | decimal EV_KEY code (evdev only)
├── NanoPTTDevicePath  string              evdev glob, e.g. "/dev/hidraw0/*"
├── NanoPTTDeviceName  string              exact evdev device name to match
├── ControlSource   string              "openvlm" (default) | "evdev"
│
├── Debug, Loopback, Trace  bool
│
└── runtime         *CommsRuntime       nil until Start() runs
```

### CommsRuntime

Private.  Wires together the live interfaces.  All fields are interfaces so
tests can swap implementations without real hardware.

```
CommsRuntime
├── encoder          AudioEncoder            Opus encoder
├── decoder          AudioDecoder            Opus decoder (nil arg → PLC)
│
├── sender           *swappableSender        atomic-swappable UDP send conn
├── receiver         *swappableReceiver      atomic-swappable UDP recv conn
├── localIP          atomic.Value (string)   source IP for loopback filter
├── rtpSess          *pionRTPSession         Packetizer + RTCP SR chain
│
├── broadcastStream  AudioStream             mic capture (start/stop per tx)
├── playbackBuffer   chan []float32          decoded PCM frames → speaker
├── beepBufferStart  []float32              1000 Hz tone (tx start)
├── beepBufferStop   []float32              600 Hz tone (tx end)
│
├── recordMutex      sync.Mutex             guards broadcasting flag
├── broadcasting     bool
└── reopenBroadcast  func() error           closure → reopenBroadcastStream
```

`sender` and `receiver` are concrete `*swappableSender` / `*swappableReceiver`
wrappers (see §10) that satisfy `PacketWriter` / `PacketReader`.  This lets
`UpdateMulticastEndpoint` atomically replace the underlying connections while
the send/receive path continues without locking.  `localIP` is an `atomic.Value`
for the same reason: read on every received packet by `receiveLoop`, written
only by `replaceNetwork`.

### pionRTPSession

Wraps a pion `Packetizer` and an interceptor chain.  One session represents one
local SSRC.  The RTCP path is one-way outbound: the `report.SenderInterceptor`
fires every 5 seconds and writes SR packets to a dedicated RTCP UDP socket.

```
pionRTPSession
├── packetizer  pionrtp.Packetizer     adds seq/ts/SSRC, fragments >MTU
├── rtpWriter   interceptor.RTPWriter  head of the interceptor chain
├── intercept   interceptor.Interceptor contains SenderInterceptor goroutine
├── ssrc        uint32
└── mu          sync.Mutex             guards Packetize (not re-entrant)
```

---

## 4. Startup Sequence

`Start()` runs in the caller's goroutine and is intentionally sequential so
failures surface as `log.Fatal` with clear stage labels.

```
Start()
  │
  ├─ 1. Guard:  if !Enable → log and return
  │
  ├─ 2. applyDefaults()
  │     fills empty fields with package-level constants
  │     BluetoothAudioDeviceHint propagates to Input/BluetoothOutputDevice when both empty
  │
  ├─ 3. [if controlSource == "openvlm"]
  │     detectAndSetALSACard()
  │       scans /proc/asound/card*/usbid for VID=0x0D8C PID=0x013C
  │       sets ALSA_CARD env var before portaudio.Initialize()
  │
  ├─ 4. Set log level
  │     Trace → TraceLevel; Debug → DebugLevel
  │     [if Debug] logBluetoothInputDeviceList()
  │
  ├─ 5. buildCodec()
  │     newOpusEncoder() — 48 kHz, mono, VoIP, 32 kbps, complexity=10
  │     newOpusDecoder() — 48 kHz, mono
  │
  ├─ 6. Build playback channel + beep buffers
  │     chan []float32 (depth = PlaybackDepth, default 10)
  │     beepStart: 1000 Hz sine × 0.2 amplitude, frameSize samples
  │     beepStop:   600 Hz sine × 0.2 amplitude, frameSize samples
  │
  ├─ 7. buildNetwork()
  │     getIfaceIPv4(Iface)                     → localIP, *net.Interface
  │     net.DialUDP(localIP → mcast:port)       → rawSender (PacketWriter)
  │     net.ListenUDP(0.0.0.0:port)             → rawReceiver (PacketReader)
  │     joinMulticastGroup(ifi, recvConn, group)
  │     net.DialUDP(localIP → mcast:port+1)     → rawRTCPSender
  │
  ├─ 8. newPionRTPSession(ssrcFromID(RtpID), rawSender, rawRTCPSender, log)
  │     builds interceptor chain with report.SenderInterceptor (5 s interval)
  │     binds packetizer through chain down to baseRTPWriter
  │
  ├─ 9. Assemble CommsRuntime
  │     sender   = newSwappableSender(rawSender)
  │     receiver = newSwappableReceiver(rawReceiver)
  │     rt.localIP.Store(localIP)          ← atomic.Value
  │
  ├─10. silenceALSAProbeNoise()            ← CGo: replace ALSA error handler
  │     portaudio.Initialize()
  │     restoreALSAErrorHandler()          ← CGo: restore default handler
  │     Register SIGTERM goroutine → portaudio.Terminate() + os.Exit(0)
  │
  ├─11. buildAudio(rt)
  │     resolveAudioDevice(BluetoothOutputDevice)  → *portaudio.DeviceInfo
  │     resolveAudioDevice(BluetoothInputDevice)   → *portaudio.DeviceInfo
  │     portaudio.OpenStream(output, playback callback) → playbackStream
  │     openBroadcastStreamOn(inDev, rt) → broadcastStream
  │     Wire rt.reopenBroadcast = func(){ reopenBroadcastStream(rt, inDev) }
  │
  ├─12. playbackStream.Start()
  │
  ├─13. buildEventSource()
  │     switch ControlSource:
  │       "openvlm" → NewOpenVLMSource(log)
  │       "evdev" → findCommDevice() → NewEvdevSource(dev, CommKey, log)
  │
  └─14. Run(ctx, rt, src)   ← blocks until ctx/signal
```

---

## 5. Goroutine Topology

Once `Start` reaches step 14, the following goroutines are live:

```
  main goroutine
  └── Start() → Run()
        │
        ├── go receiveLoop(ctx, rt)
        │     │
        │     └── go playoutLoop(ctx, jitter, rt)   ← always started
        │
        └── [event loop — blocks in Run()]
              for ev := range src.Events(ctx):
                PTTDown   → beginTransmission(rt)
                PTTUp     → endTransmission(rt)
                PTTToggle → beginTransmission / endTransmission by state

  [background, wired in Start()]
  └── go func() { <-cfg.Interrupt → portaudio.Terminate(); os.Exit(0) }
  └── go func() { <-c (SIGTERM)   → cancel() }

  [pion-internal, managed by the interceptor chain]
  └── report.SenderInterceptor timer → RTCP SR every 5 s

  [EventSource-internal]
  └── openvlmSource.Events goroutine  — blocks on hid.Device.Read()
   or evdevSource.Events goroutine  — blocks on evdev.Device.ReadOne()

  [PortAudio-internal, managed by the audio library]
  └── playback callback  — runs on PortAudio thread, drains playbackBuffer
  └── broadcast callback — runs on PortAudio thread, encodes + sends RTP
```

Context cancellation causes `receiveLoop`, `playoutLoop`, and `Run` to exit.
PortAudio streams are stopped/closed via `defer` in `Start`.  The pion
interceptor chain is shut down by `sess.close()` (also deferred).

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
  apply MicGain: v *= gain; clamp to [-1.0, 1.0]
  │
  ▼
  convert float32 → int16  (multiply by 32767)
  │
  ▼
  AudioEncoder.Encode(pcm []int16, buf []byte) → n bytes (Opus frame)
  │
  ▼
  rt.rtpSess.send(buf[:n])
    │
    ├─ packetizer.Packetize(payload, rtpFrameSamples=960)
    │    adds RTP header: V=2, PT=111, seq++, ts+=960, SSRC
    │
    └─ rtpWriter.Write(header, payload, nil)
         │ (through interceptor chain — updates SR stats)
         ▼
         baseRTPWriter → pkt.Marshal() → PacketWriter.Write() → UDP socket
```

### SSRC derivation

```
RtpID set? ──yes──► FNV-1a 32-bit hash → SSRC
     │
     no
     ▼
hostname available? ──yes──► FNV-1a 32-bit hash → SSRC
     │
     no
     ▼
localIP string ──────────► FNV-1a 32-bit hash → SSRC
```

### beginTransmission state machine

```
PTTDown (openvlm) or PTTToggle-when-idle (evdev)
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
PTTUp (openvlm) or PTTToggle-when-broadcasting (evdev)
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

`receiveLoop` runs in its own goroutine.  A companion `playoutLoop` goroutine
is always started alongside it and drives decoding at a 20 ms tick rate.

```
receiveLoop goroutine
  │
  │  rt.receiver.ReadFromUDP(buf[1500])
  ▼
  loopback check (when Loopback == false):
    src.IP.IsLoopback() || src.IP.String() == localIP → drop, continue
  │
  ▼
  parseIncomingRTP(buf[:n])    ← pion rtp.Packet.Unmarshal
    error → log.Debug "dropping non-RTP datagram", continue
  │
  ▼
  [if Trace] log seq/ts/ssrc/payload_bytes
  │
  ▼
  copy payload bytes (releases buf to next read)
  │
  ▼
  jitter.push(pkt.SequenceNumber, payload)
     false → stale / duplicate / buffer full → silently discarded
     true  → stored in jitter buffer map[uint16][]byte
              (receiveLoop does NOT decode; playoutLoop owns that)

  ┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄ separate goroutine ┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄

playoutLoop goroutine    (ticker: 20 ms)
  │
  ├─ isBroadcasting(rt)?  true → skip tick (mic is transmitting, no playback)
  │
  ├─ jitter.popReady()
  │    case ready == true    → decodeAndQueue(rt, payload)
  │    case skipped == true  → decodeAndQueuePLC(rt)   (gap: advance seq)
  │    case neither          → jitter.shouldConceal(100 ms)?
  │                              true  → jitter.advancePast()
  │                                      decodeAndQueuePLC(rt)
  │                              false → nothing (stream idle / not started)
  │
  └─ decodeAndQueue / decodeAndQueuePLC:
       AudioDecoder.Decode(payload or nil, pcm[960])  ← nil → PLC
       int16 → float32 (÷ 32768)
       select { case playbackBuffer <- []float32: | default: drop + warn }
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

### advancePast()

Called by `playoutLoop` after emitting a PLC frame due to `shouldConceal`.
Discards any late-arriving original for `expected` and increments `expected`,
maintaining the invariant of exactly one frame produced per playout tick.

### Sequence number wrap-around

`seqLess(a, b)` casts `(a-b)` to `int16` — a negative result means `a < b`
in sequence space:

```
seqLess(a, b) = int16(a-b) < 0

Examples:
  seqLess(65535, 0) = int16(65535) = -1 → true  (65535 is "before" 0)
  seqLess(0, 65535) = int16(1)     =  1 → false
  seqLess(100, 100) = int16(0)     =  0 → false
```

---

## 9. Comm Event System

### PTTEvent type

```go
type PTTEvent uint8

const (
    PTTDown   PTTEvent = iota  // 0: hold-to-talk press (openvlm GPIO3 HIGH)
    PTTUp                       // 1: hold-to-talk release (openvlm GPIO3 LOW)
    PTTToggle                   // 2: press-to-toggle (evdev key press)
)
```

### EventSource interface

```go
type EventSource interface {
    Events(ctx context.Context) <-chan PTTEvent
}
```

Implementations return a channel that is closed when `ctx` is cancelled,
causing `Run` to exit its `select` loop cleanly.

### openvlmSource (default)

Reads HID input reports from an OpenVLM (Open Voice Link Module) USB dongle.  GPIO3 (IR1 bit 2)
maps to PTT state: HIGH → `PTTDown`, LOW → `PTTUp`.

```
openvlmSource.Events(ctx) goroutine
  │
  ├─ opener(openvlmVendorID, openvlmProductID) → HIDDevice
  │    error → log error, close channel, return
  │
  ├─ context.AfterFunc(ctx, closeDevice)  ← unblocks dev.Read on cancel
  │
  └─ loop:
       dev.Read(buf[5])
       if ctx.Err() != nil → return
       payloadStart = (n >= 5) ? 1 : 0
       ir1 = buf[payloadStart+1]
       gpio3 = (ir1 & 0x04) != 0
       gpio3 == prevGPIO3 → skip (no transition)
       gpio3 HIGH (false→true) → ch <- PTTDown
       gpio3 LOW  (true→false) → ch <- PTTUp
```

`HIDDevice` and `HIDOpener` are interfaces so unit tests inject a mock without
reaching real hardware.  `defaultHIDOpener` calls `hid.Init` + `hid.Open` and
wraps the result in `hidDeviceWrapper` which calls `hid.Exit` on `Close`.

### evdevSource

Emits only `PTTToggle` — there is no hold-to-talk in the evdev implementation.

```
evdevSource.Events(ctx) goroutine
  │
  │  dev.ReadOne() → InputEvent
  │
  ├─ ev.Type != EV_KEY → skip
  │
  ├─ key match?
  │    CommKey == "any"                         → match
  │    CommKey is decimal code == ev.Code       → match
  │    else                                     → skip
  │
  └─ ev.Value == 1 (press) → ch <- PTTToggle
     ev.Value == 0 (release) → log debug only (no event emitted)
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
           PTTDown          → beginTransmission(rt)
           PTTUp            → endTransmission(rt)
           PTTToggle        → isBroadcasting(rt)?
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
- Test: `mockEncoder` (not shown in code — encoder tests use real Opus)

### AudioDecoder (`codec.go`)

```go
type AudioDecoder interface {
    Decode(data []byte, pcm []int16) (int, error)
}
```

- `Decode(nil, pcm)` triggers Opus Packet Loss Concealment
- Production: `opusDecoder` wrapping `*opus.Decoder`
- Test: `mockDecoder` — fills PCM with a fixed `int16` value; supports error injection and explicit return-N

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

- Production: `*swappableSender` wrapping a `*net.UDPConn` (via the RTP session)
- Test: `mockWriter` — accumulates packets in `Packets [][]byte`
- Concurrent test: `safeMockWriter` — mutex-protected variant for race-detector tests

### PacketReader (`transport.go`)

```go
type PacketReader interface {
    ReadFromUDP(b []byte) (int, *net.UDPAddr, error)
    Close() error
}
```

- Production: `*swappableReceiver` wrapping a `*net.UDPConn`
- Test: `mockReader` — pre-seeded packet queue; blocks on `select{}` then errors on `Close`

### rtpSender (`rtp.go`)

```go
type rtpSender interface {
    send(payload []byte) error
}
```

- Production: `*pionRTPSession`
- Test: `mockRTPSender` — accumulates payloads for assertion

### HIDDevice (`openvlm.go`)

```go
type HIDDevice interface {
    Read(b []byte) (int, error)
    Close() error
}
```

- Production: `hidDeviceWrapper` wrapping `*hid.Device` (calls `hid.Exit` on close)
- Test: injectable mock via `HIDOpener`

### swappableSender / swappableReceiver (`transport.go`)

Concrete wrappers that support runtime multicast endpoint changes without
blocking the hot I/O path:

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

`swap` can execute concurrently with any number of `Write` or `ReadFromUDP`
calls without blocking them for more than a pointer snapshot.  Closing the old
socket after the swap unblocks any in-flight `ReadFromUDP` on the old
connection, causing `receiveLoop` to loop and immediately read from the new
socket.

### EventSource (`event.go`)

```go
type EventSource interface {
    Events(ctx context.Context) <-chan PTTEvent
}
```

- Production: `openvlmSource` (default) or `evdevSource`
- Test: `mockEventSource` (in test files) — pre-seeded `chan PTTEvent`

---

## 11. Extending the Package

### Adding a new control source

1. Create a new file and implement `EventSource`:

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
            // read hardware event…
            ch <- PTTDown  // or PTTUp / PTTToggle
        }
    }()
    return ch
}
```

2. Register it in `buildEventSource()` in `comms.go`:

```go
func (cfg *CommsConfig) buildEventSource() (EventSource, error) {
    switch cfg.ControlSource {
    case "my_source":
        return newMySource(cfg), nil
    case defaultControlSourceEvdev:
        dev := cfg.findCommDevice()
        if dev == nil {
            return nil, errors.New("comms: comm device not found")
        }
        return NewEvdevSource(dev, cfg.CommKey, cfg.Log), nil
    default: // "openvlm"
        cfg.Log.Info().Msg("comms: PTT on OpenVLM HID dongle")
        return NewOpenVLMSource(cfg.Log), nil
    }
}
```

3. Add the new value to `normalizeControlSource()` in `device.go`.

4. Add any required `CommsConfig` fields and copy them in `NewComms`.

5. Write unit tests using the existing mock pattern (`mockEventSource` in `mocks_test.go`).

### Switching to hold-to-talk semantics with evdev

The `Run` loop already handles `PTTDown` and `PTTUp` separately.  To add
hold-to-talk to the evdev backend, emit `PTTDown` on key press and `PTTUp`
on key release instead of `PTTToggle`.  No changes to `Run`,
`beginTransmission`, or `endTransmission` are required.

### Replacing the codec

Implement `AudioEncoder` and `AudioDecoder`, then wire them through
`buildCodec` in `comms.go`.  The rest of the pipeline — PortAudio callbacks,
RTP session, jitter buffer, and playout loop — is codec-agnostic.

### Changing the multicast endpoint at runtime

Use the public `UpdateMulticastEndpoint(addr, port)` function:

```go
if err := comms.UpdateMulticastEndpoint("239.255.0.1", 5010); err != nil {
    log.Error().Err(err).Msg("failed to change comms multicast endpoint")
}
```

```
UpdateMulticastEndpoint(addr, port)
  │
  ├─ guard: activeConfig nil or runtime == nil?  → error "not running"
  ├─ validate: valid IPv4 multicast address?
  ├─ validate: port in [1, 65535]?
  │
  ├─ save old McastAddr / McastPort on CommsConfig
  ├─ update McastAddr / McastPort
  ├─ buildNetwork()  → newSender, newReceiver, rawRTCPSender, newLocalIP
  │    error → roll back McastAddr/McastPort, return error
  │
  ├─ newPionRTPSession(ssrc, newSender, newRTCPSender, log)
  │    error → close new sockets, roll back, return error
  │
  └─ replaceNetwork(rt, newSender, newReceiver, newLocalIP)
       ├─ rt.receiver.swap(newReceiver)   ← receiveLoop reads from new socket
       ├─ rt.sender.swap(newSender)       ← broadcast callback writes to new socket
       ├─ rt.rtpSess = newSession         ← new Packetizer + interceptor chain
       ├─ rt.localIP.Store(newLocalIP)    ← atomic.Value
       ├─ oldReceiver.Close()             ← unblocks in-flight ReadFromUDP
       └─ oldSender.Close()              ← if the type implements io.Closer
```

---

## 12. Testing Strategy

The package is fully testable without real hardware because every external
dependency is hidden behind one of the interfaces listed in §10.

### Test file map

| File | Tests |
|---|---|
| `comms_test.go` | `NewComms`, `applyDefaults`, `applyDefaults` BluetoothAudioDeviceHint propagation |
| `receive_test.go` | `receiveLoop`, `playoutLoop`, `decodeAndQueue`, `decodeAndQueuePLC` |
| `transmit_test.go` | `beginTransmission`, `endTransmission`, `isBroadcasting`, `drainPlaybackBuffer`, `Run` dispatch |
| `rtp_test.go` | `ssrcFromID`, `pionRTPSession.send`, `parseIncomingRTP` |
| `jitter_test.go` | `push`, `popReady`, `advancePast`, `shouldConceal`, `seqLess` |
| `event_test.go` | `evdevSource.Events`, `PTTEvent` constants |
| `openvlm_test.go` | `openvlmSource.Events` (mock HID); `detectAndSetALSACardFromRoot` |
| `codec_test.go` | `newOpusEncoder`, `newOpusDecoder` round-trip |
| `transport_test.go` | `swappableSender`, `swappableReceiver`, concurrent swap |
| `alsa_test.go` | `silenceALSAProbeNoise`, `restoreALSAErrorHandler` (no-op smoke test) |
| `mocks_test.go` | All shared mock implementations |

### Mock injection pattern

Tests assemble a `CommsRuntime` directly with mock implementations rather than
calling `Start`:

```go
rt := &CommsRuntime{
    decoder:         &mockDecoder{fillValue: 8192},
    sender:          newSwappableSender(&mockWriter{}),
    receiver:        newSwappableReceiver(newMockReader(mockPacket{data: rtpBytes, src: remoteAddr})),
    rtpSess:         &mockRTPSender{},
    broadcastStream: &mockStream{},
    playbackBuffer:  make(chan []float32, 8),
    beepBufferStart: make([]float32, frameSize),
    beepBufferStop:  make([]float32, frameSize),
}
rt.localIP.Store("10.0.0.1")
cfg := &CommsConfig{Log: zerolog.Nop(), Loopback: true}
```

`sender` and `receiver` are always wrapped in `swappableSender` /
`swappableReceiver` even in tests, so `replaceNetwork` (and `localIP` reads)
work correctly.

### cancelAfterDrain pattern for receiveLoop tests

`mockReader` pre-loads packets and blocks (then errors on `Close`) when
exhausted.  `cancelAfterDrain` polls the reader's queue in a goroutine and
cancels the context once it is empty, allowing `receiveLoop` to exit cleanly:

```
test goroutine        cancelAfterDrain goroutine     receiveLoop goroutine
     │                       │                              │
     │ go receiveLoop(ctx)   │                              │
     │──────────────────────────────────────────────────►  │
     │                       │ poll len(reader.packets)     │
     │                       │──────────────────────────►  │ ReadFromUDP → pkt
     │               empty   │                              │ (push to jitter)
     │                       │ cancel(); reader.Close()     │
     │                       │                              │ ReadFromUDP → error
     │                       │                              │ ctx.Done() → return
     │◄─────────────────────────────────────────────────── │ (exited)
```

### Concurrency tests

Tests that fire many goroutines use `safeMockWriter` instead of `mockWriter`
to ensure the race detector reports genuine races in production code rather
than races inside an unsynchronised mock:

```go
wOld := &safeMockWriter{}
wNew := &safeMockWriter{}
s    := newSwappableSender(wOld)
// … launch goroutines calling s.Write() …
s.swap(wNew)
// wOld.count() + wNew.count() must equal total goroutine writes
```

### Testing OpenVLM without hardware

`openvlmSource` accepts a `HIDOpener` function via `newOpenVLMSourceWithOpener`.
Tests provide a factory that returns a mock `HIDDevice` pre-loaded with
deterministic GPIO3 state sequences:

```go
opener := func(_, _ uint16) (HIDDevice, error) {
    return &mockHIDDevice{reports: [][]byte{
        {0, 0x00, 0x04, 0, 0},  // IR1 bit 2 HIGH  → PTTDown
        {0, 0x00, 0x00, 0, 0},  // IR1 bit 2 LOW   → PTTUp
    }}, nil
}
src := newOpenVLMSourceWithOpener(opener, zerolog.Nop())
```

### Testing transmission timing

`beginTransmission` calls `time.Sleep(200 ms)` internally.  Tests that need
to observe the post-sleep state use:

```go
go cfg.beginTransmission(rt)
time.Sleep(400 * time.Millisecond)  // > 200 ms to ensure completion
```

The sleep is a deliberate audio UX delay, not a synchronisation primitive.
Tests accept the wall-clock cost.

### Testing Run() dispatch

A pre-seeded `PTTEvent` channel is closed after injection to drive `Run` to
exit quickly:

```go
evCh := make(chan PTTEvent, 1)
evCh <- PTTDown
src := &mockEventSource{ch: evCh}

ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
defer cancel()
go cfg.Run(ctx, rt, src)
time.Sleep(450 * time.Millisecond)
// assert rt.broadcasting and mock stream call counts
```

Closing `evCh` causes `Run` to return immediately via the `!ok` branch without
waiting for a timeout.
