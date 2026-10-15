// SPDX-License-Identifier: GPL-2.0-only
//
// Tab completion for git, ported from git's contrib/completion/
// git-completion.bash (Copyright (C) 2006,2007 Shawn O. Pearce and others,
// distributed under the GNU General Public License, version 2.0). See
// git-lib.js for what changed in the port and for the licensing note.
//
// Load it from init.js with:
//
//   require("./completion/git.js");
//
// Environment variables from the bash script that are honoured:
// GIT_COMPLETION_SHOW_ALL, GIT_COMPLETION_SHOW_ALL_COMMANDS and
// GIT_COMPLETION_CHECKOUT_NO_GUESS. GIT_COMPLETION_IGNORE_CASE is not
// supported: gish matches candidates case-sensitively.

"use strict";

const L = require("./git-lib.js");
const {
  words, lines, git, gitcomp, gitcompNl, gitcompBuiltin, valueOpts, completeRefs,
  completeRevlistFile, completeIndexFile, completeRemoteOrRefspec, completeStrategy,
  findOnCmdline, findLastOnCmdline, hasDoubleDash, countArguments, findRepoPath, remotes,
} = L;

const exists = (p) => gish.fs.exists(p);
const stash = (ctx) => lines(git(ctx, ["stash", "list"])).map((l) => l.replace(/:.*/, ""));

// ---- shared option lists --------------------------------------------------

const whitespaceList = "nowarn warn error error-all fix";
const patchFormat = "mbox stgit stgit-series hg mboxrd";
const showCurrentPatch = "diff raw";
const amInprogressOptions = "--skip --continue --resolved --abort --quit --show-current-patch";
const quotedCr = "nowarn warn strip";
const untrackedFileModes = "all no normal";
const diffAlgorithms = "myers minimal patience histogram";
const colorMovedOpts = "no default plain blocks zebra dimmed-zebra";
const colorMovedWsOpts = "no ignore-space-at-eol ignore-space-change ignore-all-space allow-indentation-change";
const wsErrorHighlightOpts = "context old new all default";
const conflictStyles = "diff3 merge zdiff3";
const fetchRecurseSubmodules = "yes on-demand no";
const pushRecurseSubmodules = "check on-demand only";
const sequencerInprogressOptions = "--continue --quit --abort --skip";
const rebaseInprogressOptions = "--continue --skip --abort --quit --show-current-patch";
const rebaseInteractiveInprogressOptions = rebaseInprogressOptions + " --edit-todo";
const mergetoolsCommon = "diffuse diffmerge ecmerge emerge kdiff3 meld opendiff tkdiff vimdiff nvimdiff gvimdiff xxdiff araxis p4merge bc codecompare smerge";
const diffMergesOpts = "off none on first-parent 1 separate m combined c dense-combined cc remerge r";
const logPrettyFormats = "oneline short medium full fuller reference email raw format: tformat: mboxrd";
const formatPatchExtraOptions = "--full-index --not --all --no-prefix --src-prefix= --dst-prefix= --notes";

const diffCommonOptions = `--stat --numstat --shortstat --summary
  --patch-with-stat --name-only --name-status --color
  --no-color --color-words --no-renames --check
  --color-moved --color-moved= --no-color-moved
  --color-moved-ws= --no-color-moved-ws
  --full-index --binary --abbrev --diff-filter=
  --find-copies --find-object --find-renames
  --no-relative --relative
  --find-copies-harder --ignore-cr-at-eol
  --text --ignore-space-at-eol --ignore-space-change
  --ignore-all-space --ignore-blank-lines --exit-code
  --quiet --ext-diff --no-ext-diff --unified=
  --no-prefix --src-prefix= --dst-prefix=
  --inter-hunk-context= --function-context
  --patience --histogram --minimal
  --raw --word-diff --word-diff-regex=
  --dirstat --dirstat= --dirstat-by-file
  --dirstat-by-file= --cumulative
  --diff-algorithm= --default-prefix
  --submodule --submodule= --ignore-submodules
  --indent-heuristic --no-indent-heuristic
  --textconv --no-textconv --break-rewrites
  --patch --no-patch --cc --combined-all-paths
  --anchored= --compact-summary --ignore-matching-lines=
  --irreversible-delete --line-prefix --no-stat
  --output= --output-indicator-context=
  --output-indicator-new= --output-indicator-old=
  --ws-error-highlight=
  --pickaxe-all --pickaxe-regex`;

const diffDifftoolOptions = `--cached --staged --base --ours --theirs --no-index --merge-base
  --ita-invisible-in-index --ita-visible-in-index ${diffCommonOptions}`;

const logCommonOptions = `--not --all --branches --tags --remotes
  --first-parent --merges --no-merges --max-count=
  --max-age= --since= --after= --min-age= --until= --before=
  --min-parents= --max-parents= --no-min-parents --no-max-parents`;
const logGitkOptions = `--dense --sparse --full-history --simplify-merges
  --simplify-by-decoration --left-right --notes --no-notes`;
const logShortlogOptions = "--author= --committer= --grep= --all-match --invert-grep";
const logShowOptions = "--diff-merges --diff-merges= --no-diff-merges --dd --remerge-diff";

// Option values shared by the diff family (diff, log, show, ...).
const diffValueOpts = [
  ["--diff-algorithm=", diffAlgorithms],
  ["--submodule=", L.diffSubmoduleFormats],
  ["--color-moved=", colorMovedOpts],
  ["--color-moved-ws=", colorMovedWsOpts],
  ["--ws-error-highlight=", wsErrorHighlightOpts],
];

// ---- the commands, one per _git_<name> in the bash script -------------------

const commands = {};

commands.am = (ctx) => {
  const dir = findRepoPath(ctx);
  if (exists(dir + "/rebase-apply")) return gitcomp(ctx, amInprogressOptions);
  const r = valueOpts(ctx, [
    ["--whitespace=", whitespaceList],
    ["--patch-format=", patchFormat],
    ["--show-current-patch=", showCurrentPatch],
    ["--quoted-cr=", quotedCr],
  ]);
  if (r) return r;
  if (ctx.cur.startsWith("--")) return gitcompBuiltin(ctx, "am", "", amInprogressOptions);
  return [];
};

commands.apply = (ctx) => {
  const r = valueOpts(ctx, [["--whitespace=", whitespaceList]]);
  if (r) return r;
  return ctx.cur.startsWith("--") ? gitcompBuiltin(ctx, "apply") : [];
};

commands.add = (ctx) => {
  const r = valueOpts(ctx, [["--chmod=", "+x -x"]]);
  if (r) return r;
  if (ctx.cur.startsWith("--")) return gitcompBuiltin(ctx, "add");
  const opts = findOnCmdline(ctx, "-u --update") ? "--modified" : "--others --modified --directory --no-empty-directory";
  return completeIndexFile(ctx, opts);
};

commands.stage = commands.add;

commands.archive = (ctx) => {
  const f = ctx.cur.startsWith("--format=") && gitcomp(ctx, words(L.gitRaw(["archive", "--list"])), "--format=", ctx.cur.slice(9));
  if (f) return f;
  if (ctx.cur.startsWith("--remote=")) return gitcompNl(ctx, remotes(ctx), "--remote=", ctx.cur.slice(9));
  if (ctx.cur.startsWith("--")) return gitcompBuiltin(ctx, "archive", "--format= --list --verbose --prefix= --worktree-attributes");
  return completeRevlistFile(ctx);
};

commands.bisect = (ctx) => {
  if (hasDoubleDash(ctx)) return [];
  const subcommands = "start bad good skip reset visualize replay log run";
  const sub = findOnCmdline(ctx, subcommands);
  if (!sub) {
    const dir = findRepoPath(ctx);
    return gitcomp(ctx, exists(dir + "/BISECT_START") ? subcommands : "replay start");
  }
  return ["bad", "good", "reset", "skip", "start"].includes(sub) ? completeRefs(ctx) : [];
};

commands.branch = (ctx) => {
  let onlyLocalRef = false;
  let hasR = false;
  for (let c = ctx.cmdIdx; c < ctx.cword; c++) {
    const w = ctx.words[c];
    if (["-d", "-D", "--delete", "-m", "-M", "--move", "-c", "-C", "--copy"].includes(w)) onlyLocalRef = true;
    if (w === "-r" || w === "--remotes") hasR = true;
  }
  if (ctx.cur.startsWith("--set-upstream-to=")) {
    return completeRefs(ctx, { pfx: "--set-upstream-to=", cur: ctx.cur.slice(18) });
  }
  if (ctx.cur.startsWith("--")) return gitcompBuiltin(ctx, "branch");
  if (onlyLocalRef && !hasR) return L.heads(ctx, "", ctx.cur);
  return completeRefs(ctx);
};

commands.bundle = (ctx) => {
  const sub = ctx.words[ctx.cmdIdx + 1];
  if (ctx.cword === ctx.cmdIdx + 1) return gitcomp(ctx, "create list-heads verify unbundle");
  if (ctx.cword > ctx.cmdIdx + 2 && sub === "create") return completeRevlistFile(ctx);
  return [];
};

// __git_checkout_default_dwim_mode
function checkoutDwim(ctx) {
  let dwim = L.env("GIT_COMPLETION_CHECKOUT_NO_GUESS") === "1" ? false : true;
  if (findOnCmdline(ctx, "--no-track")) dwim = false;
  if (git(ctx, ["config", "--type=bool", "checkout.guess"]).trim() === "false") dwim = false;
  const last = findLastOnCmdline(ctx, "--guess --no-guess");
  if (last === "--guess") dwim = true;
  else if (last === "--no-guess") dwim = false;
  return dwim;
}

commands.checkout = (ctx) => {
  if (hasDoubleDash(ctx)) return [];
  const dwim = checkoutDwim(ctx);
  if (["-b", "-B", "--orphan"].includes(ctx.prev)) return completeRefs(ctx, { dwim, mode: "heads" });
  const r = valueOpts(ctx, [["--conflict=", conflictStyles]]);
  if (r) return r;
  if (ctx.cur.startsWith("--")) return gitcompBuiltin(ctx, "checkout");
  if (findOnCmdline(ctx, "-b -B -d --detach --orphan")) return completeRefs(ctx, { mode: "refs" });
  if (findOnCmdline(ctx, "-t --track")) return completeRefs(ctx, { mode: "remote-heads" });
  return completeRefs(ctx, { dwim, mode: "refs" });
};

commands.switch = (ctx) => {
  const dwim = checkoutDwim(ctx);
  if (["-c", "-C", "--orphan"].includes(ctx.prev)) return completeRefs(ctx, { dwim, mode: "heads" });
  const r = valueOpts(ctx, [["--conflict=", conflictStyles]]);
  if (r) return r;
  if (ctx.cur.startsWith("--")) return gitcompBuiltin(ctx, "switch");
  if (findOnCmdline(ctx, "--orphan")) return [];
  if (findOnCmdline(ctx, "-c -C -d --detach")) return completeRefs(ctx, { mode: "refs" });
  if (findOnCmdline(ctx, "-t --track")) return completeRefs(ctx, { mode: "remote-heads" });
  return completeRefs(ctx, { dwim, mode: "heads" });
};

commands["cherry-pick"] = (ctx) => {
  const dir = findRepoPath(ctx);
  if (exists(dir + "/CHERRY_PICK_HEAD")) return gitcomp(ctx, sequencerInprogressOptions);
  const s = completeStrategy(ctx);
  if (s) return s;
  if (ctx.cur.startsWith("--")) return gitcompBuiltin(ctx, "cherry-pick", "", sequencerInprogressOptions);
  return completeRefs(ctx);
};

commands.clean = (ctx) =>
  ctx.cur.startsWith("--") ? gitcompBuiltin(ctx, "clean") : completeIndexFile(ctx, "--others --directory");

commands.clone = (ctx) => {
  if (ctx.prev === "-c" || ctx.prev === "--config") return L.completeConfigNameAndValue(ctx);
  if (ctx.cur.startsWith("--config=")) return L.completeConfigNameAndValue(ctx, ctx.cur.slice(9), "--config=");
  return ctx.cur.startsWith("--") ? gitcompBuiltin(ctx, "clone") : [];
};

commands.commit = (ctx) => {
  if (ctx.prev === "-c" || ctx.prev === "-C") return completeRefs(ctx);
  const cur = ctx.cur;
  let r = valueOpts(ctx, [
    ["--cleanup=", "default scissors strip verbatim whitespace"],
    ["--untracked-files=", untrackedFileModes],
  ]);
  if (r) return r;
  for (const o of ["--reuse-message=", "--reedit-message=", "--fixup=", "--squash="]) {
    if (cur.startsWith(o)) return completeRefs(ctx, { pfx: o, cur: cur.slice(o.length) });
  }
  if (cur.startsWith("--trailer=")) {
    const tokens = lines(git(ctx, ["config", "--name-only", "--get-regexp", "^trailer\\..*\\.key$"])).map((k) =>
      k.replace(/^trailer\./, "").replace(/\.[^.]*$/, ""),
    );
    return gitcompNl(ctx, tokens, "--trailer=", cur.slice(10), ":");
  }
  if (cur.startsWith("--")) return gitcompBuiltin(ctx, "commit");
  return completeIndexFile(ctx, git(ctx, ["rev-parse", "--verify", "--quiet", "HEAD"]) ? "--committable" : "--cached");
};

commands.describe = (ctx) => (ctx.cur.startsWith("--") ? gitcompBuiltin(ctx, "describe") : completeRefs(ctx));

commands.diff = (ctx) => {
  if (hasDoubleDash(ctx)) return [];
  const r = valueOpts(ctx, diffValueOpts);
  if (r) return r;
  if (ctx.cur.startsWith("--")) return gitcomp(ctx, diffDifftoolOptions);
  return completeRevlistFile(ctx);
};

commands.difftool = (ctx) => {
  if (hasDoubleDash(ctx)) return [];
  const r = valueOpts(ctx, [["--tool=", mergetoolsCommon + " kompare"]]);
  if (r) return r;
  if (ctx.cur.startsWith("--")) return gitcompBuiltin(ctx, "difftool", diffDifftoolOptions);
  return completeRevlistFile(ctx);
};

commands.fetch = (ctx) => {
  const r = valueOpts(ctx, [
    ["--recurse-submodules=", fetchRecurseSubmodules],
    ["--filter=", "blob:none blob:limit= sparse:oid="],
  ]);
  if (r) return r;
  if (ctx.cur.startsWith("--")) return gitcompBuiltin(ctx, "fetch");
  return completeRemoteOrRefspec(ctx);
};

commands["format-patch"] = (ctx) => {
  const r = valueOpts(ctx, [["--thread=", "deep shallow"]]);
  if (r) return r;
  const m = /^(--(?:base|interdiff|range-diff)=)(.*)$/s.exec(ctx.cur);
  if (m) return completeRefs(ctx, { pfx: m[1], cur: m[2] });
  if (ctx.cur.startsWith("--")) return gitcompBuiltin(ctx, "format-patch", formatPatchExtraOptions);
  return completeRevlistFile(ctx);
};

commands.fsck = (ctx) => (ctx.cur.startsWith("--") ? gitcompBuiltin(ctx, "fsck") : []);

commands.grep = (ctx) => {
  if (hasDoubleDash(ctx)) return [];
  if (ctx.cur.startsWith("--")) return gitcompBuiltin(ctx, "grep");
  // As in bash, symbols (from a tags file) are all that is offered for the
  // pattern, which is the first argument or the one after an option.
  if (ctx.cword === ctx.cmdIdx + 1 || ctx.prev.startsWith("-")) return L.completeSymbol(ctx);
  return completeRefs(ctx);
};

commands.help = (ctx) => {
  if (ctx.cur.startsWith("--")) return gitcompBuiltin(ctx, "help");
  return gitcomp(ctx, [...lines(git(ctx, ["--list-cmds=main,nohelpers,alias,list-guide"])), "gitk"]);
};

commands.init = (ctx) => {
  const r = valueOpts(ctx, [["--shared=", "false true umask group all world everybody"]]);
  if (r) return r;
  return ctx.cur.startsWith("--") ? gitcompBuiltin(ctx, "init") : [];
};

commands["ls-files"] = (ctx) =>
  ctx.cur.startsWith("--") ? gitcompBuiltin(ctx, "ls-files") : completeIndexFile(ctx, "--cached");

commands["ls-remote"] = (ctx) =>
  ctx.cur.startsWith("--") ? gitcompBuiltin(ctx, "ls-remote") : gitcompNl(ctx, remotes(ctx));

commands["ls-tree"] = (ctx) => (ctx.cur.startsWith("--") ? gitcompBuiltin(ctx, "ls-tree") : completeRevlistFile(ctx));

commands.log = (ctx) => {
  if (hasDoubleDash(ctx)) return [];
  const dir = findRepoPath(ctx);
  const merge = exists(dir + "/MERGE_HEAD") ? "--merge" : "";
  const cur = ctx.cur;
  const sym = ctx.prev + "," + cur;
  if (/^-L,:.*:/.test(sym)) return [];
  if (/^-L,:/.test(sym)) return L.completeSymbol(ctx, { cur: cur.slice(1), sfx: ":" });
  if (/^-[GS],/.test(sym)) return L.completeSymbol(ctx);
  const r = valueOpts(ctx, [
    ["--pretty=", logPrettyFormats + " " + L.configVariables(ctx, "pretty").join(" ")],
    ["--format=", logPrettyFormats + " " + L.configVariables(ctx, "pretty").join(" ")],
    ["--date=", L.logDateFormats],
    ["--decorate=", "full short no"],
    ["--no-walk=", "sorted unsorted"],
    ["--diff-merges=", diffMergesOpts],
    ...diffValueOpts,
  ]);
  if (r) return r;
  if (cur.startsWith("--")) {
    return gitcomp(
      ctx,
      `${logCommonOptions} ${logShortlogOptions} ${logGitkOptions} ${logShowOptions}
      --root --topo-order --date-order --reverse --follow --full-diff
      --abbrev-commit --no-abbrev-commit --abbrev= --relative-date --date=
      --pretty= --format= --oneline --show-signature --cherry-mark --cherry-pick
      --graph --decorate --decorate= --no-decorate --walk-reflogs
      --no-walk --no-walk= --do-walk --parents --children
      --expand-tabs --expand-tabs= --no-expand-tabs ${merge} ${diffCommonOptions}`,
    );
  }
  if (/^-L:.*:/.test(cur)) return [];
  if (cur.startsWith("-L:")) return L.completeSymbol(ctx, { cur: cur.slice(3), sfx: ":" });
  if (cur.startsWith("-G")) return L.completeSymbol(ctx, { pfx: "-G", cur: cur.slice(2) });
  if (cur.startsWith("-S")) return L.completeSymbol(ctx, { pfx: "-S", cur: cur.slice(2) });
  return completeRevlistFile(ctx);
};

commands.whatchanged = commands.log;

commands.merge = (ctx) => {
  const s = completeStrategy(ctx);
  if (s) return s;
  return ctx.cur.startsWith("--") ? gitcompBuiltin(ctx, "merge") : completeRefs(ctx);
};

commands.mergetool = (ctx) => {
  const r = valueOpts(ctx, [["--tool=", mergetoolsCommon + " tortoisemerge"]]);
  if (r) return r;
  return ctx.cur.startsWith("--") ? gitcomp(ctx, "--tool= --prompt --no-prompt --gui --no-gui") : [];
};

commands["merge-base"] = (ctx) => (ctx.cur.startsWith("--") ? gitcompBuiltin(ctx, "merge-base") : completeRefs(ctx));

commands.mv = (ctx) => {
  if (ctx.cur.startsWith("--")) return gitcompBuiltin(ctx, "mv");
  return completeIndexFile(ctx, countArguments(ctx, "mv") > 0 ? "--cached --others --directory" : "--cached");
};

commands.notes = (ctx) => {
  const subcommands = "add append copy edit get-ref list merge prune remove show";
  const sub = findOnCmdline(ctx, subcommands);
  const cur = ctx.cur;
  if (!sub) {
    if (cur.startsWith("--")) return gitcompBuiltin(ctx, "notes");
    return ctx.prev === "--ref" ? completeRefs(ctx) : gitcomp(ctx, subcommands + " --ref");
  }
  const m = /^(--(?:reuse|reedit)-message=)(.*)$/s.exec(cur);
  if (m) return completeRefs(ctx, { pfx: m[1], cur: m[2] });
  if (cur.startsWith("--")) return gitcompBuiltin(ctx, "notes_" + sub);
  if (sub === "prune" || sub === "get-ref") return [];
  return ctx.prev === "-m" || ctx.prev === "-F" ? [] : completeRefs(ctx);
};

commands.pull = (ctx) => {
  const s = completeStrategy(ctx);
  if (s) return s;
  const r = valueOpts(ctx, [["--recurse-submodules=", fetchRecurseSubmodules]]);
  if (r) return r;
  return ctx.cur.startsWith("--") ? gitcompBuiltin(ctx, "pull") : completeRemoteOrRefspec(ctx);
};

commands.push = (ctx) => {
  const cur = ctx.cur;
  if (ctx.prev === "--repo") return gitcompNl(ctx, remotes(ctx));
  if (ctx.prev === "--recurse-submodules") return gitcomp(ctx, pushRecurseSubmodules);
  if (cur.startsWith("--repo=")) return gitcompNl(ctx, remotes(ctx), "--repo=", cur.slice(7));
  const r = valueOpts(ctx, [["--recurse-submodules=", pushRecurseSubmodules]]);
  if (r) return r;
  if (cur.startsWith("--force-with-lease=")) {
    // __git_complete_force_with_lease
    const v = cur.slice(19);
    const colon = v.indexOf(":");
    const pfx = "--force-with-lease=" + (colon >= 0 ? v.slice(0, colon + 1) : "");
    return completeRefs(ctx, { pfx, cur: colon >= 0 ? v.slice(colon + 1) : v });
  }
  if (cur.startsWith("--")) return gitcompBuiltin(ctx, "push");
  return completeRemoteOrRefspec(ctx);
};

commands["range-diff"] = (ctx) =>
  ctx.cur.startsWith("--")
    ? gitcomp(ctx, `--creation-factor= --no-dual-color ${diffCommonOptions}`)
    : completeRevlistFile(ctx);

commands.rebase = (ctx) => {
  const dir = findRepoPath(ctx);
  if (exists(dir + "/rebase-merge/interactive")) return gitcomp(ctx, rebaseInteractiveInprogressOptions);
  if (exists(dir + "/rebase-apply") || exists(dir + "/rebase-merge")) return gitcomp(ctx, rebaseInprogressOptions);
  const s = completeStrategy(ctx);
  if (s) return s;
  const r = valueOpts(ctx, [["--whitespace=", whitespaceList]]);
  if (r) return r;
  if (ctx.cur.startsWith("--onto=")) return completeRefs(ctx, { pfx: "--onto=", cur: ctx.cur.slice(7) });
  if (ctx.cur.startsWith("--")) return gitcompBuiltin(ctx, "rebase", "", rebaseInteractiveInprogressOptions);
  return completeRefs(ctx);
};

commands.reflog = (ctx) => (findOnCmdline(ctx, "show delete expire") ? completeRefs(ctx) : gitcomp(ctx, "show delete expire"));

// __git_config_get_set_variables: names set in the config file the command
// line points at (or the merged config).
function configGetSetVariables(ctx) {
  let file = [];
  for (let c = ctx.cword; c > ctx.cmdIdx; c--) {
    const w = ctx.words[c];
    if (w === "--system" || w === "--global" || w === "--local" || w.startsWith("--file=")) {
      file = [w];
      break;
    }
    if (w === "-f" || w === "--file") {
      file = [w, ctx.words[c + 1]];
      break;
    }
  }
  return lines(git(ctx, ["config", ...file, "--name-only", "--list"]));
}

commands.config = (ctx) => {
  if (["--get", "--get-all", "--unset", "--unset-all"].includes(ctx.prev)) return gitcompNl(ctx, configGetSetVariables(ctx));
  if (ctx.prev.includes(".")) return L.completeConfigValue(ctx, ctx.prev, ctx.cur);
  return ctx.cur.startsWith("--") ? gitcompBuiltin(ctx, "config") : L.completeConfigName(ctx);
};

commands.remote = (ctx) => {
  const subcommands = "add rename remove set-head set-branches get-url set-url show prune update";
  const sub = findOnCmdline(ctx, subcommands);
  const cur = ctx.cur;
  if (!sub) return cur.startsWith("--") ? gitcompBuiltin(ctx, "remote") : gitcomp(ctx, subcommands);
  if (cur.startsWith("--")) {
    return ["add", "set-head", "set-branches", "update", "set-url", "get-url", "prune"].includes(sub)
      ? gitcompBuiltin(ctx, "remote_" + sub)
      : [];
  }
  switch (sub) {
    case "add":
      return [];
    case "set-head":
    case "set-branches":
      return completeRemoteOrRefspec(ctx);
    case "update":
      return gitcomp(ctx, [...remotes(ctx), ...L.configVariables(ctx, "remotes")]);
    default:
      return gitcompNl(ctx, remotes(ctx));
  }
};

commands.replace = (ctx) => {
  const r = valueOpts(ctx, [["--format=", "short medium long"]]);
  if (r) return r;
  return ctx.cur.startsWith("--") ? gitcompBuiltin(ctx, "replace") : completeRefs(ctx);
};

commands.rerere = (ctx) => (findOnCmdline(ctx, "clear forget diff remaining status gc") ? [] : gitcomp(ctx, "clear forget diff remaining status gc"));

commands.reset = (ctx) => {
  if (hasDoubleDash(ctx)) return [];
  return ctx.cur.startsWith("--") ? gitcompBuiltin(ctx, "reset") : completeRefs(ctx);
};

commands.restore = (ctx) => {
  if (ctx.prev === "-s") return completeRefs(ctx);
  const r = valueOpts(ctx, [["--conflict=", conflictStyles]]);
  if (r) return r;
  if (ctx.cur.startsWith("--source=")) return completeRefs(ctx, { pfx: "--source=", cur: ctx.cur.slice(9) });
  if (ctx.cur.startsWith("--")) return gitcompBuiltin(ctx, "restore");
  return git(ctx, ["rev-parse", "--verify", "--quiet", "HEAD"]) ? completeIndexFile(ctx, "--modified") : [];
};

commands.revert = (ctx) => {
  const dir = findRepoPath(ctx);
  if (exists(dir + "/REVERT_HEAD")) return gitcomp(ctx, sequencerInprogressOptions);
  const s = completeStrategy(ctx);
  if (s) return s;
  return ctx.cur.startsWith("--") ? gitcompBuiltin(ctx, "revert", "", sequencerInprogressOptions) : completeRefs(ctx);
};

commands.rm = (ctx) => (ctx.cur.startsWith("--") ? gitcompBuiltin(ctx, "rm") : completeIndexFile(ctx, "--cached"));

commands["send-email"] = (ctx) => {
  const aliases = () => words(git(ctx, ["send-email", "--dump-aliases"]));
  if (["--to", "--cc", "--bcc", "--from"].includes(ctx.prev)) return gitcomp(ctx, aliases());
  const r = valueOpts(ctx, [
    ["--confirm=", L.sendEmailConfirm],
    ["--suppress-cc=", L.sendEmailSuppressCc],
    ["--smtp-encryption=", "ssl tls"],
    ["--thread=", "deep shallow"],
  ]);
  if (r) return r;
  const m = /^(--(?:to|cc|bcc|from)=)(.*)$/s.exec(ctx.cur);
  if (m) return gitcomp(ctx, aliases(), m[1], m[2]);
  if (ctx.cur.startsWith("--")) return gitcompBuiltin(ctx, "send-email", formatPatchExtraOptions);
  return completeRevlistFile(ctx);
};

commands.shortlog = (ctx) => {
  if (hasDoubleDash(ctx)) return [];
  if (ctx.cur.startsWith("--")) {
    return gitcomp(ctx, `${logCommonOptions} ${logShortlogOptions} --numbered --summary --email`);
  }
  return completeRevlistFile(ctx);
};

commands.show = (ctx) => {
  if (hasDoubleDash(ctx)) return [];
  const pretty = logPrettyFormats + " " + L.configVariables(ctx, "pretty").join(" ");
  const r = valueOpts(ctx, [["--pretty=", pretty], ["--format=", pretty], ["--diff-merges=", diffMergesOpts], ...diffValueOpts]);
  if (r) return r;
  if (ctx.cur.startsWith("--")) {
    return gitcomp(
      ctx,
      `--pretty= --format= --abbrev-commit --no-abbrev-commit --oneline --show-signature
      --expand-tabs --expand-tabs= --no-expand-tabs ${logShowOptions} ${diffCommonOptions}`,
    );
  }
  return completeRevlistFile(ctx);
};

commands["show-branch"] = (ctx) => (ctx.cur.startsWith("--") ? gitcompBuiltin(ctx, "show-branch") : completeRevlistFile(ctx));

commands["sparse-checkout"] = (ctx) => {
  const subcommands = "list init set disable add reapply";
  const sub = findOnCmdline(ctx, subcommands);
  if (!sub) return gitcomp(ctx, subcommands);
  if (ctx.cur.startsWith("--")) return gitcompBuiltin(ctx, "sparse-checkout_" + sub, "", "--");
  if ((sub === "set" || sub === "add") && (git(ctx, ["config", "core.sparseCheckoutCone"]).trim() === "true" || findOnCmdline(ctx, "--cone"))) {
    return L.completeDirectories(ctx);
  }
  return [];
};

commands.status = (ctx) => {
  const r = valueOpts(ctx, [
    ["--ignore-submodules=", "none untracked dirty all"],
    ["--untracked-files=", untrackedFileModes],
    ["--column=", "always never auto column row plain dense nodense"],
  ]);
  if (r) return r;
  if (ctx.cur.startsWith("--")) return gitcompBuiltin(ctx, "status");
  const state = L.getOptionValue(ctx, "-u", "--untracked-files=", untrackedFileModes, "status.showUntrackedFiles");
  let opts = "";
  if (state !== "no") {
    opts = "--cached --directory --no-empty-directory --others";
    if (findOnCmdline(ctx, "--ignored")) opts += " --ignored --exclude=*";
  }
  return completeIndexFile(ctx, opts);
};

commands.stash = (ctx) => {
  const subcommands = "push list show apply clear drop pop create branch";
  const sub = findOnCmdline(ctx, subcommands + " save");
  const cur = ctx.cur;
  if (!sub) {
    if (cur.startsWith("--")) return gitcompBuiltin(ctx, "stash_push");
    if (ctx.cword - ctx.cmdIdx !== 1) return [];
    return cur.startsWith("sa") ? gitcomp(ctx, "save") : gitcomp(ctx, subcommands);
  }
  if (cur.startsWith("--")) {
    if (sub === "list") return gitcompBuiltin(ctx, "stash_list", logCommonOptions + " " + diffCommonOptions);
    if (sub === "show") return gitcompBuiltin(ctx, "stash_show", diffCommonOptions);
    return gitcompBuiltin(ctx, "stash_" + sub);
  }
  if (sub === "branch") return ctx.cword === ctx.cmdIdx + 2 ? completeRefs(ctx) : gitcompNl(ctx, stash(ctx));
  if (["show", "apply", "drop", "pop"].includes(sub)) return gitcompNl(ctx, stash(ctx));
  return [];
};

commands.submodule = (ctx) => {
  if (hasDoubleDash(ctx)) return [];
  const subcommands = "add status init deinit update set-branch set-url summary foreach sync absorbgitdirs";
  const sub = findOnCmdline(ctx, subcommands);
  if (!sub) return ctx.cur.startsWith("--") ? gitcomp(ctx, "--quiet") : gitcomp(ctx, subcommands);
  if (!ctx.cur.startsWith("--")) return [];
  const opts = {
    add: "--branch --force --name --reference --depth",
    status: "--cached --recursive",
    deinit: "--force --all",
    update: `--init --remote --no-fetch --recommend-shallow --no-recommend-shallow
      --force --rebase --merge --reference --depth --recursive --jobs`,
    "set-branch": "--default --branch",
    summary: "--cached --files --summary-limit",
    foreach: "--recursive",
    sync: "--recursive",
  }[sub];
  return opts ? gitcomp(ctx, opts) : [];
};

commands.svn = (ctx) => {
  const subcommands = `init fetch clone rebase dcommit log find-rev set-tree commit-diff info
    create-ignore propget proplist show-ignore show-externals branch tag blame migrate mkdirs reset gc`;
  const sub = findOnCmdline(ctx, subcommands);
  if (!sub) return gitcomp(ctx, subcommands);
  if (!ctx.cur.startsWith("--")) return [];
  const remote = "--username= --config-dir= --no-auth-cache";
  const fc = `--follow-parent --authors-file= --repack= --no-metadata --use-svm-props
    --use-svnsync-props --log-window-size= --no-checkout --quiet --repack-flags
    --use-log-author --localtime --add-author-from --recursive
    --ignore-paths= --include-paths= ${remote}`;
  const init = `--template= --shared= --trunk= --tags= --branches= --stdlayout --minimize-url
    --no-metadata --use-svm-props --use-svnsync-props --rewrite-root= --prefix= ${remote}`;
  const cmt = "--edit --rmdir --find-copies-harder --copy-similarity=";
  const opts = {
    fetch: `--revision= --fetch-all ${fc}`,
    clone: `--revision= ${fc} ${init}`,
    init,
    dcommit: `--merge --strategy= --verbose --dry-run --fetch-all --no-rebase --commit-url
      --revision --interactive ${cmt} ${fc}`,
    "set-tree": `--stdin ${cmt} ${fc}`,
    "create-ignore": "--revision=",
    propget: "--revision=",
    proplist: "--revision=",
    "show-ignore": "--revision=",
    "show-externals": "--revision=",
    mkdirs: "--revision=",
    log: `--limit= --revision= --verbose --incremental --oneline --show-commit
      --non-recursive --authors-file= --color`,
    rebase: `--merge --verbose --strategy= --local --fetch-all --dry-run ${fc}`,
    "commit-diff": `--message= --file= --revision= ${cmt}`,
    info: "--url",
    branch: "--dry-run --message --tag",
    tag: "--dry-run --message",
    blame: "--git-format",
    migrate: "--config-dir= --ignore-paths= --minimize --no-auth-cache --username=",
    reset: "--revision= --parent",
  }[sub];
  return opts ? gitcomp(ctx, opts) : [];
};

commands.tag = (ctx) => {
  let f = false;
  for (let c = ctx.cmdIdx; c < ctx.cword; c++) {
    const w = ctx.words[c];
    if (["-d", "--delete", "-v", "--verify"].includes(w)) return L.tags(ctx, "", ctx.cur);
    if (w === "-f") f = true;
  }
  let out = [];
  if (ctx.prev === "-m" || ctx.prev === "-F") {
    out = [];
  } else if (ctx.prev.startsWith("-") || ctx.prev === "tag") {
    if (f) out = L.tags(ctx, "", ctx.cur);
  } else {
    out = completeRefs(ctx);
  }
  return ctx.cur.startsWith("--") ? out.concat(gitcompBuiltin(ctx, "tag")) : out;
};

commands.worktree = (ctx) => {
  const subcommands = "add list lock move prune remove unlock";
  const found = findOnCmdline(ctx, subcommands, true);
  if (!found) return gitcomp(ctx, subcommands);
  const [subIdx, sub] = found;
  if (ctx.cur.startsWith("--")) return gitcompBuiltin(ctx, "worktree_" + sub);
  const worktrees = () =>
    gitcompNl(ctx, git(ctx, ["worktree", "list", "--porcelain"]).split("\n").slice(1).filter((l) => l.startsWith("worktree ")).map((l) => l.slice(9)));
  switch (sub) {
    case "add":
      if (ctx.prev === "-b" || ctx.prev === "-B") return completeRefs(ctx);
      if (ctx.prev.startsWith("-")) return [];
      if (ctx.cword === subIdx + 1) return [];
      return ctx.words[ctx.cword - 2] === "-b" || ctx.words[ctx.cword - 2] === "-B" ? [] : completeRefs(ctx);
    case "lock":
    case "remove":
    case "unlock":
      return worktrees();
    case "move":
      return ctx.cword === subIdx + 1 ? worktrees() : [];
  }
  return [];
};

// Commands git can list the options of (`--list-cmds=parseopt`) and that have
// no handler above: complete their options, nothing else.
let parseoptCommands;
function supportsParseopt(ctx, name) {
  if (!parseoptCommands || !parseoptCommands.length) parseoptCommands = words(git(ctx, ["--list-cmds=parseopt"]));
  return parseoptCommands.includes(name);
}

function completeCommand(ctx, name) {
  const fn = Object.prototype.hasOwnProperty.call(commands, name) && commands[name];
  if (fn) return fn(ctx);
  if (supportsParseopt(ctx, name)) return ctx.cur.startsWith("--") ? gitcompBuiltin(ctx, name) : [];
  return null;
}

const globalOptions = `--paginate --no-pager --git-dir= --bare --version --exec-path --exec-path=
  --html-path --man-path --info-path --work-tree= --namespace= --no-replace-objects --help`;

// __git_main
function gitMain(ctx) {
  let command = "";
  for (let c = 1; c < ctx.cword; c++) {
    const w = ctx.words[c];
    if (w.startsWith("--git-dir=")) ctx.gitDir = w.slice(10);
    else if (w === "--git-dir") ctx.gitDir = ctx.words[++c];
    else if (w === "--bare") ctx.gitDir = ".";
    else if (w === "--help") {
      command = "help";
      break;
    } else if (w === "-c" || w === "--work-tree" || w === "--namespace") c++;
    else if (w === "-C") ctx.cargs.push("-C", ctx.words[++c]);
    else if (!w.startsWith("-")) {
      command = w;
      ctx.cmdIdx = c;
      break;
    }
  }

  if (!command) {
    switch (ctx.prev) {
      case "--git-dir":
      case "-C":
      case "--work-tree":
      case "--namespace":
        return [];
      case "-c":
        return L.completeConfigNameAndValue(ctx);
    }
    if (ctx.cur.startsWith("--")) return gitcomp(ctx, globalOptions);
    let list = "list-mainporcelain,others,nohelpers,alias,list-complete,config";
    if (L.env("GIT_COMPLETION_SHOW_ALL_COMMANDS") === "1") list = "builtins," + list;
    return gitcomp(ctx, lines(git(ctx, ["--list-cmds=" + list])));
  }

  const r = completeCommand(ctx, command);
  if (r !== null) return r;
  const expansion = L.aliasedCommand(ctx, command);
  if (expansion) {
    ctx.words[ctx.cmdIdx] = expansion;
    return completeCommand(ctx, expansion) || [];
  }
  return [];
}

// __gitk_main
function gitkMain(ctx) {
  if (hasDoubleDash(ctx)) return [];
  const dir = findRepoPath(ctx);
  const merge = exists(dir + "/MERGE_HEAD") ? "--merge" : "";
  if (ctx.cur.startsWith("--")) return gitcomp(ctx, `${logCommonOptions} ${logGitkOptions} ${merge}`);
  return completeRevlistFile(ctx);
}

// An empty result means "nothing special": gish then completes file names,
// as bash does with `complete -o default`.
function wrap(main) {
  return ({ command, args, current }) => {
    const out = main(L.createContext(command, args, current));
    return out && out.length ? out : null;
  };
}

gish.complete("git", wrap(gitMain));
gish.complete("gitk", wrap(gitkMain));

module.exports = { gitMain, gitkMain, commands };
