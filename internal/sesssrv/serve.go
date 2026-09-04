package sesssrv

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
)

// Session serves one JSONL RPC connection.
type Session interface {
	ServeRPC(ctx context.Context, in io.Reader, out io.Writer) error
	Close()
}

// NewSession opens a session for an accepted connection.
type NewSession func() (Session, error)

// ListenUnix binds a Unix domain socket, replacing a stale socket file.
func ListenUnix(path string) (net.Listener, error) {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return net.Listen("unix", path)
}

// DialUnix connects to a Unix domain socket.
func DialUnix(path string) (net.Conn, error) {
	return net.Dial("unix", path)
}

// Serve accepts connections until ctx is cancelled or Accept fails.
// Each connection gets its own session.
func Serve(ctx context.Context, ln net.Listener, newSession NewSession) error {
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go handleConn(ctx, conn, newSession)
	}
}

func handleConn(ctx context.Context, conn net.Conn, newSession NewSession) {
	defer func() { _ = conn.Close() }()
	sess, err := newSession()
	if err != nil {
		_ = json.NewEncoder(conn).Encode(map[string]any{"type": "error", "message": err.Error()})
		return
	}
	defer sess.Close()
	_ = sess.ServeRPC(ctx, conn, conn)
}

// Bridge copies stdin to the connection and the connection to stdout.
func Bridge(ctx context.Context, conn net.Conn, in io.Reader, out io.Writer) error {
	errc := make(chan error, 2)
	go func() {
		_, err := io.Copy(conn, in)
		if cw, ok := conn.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
		errc <- err
	}()
	go func() {
		_, err := io.Copy(out, conn)
		errc <- err
	}()
	var first error
	for range 2 {
		select {
		case <-ctx.Done():
			_ = conn.Close()
			return ctx.Err()
		case err := <-errc:
			if err != nil && err != io.EOF && first == nil {
				first = err
			}
		}
	}
	return first
}
