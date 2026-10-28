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
	h := strings.TrimSpace(os.Getenv("PIGO_OAUTH_CALLBACK_HOST"))
	if h != "" && isLoopbackHost(h) {
		return h
	}
	return "127.0.0.1"
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(host)
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

type callbackServer struct {
	ln       net.Listener
	srv      *http.Server
	mu       sync.Mutex
	done     chan struct{}
	code     string
	state    string
	clientID string
	err      error
	closed   bool
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
			err := authorizationError(q)
			writeOAuthHTML(w, 400, oauthErrorHTML("Authentication did not complete.", err.Error()))
			cs.finish("", "", "", err)
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
		cs.finish(code, state, q.Get("client_id"), nil)
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

func (cs *callbackServer) boundPort() (int, error) {
	ta, ok := cs.ln.Addr().(*net.TCPAddr)
	if !ok || ta == nil || ta.Port == 0 {
		return 0, fmt.Errorf("oauth callback server did not bind to tcp")
	}
	return ta.Port, nil
}

func (cs *callbackServer) finish(code, state, clientID string, err error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if cs.closed {
		return
	}
	cs.closed = true
	cs.code, cs.state, cs.clientID, cs.err = code, state, clientID, err
	close(cs.done)
}

func (cs *callbackServer) Cancel() {
	cs.finish("", "", "", nil)
	cs.Close()
}

func (cs *callbackServer) Wait() (code, state string, err error) {
	<-cs.done
	return cs.code, cs.state, cs.err
}

// issuedClientID is the client_id query value from a completed callback.
// It is valid after Wait returns.
func (cs *callbackServer) issuedClientID() string {
	return cs.clientID
}

func (cs *callbackServer) Close() {
	if cs.srv != nil {
		_ = cs.srv.Close()
	}
	if cs.ln != nil {
		_ = cs.ln.Close()
	}
}

func parseAuthInput(input string) (code, state string, err error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", "", nil
	}
	if u, parseErr := url.Parse(value); parseErr == nil && (u.Scheme == "http" || u.Scheme == "https") {
		if err := authorizationError(u.Query()); err != nil {
			return "", "", err
		}
		return u.Query().Get("code"), u.Query().Get("state"), nil
	}
	if i := strings.IndexByte(value, '#'); i >= 0 {
		return value[:i], value[i+1:], nil
	}
	if strings.Contains(value, "code=") || strings.Contains(value, "error=") {
		q, _ := url.ParseQuery(value)
		if err := authorizationError(q); err != nil {
			return "", "", err
		}
		return q.Get("code"), q.Get("state"), nil
	}
	return value, "", nil
}

func authorizationError(q url.Values) error {
	code := q.Get("error")
	if code == "" {
		return nil
	}
	desc := q.Get("error_description")
	if desc == "" || desc == code {
		return fmt.Errorf("oauth error: %s", code)
	}
	return fmt.Errorf("oauth error: %s: %s", code, desc)
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
