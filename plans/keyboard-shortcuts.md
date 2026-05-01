# Keyboard Shortcuts → JS Functions

## Summary

Enable users to bind keyboard shortcuts to JavaScript functions that can interact with the terminal, run subprocesses, and inject results back into the command line. Uses `term.Terminal.AutoCompleteCallback` as the hook point — no fork of x/term needed.

## Architecture

### Hook Point

`AutoCompleteCallback` fires for every keypress that x/term doesn't handle internally. Signature:

```go
func(line string, pos int, key rune) (newLine string, newPos int, ok bool)
```

Available keys (not bound by x/term): Ctrl+G (7), Tab (9), Ctrl+J (10), Ctrl+O (15), Ctrl+Q (17), Ctrl+R (18), Ctrl+S (19), Ctrl+V (22), Ctrl+X (24), Ctrl+Y (25), Ctrl+Z (26). See `plans/possible-readline.md` for full reference.

If a bound key is pressed, the callback invokes the JS function. The return value from the JS function becomes the new command line content. If no binding exists, returns `ok=false` and x/term handles the key normally.

### Flow

```
User presses Ctrl+R
    → AutoCompleteCallback fires with (line, pos, '\x12')
    → Lookup '\x12' in keybinding map
    → Found: invoke JS function with {line, pos, term} context
    → JS function runs (reads keys, writes output, spawns processes, etc.)
    → JS function returns a string (or undefined for no change)
    → Callback returns (newLine, newPos, ok) to term.Terminal
    → term.Terminal updates the displayed line
```

### JS API

#### Registration

```js
gish.bind('\x12', function(ctx) { ... });  // Ctrl+R
gish.bind('\x14', function(ctx) { ... });  // Ctrl+T
gish.unbind('\x12');                        // Remove binding
```

#### Callback Context

```js
gish.bind('\x12', function(ctx) {
    // Input state
    ctx.line   // string — current command line text
    ctx.pos    // number — cursor position (byte offset)

    // Terminal I/O primitives
    ctx.term.write(str)       // Write string to terminal
    ctx.term.readKey()        // Read single keypress, returns {char, code, ctrl}
    ctx.term.clearLine()      // ANSI: clear current line
    ctx.term.clearScreen()    // ANSI: clear screen
    ctx.term.cursorUp(n)      // ANSI: move cursor up n lines
    ctx.term.cursorDown(n)    // ANSI: move cursor down n lines
    ctx.term.size()           // Returns {width, height}

    // Terminal mode control (for spawning interactive subprocesses)
    ctx.term.restoreMode()    // Exit raw mode (cooked) — for fzf, etc.
    ctx.term.rawMode()        // Re-enter raw mode

    // Can also use existing gish APIs
    // gish.exec(), gish.spawn(), gish.env, etc.

    // Return value behavior:
    //   string  → replaces entire command line, cursor moves to end
    //   undefined/null → no change to command line
    return result;
});
```

#### Example: Pure-JS Interactive Search

```js
gish.bind('\x12', function(ctx) {
    let query = "";
    ctx.term.write("\r\nsearch: ");

    while (true) {
        let key = ctx.term.readKey();
        if (key.code === 13) break;        // Enter — accept
        if (key.code === 27) return;       // Escape — cancel
        if (key.code === 127) {            // Backspace
            query = query.slice(0, -1);
        } else if (key.char) {
            query += key.char;
        }

        ctx.term.clearLine();
        ctx.term.write("search: " + query);

        // Search and display results...
    }

    return selectedResult;
});
```

#### Example: Delegate to fzf

```js
gish.bind('\x12', function(ctx) {
    ctx.term.write("\r\n");
    ctx.term.restoreMode();

    let result = gish.exec("fzf", ["--reverse", "--height=40%"]);

    ctx.term.rawMode();
    return result.stdout.trim();
});
```

## Implementation

### Go Side

#### 1. Keybinding Map + `gish.bind()` / `gish.unbind()` (~40 LOC, scripting.go)

```go
var keyBindings = map[rune]goja.Callable{}

// In setupAPI:
gishObj.Set("bind", func(call goja.FunctionCall) goja.Value { ... })
gishObj.Set("unbind", func(call goja.FunctionCall) goja.Value { ... })
```

Validates that the key rune is provided and the callback is a function. Stores in the map.

#### 2. AutoCompleteCallback Wiring (~20 LOC, repl.go)

In `runRawREPL`, after creating the terminal:

```go
t.AutoCompleteCallback = func(line string, pos int, key rune) (string, int, bool) {
    jsmu.Lock()
    cb, ok := keyBindings[key]
    jsmu.Unlock()
    if !ok {
        return "", 0, false
    }
    return invokeKeyBinding(cb, line, pos, fd, origState)
}
```

#### 3. Key Binding Invocation (~40 LOC, repl.go or scripting.go)

`invokeKeyBinding()` builds the `ctx` object with `{line, pos, term}`, calls the JS function via `callSafe`, and translates the return value into `(newLine, newPos, ok)`:

- String return → `(returnedString, len(returnedString), true)`
- Undefined/null return → `("", 0, false)` (no change)
- Error/panic → restore raw mode if needed, return `("", 0, false)`

#### 4. `readKey()` with Escape Sequence Decoding (~70 LOC, termio.go)

New file. Reads raw bytes from the fd and decodes:
- Single byte → ASCII character or control code
- `\x1b[A/B/C/D` → arrow keys
- `\x1b[H`, `\x1b[F` → Home, End
- `\x1b[3~` → Delete
- `\x7f` → Backspace

Returns a Go struct that gets converted to a JS object `{char, code, ctrl}`:
- `char`: the printable character (empty string for control/special keys)
- `code`: numeric key code
- `ctrl`: boolean, true if Ctrl was held

#### 5. Terminal Write Primitives (~40 LOC, termio.go)

Thin wrappers around ANSI escape sequences, writing directly to the fd:
- `write(str)` → `os.File.WriteString()`
- `clearLine()` → `\033[2K\r`
- `clearScreen()` → `\033[2J\033[H`
- `cursorUp(n)` → `\033[{n}A`
- `cursorDown(n)` → `\033[{n}B`
- `size()` → `term.GetSize(fd)`

#### 6. Terminal Mode Toggling (~30 LOC, termio.go)

- `restoreMode()` → `term.Restore(fd, origState)`
- `rawMode()` → `term.MakeRaw(fd)`

These are for the subprocess case (fzf, etc.) where the child needs to manage its own terminal state.

#### 7. `ctx.term` Object Assembly (~40 LOC, scripting.go)

Build the JS `term` object with all the methods wired to the Go implementations. Passed as part of the context to the bound JS function.

### Error Handling (~20 LOC)

- If the JS function panics/throws, catch via `callSafe`, ensure raw mode is restored, return `ok=false` to AutoCompleteCallback (no line change).
- If `readKey()` hits an I/O error, throw a JS exception.
- If terminal mode toggle fails, throw a JS exception.

### Thread Safety

The callback runs inside `ReadLine` on the main goroutine. Nothing else is running JS concurrently during `ReadLine`. Lock `jsmu` for the duration of the JS call. `readKey()` blocks on fd read, which is fine — it's the only reader at that point.

## Files Touched

| File | Changes |
|------|---------|
| `scripting.go` | `gish.bind()`, `gish.unbind()`, keybinding map, `ctx.term` object assembly |
| `repl.go` | `AutoCompleteCallback` wiring, `invokeKeyBinding()` |
| `termio.go` (new) | `readKey()`, ANSI helpers, terminal mode toggling |

## Estimated Size

~260 LOC of new Go code, plus ~10 LOC of example init.js.

## Constraints

- Only keys NOT already bound by x/term can be intercepted (see list in Hook Point section above). This is an accepted limitation — we do not attempt to override x/term's built-in bindings.
- `readKey()` escape sequence decoding handles common sequences (arrows, home, end, delete, backspace). Exotic keys (F1-F12, etc.) can be added incrementally.
- `AutoCompleteCallback` is synchronous and blocking. The JS function must complete before `ReadLine` resumes. This is fine for interactive use (same as how bash blocks during fzf).
