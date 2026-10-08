package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/dop251/goja"
)

// setupConsole installs a global `console` object modeled on the one in
// node and browsers: log/info/debug write to ctx.Out, error/warn to
// ctx.ErrOut, plus assert, count, group, time and dir helpers. Calls are
// made with the jsmu lock held, so the state below needs no locking.
func setupConsole(vm *goja.Runtime, ctx *ExecCtx) {
	console := vm.NewObject()
	indent := ""
	counts := map[string]int{}
	timers := map[string]time.Time{}

	emit := func(w io.Writer, s string) {
		if indent != "" {
			s = indent + strings.ReplaceAll(s, "\n", "\n"+indent)
		}
		fmt.Fprintln(w, s)
	}
	out := func(call goja.FunctionCall) goja.Value {
		emit(ctx.Out, formatConsoleArgs(vm, call.Arguments))
		return goja.Undefined()
	}
	errOut := func(call goja.FunctionCall) goja.Value {
		emit(ctx.ErrOut, formatConsoleArgs(vm, call.Arguments))
		return goja.Undefined()
	}
	label := func(call goja.FunctionCall) string {
		if v := call.Argument(0); !goja.IsUndefined(v) {
			return v.String()
		}
		return "default"
	}

	console.Set("log", out)
	console.Set("info", out)
	console.Set("debug", out)
	console.Set("error", errOut)
	console.Set("warn", errOut)

	// console.dir(obj) — like log, but always inspects a single value
	console.Set("dir", func(call goja.FunctionCall) goja.Value {
		emit(ctx.Out, formatJSValue(vm, call.Argument(0)))
		return goja.Undefined()
	})

	// console.assert(cond, ...msg) — prints to stderr only when cond is falsy
	console.Set("assert", func(call goja.FunctionCall) goja.Value {
		if call.Argument(0).ToBoolean() {
			return goja.Undefined()
		}
		msg := "Assertion failed"
		if len(call.Arguments) > 1 {
			msg += ": " + formatConsoleArgs(vm, call.Arguments[1:])
		}
		emit(ctx.ErrOut, msg)
		return goja.Undefined()
	})

	console.Set("count", func(call goja.FunctionCall) goja.Value {
		l := label(call)
		counts[l]++
		emit(ctx.Out, fmt.Sprintf("%s: %d", l, counts[l]))
		return goja.Undefined()
	})
	console.Set("countReset", func(call goja.FunctionCall) goja.Value {
		delete(counts, label(call))
		return goja.Undefined()
	})

	group := func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) > 0 {
			emit(ctx.Out, formatConsoleArgs(vm, call.Arguments))
		}
		indent += "  "
		return goja.Undefined()
	}
	console.Set("group", group)
	console.Set("groupCollapsed", group)
	console.Set("groupEnd", func(call goja.FunctionCall) goja.Value {
		if len(indent) >= 2 {
			indent = indent[:len(indent)-2]
		}
		return goja.Undefined()
	})

	console.Set("time", func(call goja.FunctionCall) goja.Value {
		timers[label(call)] = time.Now()
		return goja.Undefined()
	})
	timeLog := func(end bool) func(goja.FunctionCall) goja.Value {
		return func(call goja.FunctionCall) goja.Value {
			l := label(call)
			start, ok := timers[l]
			if !ok {
				emit(ctx.ErrOut, fmt.Sprintf("Warning: No such label '%s' for console.time", l))
				return goja.Undefined()
			}
			msg := fmt.Sprintf("%s: %s", l, formatElapsed(time.Since(start)))
			if !end && len(call.Arguments) > 1 {
				msg += " " + formatConsoleArgs(vm, call.Arguments[1:])
			}
			emit(ctx.Out, msg)
			if end {
				delete(timers, l)
			}
			return goja.Undefined()
		}
	}
	console.Set("timeLog", timeLog(false))
	console.Set("timeEnd", timeLog(true))

	vm.Set("console", console)
}

// formatElapsed renders a duration the way node does: milliseconds with
// three decimals, switching to seconds at one second.
func formatElapsed(d time.Duration) string {
	if d >= time.Second {
		return fmt.Sprintf("%.3fs", d.Seconds())
	}
	return fmt.Sprintf("%.3fms", float64(d)/float64(time.Millisecond))
}

// formatConsoleArgs joins args with spaces. If the first argument is a
// string it is treated as a printf-style format: %s %d %i %f %j %o %O %c
// consume an argument and %% is a literal percent. Strings are printed raw;
// everything else goes through formatJSValue.
func formatConsoleArgs(vm *goja.Runtime, args []goja.Value) string {
	if len(args) == 0 {
		return ""
	}
	var sb strings.Builder
	rest := args
	if first, ok := args[0].Export().(string); ok {
		rest = args[1:]
		for i := 0; i < len(first); i++ {
			c := first[i]
			if c != '%' || i+1 >= len(first) {
				sb.WriteByte(c)
				continue
			}
			verb := first[i+1]
			if verb == '%' {
				sb.WriteByte('%')
				i++
				continue
			}
			if !strings.ContainsRune("sdifjoOc", rune(verb)) || len(rest) == 0 {
				sb.WriteByte(c)
				continue
			}
			arg := rest[0]
			rest = rest[1:]
			i++
			switch verb {
			case 's':
				if _, isStr := arg.Export().(string); isStr {
					sb.WriteString(arg.String())
				} else {
					sb.WriteString(formatJSValue(vm, arg))
				}
			case 'd', 'i', 'f':
				f := arg.ToFloat()
				if verb == 'i' && !math.IsNaN(f) && !math.IsInf(f, 0) {
					f = math.Trunc(f)
				}
				sb.WriteString(vm.ToValue(f).String())
			case 'j':
				data, err := json.Marshal(arg.Export())
				if err != nil {
					sb.WriteString("[Circular]")
				} else {
					sb.WriteString(string(data))
				}
			case 'o', 'O':
				sb.WriteString(formatJSValue(vm, arg))
			case 'c':
				// CSS styling is meaningless here; the argument is consumed.
			}
		}
	} else {
		sb.WriteString(formatConsoleValue(vm, args[0]))
		rest = args[1:]
	}
	for _, a := range rest {
		if sb.Len() > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(formatConsoleValue(vm, a))
	}
	return sb.String()
}

// formatConsoleValue prints strings raw and everything else via formatJSValue.
func formatConsoleValue(vm *goja.Runtime, v goja.Value) string {
	if _, ok := v.Export().(string); ok {
		return v.String()
	}
	return formatJSValue(vm, v)
}
