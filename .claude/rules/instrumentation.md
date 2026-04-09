# Instrumentation Rules

The `internal/instrumentation` package exposes a periodic JSON snapshot of
every runtime counter maintained by the daemon. Operators and LLMs use these
snapshots to triage audio glitches, goroutine leaks, jitter buffer pressure,
and other runtime issues without tailing logs.

`docs/instrumentation-snapshot.md` is the field reference for the snapshot
format. It is the document operators and LLMs read to discover what is
instrumented and how to interpret it. When that document drifts out of sync
with the Go types, the snapshot becomes opaque to anyone not willing to read
source — and the whole point of the framework is to avoid that.

---

## Every instrumentation change ships with a doc update

Any addition, removal, or rename of fields in a snapshot struct exposed
through the instrumentation framework **MUST** be reflected in
`docs/instrumentation-snapshot.md` in the same changeset. This is not
optional polish — treat it as a required output of the code change.

Structs this rule applies to include (non-exhaustive):

- `internal/comms/snapshot.go` — `CommsSnapshot`, `PortSnapshot`
- `internal/comms/rtp/snapshot.go` — `JitterBufferSnapshot`
- `internal/comms/audio/*.go` — `AudioEncoderSnapshot`
- `internal/comms/webaudio/*.go` — `BridgeSnapshot`
- `internal/blos/*.go` — BLOS section snapshot
- Any new producer-side `Snapshotter` adapter added to the registry

## What the doc update must contain

1. **Add a table row** under the relevant subsection (`comms.ports[*]`,
   `comms.broadcast_encoder`, `comms.web_bridge`, etc.) for each new field.
2. **Match JSON key names exactly**, in lowercase snake_case, as they
   appear in the struct tag.
3. **State the unit** (count, bytes, nanoseconds, etc.) and a one-sentence
   meaning that lets an operator understand the field without reading Go
   source.
4. **Add or extend an interpretation heuristic** under "Interpretation
   heuristics for LLM triage" whenever the field's meaning is non-obvious
   or when two fields should be cross-referenced. These heuristics are
   what make the snapshot actionable for automated triage.
5. **Keep the semver note honest.** Purely additive field changes are a
   minor bump; renames or removals are a major bump. The `schema_version`
   constant in the envelope and the doc should agree.

## What the doc update does NOT need

- No changelog entries, no migration notes. The doc is a field reference,
  not a history.
- No per-field performance or allocation caveats — the whole framework is
  atomic-load-only on the producer side and that contract lives in the
  package GoDoc, not here.

## Code-review checklist

When writing or reviewing an instrumentation change, verify:

1. Every new field on a snapshot struct has a matching row in
   `docs/instrumentation-snapshot.md`.
2. JSON tag and doc row use the same lowercase snake_case key.
3. If the field changes the interpretation of any other field, a
   cross-reference exists (either in the row text or as a new heuristic).
4. Removed or renamed fields have been removed from the doc, not just
   edited in place with a stale description.
5. The `Snapshot` method still passes its zero-allocation test — no new
   allocations introduced by the new fields.
