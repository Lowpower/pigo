package runtime

import "testing"

func TestInterpretUserBash(t *testing.T) {
	local, _, err := interpretUserBash(nil)
	if err != nil || !local {
		t.Fatalf("nil payload local=%v err=%v", local, err)
	}
	local, _, err = interpretUserBash(map[string]any{})
	if err != nil || !local {
		t.Fatalf("empty payload local=%v err=%v", local, err)
	}
	_, _, err = interpretUserBash(map[string]any{"block": true})
	if err == nil {
		t.Fatal("block should be invalid")
	}
	_, _, err = interpretUserBash(map[string]any{"operations": map[string]any{}})
	if err == nil {
		t.Fatal("operations should be invalid")
	}
	_, res, err := interpretUserBash(map[string]any{"result": map[string]any{
		"output": "ok", "cancelled": false, "truncated": true, "exitCode": 2.0,
		"fullOutputPath": "/tmp/out",
	}})
	if err != nil || res.Output != "ok" || !res.Truncated || res.FullOutputPath != "/tmp/out" || res.ExitCode == nil || *res.ExitCode != 2 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}
