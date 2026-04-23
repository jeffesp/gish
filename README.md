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
history [N] [--since DATE] [--until DATE] [--ok | --fail] [--dir PATH]
```

| Flag           | Meaning                                               |
| -------------- | ----------------------------------------------------- |
| `N`            | Show last N results (after all other filters applied) |
| `--since DATE` | Only entries with StartTime >= DATE                   |
| `--until DATE` | Only entries with StartTime <= DATE                   |
| `--ok`         | Only entries with ExitCode == 0                       |
| `--fail`       | Only entries with ExitCode != 0                       |
| `--dir PATH`   | Only entries where Dir contains PATH as a substring   |

`DATE` format: `YYYY-MM-DD` (parsed as local midnight). `--ok` and `--fail` are mutually exclusive.

## Scripting

`gish` embeds a JavaScript runtime ([goja](https://github.com/dop251/goja)) so you can define custom commands in `.js` files. Scripts are loaded from `$GISH_SCRIPTS_DIR` or `~/.config/gish/scripts/` at startup. Files are loaded in alphabetical order — prefix with `01-`, `02-`, etc. to control ordering.

### Inline JS

Evaluate JavaScript expressions directly from the prompt:

```
gish> js 2 + 2
4
gish> js JSON.stringify({a: 1})
{"a":1}
```

### The `gish` API

Scripts have access to a global `gish` object:

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

// Environment variables
gish.env.get("HOME");
gish.env.set("KEY", "val");
gish.env.unset("KEY");
gish.env.all(); // returns {KEY: "val", ...}

// Current working directory
gish.cwd();

// Output
gish.print("no newline");
gish.println("with newline");

// JSON helpers
gish.parseJSON(str); // parse with shell-friendly error messages
gish.toJSON(obj); // pretty-print (2-space indent)
gish.toJSON(obj, false); // compact
```

### Example: Git Helpers

Save this as `~/.config/gish/scripts/01-git-helpers.js`:

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
