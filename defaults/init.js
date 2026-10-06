// gish init.js: loaded at startup from the config directory
// ($GISH_CONFIG_DIR, $XDG_CONFIG_HOME/gish, or ~/.config/gish).
// Relative paths in gish.source() resolve against this directory.

// Helper: run a command and return trimmed stdout, throwing on failure.
// Example:
//   var branch = run("git", ["rev-parse", "--abbrev-ref", "HEAD"]);
function run(cmd, args) {
  var r = gish.exec(cmd, args);
  if (r.exitCode !== 0) {
    throw new Error(cmd + " failed: " + r.stderr.trim());
  }
  return r.stdout.trim();
}

gish.alias("ll", "ls -l");
gish.alias("hg", "history | grep");

// Load more scripts, e.g. a custom prompt:
// gish.source("./prompt.js");
