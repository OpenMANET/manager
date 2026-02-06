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
4. **PTT device**:
   - A matching input device is located via evdev.
   - Button press toggles transmission (start/stop mic stream).

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

The receive loop:

1. Reads UDP datagrams.
2. Optionally unwraps RTP.
3. Opus decodes into PCM.
4. Converts to `[]float32`.
5. Queues to the playback buffer.

## Multicast UDP

The sender is bound to the interface IP so outbound multicast egresses the chosen iface.
The receiver:

- Binds to `0.0.0.0:<port>` and then
- Joins the multicast group on the interface.

Loopback suppression:

- If `ptt.loopback` is false, packets from loopback or the local interface IP are dropped.

Trace logging:

- When `ptt.trace` is true, each UDP packet on the multicast port is logged with source, size, and RTP header fields when present.

## Protocol: UDP vs RTP

The protocol is controlled by `ptt.protocol` (`udp` or `rtp`), and normalized at startup.
Receive path auto-detects RTP by header (0x80/0x81) even if protocol is set to UDP.

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

## PTT device handling

The input device is selected by name from the `pttDevice` glob and `pttDeviceName`.

- If `ptt.pttKey` is `any`, any key press toggles PTT.
- Otherwise it matches a numeric key code.

On press:

1. Plays start tone.
2. Starts mic stream.

On release:

1. Stops mic stream.
2. Plays stop tone.

## Config keys

Example in `example_config.yml`:

```
ptt:
  enable: false
  mcastAddr: 224.0.0.1
  mcastPort: 5007
  protocol: udp
  rtpId: ""  # optional; defaults to hostname. set to ATAK device identifier for strict VX RTP compatibility
  pttKey: any
  debug: true
  trace: false
  loopback: true
  pttDevice: /dev/hidraw0/*
  pttDeviceName: Generic AB13X USB Audio
  inputDevice: ""   # optional; device name substring or index for capture
  outputDevice: ""  # optional; device name substring or index for playback
  playbackBuffer: 2 # optional; playback buffer depth
```

## Files of interest

- `ptt.go`: initialization, codec setup, audio streams, multicast sockets
- `comms.go`: RX loop, PTT input handling, start/stop logic
- `rtp.go`: RTP wrapping/unwrapping and protocol helpers
