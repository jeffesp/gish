// Helper: run a command and return trimmed stdout
function run(cmd, args) {
  var r = gish.exec(cmd, args);
  if (r.exitCode !== 0) {
    throw new Error(cmd + " failed: " + r.stderr.trim());
  }
  return r.stdout.trim();
}

gish.alias("ls", "lsd --group-dirs first --icon never");
gish.alias("ll", "ls -l");
gish.alias("hg", "history | grep");

gish.source("./prompt.js");
