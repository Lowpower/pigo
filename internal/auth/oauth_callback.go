package auth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

func callbackHost() string {
	if h := os.Getenv("PI_OAUTH_CALLBACK_HOST"); h != "" {
		return h
	}
	return "127.0.0.1"
}

type callbackServer struct {
	ln     net.Listener
	srv    *http.Server
	mu     sync.Mutex
	done   chan struct{}
	code   string
	state  string
	err    error
	closed bool
}

func startCallback(ctx context.Context, host string, port int, path, expectedState string) (*callbackServer, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		return nil, err
	}
	return serveCallback(ctx, ln, path, expectedState), nil
}

func startCallbackEphemeral(ctx context.Context, host, path string) (*callbackServer, error) {
	ln, err := net.Listen("tcp", host+":0")
	if err != nil {
		return nil, err
	}
	return serveCallback(ctx, ln, path, ""), nil
}

func serveCallback(ctx context.Context, ln net.Listener, path, expectedState string) *callbackServer {
	cs := &callbackServer{ln: ln, done: make(chan struct{})}
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if errVal := q.Get("error"); errVal != "" {
			writeOAuthHTML(w, 400, oauthErrorHTML("Authentication did not complete.", "Error: "+errVal))
			cs.finish("", "", fmt.Errorf("oauth error: %s", errVal))
			return
		}
		code := q.Get("code")
		state := q.Get("state")
		if code == "" {
			writeOAuthHTML(w, 400, oauthErrorHTML("Missing code or state parameter.", ""))
			return
		}
		if expectedState != "" && state != expectedState {
			writeOAuthHTML(w, 400, oauthErrorHTML("State mismatch.", ""))
			return
		}
		writeOAuthHTML(w, 200, oauthSuccessHTML("Authentication completed. You can close this window."))
		cs.finish(code, state, nil)
	})
	cs.srv = &http.Server{Handler: mux}
	go func() { _ = cs.srv.Serve(ln) }()
	go func() {
		<-ctx.Done()
		cs.Cancel()
	}()
	return cs
}

func writeOAuthHTML(w http.ResponseWriter, status int, html string) {
	w.Header().Set("content-type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(html))
}

func (cs *callbackServer) URL(path string) string {
	return "http://" + cs.ln.Addr().String() + path
}

func (cs *callbackServer) finish(code, state string, err error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if cs.closed {
		return
	}
	cs.closed = true
	cs.code, cs.state, cs.err = code, state, err
	close(cs.done)
}

func (cs *callbackServer) Cancel() {
	cs.finish("", "", nil)
	if cs.srv != nil {
		_ = cs.srv.Close()
	}
}

func (cs *callbackServer) Wait() (code, state string, err error) {
	<-cs.done
	return cs.code, cs.state, cs.err
}

func (cs *callbackServer) Close() {
	if cs.srv != nil {
		_ = cs.srv.Close()
	}
}

func parseAuthInput(input string) (code, state string) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", ""
	}
	if u, err := url.Parse(value); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		return u.Query().Get("code"), u.Query().Get("state")
	}
	if i := strings.IndexByte(value, '#'); i >= 0 {
		return value[:i], value[i+1:]
	}
	if strings.Contains(value, "code=") {
		q, _ := url.ParseQuery(value)
		return q.Get("code"), q.Get("state")
	}
	return value, ""
}

func openBrowser(target string) {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name, args = "open", []string{target}
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", target}
	default:
		name, args = "xdg-open", []string{target}
	}
	c := exec.Command(name, args...)
	_ = c.Start()
}

func notifyAuthURL(ix Interaction, rawURL, instructions string) {
	if ix.Notify != nil {
		ix.Notify(Event{Type: EventAuthURL, URL: rawURL, Instructions: instructions})
	}
	openBrowser(rawURL)
}

func notifyDevice(ix Interaction, userCode, uri string, interval, expires int) {
	if ix.Notify != nil {
		ix.Notify(Event{
			Type:             EventDeviceCode,
			UserCode:         userCode,
			VerificationURI:  uri,
			IntervalSeconds:  interval,
			ExpiresInSeconds: expires,
		})
	}
	if uri != "" {
		openBrowser(uri)
	}
}

func notifyProgress(ix Interaction, msg string) {
	if ix.Notify != nil {
		ix.Notify(Event{Type: EventProgress, Message: msg})
	}
}

func trustedHTTPURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return "", fmt.Errorf("untrusted verification URI")
	}
	return u.String(), nil
}
