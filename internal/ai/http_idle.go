package ai

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// IdleTimeoutError means response bytes stopped for httpIdleTimeoutMs.
type IdleTimeoutError struct {
	Idle time.Duration
}

func (e *IdleTimeoutError) Error() string {
	if e == nil {
		return "http idle timeout (httpIdleTimeoutMs)"
	}
	return fmt.Sprintf("http idle timeout (httpIdleTimeoutMs): no bytes for %s", e.Idle)
}

// idleConn resets the read deadline on every read so a live stream is not cut
// off by the total request length. A gap of idle with no bytes fails the read.
type idleConn struct {
	net.Conn
	idle time.Duration
}

func (c *idleConn) Read(p []byte) (int, error) {
	if c.idle > 0 {
		if err := c.SetReadDeadline(time.Now().Add(c.idle)); err != nil {
			return 0, err
		}
	}
	n, err := c.Conn.Read(p)
	if err == nil || c.idle <= 0 {
		return n, err
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return n, &IdleTimeoutError{Idle: c.idle}
	}
	return n, err
}

func defaultHTTPClient() *http.Client {
	apiMu.Lock()
	d := idleTimeout
	apiMu.Unlock()
	return newIdleHTTPClient(d)
}

func newIdleHTTPClient(idle time.Duration) *http.Client {
	if idle <= 0 {
		return &http.Client{}
	}
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Client{}
	}
	tr := base.Clone()
	tr.ResponseHeaderTimeout = idle
	orig := tr.DialContext
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		var (
			conn net.Conn
			err  error
		)
		if orig != nil {
			conn, err = orig(ctx, network, addr)
		} else {
			conn, err = (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext(ctx, network, addr)
		}
		if err != nil {
			return nil, err
		}
		return &idleConn{Conn: conn, idle: idle}, nil
	}
	return &http.Client{Transport: &idleRoundTripper{base: tr, idle: idle}}
}

// idleRoundTripper labels a response-header wait that exceeds the idle limit.
// Body reads are labeled by idleConn.
type idleRoundTripper struct {
	base http.RoundTripper
	idle time.Duration
}

func (t *idleRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err == nil || req.Context().Err() != nil || t.idle <= 0 {
		return resp, err
	}
	var idle *IdleTimeoutError
	if errors.As(err, &idle) || strings.Contains(err.Error(), "timeout awaiting response headers") {
		return nil, &IdleTimeoutError{Idle: t.idle}
	}
	return nil, err
}
