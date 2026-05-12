# Testing Rules

## When to Write Tests

- **Every behavior change must include tests.** Bug fixes, new features, and refactors that alter runtime logic require unit and/or integration tests in the same changeset. Do not ship behavior changes without corresponding test coverage.
- If a change alters expected behavior of an existing test, update the test to assert the new behavior — do not delete it.

## Go — General

- Package: `package handlers_test` (external test package). Tests live alongside the code they test.
- Use `testify/require` for fatal assertions (setup, preconditions) and `testify/assert` for non-fatal assertions in the test body.
- Use `t.Helper()` in every helper function so failures point to the call site.
- Use `t.Cleanup()` (not `defer`) for resource teardown in helpers.
- Use `zerolog.Nop()` as the logger in all test handlers and services.

## Go — Fakes / Mocks

- No mock frameworks. Write hand-rolled fakes only (see `internal/openmanet/server/handlers/mocks_test.go`).
- Keep fakes in a dedicated `mocks_test.go` file within the package under test.
- Protect mutable fake state with `sync.Mutex`; expose call counts via mutex-guarded getter methods.
- Support error injection via exported error fields (e.g., `enableErr error`); return the error from the relevant method and nil otherwise.
- Name fakes `fake<Interface>` (e.g., `fakeWireless`).

## Go — Database

- Use `newTestDB(t)` which opens an in-memory SQLite (`:memory:`) database, applies the schema inline, and registers `t.Cleanup(db.Close)`.
- Define the schema as a package-level constant `schemaSQL` kept in sync with `internal/database/schema.sql`. Never use the production database in tests.

## Go — Unit Tests

- Construct the handler struct directly with its dependencies injected:
  ```go
  svc := &handlers.FooService{DB: newTestDB(t), Log: zerolog.Nop(), Mgr: &fakeMgr{}}
  ```
- For config-backed handlers, create a real `*config.Config` backed by a temp YAML file:
  ```go
  func setupTestConfig(t *testing.T, yaml string) *config.Config { ... }
  ```
  Use `t.TempDir()` for the temp directory.
- Use table-driven tests with `t.Run()` for multiple input variants. Capture loop variables (`tc := tc`) before spawning subtests.

## Go — Integration Tests

- Tag every integration test file: `//go:build integration` (first line of file, before `package`).
- Use `newTestServer(t)` which wires all handlers behind an `httptest.NewServer`, registers `t.Cleanup(srv.Close)`, and attaches the `validate.NewInterceptor()`.
- Create ConnectRPC clients pointing at `srv.URL`:
  ```go
  client := services.NewFooServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())
  ```
- Assert ConnectRPC errors by unwrapping with `require.ErrorAs(t, err, &connectErr)` then checking `connectErr.Code()` and `connectErr.Message()`.
- Run integration tests with `make integration-test`, never with `go test ./...`.

## Go — Test Fixtures and Factories

- Store file-based fixtures under `testfixtures/<subsystem>/`. Locate them at runtime using `runtime.Caller(0)` to walk up to the module root — never hardcode absolute paths.
- Provide small factory helpers for repeated domain objects (e.g., `makeInterface(name, ifType)`, `makeStation(mac, signal)`) with sensible defaults.
- Constructor helpers for services should be named `new<Service>(deps...)`.

## Go — ConnectRPC Error Assertions

```go
var connectErr *connect.Error
require.ErrorAs(t, err, &connectErr)
assert.Equal(t, connect.CodeInvalidArgument, connectErr.Code())
assert.Contains(t, connectErr.Message(), "expected fragment")
```

For streaming RPCs, the error may surface on `stream.Receive()` rather than the initial call — handle both paths.

## Go — Proto Validation Tests

- Validate proto messages directly with a `protovalidate.Validator` created via `newValidator(t)`.
- Test boundary values: zero, negative, one below minimum, minimum, maximum, one above maximum, empty string.

---

## Frontend — General

- Test runner: Vitest. DOM environment: jsdom. Component rendering: `@testing-library/react`.
- Test files live in `src/__tests__/`, mirroring the `src/` structure. Suffix: `.test.js` / `.test.jsx`.
- Group tests with `describe('TestFeatureName', ...)` using PascalCase names.
- Reset module registry and stubs in `beforeEach` / `afterEach`:
  ```js
  beforeEach(() => { vi.resetModules(); vi.stubGlobal(...); vi.useFakeTimers(); });
  afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks(); vi.resetModules(); });
  ```

## Frontend — Module Mocking

- Mock entire modules with `vi.mock('../path/module.js', () => ({ ... }))`.
- For transport-level overrides, use `vi.doMock()` + dynamic `import()` inside each test to get a fresh module with the desired transport.
- Prefer `createRouterTransport` (from `@connectrpc/connect`) over HTTP mocks for ConnectRPC service tests. Register only the services you need; unregistered services will fail naturally.

## Frontend — Browser API Mocking

- Stub browser globals with `vi.stubGlobal('AudioContext', vi.fn(...))`.
- Implement constructor-style globals using `vi.fn(function(opts) { this.method = vi.fn(); ... })`.
- Provide factory functions for complex mock hierarchies (e.g., `createMockAudioContext()`).
- Always call `vi.unstubAllGlobals()` in `afterEach`.

## Frontend — React Component Tests

- Provide a render helper with default props and an `overrides` spread:
  ```js
  function renderFoo(overrides = {}) {
    const props = { onPress: vi.fn(), label: 'default', ...overrides };
    return { ...render(<Foo {...props} />), props };
  }
  ```
- Use `screen` queries (`getByText`, `getByLabelText`) for accessibility-aligned assertions.
- Use `container.querySelector()` for class-based assertions.
- Simulate user events with `fireEvent` from `@testing-library/react`.

## Frontend — Data Transformation Tests

- Test all data states: `null`, empty object, empty arrays, and fully populated data.
- Test edge cases: missing or empty string fields, zero numeric values, maximum values.
- Test that display formatting is correct (e.g., `1500000` → `"1.5 Mbps"`).

## Frontend — WebSocket Tests

- Implement a `MockWebSocket` class with static constants (`OPEN`, `CLOSED`, etc.) and `vi.fn()` methods (`send`, `close`).
- Collect instances in an array reset in `beforeEach` so multi-connection scenarios are trackable.
- Simulate events by calling handlers directly (e.g., `mockWsInstances[0].onopen()`).
- Use `vi.useFakeTimers()` when testing reconnect or timeout logic.

---

## What NOT to Test

- Don't test generated code in `internal/api/`, `frontend/src/gen/`, or `internal/database/models/`.
- Don't write tests that require live hardware, real network interfaces, or real SQLite files on disk — use fakes or in-memory DBs.
- Don't use `time.Sleep` in tests; use `t.TempDir()`, channels, and fake timers instead.
