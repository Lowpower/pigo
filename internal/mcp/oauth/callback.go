package oauth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Lowpower/pigo/internal/auth"
)

type callbackServer struct {
	srv      *http.Server
	redirect string
	done     chan struct{}
	stop     chan struct{}
	stopOnce sync.Once
	mu       sync.Mutex
	code     string
	iss      string
	err      error
	closed   bool
	expected string
	issuer   string
}

func listenCallback(ctx context.Context, port int, state, issuer string) (*callbackServer, error) {
	host := callbackHost()
	ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}
	tcp, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		_ = ln.Close()
		return nil, errors.New("oauth callback did not bind to tcp")
	}
	cb := &callbackServer{
		done:     make(chan struct{}),
		stop:     make(chan struct{}),
		expected: state,
		issuer:   issuer,
		redirect: "http://" + net.JoinHostPort(host, strconv.Itoa(tcp.Port)) + callbackPath,
	}
	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, cb.serve)
	cb.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = cb.srv.Serve(ln) }()
	go func() {
		select {
		case <-ctx.Done():
			cb.fail(ErrSignInCancelled)
			_ = cb.srv.Close()
		case <-cb.stop:
		}
	}()
	return cb, nil
}

func (cb *callbackServer) serve(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if errVal := q.Get("error"); errVal != "" {
		msg := errVal
		if desc := q.Get("error_description"); desc != "" {
			msg = errVal + ": " + desc
		}
		writePage(w, http.StatusBadRequest, auth.ErrorHTML("Authentication did not complete.", msg))
		cb.fail(errors.New("oauth error: " + msg))
		return
	}
	code := q.Get("code")
	state := q.Get("state")
	if code == "" {
		writePage(w, http.StatusBadRequest, auth.ErrorHTML("Missing code or state parameter.", ""))
		cb.fail(errors.New("oauth callback is missing code"))
		return
	}
	if cb.expected != "" && state != cb.expected {
		writePage(w, http.StatusBadRequest, auth.ErrorHTML("State mismatch.", ""))
		cb.fail(errors.New("oauth state mismatch"))
		return
	}
	if err := matchResponseIssuer(cb.issuer, q.Get("iss")); err != nil {
		writePage(w, http.StatusBadRequest, auth.ErrorHTML("Authorization server issuer did not match.", ""))
		cb.fail(err)
		return
	}
	writePage(w, http.StatusOK, auth.SuccessHTML("Signed in to the MCP server. You may now close this page."))
	cb.succeed(code, q.Get("iss"))
}

func writePage(w http.ResponseWriter, status int, html string) {
	w.Header().Set("content-type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(html))
}

func (cb *callbackServer) succeed(code, iss string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.closed {
		return
	}
	cb.closed = true
	cb.code = code
	cb.iss = iss
	close(cb.done)
}

func (cb *callbackServer) fail(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.closed {
		return
	}
	cb.closed = true
	cb.err = err
	close(cb.done)
}

func (cb *callbackServer) Wait(ctx context.Context) (string, string, error) {
	select {
	case <-cb.done:
	case <-ctx.Done():
		return "", "", ErrSignInCancelled
	}
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.code, cb.iss, cb.err
}

func (cb *callbackServer) Close() {
	cb.stopOnce.Do(func() { close(cb.stop) })
	if cb.srv != nil {
		_ = cb.srv.Close()
	}
}

func callbackHost() string {
	h := strings.TrimSpace(os.Getenv("PIGO_OAUTH_CALLBACK_HOST"))
	if h != "" && loopback(h) {
		return h
	}
	return "127.0.0.1"
}

func loopback(host string) bool {
	host = strings.Trim(host, "[]")
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func portOf(raw string) int {
	u, err := url.Parse(raw)
	if err != nil || u.Port() == "" {
		return 0
	}
	p, err := strconv.Atoi(u.Port())
	if err != nil || p < 1 || p > 65535 {
		return 0
	}
	return p
}
