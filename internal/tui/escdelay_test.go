package tui

import (
	"io"
	"os"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestEscDelayTimeoutEnv(t *testing.T) {
	const (
		local = 10 * time.Millisecond
		ssh   = 100 * time.Millisecond
	)
	cases := []struct {
		name string
		env  map[string]string
		want time.Duration
	}{
		{name: "local default", want: local},
		{name: "ssh connection", env: map[string]string{"SSH_CONNECTION": "1 2 3 4"}, want: ssh},
		{name: "ssh tty", env: map[string]string{"SSH_TTY": "/dev/pts/1"}, want: ssh},
		{name: "pi override", env: map[string]string{"PI_TUI_ESC_TIMEOUT": "80", "SSH_TTY": "/dev/pts/1"}, want: 80 * time.Millisecond},
		{name: "pigo wins", env: map[string]string{"PIGO_TUI_ESC_TIMEOUT": "40", "PI_TUI_ESC_TIMEOUT": "80"}, want: 40 * time.Millisecond},
		{name: "zero disables", env: map[string]string{"PIGO_TUI_ESC_TIMEOUT": "0", "SSH_CONNECTION": "1"}, want: 0},
		{name: "pi zero disables", env: map[string]string{"PI_TUI_ESC_TIMEOUT": "0"}, want: 0},
		{name: "invalid falls through", env: map[string]string{"PIGO_TUI_ESC_TIMEOUT": "abc", "PI_TUI_ESC_TIMEOUT": "-5"}, want: local},
		{name: "invalid then ssh", env: map[string]string{"PIGO_TUI_ESC_TIMEOUT": "nope", "SSH_TTY": "/dev/pts/1"}, want: ssh},
		{name: "blank is unset", env: map[string]string{"PIGO_TUI_ESC_TIMEOUT": "  ", "PI_TUI_ESC_TIMEOUT": "25"}, want: 25 * time.Millisecond},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := escDelayTimeout(func(k string) string { return tc.env[k] })
			if got != tc.want {
				t.Fatalf("timeout = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEscDelayJoinsSplitAltEnter(t *testing.T) {
	pr, pw := testPipe(t)
	r := holdReader(pr, 80*time.Millisecond)
	go func() {
		waitRead(t, pr)
		mustWrite(t, pw, []byte{0x1b})
		time.Sleep(20 * time.Millisecond)
		mustWrite(t, pw, []byte{'\r'})
	}()
	buf := make([]byte, 16)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(buf[:n]) != "\x1b\r" {
		t.Fatalf("Read = %q, want ESC CR", buf[:n])
	}
}

func TestEscDelaySplitsAfterTimeout(t *testing.T) {
	pr, pw := testPipe(t)
	timeout := 40 * time.Millisecond
	r := holdReader(pr, timeout)
	go func() {
		waitRead(t, pr)
		mustWrite(t, pw, []byte{0x1b})
		time.Sleep(timeout + 40*time.Millisecond)
		mustWrite(t, pw, []byte{'\r'})
	}()
	buf := make([]byte, 16)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("first Read: %v", err)
	}
	if string(buf[:n]) != "\x1b" {
		t.Fatalf("first Read = %q, want ESC", buf[:n])
	}
	n, err = r.Read(buf)
	if err != nil {
		t.Fatalf("second Read: %v", err)
	}
	if string(buf[:n]) != "\r" {
		t.Fatalf("second Read = %q, want CR", buf[:n])
	}
}

func TestEscDelayLoneEscapeWaitsUntilTimeout(t *testing.T) {
	pr, pw := testPipe(t)
	timeout := 60 * time.Millisecond
	r := holdReader(pr, timeout)
	type result struct {
		b   []byte
		err error
		at  time.Time
	}
	done := make(chan result, 1)
	go func() {
		buf := make([]byte, 8)
		n, err := r.Read(buf)
		done <- result{append([]byte(nil), buf[:n]...), err, time.Now()}
	}()
	waitRead(t, pr)
	wrote := time.Now()
	mustWrite(t, pw, []byte{0x1b})
	res := <-done
	if res.err != nil {
		t.Fatalf("Read: %v", res.err)
	}
	if string(res.b) != "\x1b" {
		t.Fatalf("Read = %q, want ESC", res.b)
	}
	delay := res.at.Sub(wrote)
	if delay > timeout+80*time.Millisecond {
		t.Fatalf("lone ESC delay %v exceeds timeout %v", delay, timeout)
	}
	if delay < timeout-25*time.Millisecond {
		t.Fatalf("lone ESC delay %v returned before timeout %v", delay, timeout)
	}
}

func TestEscDelayZeroDoesNotHold(t *testing.T) {
	pr, pw := testPipe(t)
	r := holdReader(pr, 0)
	go func() {
		waitRead(t, pr)
		mustWrite(t, pw, []byte{0x1b})
		time.Sleep(15 * time.Millisecond)
		mustWrite(t, pw, []byte{'\r'})
	}()
	buf := make([]byte, 8)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(buf[:n]) != "\x1b" {
		t.Fatalf("Read = %q, want lone ESC when timeout is 0", buf[:n])
	}
	n, err = r.Read(buf)
	if err != nil {
		t.Fatalf("second Read: %v", err)
	}
	if string(buf[:n]) != "\r" {
		t.Fatalf("second Read = %q, want CR", buf[:n])
	}
}

func TestEscDelayDoesNotHoldNonEsc(t *testing.T) {
	pr, pw := testPipe(t)
	r := holdReader(pr, 200*time.Millisecond)
	go func() {
		waitRead(t, pr)
		mustWrite(t, pw, []byte("\x1b[A"))
	}()
	buf := make([]byte, 8)
	start := time.Now()
	n, err := r.Read(buf)
	delay := time.Since(start)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(buf[:n]) != "\x1b[A" {
		t.Fatalf("Read = %q, want CSI up", buf[:n])
	}
	// Sync sleep is inside the blocked Read. A real ESC hold would add the 200ms timeout.
	if delay > 120*time.Millisecond {
		t.Fatalf("non-ESC read delayed %v", delay)
	}
}

func TestEscDelayReaderKeepsFileIdentity(t *testing.T) {
	pr, _ := testPipe(t)
	r := newEscDelayReader(pr, time.Millisecond)
	if r.Fd() != pr.Fd() {
		t.Fatalf("Fd = %d, want %d", r.Fd(), pr.Fd())
	}
	if r.Name() != pr.Name() {
		t.Fatalf("Name = %q, want %q", r.Name(), pr.Name())
	}
}

func TestSplitAltEnterReachesBubbleTea(t *testing.T) {
	pr, pw := testPipe(t)
	keys := make(chan string, 4)
	out := &viewSignal{ch: make(chan struct{})}
	p := tea.NewProgram(keySink{got: keys, stop: 1},
		tea.WithInput(newEscDelayReader(pr, 80*time.Millisecond)),
		tea.WithOutput(out),
		tea.WithoutSignalHandler(),
	)
	done := make(chan error, 1)
	go func() {
		_, err := p.Run()
		done <- err
	}()
	waitView(t, out)
	time.Sleep(20 * time.Millisecond)
	mustWrite(t, pw, []byte{0x1b})
	time.Sleep(20 * time.Millisecond)
	mustWrite(t, pw, []byte{'\r'})

	select {
	case got := <-keys:
		if got != "alt+enter" {
			t.Fatalf("key = %q, want alt+enter", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for alt+enter")
	}
	waitProgram(t, p, done, pw)
}

func TestSplitAltEnterPastTimeoutIsEscThenEnter(t *testing.T) {
	pr, pw := testPipe(t)
	timeout := 40 * time.Millisecond
	keys := make(chan string, 4)
	out := &viewSignal{ch: make(chan struct{})}
	p := tea.NewProgram(keySink{got: keys, stop: 2},
		tea.WithInput(newEscDelayReader(pr, timeout)),
		tea.WithOutput(out),
		tea.WithoutSignalHandler(),
	)
	done := make(chan error, 1)
	go func() {
		_, err := p.Run()
		done <- err
	}()
	waitView(t, out)
	time.Sleep(20 * time.Millisecond)
	mustWrite(t, pw, []byte{0x1b})
	time.Sleep(timeout + 40*time.Millisecond)
	mustWrite(t, pw, []byte{'\r'})

	var got []string
	deadline := time.After(2 * time.Second)
	for len(got) < 2 {
		select {
		case k := <-keys:
			got = append(got, k)
		case <-deadline:
			t.Fatalf("keys = %v, want esc then enter", got)
		}
	}
	if got[0] != "esc" || got[1] != "enter" {
		t.Fatalf("keys = %v, want esc then enter", got)
	}
	waitProgram(t, p, done, pw)
}

type keySink struct {
	got  chan string
	stop int
	n    int
}

func (m keySink) Init() tea.Cmd { return nil }

func (m keySink) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	m.n++
	m.got <- k.String()
	if m.n >= m.stop {
		return m, tea.Quit
	}
	return m, nil
}

func (m keySink) View() string { return "ready" }

type viewSignal struct {
	ch   chan struct{}
	once sync.Once
}

func (w *viewSignal) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.ch) })
	return len(p), nil
}

// holdReader matches production: Bubble Tea calls Fd() before reading,
// which puts the descriptor into blocking mode.
func holdReader(f *os.File, timeout time.Duration) *escDelayReader {
	r := newEscDelayReader(f, timeout)
	_ = r.Fd()
	return r
}

func testPipe(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = pr.Close()
		_ = pw.Close()
	})
	return pr, pw
}

func mustWrite(t *testing.T, w io.Writer, b []byte) {
	t.Helper()
	if _, err := w.Write(b); err != nil {
		t.Errorf("write: %v", err)
	}
}

// waitRead blocks until a blocked Read is likely sitting on an empty pipe.
// Callers start Read first, then call this, then write.
func waitRead(t *testing.T, _ *os.File) {
	t.Helper()
	time.Sleep(30 * time.Millisecond)
}

func waitView(t *testing.T, out *viewSignal) {
	t.Helper()
	select {
	case <-out.ch:
	case <-time.After(2 * time.Second):
		t.Fatal("program did not render")
	}
}

func waitProgram(t *testing.T, p *tea.Program, done <-chan error, pw *os.File) {
	t.Helper()
	_ = pw.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("program: %v", err)
		}
	case <-time.After(2 * time.Second):
		p.Quit()
		t.Fatal("program did not exit")
	}
}
