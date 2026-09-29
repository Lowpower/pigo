package bugreport

import (
	"os"
	"testing"
	"time"
)

func TestCrashLogRoundTrip(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 6; i++ {
		if _, ok := RecordCrash(dir, CrashInput{Kind: KindFatal, Err: i, Cwd: "/tmp"}); !ok {
			t.Fatal("record")
		}
	}
	got := ReadCrashLog(dir)
	if len(got) != 5 {
		t.Fatalf("len %d", len(got))
	}
	if got[0].Message == "0" {
		t.Fatal("oldest record kept")
	}
	info, err := os.Stat(CrashPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}

	now := time.Now()
	first := TakeUnnotifiedCrash(dir, now)
	if first == nil {
		t.Fatal("expected crash")
	}
	if again := TakeUnnotifiedCrash(dir, now); again != nil {
		t.Fatal("second take")
	}
	ClearCrashLog(dir)
	if len(ReadCrashLog(dir)) != 0 {
		t.Fatal("cleared log still readable")
	}
}

func TestTakeUnnotifiedIgnoresOldCrashes(t *testing.T) {
	dir := t.TempDir()
	old := CrashRecord{
		Timestamp: time.Now().Add(-8 * 24 * time.Hour).UTC().Format(time.RFC3339Nano),
		Version:   "v",
		Kind:      KindUncaught,
		Message:   "old",
		Cwd:       "/tmp",
	}
	if err := writeCrashLog(CrashPath(dir), []CrashRecord{old}); err != nil {
		t.Fatal(err)
	}
	if TakeUnnotifiedCrash(dir, time.Now()) != nil {
		t.Fatal("old crash announced")
	}
	if ReadCrashLog(dir)[0].Notified {
		t.Fatal("old crash should stay unnotified")
	}
}
