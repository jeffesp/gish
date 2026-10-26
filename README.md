# gish

This is a shell written in Go. A 'go-ish' shell. It is not compatible with other shells, although if you only do basic stuff (and do not rely on scripts) it will work fine.

It is built only for my use, and I do not have particularly complex requirements. At least in some respects. My day-to-day at the
command prompt is pretty simple. I only use a few keyboard shortcuts to navigate the input, and only do very basic stuff. But I also
want to have more capability for scripting with a real language, which is the motive for this project. I want to use JS for that, and
add support for structured pipelines.

## Status

This is nowhere near a real shell yet. BUT, I will be adding globs and tab-completion next, and I think with those it will be my daily driver.

## Builtins

The following builtins are available:

### Environment Manipulation - `env`, `set`, and `unset`

- `env` will print all the current environment variables.
- `set KEY VALUE` will either create or overwrite an environment variable
- `unset KEY` will remove the environment variable

### Working Dir - `cwd`, and `cd`

- `cwd` prints current working directory
- `cd` changes current working directory

### Printing - `echo`

- `echo INPUT` will print INPUT back to the terminal.

### History - `history`, `!!`, and `![prog]`

- `history` - lots of options as history is more than just the command log. See the section on History below.
- `!!` - run last command
- `![prog]` - rerun last `prog` from history

### Other - `clear`, `js`

- `clear` clears the screen. Ctrl-L will also do that.
- `js` runs JavaScript inline - see the section on JavaScript below.

## Moving by word

- Alt-B / Alt-F move back / forward over a run of letters and digits, so `a/b` takes two steps (it stops at the slash).
- Ctrl-Left / Ctrl-Right (and Alt-Left / Alt-Right) move over a whitespace-delimited word, so `a/b` is one step.

## History

The history file is saved to `~/.gish_history` by default. That can be changed by setting `GISH_HISTORY_FILE` environment variable.
If you want to limit the number of history entries, set `GISH_HISTORY_MAX`. If that is unset or zero then history is unlimited.

On each file execution we save the following pieces of information:

- command as entered
- current working directory
- exit code
- start time
- end time
- session id

I think most of these are self-explanatory. Session id is just the PID of the current shell.

### Querying History

All flags are optional and combinable.

```
history [N] [--all] [--long] [--since DATE] [--until DATE] [--ok | --fail] [--dir PATH]
```

By default only the current session's entries are shown, one per line, as `number  exit-code  command`:

```
gish> history
    1    0  ls
    2    1  false
    3    0  echo hi
```

The number is the entry's position in the full history file, so it works with `!N` (for example `!3` reruns `echo hi`). It stays the same when other flags filter the list. `--long` adds the start time, directory and duration to each line. `history --help` (or `-h`) prints a short summary of the flags below.

| Flag           | Meaning                                               |
| -------------- | ----------------------------------------------------- |
| `N`            | Show last N results (after all other filters applied) |
| `--all`        | Include entries from all sessions, not just this one  |
| `--long`       | Also show start time, directory and duration          |
| `--since DATE` | Only entries with StartTime >= DATE                   |
| `--until DATE` | Only entries with StartTime <= end of DATE            |
| `--ok`         | Only entries with ExitCode == 0                       |
| `--fail`       | Only entries with ExitCode != 0                       |
| `--dir PATH`   | Only entries where Dir contains PATH as a substring   |

`DATE` format: `YYYY-MM-DD` (parsed as local midnight). `--ok` and `--fail` are mutually exclusive.

## Command Lists

Join commands on one line with `;`, `&&` and `||`:

```
gish> mkdir foo && cd foo
gish> make test || echo failed
gish> cd /tmp; ls
```

- `a ; b` runs `b` after `a` whatever happened.
- `a && b` runs `b` only if `a` exited 0.
- `a || b` runs `b` only if `a` exited non-zero.
- They chain left to right, like in bash: `false && echo x || echo y` prints `y`.
- Each side can be a pipeline: `ls | grep go && echo found`.
- `$?` is the exit code of the last command that ran. Quote the operator (`';'`, `'&&'`) or escape it (`\;`) to pass it as an argument.
- Aliases may contain these operators: `gish.alias("up", "git pull && git push")`.
- Not supported: grouping with `( )` or `{ }`, and background jobs. A trailing `&` is rejected with an error rather than silently running in the foreground.

## Redirection

Send a command's streams to files, or read its input from one:

```
gish> echo hello > out.txt
gish> echo more >> out.txt
gish> make 2> errors.txt
gish> make &> build.log
gish> sort < names.txt
```

| Operator       | Named form    | Meaning                          |
| -------------- | ------------- | -------------------------------- |
| `>`            | `out>`        | stdout to file (overwrite)       |
| `>>`           | `out>>`       | stdout to file (append)          |
| `2>`           | `err>`        | stderr to file (overwrite)       |
| `2>>`          | `err>>`       | stderr to file (append)          |
| `&>`           | `out+err>`    | stdout and stderr to one file    |
| `&>>`          | `out+err>>`   | both, appended                   |
| `<`            | `in<`         | stdin from file                  |

- Operators must start a word: `echo > f` redirects but `echo>f` is one word. The file name may follow immediately (`2>/dev/null`).
- Each command in a pipeline has its own redirects. `cmd 2> /dev/null | next` discards `cmd`'s errors and pipes its output. `cmd > f | next` writes to `f` and `next` gets nothing.
- If a stream is redirected more than once, the last one wins.
- Quote or escape an operator to pass it as an argument: `echo '>'`.
- Not supported yet: `2>&1` style descriptor duplication (use `&>` or `|&`), here-documents, and redirect options on `gish.exec`/`gish.spawn`.

## Redirection

Send a command's streams to files, or read its input from one:

```
gish> echo hello > out.txt
gish> echo more >> out.txt
gish> make 2> errors.txt
gish> make &> build.log
gish> sort < names.txt
```

| Operator       | Named form    | Meaning                          |
| -------------- | ------------- | -------------------------------- |
| `>`            | `out>`        | stdout to file (overwrite)       |
| `>>`           | `out>>`       | stdout to file (append)          |
| `2>`           | `err>`        | stderr to file (overwrite)       |
| `2>>`          | `err>>`       | stderr to file (append)          |
| `&>`           | `out+err>`    | stdout and stderr to one file    |
| `&>>`          | `out+err>>`   | both, appended                   |
| `<`            | `in<`         | stdin from file                  |

- Operators must start a word: `echo > f` redirects but `echo>f` is one word. The file name may follow immediately (`2>/dev/null`).
- Each command in a pipeline has its own redirects. `cmd 2> /dev/null | next` discards `cmd`'s errors and pipes its output. `cmd > f | next` writes to `f` and `next` gets nothing.
- If a stream is redirected more than once, the last one wins.
- Quote or escape an operator to pass it as an argument: `echo '>'`.
- Not supported yet: `2>&1` style descriptor duplication (use `&>` or `|&`), here-documents, and redirect options on `gish.exec`/`gish.spawn`.

## Last Exit Code

After each command runs, its exit code is available in three places:

- `$?` (or `${?}`) expands to it on the next line, like classic shells:

  ```
  gish> false
  gish> echo $?
  1
  ```

- `$GISH_LASTEXIT` holds the same value in the environment passed to spawned child processes.
- `gish.lastExitCode()` returns the same value from JavaScript.

## Scripting

`gish` embeds a JavaScript runtime ([goja](https://github.com/dop251/goja)) so you can define custom commands in JS. At startup, gish loads `init.js` from its config directory. The config directory is resolved in order: `$GISH_CONFIG_DIR`, `$XDG_CONFIG_HOME/gish`, or `~/.config/gish`. On first run, if that directory does not exist, gish creates it and writes a basic `init.js` (an existing directory is never touched).

### Inline JS

Evaluate JavaScript expressions directly from the prompt:

```
gish> js 2 + 2
4
gish> js JSON.stringify({a: 1})
{"a":1}
```

### Error traces

When JavaScript throws (from `js`, `source`, `init.js`, a registered command, or the prompt function), gish writes the full stack trace to its own file and appends the path to the error message:

```
gish> js function f() { throw new Error("x") } f()
Error: x (trace: /home/me/.local/state/gish/traces/20260101-120000.000-1234-1.txt)
```

- Traces are written to `$GISH_TRACE_DIR`, `$XDG_STATE_HOME/gish/traces`, or `~/.local/state/gish/traces`, in that order. The directory is created if needed.
- At startup, traces older than `$GISH_KEEP_TRACE_DAYS` (default `7`) are deleted. Set it to `0` to keep them forever.
- Set `GISH_TRACES=0` to disable trace files entirely.
- A failing prompt function is traced once per distinct error, not on every prompt.

### The `gish` API

Scripts have access to a global `gish` object. For editor hover docs and completion (e.g. VS Code), copy [`js/gish.d.ts`](js/gish.d.ts) into your config directory next to `init.js`, along with [`js/jsconfig.json`](js/jsconfig.json).

```javascript
// Register a shell command
gish.register("name", function (ctx) {
  // ctx.line  — raw input string
  // ctx.name  — command name
  // ctx.args  — array of argument strings
});

// Run an external command and capture its output
var r = gish.exec("git", ["status", "--short"]);
// r.stdout, r.stderr, r.exitCode

// Stream command output line-by-line
var r = gish.spawn("ls", ["-al"], function (line) {
  gish.println(">> " + line);
});
// r.stderr, r.exitCode (stdout is consumed by the callback and given to the callback line-by-line)

// Options: working directory, extra environment, stdin, timeout (ms)
var r = gish.exec("git", ["ls-remote", "origin"], {
  cwd: "/path/to/repo",
  env: { GIT_TERMINAL_PROMPT: "0" },
  stdin: "text for the process",
  timeout: 2000,
}); // r.timedOut is true if the timeout killed it

// Read-only filesystem helpers
gish.fs.readFile("notes.txt"); // text; throws if unreadable
gish.fs.exists("notes.txt"); // boolean
gish.fs.readdir("."); // ["a", "b"]
gish.fs.stat("notes.txt"); // {isDir, isFile, size, mtime} or null

// Environment variables
gish.env.get("HOME");
gish.env.set("KEY", "val");
gish.env.unset("KEY");
gish.env.all(); // returns {KEY: "val", ...}

// Current working directory
gish.cwd();

// Exit code of the most recently run command
gish.lastExitCode();

// Output
gish.print("no newline");
gish.println("with newline");

// Set the terminal window title
gish.title("my title");

// JSON helpers
gish.parseJSON(str); // parse with shell-friendly error messages
gish.toJSON(obj); // pretty-print (2-space indent)
gish.toJSON(obj, false); // compact
```

### Modules - `require`

`require("./path")` loads a CommonJS module (`module.exports`, `exports`, `__dirname`, `__filename`). Relative paths resolve against the requiring file, or the current directory at the prompt; `.js`, `/index.js` and `.json` are found without an extension. Each module runs in its own scope and is cached. Only file paths work, not npm packages (which mostly rely on Node's APIs that gish does not have).

### Tab completion - `gish.complete`

```javascript
gish.complete("git", ({ command, args, current, line }) => {
  if (args.length === 0) return ["status", "--format="];
  return null; // fall back to file completion
});
```

Return an array of strings. When the only match ends in `/`, `=`, `:`, `.` or `-` (for example `--format=` or `HEAD:`), no space is added so you can keep typing. Return `[]` for no candidates, or nothing to fall back to file completion. `gish.completeFiles(prefix, {dirsOnly})` returns the file candidates so a completer can mix them with its own. A completer also applies to aliases that expand to the command (`alias g git`).

### Git completion

[`js/completion/git.js`](js/completion/git.js) (with `git-lib.js` beside it) is a port of git's own `git-completion.bash` to `gish.complete`, so Tab knows git's commands, options, branches, tags, remotes, stash entries, config keys, and paths in the index or working tree. Copy the `completion` directory into your config directory and add this to `init.js`:

```javascript
require("./completion/git.js");
```

It asks git itself for most of what it offers (`--list-cmds`, `--git-completion-helper`, `for-each-ref`, `ls-files`), so it follows your git version. It honours `-C`, `--git-dir`, git aliases (`git co <tab>`) and gish aliases (`alias g git`). `GIT_COMPLETION_SHOW_ALL`, `GIT_COMPLETION_SHOW_ALL_COMMANDS` and `GIT_COMPLETION_CHECKOUT_NO_GUESS` work as they do in the bash script; `GIT_COMPLETION_IGNORE_CASE` does not. Differences from bash: directories get a trailing `/` and no space, and `--no-` is offered where bash prints `--no-...`.

These two files are GPL-2.0 (the same licence as the script they are ported from), unlike the rest of gish, which is Apache-2.0. They are loaded at run time and are not part of the gish binary.

### `console`

A global `console` works like the one in node and browsers:

```javascript
console.log("a", 1, {k: "v"}); // stdout, space-separated; objects pretty-printed
console.log("%s is %d", "x", 42); // %s %d %i %f %j %o %O %c, %% for a literal %
console.info(...); console.debug(...); // same as log
console.error(...); console.warn(...); // stderr
console.dir(obj);
console.assert(cond, "message"); // stderr, only when cond is falsy
console.count("label"); console.countReset("label");
console.group("title"); console.groupEnd(); // indents output
console.time("t"); console.timeLog("t"); console.timeEnd("t");
```

Not implemented: `console.table` and `console.trace`.

### Example: Set the Title From the Prompt

The prompt function runs before each prompt, so it can also set the window title:

```javascript
gish.setPrompt(function () {
  gish.title(gish.cwd());
  return "gish> ";
});
```

### Example: Git Helpers

Save this in `~/.config/gish/init.js` (or source it from init.js):

```javascript
// Helper: run a command and return trimmed stdout
function run(cmd, args) {
  var r = gish.exec(cmd, args);
  if (r.exitCode !== 0) {
    throw new Error(cmd + " failed: " + r.stderr.trim());
  }
  return r.stdout.trim();
}

// Helper: parse lines into array, filtering empties
function lines(str) {
  return str.split("\n").filter(function (l) {
    return l.length > 0;
  });
}

// "gs" — compact git status with file count
gish.register("gs", function (ctx) {
  var status = run("git", ["status", "--short"]);
  if (status.length === 0) {
    gish.println("clean");
    return;
  }
  var files = lines(status);
  gish.println(files.length + " changed file(s):");
  files.forEach(function (f) {
    gish.println(f);
  });
});

// "gb" — list branches with current branch highlighted
gish.register("gb", function (ctx) {
  var output = run("git", ["branch", "--no-color"]);
  lines(output).forEach(function (b) {
    if (b.indexOf("*") === 0) {
      gish.println(">> " + b.substring(2));
    } else {
      gish.println("   " + b.trim());
    }
  });
});

// "gl" — git log with optional count (default 5)
gish.register("gl", function (ctx) {
  var n = ctx.args.length > 0 ? ctx.args[0] : "5";
  var log = run("git", ["log", "--oneline", "-" + n]);
  gish.println(log);
});
```

Then use them:

```
gish> gs
3 changed file(s):
   M scripting.go
  ?? scripting_test.go
  ?? README.md
gish> gb
>> main
   feature/scripting
gish> gl 3
d517494 Don't exit on signals
5e47c92 add exec context
1725224 add history implementation
```

## Installing

- **macOS** (Homebrew): `brew install jeffesp/tap/gish`
- **Windows** (Scoop): `scoop bucket add jeffesp https://github.com/jeffesp/scoop-bucket` then `scoop install gish`
- **Linux**: download the `.deb` or `.rpm` from the [releases page](https://github.com/jeffesp/gish/releases)
- Or download an archive for any platform from the releases page.

## Building

```
go build -o gish .
```

The version string defaults to `dev` and can be stamped at build time:

```
go build -trimpath -ldflags "-s -w -X main.Version=v1.0.0" -o gish .
gish --version   # or gish -v
```

Releases are built with [GoReleaser](https://goreleaser.com) (`.goreleaser.yaml`). When a `v*` tag is
pushed, the release workflow (`.github/workflows/release.yml`) builds static binaries for
linux/darwin/windows on amd64 and arm64 and attaches the archives, `.deb` and `.rpm` packages, and
`SHA256SUMS.txt` to the GitHub release. The Linux packages install `/usr/bin/gish` and register it in
`/etc/shells`. To try a release build locally:

```
goreleaser release --snapshot --clean
```
