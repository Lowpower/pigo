package shell

import "testing"

func TestANSIStripperHoldsSplitColor(t *testing.T) {
	var s ANSIStripper
	got := s.Write("\x1b") + s.Write("[31mred\x1b[0m") + s.Flush()
	if got != "red" {
		t.Fatalf("got %q", got)
	}
}

func TestANSIStripperHoldsSplitReset(t *testing.T) {
	var s ANSIStripper
	got := s.Write("\x1b[31mERROR\x1b[0") + s.Write("m\n") + s.Flush()
	if got != "ERROR\n" {
		t.Fatalf("got %q", got)
	}
}

func TestANSIStripperPassesPlainText(t *testing.T) {
	var s ANSIStripper
	got := s.Write("hello") + s.Write(" world") + s.Flush()
	if got != "hello world" {
		t.Fatalf("got %q", got)
	}
}

func TestANSIStripperDropsOneShotSGR(t *testing.T) {
	var s ANSIStripper
	if got := s.Write("\x1b[31mred\x1b[0m") + s.Flush(); got != "red" {
		t.Fatalf("got %q", got)
	}
}

func TestANSIStripperHoldsSplitOSC(t *testing.T) {
	var s ANSIStripper
	got := s.Write("a\x1b]0;title") + s.Write("\x07b") + s.Flush()
	if got != "ab" {
		t.Fatalf("got %q", got)
	}
}

func TestANSIStripperDropsC1CSI(t *testing.T) {
	var s ANSIStripper
	if got := s.Write("\x9b31mred\x9b0m") + s.Flush(); got != "red" {
		t.Fatalf("got %q", got)
	}
}

func TestANSIStripperKeepsUTF8(t *testing.T) {
	text := string([]byte{0xe4, 0x9b, 0x80})
	var s ANSIStripper
	if got := s.Write(text[:2]) + s.Write(text[2:]) + s.Flush(); got != text {
		t.Fatalf("got %q", got)
	}
}

func TestANSIStripperFlushEmitsUnfinished(t *testing.T) {
	var s ANSIStripper
	if got := s.Write("x\x1b") + s.Flush(); got != "x\x1b" {
		t.Fatalf("got %q", got)
	}
}
