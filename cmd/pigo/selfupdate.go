package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Lowpower/pigo/internal/version"
)

const selfUpdateRepo = "Lowpower/pigo"

var (
	selfUpdateAPI  = "https://api.github.com/repos/" + selfUpdateRepo + "/releases/latest"
	selfUpdateHTTP = http.DefaultClient
)

type githubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func runSelfUpdate(ctx context.Context, dest string, force bool) error {
	if dest == "" {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("locate pigo binary: %w", err)
		}
		dest = exe
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, selfUpdateAPI, nil)
	if err != nil {
		return err
	}
	req.Header.Set("accept", "application/vnd.github+json")
	req.Header.Set("user-agent", "pigo")
	client := selfUpdateHTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("pigo cannot self-update this installation: github %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return fmt.Errorf("decode release: %w", err)
	}
	tag := strings.TrimPrefix(strings.TrimSpace(rel.TagName), "v")
	if tag == "" {
		return fmt.Errorf("pigo cannot self-update this installation: missing release tag")
	}
	if !force && sameRelease(version.Version, tag) {
		fmt.Fprintf(os.Stderr, "pigo is already at %s\n", tag)
		return nil
	}
	asset, url := pickSelfUpdateAsset(rel, tag)
	if url == "" {
		return fmt.Errorf("pigo cannot self-update this installation: no asset for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	bin, err := downloadSelfUpdateBinary(ctx, client, url, strings.HasSuffix(strings.ToLower(asset), ".zip"))
	if err != nil {
		return err
	}
	if err := installSelfUpdateBinary(dest, bin); err != nil {
		return err
	}
	fmt.Printf("Updated pigo from %s to %s\n", version.Version, tag)
	return nil
}

func sameRelease(current, tag string) bool {
	return strings.TrimPrefix(current, "v") == strings.TrimPrefix(tag, "v")
}

func pickSelfUpdateAsset(rel githubRelease, tag string) (name, url string) {
	wantTar := fmt.Sprintf("pigo_%s_%s_%s.tar.gz", tag, runtime.GOOS, runtime.GOARCH)
	wantZip := fmt.Sprintf("pigo_%s_%s_%s.zip", tag, runtime.GOOS, runtime.GOARCH)
	for _, a := range rel.Assets {
		switch a.Name {
		case wantTar, wantZip:
			return a.Name, a.BrowserDownloadURL
		}
	}
	return "", ""
}

func downloadSelfUpdateBinary(ctx context.Context, client *http.Client, url string, zipArchive bool) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("user-agent", "pigo")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("download asset: %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if zipArchive {
		return extractNamedFileZip(body, "pigo.exe", "pigo")
	}
	return extractNamedFileTarGz(body, "pigo", "pigo.exe")
}

func extractNamedFileTarGz(raw []byte, names ...string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		base := filepath.Base(hdr.Name)
		if !want[base] {
			continue
		}
		return io.ReadAll(tr)
	}
	return nil, fmt.Errorf("archive missing pigo binary")
}

func extractNamedFileZip(raw []byte, names ...string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	for _, f := range zr.File {
		if !want[filepath.Base(f.Name)] {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(rc)
		_ = rc.Close()
		return b, err
	}
	return nil, fmt.Errorf("archive missing pigo binary")
}

func installSelfUpdateBinary(dest string, bin []byte) error {
	dir := filepath.Dir(dest)
	tmp, err := os.CreateTemp(dir, "pigo-update-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(bin); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, dest); err != nil {
		backup := dest + ".old"
		_ = os.Remove(backup)
		if err2 := os.Rename(dest, backup); err2 != nil {
			_ = os.Remove(tmpName)
			return err
		}
		if err2 := os.Rename(tmpName, dest); err2 != nil {
			_ = os.Rename(backup, dest)
			_ = os.Remove(tmpName)
			return err2
		}
		_ = os.Remove(backup)
	}
	return nil
}
