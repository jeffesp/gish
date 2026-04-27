var symbols = { A: "+", M: "~", D: "-", R: "~", C: "+", U: "=" };

function parseGitStatus(output) {
  var lines = output.split("\n").filter(function (l) {
    return l.length > 0;
  });
  var branch = "";
  var ahead = 0;
  var behind = 0;
  var counts = { staged: {}, unstaged: {}, untracked: {} };

  lines.forEach((line) => {
    if (line.startsWith("# branch.head ")) {
      branch = line.split(" ")[2];
    } else if (line.startsWith("# branch.ab ")) {
      var parts = line.split(" ");
      ahead = parseInt(parts[2]);
      behind = Math.abs(parseInt(parts[3]));
    } else if (line[0] === "?") {
      counts.untracked["?"] = (counts.untracked["?"] || 0) + 1;
    } else if (line[0] === "1" || line[0] === "2") {
      var [x, y] = line.split(" ")[1];
      if (x !== "." && symbols[x]) {
        counts.staged[symbols[x]] = (counts.staged[symbols[x]] || 0) + 1;
      }
      if (y !== "." && symbols[y]) {
        counts.unstaged[symbols[y]] = (counts.unstaged[symbols[y]] || 0) + 1;
      }
    } else if (line[0] === "u") {
      counts.staged["="] = (counts.staged["="] || 0) + 1;
    }
  });

  var staged = formatCounts(counts.staged);
  var unstaged = formatCounts(counts.unstaged);
  var untracked = formatCounts(counts.untracked);

  var parts = [branch];
  if (ahead || behind) parts.push(`⇡${ahead}⇣${behind}`);

  var fileStatus = [staged, unstaged, untracked].filter(Boolean).join("|");
  if (fileStatus) parts.push(fileStatus);

  return parts.join(" ");
}

function formatCounts(obj) {
  var keys = Object.keys(obj);
  if (keys.length === 0) return "";
  return keys
    .map(function (k) {
      return k + obj[k];
    })
    .join("");
}

gish.setPrompt(function () {
  var dirPart = gish.cwd().split("/").slice(-2).join("/");

  var result = gish.exec("git", ["status", "--porcelain=v2", "--branch"]);
  if (result.exitCode !== 0) return dirPart + " > ";

  var st = parseGitStatus(result.stdout);
  return dirPart + " (" + st + ") > ";
});
