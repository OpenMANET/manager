# Comms (Push-to-Talk) internals

This directory implements a multicast PTT audio pipeline in Go using PortAudio,
Opus, and the [pion](https://github.com/pion) RTP/RTCP stack.  It is the
successor to the `ptt` package and replaces hand-rolled RTP framing with
standard pion packetization and built-in RTCP Sender Report generation.

Build with `-tags omd_omit_comms` to exclude the entire package from the
binary (a stub `doc.go` kept behind the matching positive tag satisfies the
import graph).

---

## High-level flow

1. **Configuration** is loaded from `ptt.*` keys (see `internal/config/config.go`).
2. **Audio**:
   - Opus encoder/decoder are created with the VoIP profile.
   - PortAudio playback stream runs continuously.
   - PortAudio mic capture stream is opened/started on PTT press and stopped on release.
3. **Network**:
   - A UDP sender is dialled to the multicast group from the selected interface IP.
   - A UDP receiver listens on `0.0.0.0:<port>` and joins the multicast group.
   - A second UDP socket on `<port>+1` carries outbound RTCP Sender Reports.
4. **PTT control source** (selected by `controlSource`):
   - `openvlm` (default): monitors GPIO3 on an OpenVLM (Open Voice Link Module) USB HID dongle;
     GPIO3 HIGH → `PTTDown`, LOW → `PTTUp` (hold-to-talk).
   - `evdev`: monitors a Linux input device; key press → `PTTToggle`
     (press-to-toggle).

---

## Audio/codec parameters

These constants are defined in `comms.go`:

- Sample rate: **48 000 Hz**
- Channels: **1 (mono)**
- Frame size: **960 samples** (20 ms at 48 kHz)
- Opus bitrate: **32 000 bps**
- Opus complexity: **10**
- Packet loss percent: **30**
- In-band FEC: **disabled**
- DTX: **disabled**

### Playback device latency

The PortAudio output stream is opened with
`Latency = outDev.DefaultHighOutputLatency` (the host API's preferred
high-latency configuration). On Linux ALSA this is typically 30–60 ms of
internal device buffer depth, which gives the audio thread that much
scheduling slack before the DAC underruns. The callback chunk size stays at
`frameSize` (one Opus frame, 20 ms) so `playoutOneFrame` produces exactly one
frame per call as before.

This is the only layer of buffering that protects against playback-side OS
scheduling stalls — the Go-side jitter buffer (`pc.jitter`) sits upstream of
the DAC and cannot help once the audio thread is preempted. The two layers
absorb different classes of stutter:

| Class of stutter | Mitigated by |
|---|---|
| Network arrival jitter (out-of-order, late packets) | Go jitter prebuffer |
| Brief packet loss bursts | Opus PLC + jitter buffer |
| Playback-side OS scheduling stalls | PortAudio output device buffer |

The mic capture stream uses the default minimum latency: capture-side latency
only affects mouth-to-ear delay, not stutter, and the Opus encoder is happy to
consume late frames.

The actual granted latency (which may differ from the suggestion if the host
API clamps it) is logged at Debug level on stream open as
`comms: playback stream opened` with `requested_latency` and
`actual_output_latency` fields.

The mic callback:

1. Receives `[]float32` PCM from PortAudio.
2. Applies `MicGain` (clamp to ±1.0).
3. Converts to `[]int16`.
4. Opus-encodes into a byte slice.
5. Sends via `rt.rtpSess.send(payload)` — the pion Packetizer adds the RTP
   header and the interceptor chain appends RTCP SR stats.

The receive path:

1. Reads UDP datagrams from the multicast group.
2. Parses them as RTP using `pion/rtp.Packet.Unmarshal`.
3. Pushes the payload + sequence number into the jitter buffer.
4. The playout loop (`playoutLoop`) drains the jitter buffer at 20 ms ticks,
   applying PLC for gaps.
5. Opus decodes into PCM, converts to `[]float32`, queues to the playback channel.

---

## Multicast UDP

The sender is dialled from the interface IP so outbound multicast egresses the
chosen interface.  The receiver:

- Binds to `0.0.0.0:<port>` and then
- Joins the multicast group on the interface (`ipv4.NewConn.JoinGroup`).

An RTCP socket is opened on `<port>+1` (standard RTP port-pairing) for RTCP
Sender Reports only; inbound RTCP is not processed.

Loopback suppression:

- If `loopback` is `false`, packets from any loopback address or from the local
  interface IP are silently dropped.

Trace logging:

- When `trace` is `true`, each incoming RTP packet is logged with source
  address, sequence number, timestamp, SSRC, and payload size.

### Changing the multicast endpoint at runtime

`UpdateMulticastEndpoint` can be called from anywhere in the application to
move the live subsystem to a different multicast address or port without
restarting it:

```go
if err := comms.UpdateMulticastEndpoint("239.255.0.1", 5010); err != nil {
    log.Error().Err(err).Msg("failed to change multicast endpoint")
}
```

The function:
1. Validates that `addr` is an IPv4 multicast address and `port` is in `[1, 65535]`.
2. Opens a new UDP sender, receiver, and RTCP sender for the new endpoint.
3. Creates a fresh pion RTP session with the new sockets.
4. Atomically replaces the sender and receiver inside the running runtime.
5. Closes the old sockets (unblocking any in-flight `ReadFromUDP` immediately).
6. On error, leaves config and sockets unchanged (the old endpoint continues).

`McastAddr` and `McastPort` in `CommsConfig` are updated on success so
subsequent calls to `buildNetwork` use the new values.

---

## RTP / RTCP stack

The comms package uses [pion](https://github.com/pion) for all RTP/RTCP work:

- **`pion/rtp`** — `Packetizer` adds sequence numbers, timestamps, and the RTP
  header; `Packet.Unmarshal` parses inbound datagrams.
- **`pion/interceptor`** — interceptor chain sits between the Packetizer and the
  wire.  The only interceptor registered is:
  - `report.SenderInterceptor` (interval: 5 s) — generates outbound RTCP
    Sender Reports that give receivers clock reference and packet count.
- Inbound RTCP is **not** processed: in a multicast PTT topology there is no
  single feedback path and each transmission may have many simultaneous
  receivers.

RTP encapsulation details:

```
0               1               2               3
0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|V=2|P|X| CC=0 |M|  PT=111      |       sequence number         |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                         timestamp                             |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                            SSRC                               |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                       Opus payload…                           |
```

- **Payload type**: `111` (standard dynamic PT for Opus)
- **Clock rate**: `48 000 Hz`
- **MTU**: `1400 bytes`
- **SSRC**: FNV-1a hash of `RtpID` (falls back to hostname, then local IP)

### RTP ID and SSRC

`ptt.rtpId` controls SSRC derivation:

1. Uses `ptt.rtpId` if set.
2. Otherwise uses the system hostname.
3. If neither is available, falls back to the local interface IP.

SSRC is computed as the **FNV-1a 32-bit hash** of the chosen string.

---

## RTP jitter buffer

A sequence-number-ordered jitter buffer (`rtpJitterBuffer` in `jitter.go`)
smooths network reordering and provides Packet Loss Concealment:

- **Prebuffer**: waits for `jitterPrebufferPackets` (5) frames before beginning
  playout, absorbing early-arrival jitter (~100 ms safety margin).
- **Max depth**: drops newly arriving packets once `jitterMaxDepth` (24) frames
  are already buffered — prevents unbounded memory growth.
- **Gap detection**: if the expected sequence number is missing and ≥ half the
  max depth is occupied, the frame is skipped and PLC is applied.
- **Idle concealment**: `shouldConceal` returns true when any packet arrived
  within the last 200 ms (`concealRecentWindow`), allowing playout to generate
  Opus PLC frames during a gap in an otherwise active stream. Capped at
  `maxConsecutivePLC` (10 frames ≈ 200 ms) before falling back to silence.
- **Playout clock**: the PortAudio output callback drives playout directly,
  calling `playoutOneFrame` once per audio period (20 ms). One frame is
  produced per call regardless of whether a real payload or a PLC frame is used.

Sequence numbers use **uint16 wrap-around-aware** comparison (`seqLess`) so
streams that cross the 65 535 → 0 boundary are handled correctly.

---

## PTT control handling

### `openvlm` backend (default)

The OpenVLM (Open Voice Link Module) is a USB HID audio dongle widely used as a
push-to-talk controller.  The source maps GPIO3 in the HID report to PTT state:

| IR1 bit 2 (GPIO3) | Transition | PTTEvent emitted |
|---|---|---|
| LOW → HIGH | Button pressed | `PTTDown` |
| HIGH → LOW | Button released | `PTTUp` |

The HID report structure:

```
Byte 0: Report ID (prepended by OS — shifted by 1 when n ≥ 5)
Byte 1: IR0 (GPIO8–GPIO5)
Byte 2: IR1 (GPIO4–GPIO1) ← bit 2 = GPIO3 = PTT
Byte 3: IR2
Byte 4: IR3
```

ALSA card auto-detection runs before `portaudio.Initialize()` when
`controlSource` is `openvlm`: it scans `/proc/asound/card*/usbid` for
`0d8c:013c` and sets `ALSA_CARD` to the matching card number so PortAudio
selects the correct sound card.  If `ALSA_CARD` is already set, it is left
unchanged.

`PTTDown` → `beginTransmission`; `PTTUp` → `endTransmission`.

### `evdev` backend

The input device is selected by matching `NanoPTTDeviceName` against devices
discovered via the `NanoPTTDevicePath` pattern.

- If `commKey` is `any`, any key press emits `PTTToggle`.
- Otherwise the key code must match the decimal `EV_KEY` code.

On each matching **key press** (`EV_KEY` value = 1), a `PTTToggle` event is
emitted.  The `Run` loop checks current broadcasting state and calls
`beginTransmission` or `endTransmission` accordingly — a **press-to-toggle**
model.  Key releases are logged at debug level but produce no event.

### Transmission lifecycle

`beginTransmission`:

1. Acquires lock, sets `broadcasting = true`, releases lock.
2. `drainPlaybackBuffer()` — discards queued audio to avoid stale frames.
3. Queues the 1 000 Hz start-tone (0.2 amplitude, 20 ms = `frameSize` samples).
4. Sleeps 200 ms (lets the tone play before the mic opens).
5. Ensures `broadcastStream` is non-nil; reopens via `rt.reopenBroadcast` if nil.
6. Calls `broadcastStream.Start()`; on failure attempts one reopen-and-retry.

`endTransmission`:

1. Checks `broadcasting`; returns immediately if already idle (idempotent).
2. Calls `broadcastStream.Stop()`.
3. `drainPlaybackBuffer()`.
4. Queues the 600 Hz stop-tone (0.2 amplitude, 20 ms).
5. Acquires lock, sets `broadcasting = false`, releases lock.

---

## ALSA noise suppression

`alsa_silence.go` uses CGo to temporarily replace the ALSA error handler with
a no-op for the duration of `portaudio.Initialize()`.  PortAudio's ALSA backend
probes every virtual PCM alias in `/usr/share/alsa/alsa.conf` (rear, hdmi, etc.)
and logs "Unknown PCM" for each alias not present in the active card's profile.
These are expected probe failures, not real errors.  The default handler is
restored by `restoreALSAErrorHandler()` immediately after initialization.

---

## Config keys

Example in `example_config.yml`:

```yaml
ptt:
  enable: false
  mcastAddr: 224.0.0.1
  mcastPort: 5007
  rtpId: ""              # optional; defaults to hostname
  pttKey: any            # "any" or decimal EV_KEY code (evdev only)
  debug: true
  trace: false
  loopback: true
  pttDevice: /dev/hidraw0/*    # glob for evdev device enumeration (NanoPTTDevicePath)
  pttDeviceName: AllInOneCable # exact evdev device name (NanoPTTDeviceName)
  controlSource: openvlm         # openvlm (default) or evdev
  BluetoothAudioDeviceHint: ""          # optional shared substring for both input/output (e.g. "OpenVLM")
  BluetoothInputDevice: ""              # optional; device name substring or index for capture
  BluetoothOutputDevice: ""             # optional; device name substring or index for playback
  micGain: 8.0                 # float32; >1 amplifies, <1 attenuates
```

`pttDevice` / `pttDeviceName` are only relevant when `controlSource: evdev`.
`ALSA_CARD` auto-detection is only performed when `controlSource: openvlm`.

---

## Source files

| File | Responsibility |
|---|---|
| `comms.go` | `CommsConfig`/`CommsRuntime` structs; `NewComms`; `applyDefaults`; `buildCodec`, `buildNetwork`, `buildAudio`, `buildEventSource`; `replaceNetwork`; `UpdateMulticastEndpoint`; `Start` |
| `receive.go` | `receiveLoop`, `playoutLoop`, `decodeAndQueue`, `decodeAndQueuePLC` |
| `transmit.go` | `isBroadcasting`, `drainPlaybackBuffer`, `beginTransmission`, `endTransmission`, `Run` |
| `rtp.go` | `pionRTPSession` (pion Packetizer + interceptor chain); `ssrcFromID`; `parseIncomingRTP` |
| `jitter.go` | `rtpJitterBuffer`: sequence-ordered playout buffer with PLC gap detection |
| `event.go` | `PTTEvent` constants (`PTTDown`, `PTTUp`, `PTTToggle`); `EventSource` interface; `evdevSource` backend |
| `openvlm.go` | `openvlmSource`; `HIDDevice`/`HIDOpener` abstractions; `detectAndSetALSACard` |
| `device.go` | `normalizeControlSource`; `resolveAudioDevice`; `findCommDevice`; `getIfaceIPv4`; `joinMulticastGroup` |
| `codec.go` | `AudioEncoder`/`AudioDecoder` interfaces; Opus encoder/decoder constructors |
| `stream.go` | `AudioStream` interface; `portaudioStream` wrapper |
| `transport.go` | `PacketWriter`/`PacketReader` interfaces; `swappableSender`/`swappableReceiver` atomic-swap wrappers |
| `alsa_silence.go` | CGo helpers — `silenceALSAProbeNoise` / `restoreALSAErrorHandler` |
| `doc.go` | Package-level doc comment; build-tag stub for `omd_omit_comms` |
