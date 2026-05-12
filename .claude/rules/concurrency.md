# Concurrency Safety Rules

All Go code must be concurrency safe. Apply these rules to every struct, function, and method you write or modify.

---

## Shared State

- Protect all mutable shared state with `sync.Mutex` or `sync.RWMutex`. Name the field `mu` and place it directly above the fields it guards, with a comment indicating what it protects:
  ```go
  mu      sync.Mutex // protects the fields below
  counter int
  cache   map[string]string
  ```
- Use `sync.RWMutex` when reads significantly outnumber writes. Use `RLock()`/`RUnlock()` for read-only paths and `Lock()`/`Unlock()` for write paths.
- Keep critical sections as short as possible — do not hold a lock while performing I/O, network calls, or channel operations.
- Never embed a mutex in a value type that could be copied. Use a pointer receiver on all methods that touch the mutex.

## Channels and Goroutines

- Every goroutine you spawn must have a clear shutdown path. Prefer `context.Context` cancellation or a dedicated `done` / `quit` channel.
- Always use `select` with a `case <-ctx.Done()` (or equivalent) in goroutine loops so they terminate when the context is cancelled.
- Ensure channels are closed exactly once and only by the sender. Never close a channel from the receiver side.
- Use buffered channels deliberately — document why the buffer size was chosen.

## Concurrent Map Access

- Never read and write a plain `map` from multiple goroutines without synchronization. Use either:
  - A `sync.Mutex` / `sync.RWMutex` guarding the map, or
  - `sync.Map` when the access pattern fits (stable keys, high read ratio).

## Atomic Operations

- Use `sync/atomic` or `atomic.Value` only for simple counters and flags. Prefer mutexes for anything more complex — clarity matters more than micro-optimization.

## Testing

- All test fakes that hold mutable state must be mutex-protected (this is already enforced in testing rules).
- Use the `-race` flag (`make test-race`) to verify new and modified code is free of data races.
- When writing tests that spawn goroutines, use `t.Cleanup()` to ensure they are joined before the test exits.

## Code Review Checklist

When writing or reviewing Go code, verify:

1. Every struct field accessed from multiple goroutines is protected.
2. Every goroutine has a shutdown path.
3. No lock is held across blocking operations (I/O, channel sends, network calls).
4. No plain `map` is accessed concurrently without synchronization.
5. `defer mu.Unlock()` is used immediately after `mu.Lock()` to prevent deadlocks on early returns.
