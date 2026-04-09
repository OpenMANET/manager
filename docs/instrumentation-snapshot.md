# Instrumentation Snapshot

openmanetd can periodically capture a JSON "snapshot" of every runtime
counter and metric it maintains and write it to disk. An operator can
pull the latest file off a device to troubleshoot audio glitches, goroutine
leaks, jitter buffer pressure, and other runtime issues — or hand the file
directly to an LLM for triage.

This document is the field reference for the snapshot format. The
authoritative schema is the Go type `internal/instrumentation.Envelope`;
this markdown is the human- and LLM-readable companion. Field names here
match the JSON keys exactly.

## Enabling snapshots

Set the following keys in `/etc/openmanetd/config.yml` and restart the
daemon:

```yaml
instrumentation:
  enable: true          # start the periodic worker at boot (default: false)
  intervalSecs: 60      # capture period in seconds (default: 60)
  snapshotDir: /tmp     # directory the worker writes snapshot files into
```

When `enable` is `false` (the default) the snapshot worker is never
started, the registry is not constructed, and the daemon pays no runtime
cost beyond a handful of function pointers.

When enabled, the worker writes one file per tick named
`openmanetd-snapshot-<unix-nanos>.json`. The first snapshot is written
immediately at daemon boot so operators don't have to wait a full interval.

**The worker does not rotate or clean up old files.** `/tmp` on most
OpenMANET target devices is a tmpfs mount, so unbounded snapshot
accumulation consumes RAM. Pick an interval that matches your deployment
(e.g. 300 for once-per-five-minutes on a node left to run overnight) and
clean up old files manually when you're done capturing.

## Top-level envelope

Every snapshot is a single JSON object with this shape:

```json
{
  "schema_version": "1.0.0",
  "captured_at_start": "2026-04-09T12:34:56.789012345Z",
  "captured_at_end":   "2026-04-09T12:34:56.789013101Z",
  "daemon": { ... },
  "runtime": { ... },
  "sections": [
    { "name": "comms", "data": { ... } },
    { "name": "blos",  "data": { ... } }
  ]
}
```

| Field | Type | Meaning |
|---|---|---|
| `schema_version` | string | Semver of the envelope schema. Bump minor for additive fields, major for breaking changes. The current value is `1.0.0`. |
| `captured_at_start` | RFC3339 timestamp | Wall-clock time when the capture loop began reading counters. |
| `captured_at_end` | RFC3339 timestamp | Wall-clock time when the capture loop finished. The difference `captured_at_end - captured_at_start` bounds the counter-read skew window; in practice this is microseconds. |
| `daemon.version` | string | openmanetd build version. Empty until the build system populates it. |
| `daemon.hostname` | string | The result of `os.Hostname()` at daemon startup. |
| `daemon.pid` | int | Unix process ID. |
| `daemon.started_at` | RFC3339 timestamp | When the instrumentation registry was constructed (approximately daemon startup). |
| `runtime` | object | Go runtime stats. See **Runtime** below. |
| `sections` | array of `{name, data}` | Per-subsystem counter payloads. See **Sections** below. |

## Runtime

Go runtime statistics collected via `runtime.ReadMemStats` and
`runtime.NumGoroutine`. A rising `num_goroutine` across successive
snapshots typically indicates a goroutine leak; cross-reference with
`/debug/pprof/goroutine` if pprof is enabled.

| Field | Type | Unit | Meaning |
|---|---|---|---|
| `num_goroutine` | int | count | Total live goroutines at capture time. |
| `mem_alloc_bytes` | uint64 | bytes | Bytes of allocated heap objects (same as `runtime.MemStats.Alloc`). |
| `mem_sys_bytes` | uint64 | bytes | Total memory obtained from the OS. |
| `heap_inuse_bytes` | uint64 | bytes | Bytes in in-use spans. |
| `stack_inuse_bytes` | uint64 | bytes | Bytes used by stack spans. |
| `num_gc` | uint32 | count | Number of completed GC cycles since process start. |
| `last_gc_unix_nano` | int64 | unix-nanoseconds | Time of the most recent GC cycle, 0 if none. |
| `gc_pause_last_ns` | uint64 | nanoseconds | Duration of the most recent GC stop-the-world pause. |

## Sections

Each section is `{ "name": "<producer>", "data": <producer-defined> }`.
A producer is a named subsystem in the daemon; the snapshot framework
makes no assumptions about the shape of its `data`. Sections are
documented below in the order they are registered.

### `comms` — audio and RTP pipeline

The comms subsystem owns multicast audio over RTP with Opus encoding.
Counters are updated from real-time audio threads via `sync/atomic`,
so reading them does not stall the TX or RX paths.

```json
{
  "enabled": true,
  "broadcasting": false,
  "remote_rx_active": false,
  "control_source": "openvlm",
  "broadcast_encoder": { ... },
  "web_bridge": { ... },
  "ports": [ ... ]
}
```

| Field | Type | Meaning |
|---|---|---|
| `enabled` | bool | `true` when the comms runtime is published. When `false`, every other field in this section is zero by construction and should be read as "subsystem off", not "subsystem broken". |
| `broadcasting` | bool | `true` when the TX gate is currently open (mid-PTT). |
| `remote_rx_active` | bool | `true` when the half-duplex cache reports a remote packet was received recently; TX is blocked while this is set. |
| `control_source` | string | Active PTT control source: `openvlm`, `nanoptt`, `web`, or `roip`. |
| `broadcast_encoder` | object | TX-side audio encoder counters. |
| `web_bridge` | object | Web-mode RX bridge counters. |
| `ports` | array | Per-talk-group counters. |

#### `comms.broadcast_encoder`

**All counters are scoped to the current PTT cycle and reset on
`SetTxEnabled(true)`.** A snapshot taken when `tx_enabled == false`
reflects the most recent completed cycle, not a cumulative lifetime.

| Field | Unit | Meaning |
|---|---|---|
| `frames_captured` | count | Audio frames delivered by the capture callback. Expected rate: 50/sec at a 20 ms frame cadence. |
| `frames_dropped` | count | Captured frames the audio callback could not hand off to the encode goroutine because the encode channel was full. **Any non-zero value during active TX means the encode loop is behind.** |
| `frames_encoded` | count | Successfully Opus-encoded and forwarded frames. In a healthy cycle `frames_encoded ≈ frames_captured - frames_dropped`. |
| `encode_errors` | count | Opus `EncodeS16` failures. Should stay at 0. |
| `capture_gap_max_ns` | nanoseconds | Maximum observed inter-arrival gap between successive capture callbacks. Values substantially larger than 20,000,000 (20 ms) indicate audio-thread preemption. |
| `capture_late_count` | count | Number of capture callbacks whose arrival was late relative to the previous callback. |
| `encode_dur_max_ns` | nanoseconds | Maximum single-frame encode duration in the current cycle. Compare to the 20,000,000 ns frame budget — values near the budget imply a hot CPU. |
| `encode_dur_sum_ns` | nanoseconds | Sum of encode durations. |
| `encode_dur_count` | count | Number of encode durations summed. Use `encode_dur_sum_ns / encode_dur_count` for the mean. |
| `last_capture_ns` | unix-nanoseconds | Timestamp of the most recent capture callback. 0 means no callback since the last gate reset. |
| `tx_enabled` | bool | Reflects the `txEnabled` atomic gate. When `false` the encoder pipeline is dormant. |
| `over_budget_warned` | bool | Set the first time the encoder observes an encode duration that exceeds the 20 ms frame budget in the current cycle. |

#### `comms.web_bridge`

Counts frames flowing from the per-port jitter buffer into the web-mode
audio bridge that ships them to browser clients.

| Field | Unit | Meaning |
|---|---|---|
| `rx_push_in` | count | Monotonic count of frames offered to the bridge (every `PushRxFrame` invocation). |
| `rx_push_drop` | count | Monotonic count of frames dropped because the bridge's internal channel was full. Compute `rx_push_drop / rx_push_in` — sustained ratios above ~1% mean the browser client is not draining RX fast enough. |

#### `comms.ports[*]`

One entry per configured multicast talk group. The slice order mirrors
`CommsRuntime.Ports`.

| Field | Unit | Meaning |
|---|---|---|
| `address` | string | Multicast group address (e.g. `239.0.0.1`). |
| `port` | int | UDP port. |
| `send_enabled` | bool | Runtime toggle — if `false`, the TX path skips this talk group. |
| `receive_enabled` | bool | Runtime toggle — if `false`, incoming RTP is not pushed into the jitter buffer. |
| `playback_underruns` | count | Number of playback-side decode failures that had to recover via PLC (packet loss concealment). |
| `rx_pkts` | count | Monotonic count of successful `ReadFromUDP` returns on this port's receive socket (packets the kernel handed userspace). |
| `rx_loopback` | count | Packets dropped by the loopback filter (own-IP suppression) before reaching the RTP parser. |
| `rx_parse_errs` | count | Packets that failed `rtp.ParseIncoming`. Sustained nonzero deltas indicate a non-RTP sender aliasing the port. |
| `rx_pushed` | count | Packets that `PushWithSSRC` accepted into the jitter buffer. In a healthy stream, `rx_pushed ≈ rx_pkts - rx_loopback - rx_parse_errs`. |
| `rx_push_rejected` | count | Packets that `PushWithSSRC` rejected as stale-cursor, duplicate, or overflow. A sustained nonzero delta with `jitter.ssrc_resets` flat indicates a consumer-side cursor-advance bug or sender reordering. |
| `web_popped_skipped` | count | `webPlayoutLoop` observed the jitter buffer advancing past a missing sequence number (only happens when the buffer is half-full of out-of-order packets). Zero on the portaudio playout path. |
| `jitter.overflows` | count | Incoming packets rejected because the jitter buffer was full. Sustained non-zero deltas across snapshots mean the receiver is behind the sender or the network is bursting. |
| `jitter.ssrc_resets` | count | Mid-stream SSRC transitions the jitter buffer handled by resetting. High values (multiple per minute) suggest multiple talkers or sender restarts. |
| `jitter.idle_resets` | count | Gap-driven buffer resets (same SSRC, silence longer than the idle threshold). |
| `rx_gate.last_mark_unix_nano` | unix-nanoseconds | Timestamp of the most recent Mark call; 0 = never marked. |
| `rx_gate.threshold_ns` | nanoseconds | Half-duplex receive window. Default is 400 ms. |
| `rx_gate.active` | bool | `true` iff `last_mark_unix_nano` is within `threshold_ns` of now. |

### `blos` — BLOS / Tailscale overlay

```json
{
  "running": true
}
```

| Field | Meaning |
|---|---|
| `running` | `true` when the BLOS manager reports the Tailscale overlay is active. `false` covers both "disabled by config" and "enabled but stopped". |

## Interpretation heuristics for LLM triage

When a snapshot is provided for analysis, apply the following rules of
thumb in order and flag anything that fits.

1. **Encoder behind the budget.** If `comms.enabled` is true and
   `comms.broadcast_encoder.frames_dropped > 0` while
   `comms.broadcast_encoder.tx_enabled` is true, the encode goroutine
   cannot keep up with the 20 ms per-frame budget. Check
   `encode_dur_max_ns` against `20_000_000`. If close or over, the
   daemon is CPU-bound; if well under, the drop is from a transient
   scheduling stall on the encode goroutine.
2. **Capture thread preemption.** `comms.broadcast_encoder.capture_late_count > 0`
   or `capture_gap_max_ns ≫ 20_000_000` means the audio callback thread is
   being preempted. Look for CPU spikes or co-scheduled work.
3. **Mean encode time.** Compute `encode_dur_sum_ns / encode_dur_count`.
   A mean above ~5 ms on a full-sized device is a warning; above ~10 ms
   is a red flag.
4. **Jitter buffer pressure.** `comms.ports[*].jitter.overflows > 0`
   sustained across two snapshots means the receiver is dropping frames
   — either the sender is too fast or the network is bursting.
5. **SSRC churn.** `ssrc_resets` increasing by more than ~1 per minute
   suggests multiple talkers or a sender restart.
6. **Web RX starvation.** `rx_push_drop / rx_push_in > 1%` across two
   snapshots means the browser client is not draining received audio
   fast enough.
7. **Goroutine leak.** `runtime.num_goroutine` rising monotonically
   across successive snapshots is almost always a leak. Cross-reference
   with `/debug/pprof/goroutine` if pprof is enabled.
8. **GC pressure.** `runtime.gc_pause_last_ns > 20_000_000` (20 ms)
   means the most recent GC pause straddled an audio frame boundary and
   may have caused a stutter.
9. **RX push rejection.** `comms.ports[*].rx_push_rejected` increasing
   across two snapshots while `jitter.ssrc_resets` and `jitter.idle_resets`
   stay flat means packets are being rejected by the jitter buffer as
   stale-cursor — typically a sign of a consumer-side cursor-advance bug
   or severe sender reordering. Cross-reference with
   `comms.ports[*].rx_pkts` (did the kernel actually deliver the packets?)
   and `comms.ports[*].web_popped_skipped` (did `webPlayoutLoop` advance
   the cursor past a gap?).
10. **Comms disabled but expected.** If the user reports a broken PTT
   workflow but `comms.enabled == false`, the subsystem was either
   disabled in config or failed to start. Check daemon logs for the
   corresponding startup error.

## Skew note

The snapshot framework reads each atomic counter one at a time. Between
the first atomic load and the last, the system keeps running — so
derived values that combine multiple counters (e.g. `encode_dur_sum_ns /
encode_dur_count`) may be off by one frame. The window is bounded
exactly by `captured_at_end - captured_at_start`, typically a few
microseconds. When comparing deltas across two snapshots, treat values
that moved by less than ~50 as within the skew noise floor.

No producer-side locks are added for capture; this is an explicit
design trade-off to keep the audio hot paths untouched.

## Reusability

The `internal/instrumentation` package is self-contained and imports
only the Go standard library and zerolog. It can be lifted wholesale
into another repository — only the producer-side `Snapshotter` adapters
(in `internal/comms` and `internal/blos`) need to be adapted to the new
domain. See the package GoDoc for the contract.
