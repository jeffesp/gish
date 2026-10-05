var symbols = { A: "+", M: "~", D: "-", R: "~", C: "+", U: "=" };

function runGitStatus() {
  var result = gish.exec("git", ["status", "--porcelain=v2", "--branch"]);
  if (result.exitCode !== 0) {
    return null;
  }
  var lines = result.stdout.split("\n").filter(function (l) {
    return l.length > 0;
  });
  var branch = "";
  var ahead = 0;
  var behind = 0;
  var items = { staged: {}, unstaged: {}, untracked: {} };

  lines.forEach((line) => {
    if (line.startsWith("# branch.head ")) {
      branch = line.split(" ")[2];
    } else if (line.startsWith("# branch.ab ")) {
      var parts = line.split(" ");
      ahead = parseInt(parts[2]);
      behind = Math.abs(parseInt(parts[3]));
    } else if (line[0] === "?") {
      (items.untracked["?"] ||= []).push(line.slice(2));
    } else if (line[0] === "1" || line[0] === "2") {
      // "1 XY sub mH mI mW hH hI path" (8 fields before the path)
      // "2 XY sub mH mI mW hH hI Xscore path\torigPath" (9 fields before)
      var statusLine = line.split(" ");
      var file = statusLine
        .slice(line[0] === "1" ? 8 : 9)
        .join(" ")
        .split("\t")[0];
      var [x, y] = statusLine[1];

      if (x !== "." && symbols[x]) {
        (items.staged[symbols[x]] ||= []).push(file);
      }
      if (y !== "." && symbols[y]) {
        (items.unstaged[symbols[y]] ||= []).push(file);
      }
    } else if (line[0] === "u") {
      // "u XY sub m1 m2 m3 mW h1 h2 h3 path" (10 fields before the path)
      var unmerged = line.split(" ").slice(10).join(" ");
      (items.unstaged["="] ||= []).push(unmerged);
    }
  });

  return { branch, ahead, behind, items };
}

// Flatten a {symbol: [files]} group into a unique list of files.
function getAnyStatusFiles(statusItem) {
  var seen = {};
  Object.keys(statusItem).forEach(function (k) {
    statusItem[k].forEach(function (f) {
      seen[f] = true;
    });
  });
  return Object.keys(seen);
}

gish.complete("git", ({ args }) => {
  if (args.length === 0) {
    return ["add", "branch", "commit", "checkout", "status"];
  }
  if (args[0] === "checkout") {
    return run("git", ["branch", "--format=%(refname:short)"]).split("\n");
  }
  if (args[0] === "add") {
    // get files that are possible to add
    const status = runGitStatus();
    if (!status) return;

    // Only files that `git add` can still act on: modified and untracked.
    return getAnyStatusFiles({
      ...status.items.unstaged,
      ...status.items.untracked,
    });
  }
});
