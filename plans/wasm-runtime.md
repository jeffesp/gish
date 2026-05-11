# WASM Runtime for Gish: Feasibility Analysis

## Context

Thought experiment: replace gish's Goja (pure-Go JS engine) with a WASM runtime. The shell would execute `.wasm` binaries directly, with a built-in `build` command that compiles JS source to WASM on demand. Users writing in other languages (Rust, C, Go, etc.) could compile to WASM externally and the shell would run those too.

## The Key Library: wazero

**`github.com/tetratelabs/wazero`** — pure-Go WASM runtime, zero CGO, zero external deps.

This is the clear choice. It preserves gish's current property of being a single `go build` with no C toolchain. Production-proven (used by Docker, Helm, Trivy). ~5.5MB binary size impact, ~3ms cold start per module.

Full WASI Preview 1 support covers everything a shell command needs:
- stdin/stdout/stderr
- filesystem access
- environment variables
- command-line arguments
- exit codes

### What the API looks like

```go
import (
    "github.com/tetratelabs/wazero"
    "github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

func runWasm(wasmBytes []byte, args []string, ctx *ExecCtx) error {
    wctx := context.Background()
    r := wazero.NewRuntime(wctx)
    defer r.Close(wctx)

    wasi_snapshot_preview1.MustInstantiate(wctx, r)

    config := wazero.NewModuleConfig().
        WithStdin(ctx.In).
        WithStdout(ctx.Out).
        WithStderr(ctx.ErrOut).
        WithArgs(args...).
        WithFS(os.DirFS("/")).          // or a sandboxed subset
        WithSysNanotime().
        WithSysWalltime()

    // Pass env vars from shell
    for _, e := range os.Environ() {
        k, v, _ := strings.Cut(e, "=")
        config = config.WithEnv(k, v)
    }

    _, err := r.InstantiateWithConfig(wctx, wasmBytes, config)
    return err
}
```

The `ModuleConfig` maps almost 1:1 to gish's `ExecCtx` — stdin, stdout, stderr, args. Pipelines would work naturally: wire `os.Pipe()` to `WithStdin`/`WithStdout` exactly like `command.go` does today for external processes.

## Pluggable Builder System

The shell doesn't host an interpreter — it delegates compilation to external tools via a simple contract:

**Contract**: given a source file, produce a `.wasm` file. The shell doesn't care how.

### Configuration

Builders are registered as command templates in `~/.config/gish/config.toml`, keyed by file extension:

```toml
[builders]
js  = "javy build {file} -o {out}"
ts  = "javy build {file} -o {out}"
rs  = "cargo-wasi build {file} -o {out}"
go  = "sh -c 'GOOS=wasip1 GOARCH=wasm go build -o {out} {file}'"
zig = "zig build-exe -target wasm32-wasi -o {out} {file}"
```

The shell ships with a hardcoded default builder for `.js` (javy). Users override or extend for any language in their config.

### Execution flow when running a source file

```
user runs: myscript.js arg1 arg2
  1. extension = ".js" → look up builder
  2. check cache: ~/.cache/gish/wasm/<hash>.wasm
     - cache key = hash(source content) or (path, mtime)
     - if fresh → skip to step 4
  3. build: javy build myscript.js -o ~/.cache/gish/wasm/<hash>.wasm
     - if build fails → report error, stop
  4. run: wazero.InstantiateWithConfig(cachedWasm, args=["myscript.js", "arg1", "arg2"])
```

### Caching

Compiled `.wasm` output is cached in `~/.cache/gish/wasm/`. Cache invalidation options:

- **mtime-based**: fast, works for local dev (file path + mtime → cache key)
- **content hash**: robust, handles file moves/renames (sha256 of source → cache key)

mtime is probably the right default — it's what `make` does and it's fast. Content hash as an option for when it matters.

### Pre-compiled .wasm files

If the command is already a `.wasm` file, skip the builder entirely:

```
mycommand.wasm arg1 arg2    # runs directly through wazero, no build step
```

This is how users who compile externally (Rust, C, etc.) use the system — they just drop `.wasm` files somewhere on PATH or in `~/.config/gish/commands/`.

### Default: Javy for JS

**`github.com/bytecodealliance/javy`** — Bytecode Alliance project (originally Shopify). Compiles JS to WASM by embedding QuickJS.

The resulting `.wasm` is a standard WASI module. With dynamic linking, output modules are tiny (1-16 KB). With static linking, ~870 KB per module.

Javy provides `Javy.IO.readSync(fd, buffer)` / `Javy.IO.writeSync(fd, buffer)` for stdin/stdout/stderr via WASI. Scripts that just compute and return a value work simply. Scripts that need streaming I/O use the Javy APIs.

User installs Javy once (`brew install javy`), then everything just works. If javy isn't installed and someone tries to run a `.js` file, the shell gives a clear error: "builder 'javy' not found — install with: brew install javy".

## Architecture: What Changes

### Current flow
```
user types command
  → tokenize → expand → parse
  → Builtins[name]?  → run Go function
  → else             → exec.Command(name, args...)
```

### WASM flow
```
user types command
  → tokenize → expand → parse
  → Builtins[name]?      → run Go function  (cd, set, etc.)
  → is it a .wasm file?  → wazero.InstantiateWithConfig()
  → else                 → exec.Command(name, args...)
```

The WASM execution slot sits between builtins and external commands. Or, if you want `.wasm` files to be found on PATH like any other executable, you'd add WASM-awareness to the external command path.

### Goja is removed entirely — one runtime, not two

No hybrid approach. Goja goes away. Everything that was `init.js` moves to either declarative config or WASM modules.

### What init.js does today and where it moves

Looking at the actual `init.js` and `prompt.js` usage:

**1. Aliases** → declarative config (TOML)

Current:
```js
gish.alias("ls", "lsd --group-dirs first --icon never");
gish.alias("ll", "ls -l");
gish.alias("hg", "history | grep");
```

Becomes `~/.config/gish/config.toml`:
```toml
[aliases]
ls = "lsd --group-dirs first --icon never"
ll = "ls -l"
hg = "history | grep"
```

**2. Builders** → declarative config (TOML)

```toml
[builders]
js = "javy build {file} -o {out}"
ts = "javy build {file} -o {out}"
```

**3. Prompt** → a WASM module

The prompt function today shells out to `git status`, parses the output, and returns a string. In the WASM model, `prompt` is a command:

- Write `prompt.js` (or prompt in any language)
- Build it: `javy build prompt.js -o prompt.wasm`
- Configure in TOML: `prompt = "~/.config/gish/prompt.wasm"`
- Shell runs it on every prompt, captures stdout as the prompt string

The contract: the prompt module receives the current working directory as argv[1], writes the prompt string to stdout, exits 0. If it fails or isn't configured, fall back to `"gish> "`.

This is actually cleaner than the current approach — the prompt logic is a standalone program with a testable contract instead of a callback wired into a runtime.

For the prompt specifically, the shell should pre-compile and cache the wazero compiled module at startup (not just the .wasm, but wazero's native compilation) so the per-keystroke overhead is minimal. wazero's `CompileModule` returns a `CompiledModule` that can be instantiated repeatedly without re-parsing.

**4. Custom commands (`gish.register()`)** → WASM files in a commands directory

Current:
```js
gish.register("mycmd", function(ctx) { ... });
```

Becomes: a `.wasm` file (or source file with a matching builder) in `~/.config/gish/commands/`. At startup, gish scans this directory:
- `.wasm` files → registered as builtins directly
- Source files (`.js`, `.rs`, etc.) → built via the appropriate builder, then registered

The command name is the filename without extension: `mycmd.wasm` → command `mycmd`.

**5. `gish.exec()` / `gish.spawn()` from JS** → WASI stdin/stdout/subprocess

WASM modules can't call `gish.exec()` directly. Instead, they use WASI to run subprocesses. However, WASI Preview 1 does **not** support subprocess spawning — this is a WASI Preview 2 feature (`wasi:cli/command`), and wazero doesn't support P2 yet.

Workaround options:
- **Custom host function**: Export a `gish_exec(cmd, args) → (stdout, stderr, exitCode)` function from the Go host into the WASM module's import namespace. wazero supports this — you define host functions that WASM modules can call. This is the pragmatic path.
- **Wait for WASI P2**: Not practical near-term.
- **Pre-pipe**: For the prompt use case, the shell could run `git status` itself and pass the output to the prompt module via stdin. The prompt module just parses and formats.

The custom host function approach is the right one. It keeps the shell in control of subprocess execution while giving WASM modules the ability to shell out:

```go
// Host function exported to WASM modules
r.NewHostModuleBuilder("gish").
    NewFunctionBuilder().
    WithFunc(func(ctx context.Context, m api.Module, cmdPtr, cmdLen, argsPtr, argsLen uint32) uint32 {
        // Read cmd and args from WASM memory
        // Run exec.Command(cmd, args...)
        // Write result back to WASM memory
        // Return handle to result
    }).
    Export("exec").
    Instantiate(ctx)
```

**6. `gish.setPrompt()` / `gish.env` / `gish.cwd()`** → not needed

These were bridge functions between JS and Go. With WASM + WASI:
- env vars are passed via `WithEnv()` at instantiation
- cwd is passed as an argument or via env
- prompt is configured declaratively, not via callback

## Other Languages → WASM

This is the real payoff. Any language with a WASM target works:

| Language | Compile to WASM | Notes |
|----------|----------------|-------|
| Rust | `cargo build --target wasm32-wasip1` | First-class WASI support |
| C/C++ | `clang --target=wasm32-wasi` or Emscripten | Mature toolchain |
| Go | `GOOS=wasip1 GOARCH=wasm go build` | Official since Go 1.21 |
| Zig | `zig build -target wasm32-wasi` | Excellent WASM support |
| AssemblyScript | `asc file.ts -o file.wasm` | TypeScript-like, WASM-native |
| JavaScript | `javy build file.js -o file.wasm` | Via QuickJS embedding |
| Python | Via Pyodide or ComponentizeJS | Heavier, less practical |
| Swift | SwiftWasm project | Experimental |

A user could write a command in Rust for performance-critical work, another in Go, another in JS — all run the same way through gish.

## Performance Considerations

- **WASM cold start ~3ms** — imperceptible for interactive shell use
- **Compilation cache**: wazero supports ahead-of-time compilation caching. First run of a `.wasm` module compiles to native; subsequent runs are near-instant. Use `wazero.NewCompilationCacheWithDir("~/.cache/gish/wasm")`.
- **Prompt hot path**: Pre-compile the prompt module at startup via `r.CompileModule()`. Instantiate a fresh instance per prompt call (cheap) without re-parsing the WASM (expensive). This keeps prompt latency sub-millisecond after first load.
- **Memory**: Each WASM module instance uses ~10MB. For a shell where you run one command at a time, this is fine. For pipelines with multiple WASM stages, memory scales linearly.

## Practical Concerns

1. **Javy as external dependency**: Users need `javy` installed to compile JS→WASM. Clear error messaging when it's missing. Could also support other JS→WASM compilers via the builder system.

2. **Debugging**: WASM stack traces are less readable than Goja's JS errors. Source maps help but add complexity.

3. **WASM module discovery**: `~/.config/gish/commands/` directory scanned at startup. Files found on a `WASMPATH` env var or similar could also work, but start simple.

4. **WASI subprocess limitation**: WASI P1 doesn't support spawning subprocesses. Custom host functions (`gish_exec`) bridge this gap for modules that need to shell out (like the current prompt.js does with `git status`).

## Summary

| Aspect | What to use |
|--------|-------------|
| WASM runtime | `wazero` (pure Go, zero CGO) |
| JS→WASM default builder | `javy` (Bytecode Alliance, embeds QuickJS) |
| WASI for I/O | `wasi_snapshot_preview1` (bundled with wazero) |
| Shell config | TOML — `~/.config/gish/config.toml` (aliases, builders, prompt path) |
| Custom commands | `.wasm` or source files in `~/.config/gish/commands/` |
| Prompt | A WASM module: receives cwd, writes prompt string to stdout |
| Go dependency change | Remove `goja`, add `wazero` (still one dep, still pure Go) |

No Goja. One runtime. Config is declarative TOML. Everything that runs code runs through wazero.
