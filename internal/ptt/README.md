# PTT (Push-to-Talk) internals

This directory implements a multicast PTT audio pipeline in Go using PortAudio and Opus.
It is intentionally low-level and aims for ATAK VX multicast compatibility when
`ptt.protocol` is set to `rtp` and `ptt.rtpId` matches the ATAK device identifier.

## High-level flow

1. **Configuration** is loaded from `ptt.*` keys (see `internal/config/config.go`).
2. **Audio**:
   - Opus encoder/decoder are created with VOIP profile.
   - PortAudio playback stream runs continuously.
   - PortAudio mic stream is opened and started/stopped by PTT events.
3. **Network**:
   - A UDP sender is bound to the selected interface IP.
   - A UDP receiver listens on `0.0.0.0:<port>` and joins the multicast group on the interface.
4. **PTT control source**:
   - `evdev` (default): monitors a Linux input device; each matching key press **toggles** transmission (press-to-toggle).
   - `bluealsa_xevent`: placeholder for a BlueALSA HFP vendor-event backend — not yet implemented.

## Audio/codec parameters

These constants are defined in `ptt.go`:

- Sample rate: **48000 Hz**
- Channels: **1 (mono)**
- Frame size: **960 samples** (20 ms at 48 kHz)
- Opus bitrate: **32000 bps**
- Opus complexity: **10**
- Packet loss percent: **30**
- In-band FEC: **disabled** (mirrors ATAK VX defaults)
- DTX: **disabled**

The mic callback:

1. Receives `[]float32` PCM from PortAudio.
2. Converts to `[]int16`.
3. Opus encodes into a byte slice.
4. Optionally wraps in RTP (see below).
5. Sends over UDP multicast.

The receive path:

1. Reads UDP datagrams.
2. In RTP mode, pushes frames into the jitter buffer and lets `rtpPlayoutLoop` drive playout.
3. In UDP mode, auto-detects and unwraps RTP framing when present, then decodes directly.
4. Opus decodes into PCM (with PLC for missing frames in RTP mode).
5. Converts to `[]float32`.
6. Queues to the playback channel.

## Multicast UDP

The sender is bound to the interface IP so outbound multicast egresses the chosen iface.
The receiver:

- Binds to `0.0.0.0:<port>` and then
- Joins the multicast group on the interface.

Loopback suppression:

- If `ptt.loopback` is false, packets from any loopback address (`127.x.x.x`) or from
  the local interface IP are dropped.

Trace logging:

- When `ptt.trace` is true, each UDP packet on the multicast port is logged with source, size, and RTP header fields when present.

### Changing the multicast endpoint at runtime

Use `UpdateMulticastEndpoint` from anywhere in the application to move to a different
multicast address or port without restarting the subsystem:

```go
if err := ptt.UpdateMulticastEndpoint(cfg, "239.255.0.1", 5010); err != nil {
    log.Error().Err(err).Msg("failed to change multicast endpoint")
}
```

The function:
1. Validates that `addr` is an IPv4 multicast address and `port` is in `[1, 65535]`.
2. Opens a new pair of UDP sockets for the new endpoint.
3. Atomically replaces the sender and receiver inside the running runtime.
4. Closes the old sockets (which unblocks the receive goroutine immediately).
5. On any error, leaves the config and sockets unchanged.

The address and port in `PTTConfig` (`McastAddr`/`McastPort`) are also updated on
success, so subsequent calls to `buildNetwork` use the new values.

## Protocol: UDP vs RTP

The protocol is controlled by `ptt.protocol` (`udp` or `rtp`), and normalized at startup.
The receive path auto-detects RTP by its version byte (`0x80`/`0x81`) even when protocol
is set to `udp`.

### Why pick one over the other?

- Use **UDP** when both endpoints are under your control and you want the simplest path.
- Use **RTP** when you need interoperability with systems that expect RTP metadata
  (sequence/SSRC), or want to use the jitter buffer and PLC on the receive path.

Practical tradeoffs:

- **UDP advantages**
  - Smallest packet overhead.
  - Simplest send/receive behavior and easier debugging.
- **UDP downsides**
  - No explicit sequence numbers or sender identity in the packet header.

- **RTP advantages**
  - Includes sequence numbers and SSRC for receiver-side stream tracking.
  - Better fit for ecosystems that expect RTP.
  - Drives the jitter buffer and PLC on the receive path.
- **RTP downsides**
  - Extra header overhead.
  - Interop can vary by implementation details (timestamp/SSRC handling).

Current recommendation in this codebase:

- Start with **`protocol: udp`** for the simplest setup.
- Switch to **`protocol: rtp`** when compatibility with an RTP-expecting peer is needed,
  or to benefit from the jitter buffer and packet-loss concealment.

### UDP mode (default)

Raw Opus payload is sent in the UDP datagram.

### RTP mode

Each Opus payload is prefixed with a 12-byte RTP header:

```
0               1               2               3
0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|V=2|P|X| CC=0 |M|   PT=0        |       sequence number         |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                         timestamp                             |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                            SSRC                               |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                         opus payload...
```

Implementation details (matching ATAK VX multicast behavior):

- **Version**: `0x80` (V=2, no extensions, no CSRCs)
- **Payload type**: `0`
- **Sequence**: increments per packet
- **Timestamp**: `uint32(time.Now().Unix())` (seconds)
- **SSRC**: FNV-1a hash of `ptt.rtpId` (falls back to hostname, then local IP if unset)

### RTP ID and SSRC

`ptt.rtpId` controls how the **SSRC** is derived. SSRC is used by receivers to group
packets from the same talker. In VX multicast, SSRC is derived from an internal device
identifier, so matching that identifier yields the closest compatibility.

This implementation:

1. Uses `ptt.rtpId` if set.
2. Otherwise uses the system hostname.
3. If neither is available, falls back to the local IP.

SSRC is computed as **FNV-1a hash** of the chosen ID string.

Receive logic:

- RTP packets are detected by the first byte `0x80` or `0x81`.
- If protocol is set to `rtp`, packets missing a valid RTP header are dropped.
- If protocol is set to `udp`, RTP packets are still accepted and unwrapped.

## RTP jitter buffer

When `protocol: rtp` is active, a sequence-number-ordered jitter buffer (`rtpJitterBuffer`
in `jitter.go`) smooths out network reordering and provides Packet Loss Concealment (PLC):

- **Prebuffer**: waits until `rtpJitterPrebufferPackets` (3) frames are queued before
  beginning playout, to absorb early arrival jitter.
- **Max depth**: drops packets once `rtpJitterMaxDepth` (24) frames are buffered.
- **Gap detection**: if the expected sequence number is missing and at least half the
  buffer depth is occupied, the frame is skipped and PLC is applied.
- **Idle concealment**: `shouldConceal` returns true when a packet arrived within the
  last 100 ms, enabling the playout loop to generate comfort noise during a gap in an
  otherwise active stream.
- **Playout clock**: `rtpPlayoutLoop` ticks at 20 ms and drives one `popReady` per tick.

Sequence numbers use **uint16 wrap-around-aware** comparison (`seqLess`) so streams
that cross the 65535→0 boundary are handled correctly.

## PTT control handling

### `evdev` backend (default)

The input device is selected by matching `pttDeviceName` against devices discovered
by the `pttDevice` glob pattern.

- If `ptt.pttKey` is `any`, any key press toggles PTT.
- Otherwise it matches a numeric EV_KEY code (decimal).

On each matching **key press** (`EV_KEY` value=1), a `PTTToggle` event is emitted.
The `Run` loop checks the current broadcasting state and calls `beginTransmission` or
`endTransmission` accordingly — making this a **press-to-toggle** interaction model
rather than a hold-to-talk model.

`beginTransmission`:

1. Sets `broadcasting = true`.
2. Drains the playback buffer.
3. Queues the 1000 Hz start tone (0.2 amplitude, 20 ms).
4. Waits 200 ms so the tone has time to play.
5. Ensures the mic stream is open (calls `reopenBroadcast` if nil or on start failure).
6. Starts the mic capture stream.

`endTransmission`:

1. Stops the mic capture stream.
2. Drains the playback buffer.
3. Queues the 600 Hz stop tone (0.2 amplitude, 20 ms).
4. Sets `broadcasting = false`.

### `bluealsa_xevent` backend (placeholder)

This backend is **not yet implemented** (`xevent.go` is currently empty). When implemented
it will parse BlueALSA HFP vendor events (`AT+XEVENT=PTT_DOWN/PTT_UP`) and emit
`PTTDown`/`PTTUp` events for hold-to-talk semantics. This mode is useful for
speaker-mics that do not surface usable evdev key events.

## Bluetooth speaker-mic setup (BS-22 style)

The Bluetooth mic/speaker path was created with **BlueALSA** and explicit ALSA PCM names,
so `openmanetd` can target stable device names instead of changing card indexes.

1. Pair/connect the device in `bluetoothctl`.
2. Run BlueALSA with AG profiles (so HFP/HSP SCO audio + vendor AT events are available):

```bash
sudo systemctl edit bluealsa
# set ExecStart to include:
# /usr/bin/bluealsa -p hfp-ag -p hsp-ag
sudo systemctl daemon-reload
sudo systemctl restart bluealsa
```

3. Define named ALSA PCMs in `/etc/asound.conf` (replace `XX:XX:XX:XX:XX:XX` with your BT MAC):

```conf
pcm.bs22_out {
  type plug
  slave.pcm {
    type bluealsa
    interface "hci0"
    profile "sco"
    device "XX:XX:XX:XX:XX:XX"
  }
}

pcm.bs22_in {
  type plug
  slave.pcm {
    type bluealsa
    interface "hci0"
    profile "sco"
    device "XX:XX:XX:XX:XX:XX"
  }
}
```

4. Point PTT config to those names:

```yaml
ptt:
  controlSource: bluealsa_xevent
  inputDevice: bs22_in
  outputDevice: bs22_out
```

Notes:

- `controlSource: bluealsa_xevent` will handle PTT events from BlueALSA vendor events
  once the backend is implemented.
- `inputDevice` and `outputDevice` carry voice audio over SCO (mono/narrowband headset path).
- If you use `audioDeviceHint`, leave `inputDevice`/`outputDevice` empty so hint matching is applied.

## Config keys

Example in `example_config.yml`:

```yaml
ptt:
  enable: false
  mcastAddr: 224.0.0.1
  mcastPort: 5007
  protocol: udp
  rtpId: ""         # optional; defaults to hostname. Set to ATAK device identifier for strict VX RTP compatibility.
  pttKey: any
  debug: true
  trace: false
  loopback: true
  pttDevice: /dev/hidraw0/*    # glob pattern for evdev device enumeration (maps to PTTDeviceGlob)
  pttDeviceName: AllInOneCable # exact evdev device name to open
  controlSource: evdev         # evdev (default) or bluealsa_xevent (not yet implemented)
  audioDeviceHint: ""          # optional shared substring applied to BOTH input/output devices (e.g. "BS-22")
  inputDevice: ""              # optional; device name substring or index for capture
  outputDevice: ""             # optional; device name substring or index for playback
  playbackBuffer: 2            # decoded-audio channel depth
```

## Source files

| File | Responsibility |
|---|---|
| `ptt.go` | `PTTConfig`/`PTTRuntime` structs; `applyDefaults`; `buildCodec`, `buildNetwork`, `buildAudio`, `buildEventSource`; `Run`; `Start`; `replaceNetwork`; `UpdateMulticastEndpoint` |
| `comms.go` | `receiveLoop`, `rtpPlayoutLoop`, `decodeAndQueue`, `decodeAndQueuePLC`, `beginTransmission`, `endTransmission` |
| `rtp.go` | `wrapRTP`, `unwrapRTP`, `parseRTPHeader`, `normalizeProtocol`, `rtpSSRCFromID` |
| `jitter.go` | `rtpJitterBuffer`: sequence-ordered playout buffer with PLC gap detection |
| `device.go` | `resolveAudioDevice`, `normalizeControlSource`, `getIfaceIPv4`, `findPTTDevice`, `joinMulticastGroup` |
| `event.go` | `PTTEvent` constants (`PTTDown`, `PTTUp`, `PTTToggle`); `EventSource` interface; `evdevSource` |
| `stream.go` | `AudioStream` interface and `portaudioStream` wrapper |
| `transport.go` | `PacketWriter` and `PacketReader` interfaces; `swappableSender` and `swappableReceiver` atomic-swap wrappers |
| `codec.go` | `AudioEncoder` and `AudioDecoder` interfaces; Opus encoder/decoder constructors |
| `xevent.go` | BlueALSA xevent backend — placeholder, not yet implemented |
