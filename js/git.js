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
