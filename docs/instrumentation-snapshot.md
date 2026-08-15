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
  "schema_version": "1.2.0",
  "captured_at_start": "2026-04-09T12:34:56.789012345Z",
  "captured_at_end":   "2026-04-09T12:34:56.789013101Z",
  "daemon": { ... },
  "runtime": { ... },
  "sections": [
    { "name": "comms",      "data": { ... } },
    { "name": "blos",       "data": { ... } },
    { "name": "sysupgrade", "data": { ... } }
  ]
}
```

| Field | Type | Meaning |
|---|---|---|
| `schema_version` | string | Semver of the envelope schema. Bump minor for additive fields, major for breaking changes. The current value is `1.2.0`. |
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
  "fec_adapter": { ... },
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
| `fec_adapter` | object | Adaptive Opus FEC control-loop state. See **comms.fec_adapter** below. |
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
| `rx_push_in` | count | Monotonic count of frames offered to the bridge (every `PushRxFrame` invocation). Frames only reach the bridge while at least one consumer is attached — see `rx_gated_no_consumer`. |
| `rx_push_drop` | count | Monotonic count of frames discarded by the bridge: the *oldest* queued frame evicted when the ~200 ms channel is full (drop-oldest — the consumer resumes on fresh audio, never more than ~200 ms behind live), plus any stale frames flushed when the first consumer attaches. Compute `rx_push_drop / rx_push_in` — sustained ratios above ~1% mean the browser client is not draining RX fast enough. |
| `rx_gated_no_consumer` | count | Monotonic count of frames the playout drain discarded without offering to the bridge because no RPC stream was attached. Rising while `consumers` is 0 is normal idle web mode (an unattended node receiving traffic), not loss. |
| `consumers` | gauge | Number of `StreamAudioRx` RPC streams currently attached. When 0, `rx_push_in` stops advancing by design and `rx_gated_no_consumer` advances instead. |

#### `comms.fec_adapter`

Adaptive Opus FEC control loop. A single damped goroutine reads the
per-port `jitter.gap_runs_*` histogram every 2 s, maintains an EWMA
estimate of the channel loss ratio, and writes the Opus encoder's
packet-loss percentage to one of three levels (20, 30, 40) subject to
an operator-configured floor. The assumption is that this node's RX
loss is a good proxy for its own TX loss (true on omnidirectional
mesh links), so no inter-node feedback protocol is needed. See
`internal/comms/fec_adapter.go` for the state machine.

| Field | Unit | Meaning |
|---|---|---|
| `current_level` | perc (0-100) | The `packetLossPerc` the Opus encoder is currently set to. Always one of { `floor`, 30, 40 }. |
| `loss_ewma` | ratio (0.0-1.0) | The exponentially-weighted moving average of the observed loss ratio the controller is acting on. α = 0.2 at 2 s ticks gives a ~10 s response time. |
| `last_change_unix_nano` | unix-nanoseconds | Wall-clock timestamp of the most recent level change; 0 until the adapter has moved off its initial level. |
| `transitions` | count | Monotonic count of level changes since the adapter started. Rising rapidly means the network is flapping and the hysteresis bands may need widening. |
| `write_errors` | count | Monotonic count of `SetPacketLossPerc` calls that the Opus encoder rejected. Should stay at 0 in production; non-zero means something is wrong with the encoder state. |
| `floor` | perc (0-100) | The operator-configured lower bound from `comms.packetLossPerc`. The adapter will never drop below this value; it is also the initial level at startup. |

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
| `rx_push_rejected` | count | Packets that `PushWithSSRC` rejected as stale-cursor, duplicate, overflow, or oversized (payload larger than the RFC 6716 1275-byte frame cap — a conforming Opus sender never produces these). A sustained nonzero delta with `jitter.ssrc_resets` flat indicates a consumer-side cursor-advance bug, sender reordering, or a non-Opus sender aliasing the port. |
| `web_popped_skipped` | count | `webPlayoutLoop` observed the jitter buffer advancing past a missing sequence number (only happens when the buffer is half-full of out-of-order packets). Zero on the portaudio playout path. |
| `jitter.overflows` | count | Incoming packets rejected because the jitter buffer was full. Sustained non-zero deltas across snapshots mean the receiver is behind the sender or the network is bursting. |
| `jitter.ssrc_resets` | count | Mid-stream SSRC transitions the jitter buffer handled by resetting. High values (multiple per minute) suggest multiple talkers or sender restarts. |
| `jitter.idle_resets` | count | Gap-driven buffer resets (same SSRC, silence longer than the idle threshold). |
| `jitter.gap_runs_1` | count | Contiguous sequence-gap runs of length 1 frame (~20 ms) observed at the jitter buffer's skip-missing branch. Each run is counted exactly once at the skip point, not once per skipped frame. |
| `jitter.gap_runs_2_5` | count | Gap runs of 2–5 frames (40–100 ms). |
| `jitter.gap_runs_6_10` | count | Gap runs of 6–10 frames (120–200 ms). |
| `jitter.gap_runs_11_20` | count | Gap runs of 11–20 frames (220–400 ms). |
| `jitter.gap_runs_21_50` | count | Gap runs of 21–50 frames (420 ms–1 s). Runs longer than 31 cannot occur at the current `MaxDepth` of 32. |
| `jitter.gap_runs_over_50` | count | Gap runs of 51+ frames (>1 s). Will only fire if `MaxDepth` is raised above 50. |
| `rx_gate.last_mark_unix_nano` | unix-nanoseconds | Timestamp of the most recent Mark call; 0 = never marked. |
| `rx_gate.threshold_ns` | nanoseconds | Half-duplex receive window. Default is 400 ms. |
| `rx_gate.active` | bool | `true` iff `last_mark_unix_nano` is within `threshold_ns` of now. |

### `blos` — BLOS / Tailscale overlay

```json
{
  "running": true,
  "connected_since_unix_ns": 1713614400000000000,
  "backend_state": "Running",
  "peer_count": 4,
  "rx_bytes_total": 1048576,
  "tx_bytes_total": 524288,
  "rx_bps_60s": 1234.5,
  "tx_bps_60s": 678.9,
  "events_dropped": 0
}
```

| Field | Type | Unit | Meaning |
|---|---|---|---|
| `running` | bool | — | `true` when the BLOS manager reports the Tailscale overlay is active. `false` covers both "disabled by config" and "enabled but stopped". |
| `connected_since_unix_ns` | int64 | unix-nanoseconds | Wall-clock time of the first transition to BackendState="Running" in the current enable cycle. 0 when the backend has not reached Running since the last enable. Subtract from `captured_at_start` to compute uptime. |
| `backend_state` | string | — | Mirrors the Tailscale backend state at snapshot time: one of `NoState`, `NeedsLogin`, `NeedsMachineAuth`, `Stopped`, `Starting`, `Running`. Empty when the status worker has never produced a reading. |
| `peer_count` | uint32 | count | Number of Tailscale peers visible to the local node at snapshot time (len of `status.Peer`). |
| `rx_bytes_total` | uint64 | bytes | Sum of `PeerStatus.RxBytes` across all peers — cumulative bytes received since `tailscaled` last started. |
| `tx_bytes_total` | uint64 | bytes | Sum of `PeerStatus.TxBytes` across all peers. |
| `rx_bps_60s` | float64 | bytes/sec | RX rate averaged over the last 60 seconds of samples held by the status worker's rate ring. Zero until the ring has at least two samples. |
| `tx_bps_60s` | float64 | bytes/sec | TX rate averaged over the same 60-second window. |
| `events_dropped` | uint64 | count | Cumulative count of BLOS stream events the daemon dropped because a registered listener's bounded channel was full. A non-zero-and-growing value across successive snapshots indicates a slow consumer of the `StreamBLOSEvents` RPC. |

### `sysupgrade` — firmware upgrade manager

```json
{
  "phase": "idle",
  "last_error": "",
  "current_release_tag": "",
  "current_asset_name": "",
  "capable_reason": "ok",
  "last_check_unix": 1745596800,
  "downloaded_bytes": 0,
  "total_bytes": 0,
  "child_pid": 0,
  "in_progress": false,
  "capable": true
}
```

| Field | Type | Unit | Meaning |
|---|---|---|---|
| `phase` | string | — | Current high-level state: `idle`, `checking`, `downloading`, `verifying`, `ready`, `upgrading`, `failed`, or `unspecified` (only on the zero value). |
| `last_error` | string | — | Most recent error message; empty on the happy path. Populated when `phase` is `failed` or after a transient `checking` failure. |
| `current_release_tag` | string | — | Release tag currently being downloaded/installed, e.g. `v1.8.0`. Empty when no upgrade is in flight. |
| `current_asset_name` | string | — | Asset filename of the in-flight upgrade, e.g. the matching sysupgrade image. Empty when no upgrade is in flight. |
| `capable_reason` | string | — | Short human-readable reason corresponding to `capable`: `ok`, `no /sbin/sysupgrade`, `rootfs is ext4, not squashfs`, `no /overlay mount`, etc. |
| `last_check_unix` | int64 | unix-seconds | Wall-clock time of the most recent successful github fetch, or 0 if the daemon has not contacted github in the current run and the on-disk cache is empty. |
| `downloaded_bytes` | int64 | bytes | Bytes written to the destination file so far during a download. Resets to 0 when the manager returns to `idle`. |
| `total_bytes` | int64 | bytes | Expected size of the asset being downloaded. -1/0 when not yet known. |
| `child_pid` | int32 | pid | PID of the detached sysupgrade child once it has been launched via setsid+nohup. 0 before the runner returns; once non-zero the daemon has handed off control. |
| `in_progress` | bool | — | True while a per-upgrade goroutine or detached sysupgrade child is alive. Useful as a one-field check before deciding whether `phase` is meaningful. |
| `capable` | bool | — | True when all sysupgrade preconditions are satisfied (binary present, squashfs root, /overlay mounted). |

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
10. **RX loss topology.** The `jitter.gap_runs_*` buckets distinguish
    isolated packet loss from contiguous burst loss. A distribution
    weighted in `gap_runs_1` and `gap_runs_2_5` indicates short,
    recoverable losses — Opus in-band FEC (LBRR) and RFC 2198-style
    redundant payloads can mitigate these. A distribution weighted in
    `gap_runs_11_20` and higher indicates long contiguous bursts
    (>200 ms) that no bounded-latency FEC scheme can recover; this is
    typical of WiFi broadcast/multicast on interference-prone links
    and usually means the mitigation has to be masking (longer jitter
    buffer, PLC) rather than recovery. Always read these buckets
    alongside `rx_pkts` so you know the absolute scale. **Note:** the
    FEC adapter (see `comms.fec_adapter`) already consumes these
    buckets every 2 s and raises `packet_loss_perc` in response —
    operator action is only needed when the adapter has hit its
    ceiling (level 40) and loss still persists.
11. **FEC adapter pegged at max.** `comms.fec_adapter.current_level
    == 40` sustained across multiple snapshots with a non-zero
    `transitions` count means the controller has raised the level
    and has not been able to drop it back down. Cross-reference
    `comms.ports[*].jitter.gap_runs_*`: if the higher-length buckets
    (`11_20`, `21_50`, `over_50`) are contributing most of the loss,
    the controller is doing the right thing but FEC has hit its
    useful ceiling — consider widening the jitter buffer or
    diagnosing the RF environment. If only the short buckets (`1`,
    `2_5`) are contributing, the controller is behaving correctly
    for a lossy-but-recoverable channel and the level will track the
    real loss rate. A non-zero `comms.fec_adapter.write_errors`
    delta across snapshots means the encoder rejected a
    `SetPacketLossPerc` call — that should never happen in production
    and indicates the codec is in a bad state.
12. **Comms disabled but expected.** If the user reports a broken PTT
   workflow but `comms.enabled == false`, the subsystem was either
   disabled in config or failed to start. Check daemon logs for the
   corresponding startup error.
13. **Sysupgrade hung.** `sysupgrade.phase == "upgrading"` and
   `sysupgrade.child_pid > 0` mean the runner has detached the
   sysupgrade child and the daemon is waiting on a reboot. If the
   snapshot worker is still emitting captures with the same PID
   minutes later, the device did not reboot — check the per-asset
   log file under `/tmp/openmanetd/sysupgrade/<asset>.log` on the
   device. `phase == "failed"` with `last_error` mentioning
   `checksum` means the GitHub asset is corrupted or the wrong file
   was matched; `last_error` mentioning `insufficient` indicates
   `/tmp` ran out of space (a 50 MiB image on a 256 MiB tmpfs is
   tight). Cross-reference `sysupgrade.capable` and
   `sysupgrade.capable_reason` to confirm the device should ever
   have been able to flash in the first place.

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
