package instrumentation

import (
	"os"
	"runtime"
)

// RuntimeStats captures Go runtime counters that are useful for diagnosing
// memory growth, goroutine leaks, and GC pressure. Fields mirror the
// corresponding values in runtime.MemStats and runtime.NumGoroutine, with
// units documented in docs/instrumentation-snapshot.md.
type RuntimeStats struct {
	NumGoroutine    int    `json:"num_goroutine"`
	MemAllocBytes   uint64 `json:"mem_alloc_bytes"`
	MemSysBytes     uint64 `json:"mem_sys_bytes"`
	HeapInuseBytes  uint64 `json:"heap_inuse_bytes"`
	StackInuseBytes uint64 `json:"stack_inuse_bytes"`
	NumGC           uint32 `json:"num_gc"`
	LastGCUnixNano  int64  `json:"last_gc_unix_nano"`
	GCPauseLastNs   uint64 `json:"gc_pause_last_ns"`
}

// preallocMemStats is a dedicated runtime.MemStats owned by the Registry and
// reused on every Capture so runtime.ReadMemStats does not allocate. It is
// wrapped in its own type so the Registry struct layout makes the reuse
// intent obvious at a glance.
type preallocMemStats struct {
	m runtime.MemStats
}

// captureRuntime populates dst from the Go runtime. runtime.ReadMemStats is
// alloc-free when given a preallocated *MemStats pointer but is a brief
// stop-the-world operation; this is documented in the schema reference so
// operators know not to run captures in a tight loop during TX.
func (r *Registry) captureRuntime(dst *RuntimeStats) {
	ms := &r.memStats.m
	runtime.ReadMemStats(ms)

	dst.NumGoroutine = runtime.NumGoroutine()
	dst.MemAllocBytes = ms.Alloc
	dst.MemSysBytes = ms.Sys
	dst.HeapInuseBytes = ms.HeapInuse
	dst.StackInuseBytes = ms.StackInuse
	dst.NumGC = ms.NumGC
	dst.LastGCUnixNano = int64(ms.LastGC) //nolint:gosec // ms.LastGC is unix-nanoseconds; fits int64 until year 2262.

	// PauseNs is a 256-entry circular buffer; the most recent pause lives at
	// (NumGC+255) % 256. Load it via a single index rather than calling
	// debug.ReadGCStats which would allocate its []time.Duration slice.
	if ms.NumGC > 0 {
		dst.GCPauseLastNs = ms.PauseNs[(ms.NumGC+255)%256]
	} else {
		dst.GCPauseLastNs = 0
	}
}

// processID returns the current process's PID. Wrapped for symmetry with the
// rest of the captured daemon metadata.
func processID() int {
	return os.Getpid()
}
