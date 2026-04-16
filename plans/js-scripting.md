# Add JavaScript Scripting to Gish via Goja

## Context

Gish needs a scripting layer so users can define custom commands in script files loaded at startup, and evaluate inline JS expressions (foundation for future structured pipelines). After comparing Lua, JavaScript, and WASM, JavaScript was chosen because it provides native JSON handling, functional array methods (`map`, `filter`, `reduce`), optional chaining, destructuring, and template literals — all critical for the planned structured data pipeline feature.

The engine is **goja** (`github.com/dop251/goja`), a pure-Go ES5.1+ runtime with no cgo dependency.

## Files

### New
- **`scripting.go`** — JS runtime, shell API, script loading, `js` builtin
- **`scripting_test.go`** — Tests

### Modified
- **`go.mod`** — Add `github.com/dop251/goja`
- **`main.go`** — Add `InitScripting(os.Stderr)` call before `RunREPL`

### Unchanged (but key integration points)
- `builtins.go` — `RegisterBuiltin()` is how JS commands enter the dispatch table
- `repl.go` — `execLine()` dispatches through `Builtins` map; JS commands work automatically. `tokenValues()` (line 76) reused by `js` builtin
- `context.go` — `ExecCtx` struct passed to JS-registered builtins

## JS API Surface

Scripts see a global `gish` object:

```javascript
// Register a shell command
gish.register("name", function(ctx) {
    // ctx.line  — raw input string
    // ctx.name  — command name
    // ctx.args  — []string of arguments
    gish.println("output");
});

// Execute a command, get structured result
var r = gish.exec("git", ["status", "--short"]);
// r.stdout, r.stderr, r.exitCode

// Environment
gish.env.get("HOME")
gish.env.set("KEY", "val")
gish.env.unset("KEY")
gish.env.all()  // returns {KEY: "val", ...}

// Context
gish.cwd()

// Output
gish.print("no newline")
gish.println("with newline")

// JSON (convenience wrappers — native JSON.parse/stringify also work)
gish.parseJSON(str)        // parse with better shell error messages
gish.toJSON(obj)           // pretty-print (2-space indent)
gish.toJSON(obj, false)    // compact
```

## Implementation Details

### Runtime lifecycle
- Single `*goja.Runtime` created at startup in `InitScripting()`, stored as package-level var
- Reused across all command invocations (goja is single-threaded, gish processes one command at a time)
- Field name mapper: `goja.UncapFieldNameMapper()` so Go exports look idiomatic in JS

### I/O threading
The current `ExecCtx.Out` writer varies (terminal writer in raw mode, stdout in scanner mode). Since the JS runtime is long-lived but the output target changes per invocation:
- Package-level `currentOut`/`currentErrOut` vars, defaulting to `os.Stdout`/`os.Stderr`
- Updated by a `setCurrentIO()` call before each JS builtin invocation
- `gish.println` and `gish.print` write to `currentOut`
- Safe because gish is single-command-at-a-time

### `gish.register(name, callback)` flow
1. Validate callback is callable via `goja.AssertFunction`
2. Create a `BuiltinFunc` closure that:
   - Calls `setCurrentIO(ctx.Out, ctx.ErrOut)`
   - Builds a plain JS object `{line, name, args}` from `ExecCtx`
   - `args` is `[]string` (token values only — token kinds not exposed in v1)
   - Invokes the JS callback with `recover()` to catch goja panics
   - If callback returns a non-nil/non-undefined value, prints it to `ctx.Out`
3. Call `RegisterBuiltin(name, wrappedFunc)`

### `gish.exec(cmd, args)` flow
1. Run `exec.Command(cmd, args...)` with stdout/stderr captured to buffers
2. Return JS object `{stdout, stderr, exitCode}`
3. Command-not-found or fork failures become JS exceptions

### `js` builtin
```
gish> js '2 + 2'
4
gish> js 'JSON.stringify({a: 1})'
{"a":1}
```
- Joins all arg token values with spaces as the expression
- Runs via `jsVM.RunString(expr)`
- Prints non-undefined results to `ctx.Out`
- JS exceptions become Go errors (displayed by `execLine`'s existing error handling)

### Script loading
- Directory: `$GISH_SCRIPTS_DIR` or `~/.config/gish/scripts/`
- If directory doesn't exist, silently skip (no error)
- Glob `*.js`, sort alphabetically (users can prefix `01-`, `02-` for ordering)
- Compile each with `goja.Compile(filename, source, false)`, cache in `map[string]*goja.Program`
- Run each via `vm.RunProgram(compiled)`
- Errors printed to stderr with filename; loading continues past failures

### Error handling
- **Script load errors**: Print to stderr, continue loading others
- **JS exceptions in builtins**: `recover()` catches goja panics, converts `*goja.Exception` to Go `error`
- **Type errors in API**: Use `vm.NewTypeError()` for idiomatic JS errors
- **`gish.exec` failures**: Returned in result object (`exitCode != 0`); only unexpected errors throw

## Implementation Sequence

1. `go get github.com/dop251/goja`
2. Create `scripting.go` with `InitScripting`, `setupAPI` (`gish.register` + `gish.print/println`), and `builtinJS`
3. Create `scripting_test.go` — tests for register, println, inline JS eval
4. Add `gish.exec` + tests
5. Add `gish.env` + `gish.cwd` + tests
6. Add `gish.parseJSON` + `gish.toJSON` + tests
7. Add `loadScripts` + `scriptsDir` + tests (temp dirs with .js files)
8. Wire into `main.go`: `InitScripting(os.Stderr)` before `RunREPL`

## Verification

1. **Unit tests**: `go test ./...` — all new tests in `scripting_test.go`
2. **Manual smoke test**: Create `~/.config/gish/scripts/test.js` that registers a command, run gish, invoke it
3. **Inline eval**: Run `js '2+2'`, `js 'JSON.parse("{\"a\":1}").a'`, verify output
4. **Error handling**: Run `js 'throw new Error("boom")'`, verify clean error message
5. **Bad script**: Put a syntax error in a .js file, verify other scripts still load and error is printed to stderr
