# Idiomatic Go Rules

All Go code must follow idiomatic patterns as defined by the Go community. When in doubt, defer to Effective Go, the Go Code Review Comments wiki, and the Go standard library as reference implementations.

---

## Naming

- **MixedCaps/mixedCaps**, never underscores. Exported names are `PascalCase`, unexported are `camelCase`.
- **Short, clear names** — prefer `i` over `index` in tight loops, `r` for a `*http.Request` in a handler, `ctx` for `context.Context`. Longer scopes warrant longer names.
- **Acronyms are all-caps** — `ID`, `HTTP`, `URL`, `API`, `RPC`, not `Id`, `Http`, `Url`.
- **Interface names** — single-method interfaces use the method name + `er` suffix: `Reader`, `Writer`, `Closer`. Multi-method interfaces describe the capability: `ReadWriteCloser`.
- **Getters omit `Get`** — use `Name()`, not `GetName()`. Setters use `Set`: `SetName()`.
- **Package names** are lowercase, single-word, no underscores or mixedCaps. The package name should not repeat the import path (e.g., `config.Config`, not `config.ConfigStruct`).

## Error Handling

- **Check errors immediately** — never ignore a returned `error`. If intentionally discarding, assign to `_` with a comment explaining why.
- **Return `error` as the last return value**. Use `nil` for success.
- **Wrap errors with context** using `fmt.Errorf("operation: %w", err)` so the call chain is traceable.
- **Don't use `panic`** for expected error conditions. Reserve `panic` for truly unrecoverable programmer errors (e.g., invalid state at init time).
- **Sentinel errors** use `var ErrFoo = errors.New("foo")`. Check with `errors.Is`. Custom error types implement `Error() string` and are checked with `errors.As`.
- **Handle errors, don't just log them** — either return the error to the caller or take corrective action. Don't log and return the same error (causes duplicate noise).

## Control Flow

- **Early returns** — handle the error/edge case and return, keeping the happy path unindented:
  ```go
  if err != nil {
      return err
  }
  // happy path continues here
  ```
- **Avoid `else` after a return** — the `else` block is unnecessary and adds indentation.
- **Switch over if/else chains** when comparing a value against multiple constants.
- **Type switches** for interface type assertions, not chains of `if _, ok := x.(T)`.

## Structs, Methods, and Interfaces

- **Accept interfaces, return concrete types** — functions should accept the narrowest interface they need and return concrete types so callers get full access.
- **Define interfaces at the consumer, not the producer** — the package that uses the interface owns its definition.
- **Keep interfaces small** — one or two methods is ideal. Compose larger interfaces from smaller ones.
- **Use value receivers** for methods that don't mutate the receiver and for small structs. Use **pointer receivers** for methods that mutate or for large structs. Don't mix receiver types on a single type.
- **Struct literals use field names** — `Foo{Bar: 1}`, not `Foo{1}`.

## Slices, Maps, and Collections

- **`nil` slices are valid** — a `nil` slice has length 0 and can be appended to. Don't allocate empty slices unnecessarily (`var s []T` is preferred over `s := []T{}`).
- **Use `make`** to preallocate when size is known (also a performance rule).
- **Range loops** — prefer `for i, v := range slice` over manual indexing. Use `for range n` (Go 1.22+) when only the count matters.
- **Don't modify a collection while iterating** — collect indices/keys to delete, then delete in a second pass.

## Context

- **`context.Context` is always the first parameter**, named `ctx`:
  ```go
  func DoWork(ctx context.Context, id string) error { ... }
  ```
- **Never store `context.Context` in a struct field** — pass it through function arguments.
- **Propagate context** — pass `ctx` to all downstream calls (database, HTTP, RPC) so cancellation flows through.

## Goroutines and Concurrency

- Defer to the rules in `.claude/rules/concurrency.md`, which align with idiomatic Go patterns.

## Package Design

- **One package, one purpose** — a package should have a clear, narrowly scoped responsibility.
- **Avoid package-level `init()`** unless absolutely necessary (e.g., registering a driver). Prefer explicit initialization.
- **Internal packages** for implementation details that shouldn't be imported outside the module.
- **Avoid circular imports** — if two packages need each other, extract an interface or a third package.

## Testing

- **Table-driven tests** are the default pattern for multiple input/output combinations.
- **`_test.go` suffix** for test files, `_test` package suffix for black-box tests.
- **Test function names**: `TestFunctionName_condition` (e.g., `TestParseConfig_emptyInput`).
- Defer to the full testing rules in `.claude/rules/testing.md`.

## Formatting and Tooling

- Code must pass `gofmt` / `goimports` — this is non-negotiable in Go.
- Use `go vet` to catch common mistakes (already part of `make build`).
