# fzf Integration

## Summary

Let gish use [fzf](https://github.com/junegunn/fzf) as an interactive picker for history search (Ctrl+R), file insertion (Ctrl+T), and ambiguous Tab completion. fzf is an ordinary subprocess: it reads candidates on stdin, draws its UI on `/dev/tty` itself, and prints the selection to stdout. gish needs no protocol, only a key handler that runs fzf, splices the result into the line, and repaints.

## Hook points that already exist

- **`AutoCompleteCallback`** (`complete.go:completeLine`) fires for every key `x/term` doesn't bind: Ctrl+R, Ctrl+T, Ctrl+G, Ctrl+X, etc. `handleKey` releases `t.lock` around the call (`internal/term/terminal.go`, ~line 696), so blocking on a subprocess inside it is safe.
- **`spliceCompletion`, `wordAtCursor`, `shellEscape`** (`complete.go`) already put a picked path back into the line with correct quoting/escaping.
- **`ExecCtx.RestoreTerm`** (`command.go:80`) already switches the terminal out of raw mode for a child process and back. fzf sets up its own tty state, so this is belt-and-braces, but it is the established pattern.
- **`loadHistory()`** (`history.go`) returns structured entries (command, dir, exit code, times, session).

## Missing pieces

### 1. `fzfPick` primitive

```go
func fzfPick(items []string, args ...string) (selection string, ok bool)
```

- `Stdin`: the items joined by newlines (via pipe). `Stdout`: captured. `Stderr`: inherited (the UI goes to `/dev/tty`, independent of stdio).
- `ok=false` (leave the line untouched) on exit 130 (Esc/Ctrl+C), exit 1 (no match), or fzf not installed (`exec.LookPath` fails).
- Do **not** use the `term.Terminal` as stdout/stderr.

### 2. Process group / signals

Run fzf through the same machinery as external commands (`setNewProcessGroup`, `signals.go`) so Ctrl+C goes to fzf and not to gish. Check what the SIGWINCH watcher does while fzf is running (fzf handles its own resize; gish's size update should be harmless).

### 3. Repaint after fzf

`fzf --height` draws inline and leaves the screen in a state `Terminal` doesn't track; fullscreen mode uses the alternate screen and cleans up after itself. If the line is unchanged, returning `ok=true` from the callback may not redraw it, because `setLine` diffs against the old line. Likely need a small `Repaint()` method on the forked `Terminal`, next to `clearAndRepaintLinePlusNPrevious`. Verify empirically with both fullscreen and `--height=40%`.

## Integrations, in order

### Step 1: Ctrl+R history search (smallest, biggest payoff)

- Handle `'\x12'` in `completeLine` (or a dispatch table that `completeLine` consults).
- Candidates: `loadHistory()`, newest first, deduplicated by command.
- Args: `--query=<current line>`, `--scheme=history`. Optionally show dir / exit code as extra columns and filter on them (`--with-nth`, `--delimiter`), which readline's search can't do.
- Result replaces the whole line, cursor at end.

### Step 2: Tab fallback

In `completeLine`'s ambiguous branch (where it currently calls `printCandidatesBelow`), pipe `matches` to `fzfPick` instead, either always or only on a second Tab / when there are more than N candidates. Candidates are already computed, so this is ~20 lines. Splice the choice with `spliceCompletion` (trailing `/` for dirs, space otherwise, same as the unique-match case). Keep the columnar listing as the fallback when fzf is absent.

### Step 3: Ctrl+T file picker

Run fzf over a directory walk (prefer `fd` if on `$PATH`, else fzf's default walker via `FZF_DEFAULT_COMMAND`), insert the `shellEscape`d result at the cursor using `wordAtCursor` / `spliceCompletion`.

### Step 4 (optional): Alt+C cd picker

Needs Alt-key parsing in the `internal/term` fork; `bytesToKey` doesn't produce Alt chords today. Defer until the term package grows that.

## Relationship to `plans/keyboard-shortcuts.md`

That plan designs `gish.bind(key, fn)` with a JS callback context. Approach:

1. Build the Go `fzfPick` primitive and wire Ctrl+R and Tab natively (steps 1 and 2).
2. Expose it to JS as `gish.fzf(items, opts)` so Ctrl+T and custom pickers live in `js/init.js` as ordinary bindings.

The fuller `ctx.term.readKey` / cursor API from that plan is not needed for fzf.

## Testing

- `fzfPick`: unit test with a fake `fzf` script on `$PATH` (exit 0 with fixed output, exit 130, exit 1), and the not-installed case.
- Key handlers: test the line/pos returned by `completeLine` for `\x12` with a stubbed picker (make the picker a package-level `var` like `completionOut`).
- Manual: fullscreen vs `--height`, Esc leaves the line intact, Ctrl+C doesn't kill gish, wrapped long lines repaint correctly, resize while fzf is open.

## Open questions

- Should fzf be opt-in (`GISH_FZF=1` / JS setting) or auto-detected?
- Extra args: honor `$FZF_DEFAULT_OPTS` only (fzf does this itself), or add a `GISH_FZF_OPTS`?
- Tab: always fzf, or only past a candidate-count threshold?
