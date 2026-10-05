# Command-specific completers

## Summary

`gish.complete(command, fn)` registers a JS function that supplies Tab candidates for arguments of `command`. It sits beside `gish.register` and `gish.setPrompt` and is wired into `completeLine` (`complete.go`).

```js
gish.complete("git", ({ args, current, line }) => {
  if (args.length === 0) return ["add", "commit", "checkout", "status"];
  if (args[0] === "checkout") return run("git", ["branch", "--format=%(refname:short)"]).split("\n");
  // returning nothing falls back to file completion
});
```

## Callback context

An object, built from the tokens Go has already parsed, so scripts never re-implement quoting:

- `command`: the command name (first word of the pipeline segment).
- `args`: completed words after the command, **only from the cursor's own pipeline segment** (`git log | grep fo<tab>` completes for `grep`, not `git`).
- `current`: the partial word under the cursor (quotes/escapes stripped).
- `line`: the whole line, as an escape hatch.

## Return values

- Array of strings: candidates. Go filters them by prefix on `current`, dedupes and sorts, then feeds them through the existing `commonPrefix` / `spliceCompletion` / `candidateList` path, so quoting and listing behave like built-in completion.
- `null` / `undefined` (or anything that is not an array): fall back to the default file completion.
- `[]`: no candidates; nothing completes (no fallback).
- Throwing: silently fall back to default completion. Printing mid-line would corrupt the prompt.

## Implementation

- `scripting.go`: `completers` map, `gish.complete`, `jsComplete(command, args, current, line)` (takes `jsmu`).
- `complete.go`: `segmentTokens` (shared with `commandForWord`) and a scripted branch in `completeLine`. `dirOnlyCommands` / `fileOnlyCommands` still apply to the default path only.
- Tests in `scripting_test.go` and `complete_test.go`.

## Not in v1

- Alias resolution (`ll <tab>` finding `ls`'s completer). Needs loop protection.
- Descriptions or per-candidate metadata.
- Async/slow completers: a slow function blocks the editor, same as other shells.
- Migrating `dirOnlyCommands` / `fileOnlyCommands` onto this table.
