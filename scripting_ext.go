package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dop251/goja"
)

// execOpts are the optional third argument of gish.exec.
type execOpts struct {
	Dir     string
	Env     map[string]string
	Stdin   *string
	Timeout time.Duration
}

func parseExecOpts(vm *goja.Runtime, v goja.Value) execOpts {
	var o execOpts
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return o
	}
	m, ok := v.Export().(map[string]any)
	if !ok {
		panic(vm.NewTypeError("exec options must be an object"))
	}
	if d, ok := m["cwd"].(string); ok {
		o.Dir = d
	}
	if e, ok := m["env"].(map[string]any); ok {
		o.Env = map[string]string{}
		for k, val := range e {
			o.Env[k] = fmt.Sprint(val)
		}
	}
	if s, ok := m["stdin"].(string); ok {
		o.Stdin = &s
	}
	switch t := m["timeout"].(type) {
	case int64:
		o.Timeout = time.Duration(t) * time.Millisecond
	case float64:
		o.Timeout = time.Duration(t * float64(time.Millisecond))
	}
	return o
}

// runExec implements gish.exec(cmd, args, opts?).
func runExec(vm *goja.Runtime, call goja.FunctionCall) goja.Value {
	cmdName := call.Argument(0).String()
	cmdArgs := exportStringSlice(vm, call.Argument(1))
	opts := parseExecOpts(vm, call.Argument(2))

	ctx := context.Background()
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, cmdName, cmdArgs...)
	cmd.WaitDelay = 500 * time.Millisecond
	cmd.Dir = opts.Dir
	if opts.Env != nil {
		cmd.Env = os.Environ()
		for k, v := range opts.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	if opts.Stdin != nil {
		cmd.Stdin = strings.NewReader(*opts.Stdin)
	}
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded)
	result := vm.NewObject()
	result.Set("stdout", stdout.String())
	result.Set("stderr", stderr.String())
	result.Set("timedOut", timedOut)
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		switch {
		case timedOut:
			code = 1
			if errors.As(err, &exitErr) && exitErr.ExitCode() > 0 {
				code = exitErr.ExitCode()
			}
		case errors.As(err, &exitErr):
			code = exitErr.ExitCode()
		default:
			// could not start the process
			panic(vm.NewGoError(err))
		}
	}
	result.Set("exitCode", code)
	return result
}

// setupFS adds gish.fs: read-only filesystem helpers. Paths are relative to
// the shell's current directory.
func setupFS(vm *goja.Runtime, gishObj *goja.Object) {
	fs := vm.NewObject()
	fs.Set("readFile", func(call goja.FunctionCall) goja.Value {
		b, err := os.ReadFile(call.Argument(0).String())
		if err != nil {
			panic(vm.NewGoError(err))
		}
		return vm.ToValue(string(b))
	})
	fs.Set("exists", func(call goja.FunctionCall) goja.Value {
		_, err := os.Stat(call.Argument(0).String())
		return vm.ToValue(err == nil)
	})
	fs.Set("readdir", func(call goja.FunctionCall) goja.Value {
		entries, err := os.ReadDir(call.Argument(0).String())
		if err != nil {
			panic(vm.NewGoError(err))
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		return vm.ToValue(names)
	})
	// stat returns null when the path does not exist.
	fs.Set("stat", func(call goja.FunctionCall) goja.Value {
		info, err := os.Stat(call.Argument(0).String())
		if err != nil {
			return goja.Null()
		}
		o := vm.NewObject()
		o.Set("isDir", info.IsDir())
		o.Set("isFile", info.Mode().IsRegular())
		o.Set("size", info.Size())
		o.Set("mtime", info.ModTime().UnixMilli())
		return o
	})
	gishObj.Set("fs", fs)
}

// jsCompleteFiles implements gish.completeFiles(prefix, {dirsOnly}): the same
// path candidates Tab offers by default.
func jsCompleteFiles(vm *goja.Runtime) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		matches := fileCandidates(call.Argument(0).String())
		if o, ok := call.Argument(1).Export().(map[string]any); ok {
			if d, _ := o["dirsOnly"].(bool); d {
				matches = filterEntries(matches, true)
			}
		}
		if matches == nil {
			matches = []string{}
		}
		return vm.ToValue(matches)
	}
}

// modules caches CommonJS modules by absolute path. Guarded by jsmu, which
// is held whenever JS (and so require) runs.
var modules = map[string]*goja.Object{}

// setupRequire defines a global require(path) for CommonJS modules. Only
// relative and absolute file paths are supported. Relative paths resolve
// against the requiring module's directory, or the current directory for
// top-level code. ".js" and "/index.js" are tried when the path has no
// extension; ".json" files are parsed.
func setupRequire(vm *goja.Runtime) {
	modules = map[string]*goja.Object{}
	vm.Set("require", makeRequire(vm, ""))
}

func makeRequire(vm *goja.Runtime, dir string) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		spec := call.Argument(0).String()
		if !strings.HasPrefix(spec, "./") && !strings.HasPrefix(spec, "../") && !filepath.IsAbs(spec) {
			panic(vm.NewTypeError("require: only relative or absolute paths are supported, got %q", spec))
		}
		base := dir
		if base == "" {
			var err error
			if base, err = os.Getwd(); err != nil {
				panic(vm.NewGoError(err))
			}
		}
		path := spec
		if !filepath.IsAbs(path) {
			path = filepath.Join(base, path)
		}
		path, err := resolveModule(path)
		if err != nil {
			panic(vm.NewGoError(fmt.Errorf("require %q: %w", spec, err)))
		}
		if m, ok := modules[path]; ok {
			return m.Get("exports")
		}
		src, err := os.ReadFile(path)
		if err != nil {
			panic(vm.NewGoError(err))
		}

		module := vm.NewObject()
		exports := vm.NewObject()
		module.Set("exports", exports)
		if strings.HasSuffix(path, ".json") {
			var v goja.Value
			func() {
				defer func() {
					if r := recover(); r != nil {
						panic(vm.NewTypeError("require %q: bad JSON", spec))
					}
				}()
				v, _ = vm.RunString("(" + string(src) + ")")
			}()
			module.Set("exports", v)
			modules[path] = module
			return v
		}
		// Registered before running so circular requires see partial exports.
		modules[path] = module

		// The wrapper stays on the file's first line so error positions match.
		prg, err := goja.Compile(path, "(function (exports, require, module, __filename, __dirname) {"+string(src)+"\n})", false)
		if err != nil {
			delete(modules, path)
			panic(vm.NewGoError(fmt.Errorf("%s: %w", path, err)))
		}
		wrapper, err := vm.RunProgram(prg)
		if err != nil {
			delete(modules, path)
			panic(err)
		}
		fn, _ := goja.AssertFunction(wrapper)
		modDir := filepath.Dir(path)
		_, err = fn(exports, exports, vm.ToValue(makeRequire(vm, modDir)), module, vm.ToValue(path), vm.ToValue(modDir))
		if err != nil {
			delete(modules, path)
			panic(err)
		}
		return module.Get("exports")
	}
}

func resolveModule(path string) (string, error) {
	candidates := []string{path}
	if filepath.Ext(path) == "" {
		candidates = append(candidates, path+".js", filepath.Join(path, "index.js"))
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.Mode().IsRegular() {
			return c, nil
		}
	}
	return "", fmt.Errorf("module not found: %s", path)
}
