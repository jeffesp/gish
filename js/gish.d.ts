/**
 * Type declarations for the global `gish` object available to gish scripts.
 * Editors such as VS Code pick this up automatically for JS files in the
 * same folder (see jsconfig.json), giving hover docs and completion.
 */

/** Context passed to commands registered with `gish.register`. */
interface GishCommandContext {
  /** The raw command line as typed (empty inside a pipeline stage). */
  line: string;
  /** The command name. */
  name: string;
  /** Arguments passed to the command, with quoting/expansion applied. */
  args: string[];
}

/** Result of `gish.exec`. */
interface GishExecResult {
  /** Captured standard output. */
  stdout: string;
  /** Captured standard error. */
  stderr: string;
  /** Process exit code (0 on success). */
  exitCode: number;
  /** True if the `timeout` option expired and the process was killed. */
  timedOut: boolean;
}

/** Options for `gish.exec`. */
interface GishExecOptions {
  /** Working directory for the process. */
  cwd?: string;
  /** Extra environment variables, added to gish's own environment. */
  env?: Record<string, string>;
  /** Text written to the process's standard input. */
  stdin?: string;
  /** Kill the process after this many milliseconds. */
  timeout?: number;
}

/** Result of `gish.spawn`. Stdout is delivered line by line to the callback. */
interface GishSpawnResult {
  /** Captured standard error. */
  stderr: string;
  /** Process exit code (0 on success). */
  exitCode: number;
}

/** Environment variable access. */
interface GishEnv {
  /** Get an environment variable ("" if unset). */
  get(name: string): string;
  /** Set an environment variable for gish and its child processes. */
  set(name: string, value: string): void;
  /** Remove an environment variable. */
  unset(name: string): void;
  /** Return all environment variables as a name → value object. */
  all(): Record<string, string>;
}

declare namespace gish {
  /**
   * Register a custom shell command implemented in JS.
   * If the callback returns a non-null value, it is printed (followed by a
   * newline) to the command's output. Throwing makes the command fail; the
   * error message names a trace file with the full stack (see the README's
   * "Error traces" section).
   *
   * @example
   * gish.register("greet", (ctx) => "hello " + ctx.args[0]);
   */
  function register(
    name: string,
    callback: (ctx: GishCommandContext) => unknown,
  ): void;

  /** Write a value to the shell output without a trailing newline. Objects and arrays are pretty-printed as JSON. */
  function print(value?: unknown): void;

  /** Write a value to the shell output followed by a newline. Objects and arrays are pretty-printed as JSON. */
  function println(value?: unknown): void;

  /**
   * Run a command to completion and capture its output.
   * A non-zero exit does not throw; check `exitCode`. Throws if the process
   * cannot be started.
   *
   * @param cmd Executable name or path.
   * @param args Argument list (no shell interpretation).
   * @param opts Working directory, extra environment, stdin and timeout.
   */
  function exec(
    cmd: string,
    args?: string[],
    opts?: GishExecOptions,
  ): GishExecResult;

  /**
   * Run a command, calling `onLine` for each line of stdout as it arrives.
   * Throwing from `onLine` kills the process and rethrows the error.
   *
   * @param cmd Executable name or path.
   * @param args Argument list (no shell interpretation).
   * @param onLine Called with each stdout line (without the newline).
   */
  function spawn(
    cmd: string,
    args: string[],
    onLine: (line: string) => void,
  ): GishSpawnResult;

  /** Environment variable helpers. */
  const env: GishEnv;

  /** Read-only filesystem helpers. Relative paths use the current directory. */
  const fs: {
    /** Read a file as text. Throws if it cannot be read. */
    readFile(path: string): string;
    /** True if the path exists. */
    exists(path: string): boolean;
    /** Names of the entries in a directory. Throws if it cannot be read. */
    readdir(path: string): string[];
    /** Information about a path, or null if it does not exist. */
    stat(
      path: string,
    ): { isDir: boolean; isFile: boolean; size: number; mtime: number } | null;
  };

  /**
   * Register a Tab completer for a command (or for aliases that expand to it).
   * Return an array of candidates: strings, or `{text, space: false}` for a
   * candidate that should not get a trailing space (like `--format=`).
   * Return nothing to fall back to file completion, `[]` for no candidates.
   */
  function complete(
    command: string,
    callback: (ctx: {
      command: string;
      args: string[];
      current: string;
      line: string;
    }) => (string | { text: string; space?: boolean })[] | null | undefined,
  ): void;

  /** The file and directory candidates Tab would offer for `prefix`. */
  function completeFiles(
    prefix: string,
    opts?: { dirsOnly?: boolean },
  ): string[];

  /** Set the terminal window title. */
  function title(text: string): void;

  /** Exit code of the last command run at the prompt. */
  function lastExitCode(): number;

  /** Current working directory (absolute path). */
  function cwd(): string;

  /** Parse a JSON string. Throws a TypeError with a readable message on bad input. */
  function parseJSON(json: string): any;

  /**
   * Serialize a value to JSON.
   * @param value The value to serialize.
   * @param pretty Indent with 2 spaces (default `true`); pass `false` for compact output.
   */
  function toJSON(value: unknown, pretty?: boolean): string;

  /**
   * Load and execute a JS file. Relative paths resolve against the current
   * directory (while init.js runs, that is the config directory).
   * Returns the file's completion value. Errors thrown by the file propagate
   * to the caller, and the inner stack is kept in the trace file.
   */
  function source(path: string): any;

  /**
   * Define a command alias, e.g. `gish.alias("ll", "ls -l")`.
   */
  function alias(name: string, body: string): void;

  /** Remove a previously defined alias. */
  function unalias(name: string): void;

  /**
   * Set the function used to build the prompt. It is called before each
   * prompt is shown; return the prompt string. If it throws or returns
   * undefined, the default `"gish> "` is used. A thrown error is reported once
   * on stderr, with the path of a trace file holding the full stack, until the
   * error changes.
   */
  function setPrompt(fn: () => string | undefined): void;
}

/**
 * Load a CommonJS module by relative or absolute path. Relative paths resolve
 * against the requiring file, or the current directory at the prompt. `.js`
 * and `/index.js` are tried when there is no extension; `.json` is parsed.
 * Modules are cached. Packages from npm are not supported.
 */
declare function require(path: string): any;
declare const module: { exports: any };
declare const exports: any;
declare const __filename: string;
declare const __dirname: string;
