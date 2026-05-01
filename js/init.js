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
  var status = run("git", ["status", "--short", "--porcelain=v1"]);
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

gish.alias("ls", "lsd --group-dirs first --icon never");
gish.alias("ll", "ls -l");
gish.alias("hg", "history | grep");

gish.source("./prompt.js");
