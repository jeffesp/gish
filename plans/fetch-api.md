# Plan: `fetch` API for gish JS

## Status quo

- Runtime is goja (`scripting.go`). It supports `Promise` and `async`/`await`, but has no event loop, no `setTimeout`, and no Node/browser globals.
- One global `jsVM` guarded by `jsmu`. Every entry point runs JS synchronously: `js`, `source`, the prompt function, completers, and `gish.register` callbacks.
- goja Promise reactions run on the job queue after the outermost call returns. A `Runtime` is not goroutine-safe, so a goroutine cannot resolve a promise directly.

## Work items

1. **Event loop (main design risk).**
   - Use `github.com/dop251/goja_nodejs/eventloop` or a small custom loop: a channel of `func(*goja.Runtime)` callbacks drained under `jsmu`.
   - `fetch` runs the `http.Client` request in a goroutine, then posts a callback that resolves or rejects the promise.
   - Simplest integration: `js`, `source` and the other entry points drain pending work before returning (blocking-ish semantics, no background goroutine touching the VM while the user types).
   - A true background loop would also enable `setTimeout`, but needs a locking pass over the prompt and completer paths.

2. **Top-level `await` in the `js` builtin.** `RunString` is script mode, so `js await fetch(url)` does not parse. Wrap the expression in an async IIFE, drain the loop until the promise settles, then print the resolved value or the rejection.

3. **`fetch` in Go.**
   - `fetch(url, {method, headers, body, signal?})` returns a Promise of `Response`.
   - `Response`: `status`, `ok`, `statusText`, `url`, `headers`, `text()`, `json()`, `arrayBuffer()` (body methods return promises; read the body in Go).
   - `Headers`: `get`, `set`, `has`, `append`, entries/iteration (a plain wrapper is the minimum).
   - Optional extras: `URLSearchParams`, `AbortController` (most useful, cheap), `Request`, `FormData`.
   - Skip streaming `body` (`ReadableStream`).

4. **Ctrl+C.** Tie each request's `context.Context` to the existing signal handling (`signals.go`, `context.go`) so interrupting a pending `js await fetch(...)` cancels it. Otherwise a hung request wedges the shell.

5. **Housekeeping.**
   - Types in `js/gish.d.ts`, README section.
   - Tests with `httptest.Server` in `scripting_test.go`.
   - Defaults: timeout, redirect limit, proxy via env, system TLS roots.
   - Decide whether `file://` and `data:` URLs are honored (browsers don't).

## Effort

- `fetch` / `Response` / `Headers`: ~200-300 lines of Go.
- Event loop and `js`/`source` integration: ~150 lines plus tests.
- The TODO item "Tests mutate global state without cleanup guards" will matter: async tests need a clean VM per test.

## Open decision

Should pending requests complete only within the synchronous call that started them (simpler, safer; roughly a day), or run on a true background loop so `fetch(...).then(print)` fires later at the prompt (also gives timers, but needs the locking pass)?
