package codemode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// HostFunc is one synchronous function the script calls as host.<name>(...).
// args are JSON values. The returned string is the JavaScript result.
type HostFunc func(ctx context.Context, args []any) (string, error)

var workerSpawns atomic.Int64

// childBudgetNS is the deadline passed to the most recent child, or -1 when
// the script has no timeout. Tests read it to check that queue time is subtracted.
var childBudgetNS atomic.Int64

// Run executes source. Parsing happens before a worker slot is taken, and a
// slot is taken before the child process starts. host may be nil.
func Run(ctx context.Context, source string, host map[string]HostFunc) (string, error) {
	if os.Getenv(workerEnv) == "1" {
		return "", errors.New("codemode worker cannot run a nested script")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	code, opt, err := Parse(source)
	if err != nil {
		return "", err
	}
	wait := maxWorkerWait
	var configured time.Duration
	hasBudget := opt.TimeoutMS != nil
	if hasBudget {
		configured = time.Duration(*opt.TimeoutMS) * time.Millisecond
		if configured < wait {
			wait = configured
		}
	}
	queued := time.Now()
	release, err := reserve(ctx, wait)
	if err != nil {
		return "", err
	}
	defer release()
	budget := configured
	if hasBudget {
		budget -= time.Since(queued)
		if budget < time.Millisecond {
			budget = time.Millisecond
		}
	}
	return execute(ctx, code, host, budget, configured, hasBudget)
}

func execute(ctx context.Context, code string, host map[string]HostFunc, budget, configured time.Duration, hasBudget bool) (result string, err error) {
	if hasBudget {
		childBudgetNS.Store(int64(budget))
	} else {
		childBudgetNS.Store(-1)
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	cmd := exec.Command(exe)
	cmd.Env = append(withoutEnv(os.Environ(), workerEnv), workerEnv+"=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderr := &limitWriter{max: 8 << 10}
	cmd.Stderr = stderr
	workerSpawns.Add(1)
	if err := cmd.Start(); err != nil {
		return "", err
	}
	var finished bool
	finish := func() error {
		if finished {
			return nil
		}
		finished = true
		cerr := stdin.Close()
		werr := cmd.Wait()
		if werr != nil {
			return werr
		}
		return cerr
	}
	defer func() {
		if werr := finish(); werr != nil && err == nil {
			err = werr
		}
	}()

	names := make([]string, 0, len(host))
	for name := range host {
		names = append(names, name)
	}
	sort.Strings(names)
	enc := json.NewEncoder(stdin)
	var writeMu sync.Mutex
	writeMsg := func(m wire) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return enc.Encode(m)
	}
	if err := writeMsg(wire{Op: "start", Code: code, Hosts: names}); err != nil {
		return "", err
	}

	type incoming struct {
		msg wire
		err error
	}
	incomingC := make(chan incoming, 1)
	go func() {
		dec := json.NewDecoder(stdout)
		for {
			var msg wire
			derr := dec.Decode(&msg)
			incomingC <- incoming{msg: msg, err: derr}
			if derr != nil || msg.Op == "done" {
				return
			}
		}
	}()

	hostCtx, hostCancel := context.WithCancel(ctx)
	defer hostCancel()
	var (
		cancelC     = ctx.Done()
		timerC      <-chan time.Time
		killC       <-chan time.Time
		cause       string
		interrupted bool
	)
	if hasBudget {
		timer := time.NewTimer(budget)
		defer timer.Stop()
		timerC = timer.C
	}
	interrupt := func() {
		if interrupted {
			return
		}
		interrupted = true
		hostCancel()
		if werr := writeMsg(wire{Op: "interrupt"}); werr != nil && err == nil {
			err = werr
		}
		killC = time.After(2 * time.Second)
	}
	noteTimers := func() {
		select {
		case <-cancelC:
			cancelC = nil
			if cause == "" {
				cause = "cancel"
			}
			interrupt()
		default:
		}
		select {
		case <-timerC:
			timerC = nil
			if cause == "" {
				cause = "timeout"
			}
			interrupt()
		default:
		}
	}
	fail := func(scriptErr string) error {
		switch cause {
		case "timeout":
			limit := configured
			if limit == 0 {
				limit = budget
			}
			return &TimeoutError{Limit: limit}
		case "cancel":
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return context.Canceled
		default:
			if scriptErr != "" {
				return errors.New(scriptErr)
			}
			// Wait stops the stderr copy before the buffer is read.
			if werr := finish(); werr != nil && err == nil {
				err = werr
			}
			msg := firstLine(stderr.buf.String())
			if msg != "" {
				return fmt.Errorf("codemode worker exited: %s", msg)
			}
			return errors.New("codemode worker exited")
		}
	}

	for {
		select {
		case <-cancelC:
			cancelC = nil
			if cause == "" {
				cause = "cancel"
			}
			interrupt()
		case <-timerC:
			timerC = nil
			if cause == "" {
				cause = "timeout"
			}
			interrupt()
		case <-killC:
			killC = nil
			if cmd.Process != nil {
				if kerr := cmd.Process.Kill(); kerr != nil && !errors.Is(kerr, os.ErrProcessDone) && err == nil {
					err = kerr
				}
			}
		case in := <-incomingC:
			noteTimers()
			if in.err != nil {
				return "", fail("")
			}
			switch in.msg.Op {
			case "call":
				msg := in.msg
				if interrupted {
					if werr := writeMsg(wire{Op: "hostResult", ID: msg.ID, OK: false, Error: "interrupted"}); werr != nil && err == nil {
						err = werr
					}
					continue
				}
				go func() {
					defer func() {
						if rec := recover(); rec != nil {
							if werr := writeMsg(wire{Op: "hostResult", ID: msg.ID, OK: false, Error: fmt.Sprint(rec)}); werr != nil {
								hostCancel()
							}
						}
					}()
					var fn HostFunc
					if host != nil {
						fn = host[msg.Name]
					}
					var val string
					var herr error
					if fn == nil {
						herr = fmt.Errorf("unknown host function %q", msg.Name)
					} else {
						val, herr = fn(hostCtx, msg.Args)
					}
					reply := wire{Op: "hostResult", ID: msg.ID, OK: herr == nil, Value: val}
					if herr != nil {
						reply.Error = herr.Error()
					}
					if werr := writeMsg(reply); werr != nil {
						hostCancel()
					}
				}()
			case "done":
				if !in.msg.OK {
					return "", fail(in.msg.Error)
				}
				return in.msg.Value, nil
			default:
				return "", fmt.Errorf("codemode worker sent %q", in.msg.Op)
			}
		}
	}
}

func withoutEnv(env []string, key string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env))
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			out = append(out, entry)
		}
	}
	return out
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

// limitWriter keeps the first max bytes written to it.
type limitWriter struct {
	buf bytes.Buffer
	max int
}

func (w *limitWriter) Write(p []byte) (int, error) {
	if w.buf.Len() < w.max {
		n := w.max - w.buf.Len()
		if n > len(p) {
			n = len(p)
		}
		if _, err := w.buf.Write(p[:n]); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}
