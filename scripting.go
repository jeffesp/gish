package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dop251/goja"
)

var jsVM *goja.Runtime

// InitScripting creates the JS runtime, wires the gish API, and loads user scripts.
// Call after ctx.Out is set to the final writer (terminal or stdout).
func InitScripting(ctx *ExecCtx) {
	jsVM = goja.New()
	jsVM.SetFieldNameMapper(goja.UncapFieldNameMapper())

	setupAPI(jsVM, ctx)
	RegisterBuiltin("js", builtinJS)
	loadScripts(jsVM, ctx)
}

func setupAPI(vm *goja.Runtime, ctx *ExecCtx) {
	gishObj := vm.NewObject()

	// gish.register(name, callback)
	gishObj.Set("register", func(call goja.FunctionCall) goja.Value {
		name := call.Argument(0).String()
		cb, ok := goja.AssertFunction(call.Argument(1))
		if !ok {
			panic(vm.NewTypeError("second argument must be a function"))
		}
		RegisterBuiltin(name, func(bCtx *ExecCtx) error {
			jsCtx := vm.NewObject()
			jsCtx.Set("line", bCtx.Line)
			jsCtx.Set("name", bCtx.Name())
			jsCtx.Set("args", tokenValues(bCtx.Args()))

			val, err := callSafe(cb, goja.Undefined(), jsCtx)
			if err != nil {
				return err
			}
			if val != nil && !goja.IsUndefined(val) && !goja.IsNull(val) {
				fmt.Fprintln(bCtx.Out, val.String())
			}
			return nil
		})
		return goja.Undefined()
	})

	// gish.print / gish.println
	gishObj.Set("print", func(call goja.FunctionCall) goja.Value {
		fmt.Fprint(ctx.Out, call.Argument(0).String())
		return goja.Undefined()
	})
	gishObj.Set("println", func(call goja.FunctionCall) goja.Value {
		fmt.Fprintln(ctx.Out, call.Argument(0).String())
		return goja.Undefined()
	})

	// gish.exec(cmd, args)
	gishObj.Set("exec", func(call goja.FunctionCall) goja.Value {
		cmdName := call.Argument(0).String()
		cmdArgs := exportStringSlice(vm, call.Argument(1))

		cmd := exec.Command(cmdName, cmdArgs...)
		var stdout, stderr strings.Builder
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()

		result := vm.NewObject()
		result.Set("stdout", stdout.String())
		result.Set("stderr", stderr.String())
		code := 0
		if err != nil {
			code = exitCode(err)
			if code == 0 {
				// exec/fork failure — throw
				panic(vm.NewGoError(err))
			}
		}
		result.Set("exitCode", code)
		return result
	})

	// gish.spawn(cmd, args, onLine) — streaming line-by-line output
	gishObj.Set("spawn", func(call goja.FunctionCall) goja.Value {
		cmdName := call.Argument(0).String()
		cmdArgs := exportStringSlice(vm, call.Argument(1))
		cb, ok := goja.AssertFunction(call.Argument(2))
		if !ok {
			panic(vm.NewTypeError("third argument must be a function"))
		}

		cmd := exec.Command(cmdName, cmdArgs...)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			panic(vm.NewGoError(err))
		}
		var stderr strings.Builder
		cmd.Stderr = &stderr

		if err := cmd.Start(); err != nil {
			panic(vm.NewGoError(err))
		}

		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			_, cbErr := callSafe(cb, goja.Undefined(), vm.ToValue(line))
			if cbErr != nil {
				// Callback error — kill the process and return the error
				cmd.Process.Kill()
				cmd.Wait()
				panic(vm.NewGoError(cbErr))
			}
		}

		waitErr := cmd.Wait()
		result := vm.NewObject()
		result.Set("stderr", stderr.String())
		code := 0
		if waitErr != nil {
			code = exitCode(waitErr)
		}
		result.Set("exitCode", code)
		return result
	})

	// gish.env
	envObj := vm.NewObject()
	envObj.Set("get", func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(os.Getenv(call.Argument(0).String()))
	})
	envObj.Set("set", func(call goja.FunctionCall) goja.Value {
		os.Setenv(call.Argument(0).String(), call.Argument(1).String())
		return goja.Undefined()
	})
	envObj.Set("unset", func(call goja.FunctionCall) goja.Value {
		os.Unsetenv(call.Argument(0).String())
		return goja.Undefined()
	})
	envObj.Set("all", func(call goja.FunctionCall) goja.Value {
		result := vm.NewObject()
		for _, e := range os.Environ() {
			k, v, _ := strings.Cut(e, "=")
			result.Set(k, v)
		}
		return result
	})
	gishObj.Set("env", envObj)

	// gish.cwd()
	gishObj.Set("cwd", func(call goja.FunctionCall) goja.Value {
		dir, err := os.Getwd()
		if err != nil {
			panic(vm.NewGoError(err))
		}
		return vm.ToValue(dir)
	})

	// gish.parseJSON(str)
	gishObj.Set("parseJSON", func(call goja.FunctionCall) goja.Value {
		str := call.Argument(0).String()
		var obj interface{}
		if err := json.Unmarshal([]byte(str), &obj); err != nil {
			panic(vm.NewTypeError("parseJSON: %s", err.Error()))
		}
		return vm.ToValue(obj)
	})

	// gish.toJSON(obj, pretty?)
	gishObj.Set("toJSON", func(call goja.FunctionCall) goja.Value {
		obj := call.Argument(0).Export()
		pretty := true
		if len(call.Arguments) > 1 {
			pretty = call.Argument(1).ToBoolean()
		}
		var data []byte
		var err error
		if pretty {
			data, err = json.MarshalIndent(obj, "", "  ")
		} else {
			data, err = json.Marshal(obj)
		}
		if err != nil {
			panic(vm.NewGoError(err))
		}
		return vm.ToValue(string(data))
	})

	vm.Set("gish", gishObj)
}

func builtinJS(ctx *ExecCtx) error {
	args := ctx.Args()
	if len(args) == 0 {
		return fmt.Errorf("usage: js <expression>")
	}
	expr := strings.Join(tokenValues(args), " ")

	val, err := jsVM.RunString(expr)
	if err != nil {
		if ex, ok := err.(*goja.Exception); ok {
			return fmt.Errorf("%s", ex.Value().String())
		}
		return err
	}
	if val != nil && !goja.IsUndefined(val) {
		fmt.Fprintln(ctx.Out, val.String())
	}
	return nil
}

func scriptsDir() string {
	if d := os.Getenv("GISH_SCRIPTS_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "gish", "scripts")
}

func loadScripts(vm *goja.Runtime, ctx *ExecCtx) {
	dir := scriptsDir()
	if dir == "" {
		return
	}
	entries, err := filepath.Glob(filepath.Join(dir, "*.js"))
	if err != nil || len(entries) == 0 {
		return
	}
	sort.Strings(entries)

	for _, path := range entries {
		src, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(ctx.ErrOut, "gish: script %s: %v\n", filepath.Base(path), err)
			continue
		}
		prg, err := goja.Compile(filepath.Base(path), string(src), false)
		if err != nil {
			fmt.Fprintf(ctx.ErrOut, "gish: script %s: %v\n", filepath.Base(path), err)
			continue
		}
		if _, err := vm.RunProgram(prg); err != nil {
			fmt.Fprintf(ctx.ErrOut, "gish: script %s: %v\n", filepath.Base(path), err)
		}
	}
}

func exportStringSlice(vm *goja.Runtime, val goja.Value) []string {
	if goja.IsUndefined(val) || goja.IsNull(val) {
		return nil
	}
	var out []string
	if err := vm.ExportTo(val, &out); err != nil {
		panic(vm.NewTypeError("args must be an array of strings"))
	}
	return out
}

// callSafe invokes a goja callable, recovering from panics thrown by the runtime.
func callSafe(fn goja.Callable, this goja.Value, args ...goja.Value) (goja.Value, error) {
	var val goja.Value
	var jsErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				if ex, ok := r.(*goja.Exception); ok {
					jsErr = fmt.Errorf("%s", ex.Value().String())
				} else if err, ok := r.(error); ok {
					jsErr = err
				} else {
					jsErr = fmt.Errorf("%v", r)
				}
			}
		}()
		val, jsErr = fn(this, args...)
	}()
	return val, jsErr
}
