package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestServerRequiresListen(t *testing.T) {
	t.Setenv("PIGO_SERVER_LISTEN", "")
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"server"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected missing listen address")
	}
}

func TestHelpListsServerAndClient(t *testing.T) {
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--help"})
	_ = cmd.Execute()
	s := out.String()
	for _, want := range []string{"server", "client"} {
		if !strings.Contains(s, want) {
			t.Fatalf("help missing %s:\n%s", want, s)
		}
	}
}

func TestServerClientGetState(t *testing.T) {
	t.Setenv("PIGO_CODING_AGENT_DIR", t.TempDir())
	t.Setenv("PIGO_OFFLINE", "1")
	sock := filepath.Join(t.TempDir(), "pigo.sock")
	addr := "unix://" + sock

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := newRootCmd()
	srv.SetArgs([]string{"server", "--listen", addr})
	errc := make(chan error, 1)
	go func() { errc <- srv.ExecuteContext(ctx) }()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(sock); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("server socket never appeared")
		}
		time.Sleep(10 * time.Millisecond)
	}

	cli := newRootCmd()
	var out bytes.Buffer
	cli.SetIn(strings.NewReader("{\"type\":\"get_state\"}\n{\"type\":\"quit\"}\n"))
	cli.SetOut(&out)
	cli.SetErr(&out)
	cli.SetArgs([]string{"client", "--connect", addr})
	if err := cli.Execute(); err != nil {
		t.Fatal(err, out.String())
	}
	cancel()
	dec := json.NewDecoder(&out)
	var sawReady bool
	for {
		var ev map[string]any
		if err := dec.Decode(&ev); err != nil {
			break
		}
		if ev["type"] == "ready" {
			sawReady = true
		}
	}
	if !sawReady {
		t.Fatalf("client output missing ready: %s", out.String())
	}
}
