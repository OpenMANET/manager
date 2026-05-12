# API Design Rules

Rules derived from existing patterns in this codebase. Follow these when adding or modifying API services.

---

## Handler Structs

Each service handler is a struct with injected dependencies — no constructors, no `New()` functions. Fields follow this order:

```go
type FooService struct {
    Log     zerolog.Logger   // always present
    Cfg     *config.Config   // if config is needed
    Manager FooManagerInterface // domain-specific manager
    DB      *models.Queries  // if database access is needed
}
```

- Dependencies are always interfaces, never concrete types (enables hand-written fakes in tests)
- Add overrideable function fields for I/O operations that are hard to fake:
  ```go
  ParseBatHosts func(string) (*batmanadv.BatHosts, error)
  ```
  Implement a private helper that falls back to the real function when nil:
  ```go
  func (s *FooService) parseBatHosts(path string) (*batmanadv.BatHosts, error) {
      if s.ParseBatHosts != nil {
          return s.ParseBatHosts(path)
      }
      return batmanadv.ParseBatHostsFile(path)
  }
  ```
- Use `sync.Mutex` as a struct field (named `mu`) to serialize concurrent writes to shared config

---

## Error Handling

Always wrap errors with `connect.NewError`. Use the correct code:

| Code | When to use |
|------|-------------|
| `CodeInternal` | Database failures, I/O errors, service failures |
| `CodeInvalidArgument` | Missing required fields, input out of range |
| `CodeFailedPrecondition` | Service unavailable or in wrong state |
| `CodeNotFound` | Resource does not exist |

```go
// Wrapping an existing error
return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("list nodes: %w", err))

// Custom message
return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("auth_key is required"))
```

Always log before returning an error (use `.Error()` level for `CodeInternal`, `.Warn()` for degraded-but-partial):

```go
s.Log.Error().Err(err).Msg("Failed to persist config")
return nil, connect.NewError(connect.CodeInternal, err)
```

For operations that collect multiple partial results (e.g. dashboard), accumulate errors and return partial data:

```go
var errs []error
if v, err := s.Provider.GetSomething(); err != nil {
    errs = append(errs, err)
} else {
    res.Value = v
}
return res, errors.Join(errs...)
```

---

## Streaming RPCs

**Server streaming:**

```go
func (s *FooService) StreamFoo(ctx context.Context,
    _ *foov1.StreamFooRequest,
    stream *connect.ServerStream[foov1.StreamFooResponse]) error {

    for {
        select {
        case <-ctx.Done():
            return nil
        case data, ok := <-source:
            if !ok {
                return nil
            }
            if err := stream.Send(&foov1.StreamFooResponse{...}); err != nil {
                return err
            }
        }
    }
}
```

**Client streaming:**

```go
func (s *FooService) StreamFoo(_ context.Context,
    stream *connect.ClientStream[foov1.StreamFooRequest]) (*foov1.StreamFooResponse, error) {

    for stream.Receive() {
        msg := stream.Msg()
        // process msg
    }
    if err := stream.Err(); err != nil {
        return nil, err
    }
    return &foov1.StreamFooResponse{...}, nil
}
```

- Always check `ctx.Done()` in server stream loops
- Always check `stream.Err()` after client stream loop exits
- The API server has **no WriteTimeout** — intentional for long-lived streams, do not add one

---

## Proto / RPC Naming

**Service naming:** `{Domain}Service`

**Method naming** — verb-first:
- `Get{Resource}` — single item or current status
- `List{Resources}` — collection
- `Set{Property}` — modify a single property
- `Update{Resource}Config` — modify and persist configuration
- `Execute{Action}` — administrative action
- `Send{Event}` — client-initiated state change
- `Stream{Direction}` — streaming (e.g. `StreamAudioRx`, `StreamAudioTx`)

**Proto package layout:**
```
proto/openmanet/{domain}/v1/
  {domain}_service.proto   # rpc definitions
  {domain}.proto           # shared message types
  {domain}_config.proto    # config messages (if distinct)
  {domain}_status.proto    # status messages (if distinct)
```

**Validation:** Declare field constraints in proto using `buf.validate` annotations. Do not re-validate in handler code — the `validate.NewInterceptor()` interceptor handles it automatically.

```proto
import "buf/validate/validate.proto";

message SetFooRequest {
    int32 value = 1 [(buf.validate.field).int32 = {gte: 1, lte: 32}];
}
```

Use `google.protobuf.Empty` for RPCs with no meaningful request or response. Use `durationpb`/`timestamppb` for time values.

---

## Database (sqlc)

Edit only `internal/database/schema.sql` and `internal/database/query.sql`. Run `make sqlc-gen` to regenerate models. Never edit `internal/database/models/`.

**Query naming convention:**
```sql
-- name: GetFoo :one
-- name: ListFoos :many
-- name: CreateFoo :one   (prefer INSERT ... ON CONFLICT ... DO UPDATE ... RETURNING * for upserts)
-- name: UpdateFoo :exec
-- name: DeleteFoo :exec
```

**Handler usage:** Pass `ctx` from the RPC to every DB call.

```go
nodes, err := s.DB.ListFoos(ctx)
```

---

## Logging

Use the `zerolog.Logger` field named `Log`. Structured fields go before `.Msg()`.

```go
s.Log.Debug().Msg("Request received")
s.Log.Error().Err(err).Str("id", id).Msg("Failed to load foo")
s.Log.Warn().Err(err).Msg("Partial failure, continuing")
```

Level guidelines:
- `Debug` — request received, per-frame counters, verbose operational detail
- `Info` — significant lifecycle events (startup, shutdown)
- `Warn` — degraded state that doesn't prevent response
- `Error` — failure that causes the RPC to return an error

---

## Service Registration

In `internal/openmanet/server/server.go`, instantiate the handler struct as a literal and pass it to the generated `api.New{Service}Handler`:

```go
mux.Handle(foov1connect.NewFooServiceHandler(&handlers.FooService{
    Log:     log.With().Str("service", "foo").Logger(),
    Manager: fooManager,
    DB:      db,
}, connect.WithInterceptors(validateInterceptor)))
```

Wire new dependencies through the `APIServer` struct in `server.go` and `internal/openmanet/openmanet.go`.

---

## Testing

**Unit tests:** package `handlers_test`, file `{service}_test.go`.

- Use `newTestDB(t)` for in-memory SQLite (see existing tests for implementation)
- Use `zerolog.Nop()` for the logger
- Use `config.NewWithoutWatch(v)` when a real config is needed (avoids file-watcher races)
- **No mock frameworks** — write hand-written fakes only (see `mocks_test.go` pattern)

**Fake manager pattern:**

```go
type fakeFooManager struct {
    mu         sync.Mutex
    doFooCalls int
    doFooErr   error
}

func (f *fakeFooManager) DoFoo(_ context.Context) error {
    f.mu.Lock()
    defer f.mu.Unlock()
    f.doFooCalls++
    return f.doFooErr
}

func (f *fakeFooManager) getDoFooCalls() int {
    f.mu.Lock()
    defer f.mu.Unlock()
    return f.doFooCalls
}
```

- Mutex-protected for goroutine safety
- Call counters for assertion
- Configurable error returns per method

**Integration tests:** build tag `//go:build integration`, use `newTestServer(t)` + `httptest.NewServer`. Run with `make integration-test`.

**Proto validation tests:** instantiate a `protovalidate.Validator` and call `.Validate()` directly — no server needed.
