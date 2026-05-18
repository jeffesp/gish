# Readline Capabilities: Options and Trade-offs

## Current State

gish uses `golang.org/x/term` (v0.42.0) for terminal handling. The `Terminal` type provides raw-mode line editing with basic keybindings (arrows, Ctrl+A/E/K/W/U/T/L, history navigation) and two extensibility points:

- **`History` interface** — already implemented (`termHistory` in history.go), seeds from history file
- **`AutoCompleteCallback`** — fires for any key NOT already handled in the built-in switch. Receives `(line, pos, key)`, returns `(newLine, newPos, ok)`. Can intercept tab and any unbound key like Ctrl+R.

## What x/term Can Do Without a Fork

| Feature | How | Complexity |
|---------|-----|------------|
| Up/down arrow history | `t.History` interface | Done |
| Tab completion | `AutoCompleteCallback`, key='\t' | Medium — need to build candidate lookup (files, commands, builtins) and display logic |
| Ctrl+R history search | `AutoCompleteCallback`, key='\x12' (ASCII 18, unbound) | Medium — change prompt to `(reverse-search):`, filter history as user types, return match |
| `!!` / `!prefix` expansion | `expandHistory()` in execLine | Done |

## What Requires a Fork of x/term

| Feature | Why |
|---------|-----|
| Override built-in key bindings | `handleKey()` switch runs before `AutoCompleteCallback`; bound keys never reach the callback |
| Multi-line editing | `ReadLine` returns on Enter unconditionally; need to modify `handleKey` to support continuation |
| Rich interactive sub-modes | Callback can only return a replacement line, can't take over the input loop |

### Fork difficulty

The key dispatch is a single switch in `handleKey()` (~150 lines) in `terminal.go`. Adding a new binding is ~5 lines. Adding a pre-dispatch hook (e.g. `KeyCallback` that fires before the switch) would be ~15 lines and make all future bindings possible without further modifications. x/term is stable and rarely changes, so maintenance burden is low.

## Alternative: Adopt a Readline Library

| Library | Stars | Maintained | Ctrl+R | Tab | Multi-line | Custom keys | Notes |
|---------|-------|-----------|--------|-----|-----------|-------------|-------|
| [reeflective/readline](https://github.com/reeflective/readline) | ~136 | Yes (Jan 2026) | Fuzzy incremental | Yes | Yes | Full `.inputrc` | Most feature-complete. vi+emacs modes, syntax highlighting, undo/redo. Large dependency. |
| [ergochat/readline](https://github.com/ergochat/readline) | ~52 | Yes (Sep 2024) | Yes | Yes | No | Yes | Maintained fork of chzyer/readline. Smaller API surface. |
| [nyaosorg/go-readline-ny](https://github.com/nyaosorg/go-readline-ny) | ~34 | Yes (Apr 2026) | No | Yes | No | `BindKey()` API | Very active. Used by NYAGOS shell. Pluggable backends. |
| [chzyer/readline](https://github.com/chzyer/readline) | ~2.3k | No | Yes | Yes | No | Yes | Classic but unmaintained. Use ergochat fork instead. |

### Concern

Adopting a full readline library brings features gish doesn't need (vi mode, `.inputrc`, syntax highlighting) and a large dependency. But incrementally building readline features on x/term risks doing it poorly — tab completion display, search UX, multi-line editing are all non-trivial to get right.

## Recommended Approach (deferred)

1. **Now:** Use `AutoCompleteCallback` for tab completion and Ctrl+R. No fork, no new dependencies. Callback-based code carries over if we fork later.
2. **Later, if needed:** Fork x/term to add a `KeyCallback` pre-dispatch hook, enabling override of built-in bindings and multi-line support.
3. **Reconsider readline adoption** only if we find ourselves reimplementing many features that a library already provides well.

## x/term Key Bindings Reference

Currently bound keys in `terminal.go` (these CANNOT be intercepted via callback):

| Key | ASCII | Action |
|-----|-------|--------|
| Ctrl+A | 1 | Move to line start |
| Ctrl+B | 2 | Move cursor left |
| Ctrl+C | 3 | EOF / interrupt |
| Ctrl+D | 4 | EOF (empty line) or forward delete |
| Ctrl+E | 5 | Move to line end |
| Ctrl+F | 6 | Move cursor right |
| Ctrl+H | 8 | Backspace |
| Ctrl+K | 11 | Delete to end of line |
| Ctrl+L | 12 | Clear screen |
| Ctrl+N | 14 | Next history (down) |
| Ctrl+P | 16 | Previous history (up) |
| Ctrl+T | 20 | Transpose chars |
| Ctrl+U | 21 | Delete to start of line |
| Ctrl+W | 23 | Delete word backward |

Unbound and available via callback: Ctrl+G (7), Ctrl+I/Tab (9), Ctrl+J (10), Ctrl+O (15), Ctrl+Q (17), **Ctrl+R (18)**, Ctrl+S (19), Ctrl+V (22), Ctrl+X (24), Ctrl+Y (25), Ctrl+Z (26).
