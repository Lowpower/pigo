package codemode

import (
	"encoding/json"
	"errors"
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

type hostCall struct {
	resolve func(any) error
	reject  func(any) error
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
	jobs := newJobQueue()

	pending := map[int]hostCall{}
	var stop error
	rejectPending := func(reason string) {
		for id, c := range pending {
			delete(pending, id)
			_ = c.reject(reason)
		}
	}
	finish := func(reason string) {
		if stop == nil {
			stop = errors.New(reason)
		}
		rejectPending(reason)
	}
	go func() {
		for {
			var msg wire
			if err := dec.Decode(&msg); err != nil {
				vm.Interrupt("parent closed")
				jobs.push(func() { finish("parent closed") })
				return
			}
			switch msg.Op {
			case "interrupt":
				vm.Interrupt("interrupted")
				jobs.push(func() { finish("interrupted") })
			case "hostResult":
				msg := msg
				jobs.push(func() {
					c, ok := pending[msg.ID]
					if !ok {
						return
					}
					delete(pending, msg.ID)
					var err error
					if msg.OK {
						err = c.resolve(msg.Value)
					} else {
						err = c.reject(msg.Error)
					}
					if err != nil && stop == nil {
						stop = err
					}
				})
			}
		}
	}()

	host := vm.NewObject()
	var nextID int
	for _, name := range start.Hosts {
		name := name
		if err := host.Set(name, func(call goja.FunctionCall) goja.Value {
			promise, resolve, reject := vm.NewPromise()
			nextID++
			id := nextID
			pending[id] = hostCall{resolve: resolve, reject: reject}
			args := make([]any, len(call.Arguments))
			for i, arg := range call.Arguments {
				if arg != nil {
					args[i] = arg.Export()
				}
			}
			if err := enc.Encode(wire{Op: "call", ID: id, Name: name, Args: args}); err != nil {
				panic(vm.ToValue(err.Error()))
			}
			return vm.ToValue(promise)
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
	value, err = settle(jobs, value, pending, func() error { return stop })
	if err != nil {
		return writeDone(enc, "", err)
	}
	return writeDone(enc, exportValue(value), nil)
}

// settle waits until value is not a pending promise and every host call has
// finished. Host results are applied on this goroutine, which is the only one
// allowed to touch the VM.
func settle(jobs *jobQueue, value goja.Value, pending map[int]hostCall, stopped func() error) (goja.Value, error) {
	for {
		if err := stopped(); err != nil {
			return nil, err
		}
		if p := asPromise(value); p != nil {
			switch p.State() {
			case goja.PromiseStateRejected:
				return nil, errors.New(textOf(p.Result()))
			case goja.PromiseStateFulfilled:
				value = p.Result()
				continue
			}
		} else if len(pending) == 0 {
			return value, nil
		}
		fn, ok := jobs.pop()
		if !ok {
			return nil, errors.New("parent closed")
		}
		fn()
	}
}

func asPromise(v goja.Value) *goja.Promise {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return nil
	}
	p, _ := v.Export().(*goja.Promise)
	return p
}

func textOf(v goja.Value) string {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return ""
	}
	if s, ok := v.Export().(string); ok {
		return s
	}
	return v.String()
}

// jobQueue is an unbounded list of VM-thread callbacks. The reader goroutine
// only pushes; the VM goroutine pops.
type jobQueue struct {
	mu     sync.Mutex
	cond   *sync.Cond
	jobs   []func()
	closed bool
}

func newJobQueue() *jobQueue {
	q := &jobQueue{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

func (q *jobQueue) push(fn func()) {
	q.mu.Lock()
	if !q.closed {
		q.jobs = append(q.jobs, fn)
	}
	q.cond.Signal()
	q.mu.Unlock()
}

func (q *jobQueue) pop() (func(), bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.jobs) == 0 && !q.closed {
		q.cond.Wait()
	}
	if len(q.jobs) == 0 {
		return nil, false
	}
	fn := q.jobs[0]
	q.jobs = q.jobs[1:]
	return fn, true
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
