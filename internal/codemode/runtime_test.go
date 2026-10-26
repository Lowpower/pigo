package codemode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv(workerEnv) == "1" {
		os.Exit(runWorker())
	}
	os.Exit(m.Run())
}

func echoHost(calls *int) map[string]HostFunc {
	return map[string]HostFunc{
		"echo": func(ctx context.Context, args []any) (string, error) {
			if calls != nil {
				*calls++
			}
			if err := ctx.Err(); err != nil {
				return "", err
			}
			if len(args) == 0 {
				return "", nil
			}
			return fmt.Sprint(args[0]), nil
		},
	}
}

func TestRunEcho(t *testing.T) {
	workerSpawns.Store(0)
	var calls int
	got, err := Run(context.Background(), `host.echo("hi")`, echoHost(&calls))
	if err != nil {
		t.Fatal(err)
	}
	if got != "hi" || calls != 1 {
		t.Fatalf("got %q calls %d", got, calls)
	}
}

func TestRunAcceptsOutputBudget(t *testing.T) {
	got, err := Run(context.Background(), "// @options: {\"max_output_tokens\": 10}\nhost.echo(\"ok\")\n", echoHost(nil))
	if err != nil {
		t.Fatal(err)
	}
	if got != "ok" {
		t.Fatalf("got %q", got)
	}
}

func TestMisplacedOptionsDoesNotRun(t *testing.T) {
	workerSpawns.Store(0)
	var calls int
	_, err := Run(context.Background(), "host.echo(\"no\")\n// @options: {\"timeout_ms\": 5}\n", echoHost(&calls))
	if err == nil {
		t.Fatal("expected a parse error")
	}
	if calls != 0 || workerSpawns.Load() != 0 {
		t.Fatalf("calls %d spawns %d err %v", calls, workerSpawns.Load(), err)
	}
}

func TestTimeoutStopsLoop(t *testing.T) {
	start := time.Now()
	_, err := Run(context.Background(), "// @options: {\"timeout_ms\": 200}\nwhile (true) {}\n", nil)
	var timeout *TimeoutError
	if !errors.As(err, &timeout) || timeout.Waiting {
		t.Fatalf("err %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("loop ran for %s", time.Since(start))
	}
}

func TestFreezeKeepsHostJSON(t *testing.T) {
	sample := []string{"a", "b"}
	before, err := json.Marshal(sample)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Run(context.Background(), `Array.prototype.hack = true; host.echo(Object.isFrozen(Array.prototype) && Array.prototype.hack !== true ? "frozen" : "mutable")`, echoHost(nil))
	if err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(sample)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("json changed %s -> %s", before, after)
	}
	if got != "frozen" {
		t.Fatalf("prototype %q", got)
	}
}

type hold struct {
	mu       sync.Mutex
	entered  chan struct{}
	releases []chan struct{}
}

func newHold() *hold {
	return &hold{entered: make(chan struct{}, 8)}
}

func (h *hold) hosts() map[string]HostFunc {
	return map[string]HostFunc{
		"echo": func(ctx context.Context, _ []any) (string, error) {
			ch := make(chan struct{})
			h.mu.Lock()
			h.releases = append(h.releases, ch)
			h.mu.Unlock()
			h.entered <- struct{}{}
			select {
			case <-ch:
				return "ok", nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		},
	}
}

func (h *hold) waitEntered(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-h.entered:
		case <-time.After(20 * time.Second):
			t.Fatalf("worker %d did not enter", i)
		}
	}
}

func (h *hold) releaseOne(t *testing.T) {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.releases) == 0 {
		t.Fatal("nothing to release")
	}
	close(h.releases[0])
	h.releases = h.releases[1:]
}

func (h *hold) releaseAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ch := range h.releases {
		close(ch)
	}
	h.releases = nil
}

func waitQueued(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		poolMu.Lock()
		waiting := len(poolWait)
		poolMu.Unlock()
		if waiting >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("queued %d, want %d", waiting, n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestQueueWaitsUntilExit(t *testing.T) {
	workerSpawns.Store(0)
	h := newHold()
	defer h.releaseAll()
	errc := make(chan error, 3)
	var wg sync.WaitGroup
	start := func(source string) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := Run(context.Background(), source, h.hosts())
			errc <- err
		}()
	}
	start(`host.echo("a")`)
	start(`host.echo("b")`)
	h.waitEntered(t, 2)
	if workerSpawns.Load() != 2 {
		t.Fatalf("spawns %d", workerSpawns.Load())
	}
	start(`host.echo("c")`)
	waitQueued(t, 1)
	time.Sleep(100 * time.Millisecond)
	if workerSpawns.Load() != 2 {
		t.Fatalf("third spawned early: %d", workerSpawns.Load())
	}
	h.releaseOne(t)
	h.waitEntered(t, 1)
	if workerSpawns.Load() != 3 {
		t.Fatalf("spawns after release %d", workerSpawns.Load())
	}
	h.releaseAll()
	wg.Wait()
	close(errc)
	for err := range errc {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestQueueWaitExpires(t *testing.T) {
	t.Setenv("PIGO_CODEMODE_MAX_WORKERS", "1")
	workerSpawns.Store(0)
	h := newHold()
	defer h.releaseAll()
	done := make(chan error, 1)
	go func() {
		_, err := Run(context.Background(), `host.echo("hold")`, h.hosts())
		done <- err
	}()
	h.waitEntered(t, 1)
	_, err := Run(context.Background(), "// @options: {\"timeout_ms\": 300}\nhost.echo(\"late\")\n", h.hosts())
	var timeout *TimeoutError
	if !errors.As(err, &timeout) || !timeout.Waiting {
		t.Fatalf("err %v", err)
	}
	if workerSpawns.Load() != 1 {
		t.Fatalf("spawns %d", workerSpawns.Load())
	}
	h.releaseAll()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestCancelWhileQueued(t *testing.T) {
	t.Setenv("PIGO_CODEMODE_MAX_WORKERS", "1")
	workerSpawns.Store(0)
	h := newHold()
	defer h.releaseAll()
	done := make(chan error, 1)
	go func() {
		_, err := Run(context.Background(), `host.echo("hold")`, h.hosts())
		done <- err
	}()
	h.waitEntered(t, 1)
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		_, err := Run(ctx, `host.echo("late")`, h.hosts())
		errc <- err
	}()
	waitQueued(t, 1)
	cancel()
	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancel did not finish the queued run")
	}
	if workerSpawns.Load() != 1 {
		t.Fatalf("spawns %d", workerSpawns.Load())
	}
	h.releaseAll()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestPiWorkerEnvIsIgnored(t *testing.T) {
	t.Setenv("PIGO_CODEMODE_MAX_WORKERS", "")
	t.Setenv("PI_CODEMODE_MAX_WORKERS", "1")
	workerSpawns.Store(0)
	h := newHold()
	defer h.releaseAll()
	errc := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := Run(context.Background(), `host.echo("hold")`, h.hosts())
			errc <- err
		}()
	}
	h.waitEntered(t, 2)
	if workerSpawns.Load() != 2 {
		t.Fatalf("spawns %d, PI_CODEMODE_MAX_WORKERS limited pigo", workerSpawns.Load())
	}
	h.releaseAll()
	wg.Wait()
	close(errc)
	for err := range errc {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestQueueTimeCountsAgainstTimeout(t *testing.T) {
	workerSpawns.Store(0)
	h := newHold()
	defer h.releaseAll()
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			_, _ = Run(context.Background(), `host.echo("hold")`, h.hosts())
		}()
	}
	h.waitEntered(t, 2)
	done := make(chan error, 1)
	go func() {
		_, err := Run(context.Background(), "// @options: {\"timeout_ms\": 3000}\nwhile (true) {}\n", nil)
		done <- err
	}()
	waitQueued(t, 1)
	time.Sleep(300 * time.Millisecond)
	h.releaseOne(t)
	select {
	case err := <-done:
		var timeout *TimeoutError
		if !errors.As(err, &timeout) || timeout.Waiting {
			t.Fatalf("err %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("queued script did not finish")
	}
	budget := time.Duration(childBudgetNS.Load())
	if budget < 200*time.Millisecond || budget > 2900*time.Millisecond {
		t.Fatalf("child budget %s, want queue time subtracted from 3s", budget)
	}
	h.releaseAll()
	wg.Wait()
}

func TestAllocationKillsChild(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, err := Run(ctx, `var a = []; while (true) { a.push(new Array(100000)); }`, nil)
	if errors.Is(err, context.DeadlineExceeded) {
		if runtime.GOOS != "linux" {
			t.Skip("memory limit was not enforced")
		}
		t.Fatal("allocation loop was not killed")
	}
	if err == nil {
		t.Fatal("expected the child to die")
	}
	got, err := Run(context.Background(), `host.echo("ok")`, echoHost(nil))
	if err != nil || got != "ok" {
		t.Fatalf("follow-up %q %v", got, err)
	}
}
