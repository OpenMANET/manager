// Package instrumentation provides a generic, reusable framework for capturing
// runtime counters and metrics from an application into a versioned snapshot
// document. It is intentionally self-contained — it imports only the Go
// standard library and the zerolog logging package — so it can be lifted into
// another repository without modification.
//
// # Model
//
// The framework is built around three small abstractions:
//
//   - Snapshotter: implemented by producers (application subsystems) that
//     own runtime state. Refresh atomically loads the producer's current
//     state into a producer-owned snapshot struct; Data returns a stable
//     pointer to that struct. Refresh MUST NOT allocate and MUST NOT block
//     longer than a handful of atomic loads.
//
//   - Registry: holds the set of registered Snapshotters keyed by a stable
//     section name. Capture walks the registered producers, calls Refresh
//     on each, and writes the result into a caller-owned Envelope. Capture
//     is alloc-free once the Envelope's section slice capacity has been
//     established.
//
//   - Worker: runs a background goroutine that captures a snapshot on a
//     fixed interval and writes each capture to a JSON file on disk.
//
// # Performance contract
//
// The framework makes three promises:
//
//  1. Capture does not hold any lock on the producer's hot path. Producers
//     expose state via atomic fields; Refresh loads those atomically into a
//     preallocated destination. No producer hot path is stalled.
//
//  2. After the first Capture, subsequent captures are zero-allocation.
//     The caller owns the Envelope and reuses its Sections slice; the
//     registry holds a preallocated runtime.MemStats. See the package tests
//     which use testing.AllocsPerRun to prove this.
//
//  3. Skew between atomic loads is inherent (counters keep moving while we
//     read them) but bounded: every Envelope records CapturedAtStart and
//     CapturedAtEnd so consumers can reason about the read window.
//
// # JSON format
//
// The Envelope type is the authoritative schema. Its JSON representation is:
//
//	{
//	  "schema_version": "1.0.0",
//	  "captured_at_start": "2026-04-09T12:34:56.789012345Z",
//	  "captured_at_end":   "2026-04-09T12:34:56.789013101Z",
//	  "daemon": { ... },
//	  "runtime": { ... },
//	  "sections": [
//	    { "name": "<producer name>", "data": <producer-defined JSON> },
//	    ...
//	  ]
//	}
//
// Producers own the shape of their "data" payload — the framework never
// inspects it beyond passing it to encoding/json at marshal time. This keeps
// the framework decoupled from any specific application domain.
//
// # Reusability
//
// This package deliberately imports nothing from the enclosing application.
// To adopt it in a different project, copy the directory and implement the
// Snapshotter interface for your producers. The Worker, Registry, and
// Envelope types need no changes.
package instrumentation
