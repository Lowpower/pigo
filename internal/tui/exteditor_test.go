package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEditInExternalEditorSuccess(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "ed.sh")
	body := "#!/bin/sh\nprintf 'from-editor\\n' > \"$1\"\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	got, ok := EditInExternalEditor(script, "original")
	if !ok {
		t.Fatal("expected success")
	}
	if got != "from-editor" {
		t.Fatalf("got %q", got)
	}
}

func TestEditInExternalEditorKeepsOriginalOnFailure(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "ed.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, ok := EditInExternalEditor(script, "original")
	if ok || got != "" {
		t.Fatalf("ok=%v got=%q", ok, got)
	}
}

func TestEditInExternalEditorStripsBOMAndTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "ed.sh")
	body := "#!/bin/sh\nprintf '\\357\\273\\277hello\\n' > \"$1\"\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	got, ok := EditInExternalEditor(script, "")
	if !ok {
		t.Fatal("expected success")
	}
	if got != "hello" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractImagesFromClipboardPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pigo-clipboard-abc.png")
	if err := os.WriteFile(path, mustDecodePNG(), 0o600); err != nil {
		t.Fatal(err)
	}
	imgs := extractImages("look at " + path + " please")
	if len(imgs) != 1 {
		t.Fatalf("got %d images", len(imgs))
	}
	if imgs[0].MimeType != "image/png" || imgs[0].Data == "" {
		t.Fatalf("%+v", imgs[0])
	}
}

func TestExtractImagesSkipsMissingAndNonImage(t *testing.T) {
	dir := t.TempDir()
	txt := filepath.Join(dir, "notes.png")
	if err := os.WriteFile(txt, []byte("not an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	if imgs := extractImages("/no/such/pigo-clipboard-x.png " + txt); len(imgs) != 0 {
		t.Fatalf("expected no images, got %+v", imgs)
	}
}

func TestSelectImageMIMEPrefersPNG(t *testing.T) {
	got := selectImageMIME([]string{"text/plain", "image/jpeg", "image/png"})
	if baseMIME(got) != "image/png" {
		t.Fatalf("got %q", got)
	}
}
