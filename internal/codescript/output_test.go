package codescript

import (
	"context"
	"os"
	"strings"
	"testing"
)

// png1x1 is a 1x1 PNG.
const png1x1 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func TestTextSeparators(t *testing.T) {
	one, err := Run(context.Background(), `await text("only");`, Env{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(one.Render(), "==> text") {
		t.Fatalf("single item %s", one.Render())
	}
	two, err := Run(context.Background(), `await text("a"); await text("b");`, Env{})
	if err != nil {
		t.Fatal(err)
	}
	got := two.Render()
	if !strings.Contains(got, "==> text 1/2 <==\na") || !strings.Contains(got, "==> text 2/2 <==\nb") {
		t.Fatalf("two items %s", got)
	}
	three, err := Run(context.Background(), `await text("a"); await text("b"); return "c";`, Env{})
	if err != nil {
		t.Fatal(err)
	}
	got = three.Render()
	if !strings.Contains(got, "==> text 1/3 <==\na") || !strings.Contains(got, "==> text 3/3 <==\nc") {
		t.Fatalf("three items %s", got)
	}
}

func TestConsoleBlock(t *testing.T) {
	res, err := Run(context.Background(), `await console.log("one"); await console.warn("two"); return "ok";`, Env{})
	if err != nil {
		t.Fatal(err)
	}
	got := res.Render()
	if !strings.Contains(got, "<console_output>\nlog one\nwarn two\n</console_output>") {
		t.Fatalf("console %s", got)
	}
	if strings.Contains(got, "==> text") {
		t.Fatalf("return alone should not be separated: %s", got)
	}
}

func TestInvalidImageStaysOutOfResult(t *testing.T) {
	res, err := Run(context.Background(), `await text("keep"); await image({type:"image", data:"@@not-base64@@", mimeType:"image/png"});`, Env{})
	if err == nil {
		t.Fatal("invalid image succeeded")
	}
	if len(res.Images) != 0 || len(res.Texts) != 1 || res.Texts[0] != "keep" {
		t.Fatalf("%#v", res)
	}
	if strings.Contains(res.Render(), "@@not-base64@@") || strings.Contains(err.Error(), "@@not-base64@@") {
		t.Fatalf("invalid payload leaked: %s %v", res.Render(), err)
	}
}

func TestImageDataURL(t *testing.T) {
	res, err := Run(context.Background(), `await image("data:image/png;base64,`+png1x1+`"); return "ok";`, Env{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Images) != 1 || res.Images[0].MimeType != "image/png" || res.Images[0].Path == "" {
		t.Fatalf("%#v", res.Images)
	}
	t.Cleanup(func() { _ = os.Remove(res.Images[0].Path) })
	info, err := os.Stat(res.Images[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	if !strings.Contains(res.Render(), res.Images[0].Path) || !strings.Contains(res.Render(), `"mimeType":"image/png"`) {
		t.Fatalf("render %s", res.Render())
	}
}

func TestOutputCharLimitKeepsEarlierText(t *testing.T) {
	r := &runner{}
	if _, err := r.addText(context.Background(), strings.Repeat("a", maxOutputChars)); err != nil {
		t.Fatal(err)
	}
	if _, err := r.addText(context.Background(), "b"); err == nil || !strings.Contains(err.Error(), "16777216") {
		t.Fatal(err)
	}
	if len(r.result.Texts) != 1 || strings.Contains(r.result.Texts[0], "b") {
		t.Fatalf("texts=%d", len(r.result.Texts))
	}
}

func TestTooManyItemsStopsTheScript(t *testing.T) {
	res, err := Run(context.Background(), `
		try {
			for (let i = 0; i < 100001; i++) await text("x");
		} catch (e) {
			await text("AFTER_LIMIT");
		}
		return "done";
	`, Env{})
	if err == nil || !strings.Contains(err.Error(), "100000") {
		t.Fatalf("err %v", err)
	}
	if len(res.Texts) != maxOutputItems {
		t.Fatalf("texts %d", len(res.Texts))
	}
	for _, text := range res.Texts {
		if text == "AFTER_LIMIT" {
			t.Fatal("output continued after the cap")
		}
	}
	if res.Value == "done" {
		t.Fatal("return was kept after the cap")
	}
}

func TestFormatListingBudget(t *testing.T) {
	tools := []Tool{
		{Name: "a", Namespace: "ns", Description: "one"},
		{Name: "b", Namespace: "ns", Description: "two"},
	}
	names := FormatListing(tools, 0)
	if !strings.Contains(names, "Namespaces:") || !strings.Contains(names, "ns") || strings.Contains(names, "await tools") {
		t.Fatalf("namespaces %s", names)
	}
	full := FormatListing(tools, 3000)
	if !strings.Contains(full, "await tools.a") || !strings.Contains(full, "await tools.b") || !strings.Contains(full, "ns") {
		t.Fatalf("full %s", full)
	}
	tiny := FormatListing(tools, 1)
	if strings.Contains(tiny, "await tools") {
		t.Fatalf("budget 1 listed a declaration:\n%s", tiny)
	}
	if FormatListing(nil, 0) != "" {
		t.Fatal("empty listing")
	}
}
