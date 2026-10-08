var symbols = { A: "+", M: "~", D: "-", R: "~", C: "+", U: "=" };

function parseGitStatus(statusSummary) {
  var staged = formatCounts(statusSummary.items.staged);
  var unstaged = formatCounts(statusSummary.items.unstaged);
  var untracked = formatCounts(statusSummary.items.untracked);

  var parts = [statusSummary.branch];
  if (statusSummary.ahead || statusSummary.behind)
    parts.push(`⇡${ahead}⇣${behind}`);

  var fileStatus = [staged, unstaged, untracked].filter(Boolean).join("|");
  if (fileStatus) parts.push(fileStatus);

  return parts.join(" ");
}

function formatCounts(obj) {
  var keys = Object.keys(obj);
  if (keys.length === 0) return "";
  return keys
    .map(function (k) {
      return k + String(obj[k].length);
    })
    .join("");
}

gish.setPrompt(function () {
  var dirPart = gish.cwd().split("/").slice(-2).join("/");

  var result = runGitStatus();
  if (!result) return dirPart + " > ";

  var st = parseGitStatus(result);
  return dirPart + " (" + st + ") > ";
});
