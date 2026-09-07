package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunSelfUpdateInstallsRelease(t *testing.T) {
	bin := []byte("#!/bin/sh\necho pigo-new\n")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "pigo", Mode: 0755, Size: int64(len(bin))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(bin); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()
	_ = gz.Close()
	archive := buf.Bytes()

	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "v9.9.9",
			"assets": []map[string]string{{
				"name":                 "pigo_9.9.9_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz",
				"browser_download_url": "http://" + r.Host + "/asset",
			}},
		})
	})
	mux.HandleFunc("/asset", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	oldAPI, oldHTTP := selfUpdateAPI, selfUpdateHTTP
	selfUpdateAPI = srv.URL + "/releases/latest"
	selfUpdateHTTP = srv.Client()
	t.Cleanup(func() {
		selfUpdateAPI, selfUpdateHTTP = oldAPI, oldHTTP
	})

	dest := filepath.Join(t.TempDir(), "pigo")
	if err := os.WriteFile(dest, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := runSelfUpdate(context.Background(), dest, true); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, bin) {
		t.Fatalf("dest = %q", got)
	}
}

func TestRunSelfUpdateMissingRelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, "not found")
	}))
	t.Cleanup(srv.Close)
	oldAPI, oldHTTP := selfUpdateAPI, selfUpdateHTTP
	selfUpdateAPI = srv.URL
	selfUpdateHTTP = srv.Client()
	t.Cleanup(func() {
		selfUpdateAPI, selfUpdateHTTP = oldAPI, oldHTTP
	})
	err := runSelfUpdate(context.Background(), filepath.Join(t.TempDir(), "pigo"), true)
	if err == nil || !strings.Contains(err.Error(), "self-update") {
		t.Fatalf("err=%v", err)
	}
}
