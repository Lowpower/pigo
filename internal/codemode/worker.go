package codemode

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/dop251/goja"
)

const freezeSource = `
Object.freeze(Object.prototype);
Object.freeze(Array.prototype);
Object.freeze(Function.prototype);
Object.freeze(String.prototype);
Object.freeze(Number.prototype);
Object.freeze(Boolean.prototype);
Object.freeze(Error.prototype);
Object.freeze(host);
`

// DispatchWorker runs the sandbox child and exits when this process was
// re-exec'd as a worker. The parent process returns without doing anything.
func DispatchWorker() {
	if os.Getenv(workerEnv) != "1" {
		return
	}
	os.Exit(runWorker())
}

func runWorker() int {
	if err := applyMemoryLimits(); err != nil {
		encErr := json.NewEncoder(os.Stdout).Encode(wire{Op: "done", OK: false, Error: err.Error()})
		fmt.Fprintf(os.Stderr, "codemode worker: %v\n", err)
		if encErr != nil {
			fmt.Fprintf(os.Stderr, "codemode worker: %v\n", encErr)
		}
		return 1
	}
	if err := serve(); err != nil {
		fmt.Fprintf(os.Stderr, "codemode worker: %v\n", err)
		return 1
	}
	return 0
}

type waiter struct {
	ch   chan wire
	once sync.Once
}

func (w *waiter) send(m wire) {
	w.once.Do(func() { w.ch <- m })
}

func serve() error {
	dec := json.NewDecoder(os.Stdin)
	var start wire
	if err := dec.Decode(&start); err != nil {
		return err
	}
	if start.Op != "start" {
		return fmt.Errorf("expected start, got %q", start.Op)
	}
	enc := json.NewEncoder(os.Stdout)
	vm := goja.New()

	var mu sync.Mutex
	pending := map[int]*waiter{}
	failAll := func(err string) {
		mu.Lock()
		defer mu.Unlock()
		for id, w := range pending {
			delete(pending, id)
			w.send(wire{OK: false, Error: err})
		}
	}
	go func() {
		for {
			var msg wire
			if err := dec.Decode(&msg); err != nil {
				vm.Interrupt("parent closed")
				failAll("parent closed")
				return
			}
			switch msg.Op {
			case "interrupt":
				vm.Interrupt("interrupted")
				failAll("interrupted")
			case "hostResult":
				mu.Lock()
				w := pending[msg.ID]
				delete(pending, msg.ID)
				mu.Unlock()
				if w != nil {
					w.send(msg)
				}
			}
		}
	}()

	host := vm.NewObject()
	var nextID int
	for _, name := range start.Hosts {
		name := name
		if err := host.Set(name, func(call goja.FunctionCall) goja.Value {
			nextID++
			id := nextID
			w := &waiter{ch: make(chan wire, 1)}
			mu.Lock()
			pending[id] = w
			mu.Unlock()
			args := make([]any, len(call.Arguments))
			for i, arg := range call.Arguments {
				if arg != nil {
					args[i] = arg.Export()
				}
			}
			if err := enc.Encode(wire{Op: "call", ID: id, Name: name, Args: args}); err != nil {
				panic(vm.ToValue(err.Error()))
			}
			rep := <-w.ch
			if !rep.OK {
				panic(vm.ToValue(rep.Error))
			}
			return vm.ToValue(rep.Value)
		}); err != nil {
			return err
		}
	}
	if err := vm.Set("host", host); err != nil {
		return err
	}
	if _, err := vm.RunString(freezeSource); err != nil {
		return writeDone(enc, "", err)
	}
	value, err := vm.RunString(start.Code)
	if err != nil {
		return writeDone(enc, "", err)
	}
	return writeDone(enc, exportValue(value), nil)
}

func writeDone(enc *json.Encoder, value string, err error) error {
	msg := wire{Op: "done", OK: err == nil, Value: value}
	if err != nil {
		msg.Error = err.Error()
	}
	return enc.Encode(msg)
}

func exportValue(v goja.Value) string {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return ""
	}
	exported := v.Export()
	if s, ok := exported.(string); ok {
		return s
	}
	b, err := json.Marshal(exported)
	if err != nil {
		return v.String()
	}
	return string(b)
}
