---
name: golang
description: Go development guidelines covering idiomatic style, error handling, concurrency, context, and patterns for long-running network daemons.
---

# Go Development

You are an expert in idiomatic Go, with deep knowledge of concurrency, the standard library, and long-running network services.

## Core Principles

- Write simple, clear, idiomatic Go. Prefer the obvious solution over the clever one.
- Let the standard library do the work; reach for a dependency only when it earns its place.
- Accept interfaces, return concrete types. Keep interfaces small and define them where they're consumed, not where they're implemented.
- Make the zero value useful so callers can use a type without explicit initialization.
- `gofmt` is not optional. Run `go vet` and `golangci-lint`; treat their findings as bugs.

## Error Handling

- Return errors, don't panic. Reserve `panic` for truly unrecoverable programmer errors (and `recover` only at goroutine boundaries you own).
- Wrap with context as errors propagate: `fmt.Errorf("loading config: %w", err)`. Add what the caller can't already see.
- Compare with `errors.Is` and unwrap to concrete types with `errors.As` — never string-match error text.
- Define sentinel errors (`var ErrNotFound = errors.New(...)`) or typed errors for conditions callers branch on.
- Handle an error once: log it or return it, not both.

## Concurrency

- Don't reach for goroutines by default — add concurrency when there's a real need, not speculatively.
- "Share memory by communicating": prefer channels to coordinate ownership; use `sync.Mutex` for simple shared state.
- The goroutine that creates work owns its lifecycle. Every goroutine must have a clear, guaranteed exit path — no leaks.
- Always know which goroutine closes a channel (the sender, never a receiver). Use `sync.WaitGroup` to wait for completion.
- Protect shared state and run tests with `-race` in CI.
- Bound concurrency with worker pools or a semaphore channel rather than spawning unbounded goroutines per request/event.

## Context

- Pass `context.Context` as the first parameter (`ctx`) to any blocking, I/O, or potentially long-running call. Never store it in a struct.
- Plumb the same context through call chains so cancellation and deadlines propagate.
- Honor cancellation: select on `ctx.Done()` in loops and blocking operations; return `ctx.Err()`.
- Use `context.WithTimeout`/`WithCancel` for outbound calls; always `defer cancel()`.
- Context is for cancellation and request-scoped values, not for passing optional parameters.

## Long-Running Daemons

- Drive the process from a root context cancelled on `SIGINT`/`SIGTERM` (`signal.NotifyContext`); thread it into every subsystem for graceful shutdown.
- Make startup fail loudly (validate config, dial dependencies) but make steady-state resilient: reconnect with backoff, never crash on a transient peer failure.
- Use `time.Ticker`/`time.After` with context rather than bare `time.Sleep` in loops so waits stay cancellable.
- Use structured logging with levels and key/value fields; log at boundaries (connect, disconnect, retry, shutdown), not in hot loops.
- Set timeouts on every network operation (HTTP clients, RCON, sockets). The default `http.Client` has no timeout — don't use it as-is.

## Code Organization

- Package by capability, not by layer. Package names are short, lowercase, no underscores, and not stuttering (`bridge.New`, not `bridge.NewBridge`).
- Keep `main` thin: parse config, wire dependencies, start, wait for shutdown. Put logic in packages.
- Export the minimum. Unexported by default; widen only when a caller needs it.
- `internal/` for code that must not be imported outside the module.

## Testing

- Use the standard `testing` package with table-driven tests and subtests (`t.Run`).
- Prefer `t.TempDir`, `t.Cleanup`, and `httptest` over hand-rolled fixtures and live network calls.
- Test behavior through exported APIs; avoid asserting on internals.
- Run `go test -race ./...`. A test that's flaky under `-race` is a real bug.
- `testify` is fine for assertions when the project already uses it — match what's there.

## Dependencies & Tooling

- Use Go modules; keep `go.mod`/`go.sum` tidy with `go mod tidy`.
- Vendor or pin deliberately; review what each new dependency pulls in.
- Build with `CGO_ENABLED=0` for static, easily-containerized binaries unless a dep requires cgo.
