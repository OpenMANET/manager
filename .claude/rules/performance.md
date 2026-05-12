# Performance and Resource Efficiency Rules

Target devices include resource-constrained hardware (e.g., MIPS routers, embedded ARM boards). All Go code must be written with memory and CPU efficiency in mind.

---

## Memory

- **Preallocate slices and maps** when the size is known or can be estimated:
  ```go
  results := make([]Node, 0, len(input))   // not: var results []Node
  lookup := make(map[string]int, len(keys)) // not: make(map[string]int)
  ```
- **Reuse buffers** for repeated I/O operations. Use `sync.Pool` for frequently allocated short-lived objects (e.g., byte buffers in hot paths):
  ```go
  var bufPool = sync.Pool{
      New: func() any { return new(bytes.Buffer) },
  }
  ```
- **Avoid unnecessary allocations** in loops — move allocations outside the loop body when possible, reuse slices by reslicing (`s[:0]`) instead of reallocating.
- **Use pointer receivers** on large structs to avoid copying. Use value receivers only for small, immutable types.
- **Prefer `[]byte` over `string`** for data that is read, transformed, and discarded — avoid repeated `string(b)` / `[]byte(s)` conversions.
- **Release references promptly** — set large slices/maps to `nil` when no longer needed so the GC can reclaim them, especially in long-lived structs.
- **Avoid unbounded growth** — caches, buffers, and queues must have a size limit or eviction policy. Never let an in-memory collection grow without bound.

## CPU

- **Avoid unnecessary work** — don't recompute values that haven't changed. Cache results of expensive operations (parsing, regex compilation, file reads) when inputs are stable.
- **Compile regexps once** at package level with `regexp.MustCompile`, never inside a function body.
- **Use `strings.Builder`** for multi-step string construction, not repeated concatenation.
- **Prefer `strconv`** over `fmt.Sprintf` for simple type conversions (int-to-string, float-to-string).
- **Minimize reflection** — avoid `reflect` and `fmt.Sprintf("%v", ...)` in hot paths. Use concrete types and type switches.
- **Choose efficient data structures** — use maps for lookups, sorted slices with binary search when order matters, and consider bitfields/bitmasks for flag sets.

## I/O and Network

- **Use buffered I/O** (`bufio.Reader` / `bufio.Writer`) for file and network operations.
- **Stream large data** — process line-by-line or chunk-by-chunk rather than reading entire files into memory.
- **Set appropriate buffer sizes** — default `bufio` buffer (4KB) is often too small for network reads; size buffers to match expected payloads.

## Goroutines

- **Don't leak goroutines** — every goroutine must exit when its work is done or its context is cancelled. A leaked goroutine is a memory leak.
- **Limit concurrency** — use semaphores (`chan struct{}`) or worker pools to bound the number of concurrent goroutines, especially for I/O-bound work.
- **Prefer a single goroutine with a ticker** over spawning a new goroutine per event when processing periodic work.

## Code Review Checklist

When writing or reviewing Go code, verify:

1. Slices and maps are preallocated when size is known.
2. No allocations inside hot loops that could be hoisted out.
3. No unbounded caches, buffers, or queues.
4. Regexps are compiled once at package level.
5. Strings are built with `strings.Builder`, not concatenation.
6. Large data is streamed, not loaded entirely into memory.
7. Goroutine count is bounded and all goroutines have shutdown paths.
