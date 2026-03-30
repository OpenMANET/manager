# OpenMANETd — Claude Code Guide

## Commands

| Task | Command |
|------|---------|
| Full build (CGO + all hardware deps) | `make build` |
| Lite build (no whisper/comms, ~5MB) | `make build-lite` |
| Build React frontend to `static/` | `make frontend` |
| Run Go tests | `make test` |
| Tests with race detector | `make test-race` |
| Integration tests (no hardware needed) | `make integration-test` |
| Frontend tests | `make test-frontend` |
| Lint React frontend | `make lint-frontend` |
| Format Go code | `make fmt` |
| Lint Go with auto-fix | `make lint-go` |
| Lint React & Go | `make lint` |
| Generate protobuf code | `make buf` |
| Generate sqlc database code | `make sqlc-gen` |

`make build` runs fmt → vet → buf → sqlc-gen → frontend → compile in sequence.

## Architecture

```
cmd/                              Cobra CLI: root.go (full daemon), frontend.go (frontend-only mode)
internal/openmanet/               Start() wires all subsystems
internal/openmanet/server/        ConnectRPC HTTP server setup + service registration
internal/openmanet/server/handlers/   Service handler implementations (one file per service)
internal/api/                     AUTO-GENERATED protobuf + ConnectRPC Go stubs — DO NOT EDIT
internal/frontend/                HTTP server: serves React SPA, proxies /api/* and /ws to port 8087
internal/database/                SQLite: schema.sql + query.sql; models/ is generated
internal/config/                  Viper-backed config (reads /etc/openmanetd/config.yml)
internal/comms/                   Opus/RTP audio (portaudio, CGO required)
internal/blos/                    BLOS/Tailscale overlay
internal/mgmt/                    Alfred mesh management + wireless config
proto/                            Git submodule — .proto source files
frontend/                         React SPA (Vite); build output → static/
frontend/src/gen/                 AUTO-GENERATED protobuf JS clients — DO NOT EDIT
static/                           Embedded build output (go:embed target in static.go)
```

Ports: API + WebSocket = **8087**. Frontend server = **8081** (proxies `/api/*` and `/ws` → 8087).

## Code Generation

**Protobuf (`make buf`)**
- Source: `proto/` git submodule — if empty: `git submodule update --init --recursive`
- Config: `buf.yaml`, `buf.gen.yaml`
- Go output: `internal/api/` — never edit
- JS output: `frontend/src/gen/` — never edit

**Database (`make sqlc-gen`)**
- Edit: `internal/database/schema.sql` (schema) and `internal/database/query.sql` (queries)
- Config: `sqlc.yaml`
- Output: `internal/database/models/` — never edit

## Adding a New API Service

1. Add `.proto` file in `proto/openmanet/<service>/v1/`
2. `make buf` — generates Go in `internal/api/` and JS in `frontend/src/gen/`
3. Create `internal/openmanet/server/handlers/<service>.go`:
   - Struct with `Log zerolog.Logger` + dependencies
   - Implement generated `<Service>Handler` interface
   - Return `connect.NewError(connect.CodeInternal, err)` for errors
4. Register in `internal/openmanet/server/server.go` — add `api.Handle(...)` call
5. Wire dependencies via `APIServer` struct in `server.go` and `internal/openmanet/openmanet.go`
6. Write comprehensive unit and integration tests.  The test environment is not the same as production.  Mock tests where appropriate.
7. All code should lint cleanly

## Frontend Development

**Vite hot-reload dev server:**
```bash
cd frontend && VITE_API_TARGET=http://<remote-host>:8081 npm run dev
```

Proxy rules (`frontend/vite.config.js`):
- `/ws`, `/api`, `/whisper` → `VITE_API_TARGET` (default `http://localhost:8080`)
- `/rpc` → `VITE_RPC_TARGET` (default `http://localhost:8087`), `/rpc` prefix stripped before forwarding

**Frontend-only binary mode** (no local database/hardware):
```bash
./bin/openmanetd frontend --api-address http://<remote-host>:8087
```

**Linting:**
Run `make lint-frontend` before committing frontend changes. The ESLint config at `frontend/eslint.config.js` covers all `.js`/`.jsx` files under `frontend/src/` (excluding `src/gen/`). To auto-fix fixable issues: `npm --prefix frontend run lint:fix`.

## Gotchas

- **CGO required** for full build (`go-sqlite3`, `portaudio`, `go-alfred`). Use `make build-lite` to skip comms/whisper. The DevContainer has all required native libraries.
- **`proto/` is a git submodule.** If empty: `git submodule update --init --recursive`, then `make buf`.
- **`static/` must be populated** before compiling the Go binary. The `//go:embed static/*` directive in `static.go` fails at compile time if the directory is empty. Run `make frontend` first, or use `make build`.
- **Never edit `internal/api/`, `frontend/src/gen/`, or `internal/database/models/`** — generated, will be overwritten.
- **No WriteTimeout on the API server** — intentional, required for long-lived streaming RPCs in CommsService.

## Testing Patterns

- **Unit tests**: package `handlers_test`, file `<service>_test.go`. Use `newTestDB(t)` for in-memory SQLite, `zerolog.Nop()` for logger.
- **No mock framework**: hand-written fakes only (see `mocks_test.go` pattern in handlers/).
- **Integration tests**: build tag `//go:build integration`. Use `newTestServer(t)` + `httptest.NewServer`. Run with `make integration-test`.
- **Frontend tests**: Vitest + jsdom. Run with `make test-frontend`.
