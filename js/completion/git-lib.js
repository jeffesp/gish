// SPDX-License-Identifier: GPL-2.0-only
//
// Helpers for js/completion/git.js, a port of git's contrib/completion/
// git-completion.bash (Copyright (C) 2006,2007 Shawn O. Pearce and others,
// distributed under the GNU General Public License, version 2.0) to gish's
// completion API. This file and git.js are GPL-2.0, unlike the rest of gish
// (Apache-2.0); they are loaded at runtime and not part of the gish binary.
//
// Function names follow the bash script where there is a counterpart
// (gitcomp is __gitcomp, completeRefs is __git_complete_refs, ...).
//
// Differences that come from gish rather than bash:
//   - gish hands completers dequoted words and replaces the whole word under
//     the cursor, so candidates for `--opt=value` and `rev:path` carry the
//     whole word, not just the part after the separator.
//   - bash falls back to filename completion when COMPREPLY is empty
//     (-o default); handlers return [] for that and the entry point turns it
//     into null, which gish treats the same way.
//   - Candidates are strings (trailing space added) or {text, space: false}.

"use strict";

// Static answers from git itself (completion helper output, config variable
// names, ...), keyed by what was asked.
const cache = {};

function words(str) {
  return str.split(/\s+/).filter(Boolean);
}

function lines(str) {
  return str.split("\n").filter(Boolean);
}

function env(name) {
  return gish.env.get(name);
}

// A candidate with sfx appended. A space or no suffix means the usual
// trailing space; anything else (including "") means the word continues.
function sfxCand(text, sfx) {
  if (sfx === undefined || sfx === " ") return text;
  return { text: text + sfx, space: false };
}

function candText(c) {
  return typeof c === "string" ? c : c.text;
}

// Runs git with args and returns stdout, or "" if git failed, timed out or is
// not installed. Errors are swallowed on purpose: a completer must never print.
function gitRaw(args, opts) {
  try {
    const r = gish.exec("git", args, {
      timeout: 5000,
      ...opts,
      env: {
        GIT_OPTIONAL_LOCKS: "0",
        GIT_TERMINAL_PROMPT: "0",
        ...(opts && opts.env),
      },
    });
    return r.exitCode === 0 && !r.timedOut ? r.stdout : "";
  } catch (e) {
    return "";
  }
}

// Like gitRaw, but honours the -C and --git-dir options typed on the command
// line (__git).
function git(ctx, args, opts) {
  const pre = [...ctx.cargs];
  if (ctx.gitDir != null) pre.push("--git-dir=" + ctx.gitDir);
  return gitRaw(pre.concat(args), opts);
}

// The state bash keeps in `cur`, `prev`, `words`, `cword` and a few
// __git_* globals.
function createContext(command, args, current) {
  const w = [command, ...args, current];
  return {
    words: w,
    cword: w.length - 1,
    cur: current,
    prev: w.length > 1 ? w[w.length - 2] : "",
    cmdIdx: 0,
    cargs: [],
    gitDir: null,
    repoPath: undefined,
  };
}

// The repository's git directory, taking -C and --git-dir into account ("" if
// there is none). Cached in ctx.
function findRepoPath(ctx) {
  if (ctx.repoPath !== undefined) return ctx.repoPath;
  let p = "";
  const isDir = (d) => {
    const s = gish.fs.stat(d);
    return !!s && s.isDir;
  };
  if (ctx.cargs.length) {
    const args = [...ctx.cargs];
    if (ctx.gitDir != null) args.push("--git-dir=" + ctx.gitDir);
    p = gitRaw([...args, "rev-parse", "--absolute-git-dir"]).trim();
  } else if (ctx.gitDir != null) {
    p = isDir(ctx.gitDir) ? ctx.gitDir : "";
  } else if (env("GIT_DIR")) {
    p = isDir(env("GIT_DIR")) ? env("GIT_DIR") : "";
  } else if (isDir(".git")) {
    p = ".git";
  } else {
    p = gitRaw(["rev-parse", "--git-dir"]).trim();
  }
  ctx.repoPath = p;
  return p;
}

// ---- generating candidates ------------------------------------------------

// __gitcomp: complete from a list of words (array or whitespace-separated
// string). pfx is prepended to each candidate, cur matches against the word,
// sfx is appended. The "--" in `git <cmd> --git-completion-helper` output
// separates options from their --no- forms, which are only listed once the
// word starts with --no-.
function gitcomp(ctx, list, pfx = "", cur = ctx.cur, sfx = "") {
  if (cur.endsWith("=")) return [];
  const items = typeof list === "string" ? words(list) : list;
  const out = [];
  const negated = cur.startsWith("--no-");
  for (const w of items) {
    if (w === "--") {
      if (negated) continue;
      // bash lists a literal "--no-..."; "--no-" is more useful to insert.
      if ("--no-".startsWith(cur) && cur.startsWith("--")) {
        out.push({ text: pfx + "--no-", space: false });
      }
      break;
    }
    const c = w + sfx;
    if (!c.startsWith(cur)) continue;
    out.push(c.endsWith("=") || c.endsWith(".") ? { text: pfx + c, space: false } : pfx + c);
  }
  return out;
}

// For `--opt=<value>` words: complete the value from list if cur starts with
// opt, else return null.
function valueOpt(ctx, opt, list) {
  if (!ctx.cur.startsWith(opt)) return null;
  return gitcomp(ctx, list, opt, ctx.cur.slice(opt.length));
}

// Tries [opt, list] pairs in order; returns the first that applies, else null.
function valueOpts(ctx, table) {
  for (const [opt, list] of table) {
    const r = valueOpt(ctx, opt, list);
    if (r) return r;
  }
  return null;
}

// __gitcomp_nl: complete from a list of names (array or newline-separated
// string). sfx defaults to a trailing space; "" and other suffixes do not get
// one.
function gitcompNl(ctx, list, pfx = "", cur = ctx.cur, sfx = undefined) {
  const items = typeof list === "string" ? lines(list) : list;
  return items.filter((n) => n.startsWith(cur)).map((n) => sfxCand(pfx + n, sfx));
}

// __gitcomp_builtin: options from `git <cmd> --git-completion-helper`.
// cmd uses "_" to separate subcommands ("stash_push"). incl is extra options
// to add, excl options to drop.
function gitcompBuiltin(ctx, cmd, incl = "", excl = "") {
  const key = cmd + "|" + incl + "|" + excl;
  let opts = cache[key];
  if (!opts) {
    const helper = env("GIT_COMPLETION_SHOW_ALL") === "1" ? "--git-completion-helper-all" : "--git-completion-helper";
    const out = git(ctx, [...cmd.replace("_", " ").split(" "), helper]);
    const drop = new Set(words(excl));
    // As in bash, a command without a completion helper offers nothing,
    // not even incl.
    if (!out) return [];
    opts = [...words(incl), ...words(out)].filter((o) => !drop.has(o));
    cache[key] = opts;
  }
  return gitcomp(ctx, opts);
}

// ---- command line inspection ----------------------------------------------

// __git_find_on_cmdline: the first of list found among the words after the
// command, or "". With idx, returns [index, word] or null.
function findOnCmdline(ctx, list, idx = false) {
  const wl = typeof list === "string" ? words(list) : list;
  for (let c = ctx.cmdIdx; c < ctx.cword; c++) {
    for (const w of wl) {
      if (w === ctx.words[c]) return idx ? [c, w] : w;
    }
  }
  return idx ? null : "";
}

// __git_find_last_on_cmdline: like findOnCmdline, but the last match.
function findLastOnCmdline(ctx, list) {
  const wl = words(list);
  for (let c = ctx.cword - 1; c > ctx.cmdIdx; c--) {
    for (const w of wl) {
      if (w === ctx.words[c]) return w;
    }
  }
  return "";
}

// __git_get_option_value: the value of `short<val>` / `long<val>` given on the
// command line (latest first), else the config value of configKey.
function getOptionValue(ctx, shortOpt, longOpt, values, configKey) {
  const vals = words(values);
  for (let c = ctx.cword - 1; c >= 0; c--) {
    for (const v of vals) {
      if (shortOpt + v === ctx.words[c] || longOpt + v === ctx.words[c]) return v;
    }
  }
  return configKey ? git(ctx, ["config", configKey]).trim() : "";
}

function hasDoubleDash(ctx) {
  for (let c = 1; c < ctx.cword; c++) {
    if (ctx.words[c] === "--") return true;
  }
  return false;
}

// __git_count_arguments: non-option arguments so far (options can sit
// anywhere, so this is approximate, as in bash).
function countArguments(ctx, cmd) {
  let c = 0;
  for (let i = ctx.cmdIdx; i < ctx.words.length; i++) {
    const w = ctx.words[i];
    if (w === "--" || w === cmd) c = 0;
    else if (w) c++;
  }
  return c;
}

// ---- refs -------------------------------------------------------------------

function forEachRef(ctx, format, patterns, opts) {
  return lines(git(ctx, ["for-each-ref", "--format=" + format, ...patterns], opts));
}

// __git_heads / __git_remote_heads / __git_tags: names under refs/<kind>/.
function listRefKind(ctx, kind, pfx = "", cur = "") {
  return forEachRef(ctx, "%(refname:strip=2)", [`refs/${kind}/${cur}*`, `refs/${kind}/${cur}*/**`]).map((r) => pfx + r);
}
const heads = (ctx, pfx, cur) => listRefKind(ctx, "heads", pfx, cur);
const remoteHeads = (ctx, pfx, cur) => listRefKind(ctx, "remotes", pfx, cur);
const tags = (ctx, pfx, cur) => listRefKind(ctx, "tags", pfx, cur);

// __git_dwim_remote_heads: remote branch names that exist on exactly one
// remote, the ones `git checkout <name>` would create a tracking branch for.
function dwimRemoteHeads(ctx, pfx = "", cur = "") {
  const names = forEachRef(ctx, "%(refname:strip=3)", [`refs/remotes/*/${cur}*`, `refs/remotes/*/${cur}*/**`]);
  const count = {};
  names.forEach((n) => (count[n] = (count[n] || 0) + 1));
  return names.filter((n) => count[n] === 1).map((n) => pfx + n);
}

// __git_remotes
function remotes(ctx) {
  const dir = findRepoPath(ctx);
  let out = [];
  if (dir && gish.fs.exists(dir + "/remotes")) {
    try {
      out = gish.fs.readdir(dir + "/remotes");
    } catch (e) {}
  }
  return out.concat(lines(git(ctx, ["remote"])));
}

// __git_refs: refs from this repository, or from `remote` (a configured
// remote, a path or a URL). Returns names with pfx prepended.
function refs(ctx, remote = "", track = "", pfx = "", cur = ctx.cur) {
  let from = "path";
  let dir = findRepoPath(ctx);
  let match = cur;
  const out = [];

  if (!remote) {
    if (!dir) return out;
  } else if (remotes(ctx).includes(remote)) {
    from = "remote";
  } else if (gish.fs.exists(remote + "/.git")) {
    dir = remote + "/.git";
  } else if (gish.fs.exists(remote)) {
    dir = remote;
  } else {
    from = "url";
  }

  if (from === "path") {
    if (match.startsWith("^")) {
      pfx += "^";
      match = match.slice(1);
    }
    let patterns;
    let format;
    if (match === "refs" || match.startsWith("refs/")) {
      format = "%(refname)";
      patterns = [match + "*", match + "*/**"];
      track = "";
    } else {
      for (const i of ["HEAD", "FETCH_HEAD", "ORIG_HEAD", "MERGE_HEAD", "REBASE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "BISECT_HEAD", "AUTO_MERGE"]) {
        if (i.startsWith(match) && gish.fs.exists(dir + "/" + i)) out.push(pfx + i);
      }
      format = "%(refname:strip=2)";
      patterns = [];
      for (const k of ["tags", "heads", "remotes"]) patterns.push(`refs/${k}/${match}*`, `refs/${k}/${match}*/**`);
    }
    const saved = ctx.gitDir;
    ctx.gitDir = dir;
    const names = forEachRef(ctx, format, patterns);
    ctx.gitDir = saved;
    names.forEach((n) => out.push(pfx + n));
    if (track) dwimRemoteHeads(ctx, pfx, match).forEach((n) => out.push(n));
    return out;
  }

  const ls = (patterns) => git(ctx, ["ls-remote", remote, ...patterns], { timeout: 3000 });
  if (match === "refs" || match.startsWith("refs/")) {
    for (const l of lines(ls([match + "*"]))) {
      const name = l.split(/\s+/)[1];
      if (name && !name.endsWith("^{}")) out.push(pfx + name);
    }
  } else if (from === "remote") {
    if ("HEAD".startsWith(match)) out.push(pfx + "HEAD");
    forEachRef(ctx, "%(refname:strip=3)", [`refs/remotes/${remote}/${match}*`, `refs/remotes/${remote}/${match}*/**`]).forEach((n) => out.push(pfx + n));
  } else {
    const patterns = ["refs/tags/" + match + "*", "refs/heads/" + match + "*", "refs/remotes/" + match + "*"];
    if ("HEAD".startsWith(match)) patterns.unshift("HEAD");
    for (const l of lines(ls(patterns))) {
      const name = l.split(/\s+/)[1];
      if (!name || name.endsWith("^{}")) continue;
      out.push(pfx + (name.startsWith("refs/") ? name.replace(/^refs\/[^/]*\//, "") : name));
    }
  }
  return out;
}

// __git_complete_refs. Options: remote, dwim, pfx, cur, sfx, mode ("refs"
// (default), "heads" or "remote-heads").
function completeRefs(ctx, o = {}) {
  const pfx = o.pfx || "";
  const cur = o.cur !== undefined ? o.cur : ctx.cur;
  let names;
  switch (o.mode || "refs") {
    case "refs":
      names = refs(ctx, o.remote || "", "", pfx, cur);
      break;
    case "heads":
      names = heads(ctx, pfx, cur);
      break;
    case "remote-heads":
      names = remoteHeads(ctx, pfx, cur);
      break;
    default:
      return [];
  }
  if (o.dwim) names = names.concat(dwimRemoteHeads(ctx, pfx, cur));
  return names.map((n) => sfxCand(n, o.sfx));
}

// ---- files ------------------------------------------------------------------

function escapeGlob(s) {
  return s.replace(/[\\*?[]/g, "\\$&");
}

// __git_index_files: first path components under root (a directory prefix
// with a trailing slash, or "") that start with match, as given by
// `git ls-files <opts>` (or the committable files for "--committable").
// Directories get a trailing slash and no space after them.
function indexFiles(ctx, opts, root, match) {
  const pattern = (match ? escapeGlob(match) : "?") + "*";
  const pre = root ? ["-C", root] : [];
  const args =
    opts === "--committable"
      ? [...pre, "-c", "core.quotePath=false", "diff-index", "-z", "--name-only", "--relative", "HEAD", "--", pattern]
      : [...pre, "-c", "core.quotePath=false", "ls-files", "-z", "--exclude-standard", ...words(opts), "--", pattern];
  const seen = new Map();
  for (const p of git(ctx, args).split("\0")) {
    if (!p) continue;
    const slash = p.indexOf("/");
    const name = slash < 0 ? p : p.slice(0, slash);
    seen.set(name, seen.get(name) || slash >= 0);
  }
  return [...seen].map(([name, isDir]) => (isDir ? { text: root + name + "/", space: false } : root + name));
}

// __git_complete_index_file
function completeIndexFile(ctx, opts) {
  const cur = ctx.cur;
  let pfx = "";
  let match = cur;
  const i = cur.lastIndexOf("/");
  if (i > 0) {
    pfx = cur.slice(0, i + 1);
    match = cur.slice(i + 1);
  }
  return indexFiles(ctx, opts, pfx, match);
}

// __git_complete_revlist_file: revisions, `a..b`, `a...b` and `rev:path`.
function completeRevlistFile(ctx) {
  const cur = ctx.cur;
  if (/\.\..+:/.test(cur)) return [];
  const colon = cur.indexOf(":");
  if (colon > 0) {
    const ref = cur.slice(0, colon);
    let rest = cur.slice(colon + 1);
    let pfx = ref + ":";
    let ls = ref;
    const slash = rest.lastIndexOf("/");
    if (slash > 0) {
      ls = ref + ":" + rest.slice(0, slash);
      pfx += rest.slice(0, slash + 1);
      rest = rest.slice(slash + 1);
    }
    const out = [];
    for (const e of git(ctx, ["ls-tree", "-z", ls]).split("\0")) {
      const m = /^\d+ (\w+) \w+\t(.*)$/s.exec(e);
      if (!m || !m[2].startsWith(rest)) continue;
      out.push(m[1] === "tree" ? { text: pfx + m[2] + "/", space: false } : pfx + m[2]);
    }
    return out;
  }
  for (const sep of ["...", ".."]) {
    const i = cur.indexOf(sep);
    if (i >= 0) return completeRefs(ctx, { pfx: cur.slice(0, i) + sep, cur: cur.slice(i + sep.length) });
  }
  return completeRefs(ctx);
}

// ---- remotes and refspecs ----------------------------------------------------

// __git_complete_remote_or_refspec: a remote name first, then refspecs.
function completeRemoteOrRefspec(ctx) {
  const cmd = ctx.words[ctx.cmdIdx];
  let cur = ctx.cur;
  let c = ctx.cmdIdx + 1;
  let remote = "";
  let pfx = "";
  let lhs = true;
  let noRefspec = false;
  if (cmd === "remote") c++;
  for (; c < ctx.cword; c++) {
    const w = ctx.words[c];
    if (w === "--mirror") {
      if (cmd === "push") noRefspec = true;
    } else if (w === "-d" || w === "--delete") {
      if (cmd === "push") lhs = false;
    } else if (w === "--all") {
      if (cmd === "push") noRefspec = true;
      else if (cmd === "fetch") return [];
    } else if (w === "--multiple") {
      noRefspec = true;
      break;
    } else if (!w.startsWith("-")) {
      remote = w;
      break;
    }
  }
  if (!remote) return gitcompNl(ctx, remotes(ctx));
  if (noRefspec) return [];
  if (remote === ".") remote = "";
  const colon = cur.indexOf(":");
  if (colon >= 0) {
    pfx = cur.slice(0, colon + 1);
    cur = cur.slice(colon + 1);
    lhs = false;
  } else if (cur.startsWith("+")) {
    pfx = "+";
    cur = cur.slice(1);
  }
  switch (cmd) {
    case "fetch":
      return lhs ? completeFetchRefspecs(ctx, remote, pfx, cur) : completeRefs(ctx, { pfx, cur });
    case "pull":
    case "remote":
      return lhs ? completeRefs(ctx, { remote, pfx, cur }) : completeRefs(ctx, { pfx, cur });
    case "push":
      return lhs ? completeRefs(ctx, { pfx, cur }) : completeRefs(ctx, { remote, pfx, cur });
  }
  return [];
}

// __git_complete_fetch_refspecs: `name:name` for each ref of the remote.
function completeFetchRefspecs(ctx, remote, pfx = "", cur = ctx.cur) {
  return refs(ctx, remote, "", "", cur).map((r) => pfx + r + ":" + r);
}

// ---- merge strategies --------------------------------------------------------

const mergeStrategyOptions = `ours theirs subtree subtree= patience
  histogram diff-algorithm= ignore-space-change ignore-all-space
  ignore-space-at-eol renormalize no-renormalize no-renames
  find-renames find-renames= rename-threshold=`;

function mergeStrategies(ctx) {
  if (cache.mergeStrategies) return cache.mergeStrategies;
  let out;
  try {
    const r = gish.exec("git", ["merge", "-s", "help"], { env: { LANG: "C", LC_ALL: "C" }, timeout: 3000 });
    out = r.stdout + "\n" + r.stderr;
  } catch (e) {
    return "";
  }
  const m = /[Aa]vailable strategies are: ([^\n]*(?:\n[^\n]+)*)/.exec(out);
  const list = m ? m[1].replace(/\.\s*$/, "").replace(/\s+/g, " ").trim() : "";
  // `git merge -s help` fails outside a work tree; ask again next time.
  if (list) cache.mergeStrategies = list;
  return list;
}

// __git_complete_strategy: returns candidates if the word is a strategy or
// strategy option, else null.
function completeStrategy(ctx) {
  switch (ctx.prev) {
    case "-s":
    case "--strategy":
      return gitcomp(ctx, mergeStrategies(ctx));
    case "-X":
      return gitcomp(ctx, mergeStrategyOptions);
  }
  return valueOpts(ctx, [
    ["--strategy=", mergeStrategies(ctx)],
    ["--strategy-option=", mergeStrategyOptions],
  ]);
}

// ---- config --------------------------------------------------------------------

// __git_get_config_variables: variable names under section, section removed.
function configVariables(ctx, section) {
  return lines(git(ctx, ["config", "--name-only", "--get-regexp", "^" + section + "\\..*"])).map((n) => n.slice(section.length + 1));
}

function configVars(ctx) {
  return (cache.configVars ||= words(gitRaw(["help", "--config-for-completion"])));
}

function configSections(ctx) {
  return (cache.configSections ||= words(gitRaw(["help", "--config-sections-for-completion"])));
}

// All commands (__git_compute_all_commands).
function allCommands(ctx) {
  return lines(git(ctx, ["--list-cmds=main,others,alias,nohelpers"]));
}

const logDateFormats = "relative iso8601 iso8601-strict rfc2822 short local default human raw unix auto: format:";
const diffSubmoduleFormats = "diff log short";
const sendEmailConfirm = "always never auto cc compose";
const sendEmailSuppressCc = "author self cc bodycc sob cccmd body all";

// __git_complete_config_variable_value: values for `varname`, completing
// cur. pfx is prepended to each candidate (for `-c name=value`).
function completeConfigValue(ctx, varname, cur, pfx = "") {
  const v = varname.toLowerCase();
  let m;
  const remoteNames = () => gitcompNl(ctx, remotes(ctx), pfx, cur);
  if (/^branch\..*\.(remote|pushremote)$/.test(v) || v === "remote.pushdefault") return remoteNames();
  if (/^branch\..*\.merge$/.test(v)) return completeRefs(ctx, { pfx, cur });
  if (/^branch\..*\.rebase$/.test(v)) return gitcomp(ctx, "false true merges interactive", pfx, cur);
  if ((m = /^remote\.(.*)\.fetch$/.exec(v))) {
    if (!cur) return [{ text: pfx + "refs/heads/", space: false }];
    const r = lines(git(ctx, ["ls-remote", m[1], "refs/heads/*"], { timeout: 3000 })).map((l) => {
      const ref = l.split(/\s+/)[1];
      return ref + ":refs/remotes/" + m[1] + "/" + ref.replace(/^refs\/heads\//, "");
    });
    return gitcompNl(ctx, r, pfx, cur);
  }
  if (/^remote\..*\.push$/.test(v)) {
    return gitcompNl(ctx, forEachRef(ctx, "%(refname):%(refname)", ["refs/heads"]), pfx, cur);
  }
  if (v === "pull.twohead" || v === "pull.octopus") return gitcomp(ctx, mergeStrategies(ctx), pfx, cur);
  if (v === "color.pager") return gitcomp(ctx, "false true", pfx, cur);
  if (/^color\..*\..*$/.test(v)) return gitcomp(ctx, "normal black red green yellow blue magenta cyan white bold dim ul blink reverse", pfx, cur);
  if (/^color\./.test(v)) return gitcomp(ctx, "false true always never auto", pfx, cur);
  if (v === "diff.submodule") return gitcomp(ctx, diffSubmoduleFormats, pfx, cur);
  if (v === "help.format") return gitcomp(ctx, "man info web html", pfx, cur);
  if (v === "log.date") return gitcomp(ctx, logDateFormats, pfx, cur);
  if (v === "sendemail.aliasfiletype") return gitcomp(ctx, "mutt mailrc pine elm gnus", pfx, cur);
  if (v === "sendemail.confirm") return gitcomp(ctx, sendEmailConfirm, pfx, cur);
  if (v === "sendemail.suppresscc") return gitcomp(ctx, sendEmailSuppressCc, pfx, cur);
  if (v === "sendemail.transferencoding") return gitcomp(ctx, "7bit 8bit quoted-printable base64", pfx, cur);
  return [];
}

// __git_complete_config_variable_name: sections, subsections, variables.
// pfx is prepended to each candidate; sfx replaces the trailing space after a
// complete variable name.
function completeConfigName(ctx, cur = ctx.cur, sfx = undefined, outer = "") {
  const lastDot = cur.lastIndexOf(".");
  const dotted = cur.slice(0, lastDot + 1); // "branch.main." / "remote."
  const tail = (s) => cur.slice(s.length);
  const sub = (list) => gitcomp(ctx, list, outer + dotted, tail(dotted), sfx === undefined ? "" : sfx);
  const nl = (list, s) => gitcompNl(ctx, list, outer + dotted, tail(dotted), s);

  if (/^branch\..*\..*/.test(cur)) return sub("remote pushRemote merge mergeOptions rebase");
  if (/^branch\..*/.test(cur)) {
    const base = "branch.";
    const c = cur.slice(base.length);
    return heads(ctx, "", c)
      .map((h) => ({ text: outer + base + h + ".", space: false }))
      .concat(gitcompNl(ctx, ["autoSetupMerge", "autoSetupRebase"], outer + base, c, sfx));
  }
  if (/^guitool\..*\..*/.test(cur)) return sub("argPrompt cmd confirm needsFile noConsole noRescan prompt revPrompt revUnmerged title");
  if (/^(difftool|man)\..*\..*/.test(cur)) return sub("cmd path");
  if (/^mergetool\..*\..*/.test(cur)) return sub("cmd path trustExitCode");
  if (/^pager\..*/.test(cur)) return nl(allCommands(ctx), sfx);
  if (/^remote\..*\..*/.test(cur)) return sub("url proxy fetch push mirror skipDefaultUpdate receivepack uploadpack tagOpt pushurl");
  if (/^remote\..*/.test(cur)) {
    const c = cur.slice("remote.".length);
    return gitcompNl(ctx, remotes(ctx), outer + "remote.", c, ".").concat(gitcompNl(ctx, ["pushDefault"], outer + "remote.", c, sfx));
  }
  if (/^url\..*\..*/.test(cur)) return sub("insteadOf pushInsteadOf");
  if (/\./.test(cur)) return gitcomp(ctx, configVars(ctx), outer, cur, sfx === undefined ? "" : sfx);
  return gitcomp(ctx, configSections(ctx), outer, cur, ".");
}

// __git_complete_config_variable_name_and_value: `section.name=value`, as
// used by `git -c` and `git clone --config`.
function completeConfigNameAndValue(ctx, cur = ctx.cur, outer = "") {
  const eq = cur.indexOf("=");
  if (eq >= 0) return completeConfigValue(ctx, cur.slice(0, eq), cur.slice(eq + 1), outer + cur.slice(0, eq + 1));
  return completeConfigName(ctx, cur, "=", outer);
}

// ---- misc -----------------------------------------------------------------------

// __git_aliased_command: the git command alias `name` ultimately runs, or "".
function aliasedCommand(ctx, name) {
  let cur = name;
  let last = "";
  const seen = [];
  while (cur) {
    if (seen.includes(cur)) return "";
    seen.push(cur);
    const cmdline = git(ctx, ["config", "--get", "alias." + cur]).trim();
    last = cur;
    cur = "";
    for (const w of words(cmdline)) {
      if (w === "!gitk" || w === "gitk") {
        cur = "gitk";
        break;
      }
      if (/^!/.test(w) || /^-/.test(w) || /=/.test(w) || ["git", "()", "{", ":"].includes(w) || w.startsWith("'")) continue;
      cur = w.replace(/;$/, "");
      break;
    }
  }
  return last !== name ? last : "";
}

// __git_complete_symbol: symbol names from a ctags `tags` file.
function completeSymbol(ctx, o = {}) {
  const file = o.tags || "tags";
  const cur = o.cur !== undefined ? o.cur : ctx.cur;
  let text;
  try {
    text = gish.fs.readFile(file);
  } catch (e) {
    return [];
  }
  const out = [];
  for (const l of text.split("\n")) {
    if (l.startsWith("!") || !l.startsWith(cur)) continue;
    out.push(sfxCand((o.pfx || "") + l.split(/\s/)[0], o.sfx));
  }
  return out;
}

// Directory completion for sparse-checkout (__gitcomp_directories).
function completeDirectories(ctx) {
  const cur = ctx.cur;
  const dir = cur.slice(0, cur.lastIndexOf("/") + 1);
  const out = [];
  for (const d of git(ctx, ["ls-tree", "-z", "-d", "--name-only", "HEAD", dir]).split("\0")) {
    if (d && d.startsWith(cur)) out.push({ text: d + "/", space: false });
  }
  if (!out.length && cur.endsWith("/")) out.push(cur);
  return out;
}

module.exports = {
  cache, words, lines, env, sfxCand, candText, git, gitRaw, createContext, findRepoPath,
  gitcomp, valueOpt, valueOpts, gitcompNl, gitcompBuiltin,
  findOnCmdline, findLastOnCmdline, getOptionValue, hasDoubleDash, countArguments,
  forEachRef, heads, remoteHeads, tags, dwimRemoteHeads, remotes, refs, completeRefs,
  indexFiles, completeIndexFile, completeRevlistFile,
  completeRemoteOrRefspec, completeFetchRefspecs,
  mergeStrategies, completeStrategy, configVariables, configVars, configSections, allCommands,
  completeConfigValue, completeConfigName, completeConfigNameAndValue,
  aliasedCommand, completeSymbol, completeDirectories,
  logDateFormats, diffSubmoduleFormats, sendEmailConfirm, sendEmailSuppressCc, mergeStrategyOptions,
};
