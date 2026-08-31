package main

import (
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/ext"
)

func TestPeelUnknownFlagsKeepsKnownAndPositional(t *testing.T) {
	rest, unknown := peelUnknownFlags([]string{"--print", "hello", "--plan"})
	if strings.Join(rest, ",") != "--print,hello" {
		t.Fatalf("rest=%v", rest)
	}
	if len(unknown) != 1 || unknown[0].Name != "plan" || unknown[0].HasValue {
		t.Fatalf("unknown=%+v", unknown)
	}
}

func TestPeelUnknownFlagsEqualsAndToken(t *testing.T) {
	rest, unknown := peelUnknownFlags([]string{"--foo=bar", "--name", "s1", "--baz", "qux"})
	if strings.Join(rest, ",") != "--name,s1" {
		t.Fatalf("rest=%v", rest)
	}
	if len(unknown) != 2 {
		t.Fatalf("unknown=%+v", unknown)
	}
	if unknown[0].Name != "foo" || unknown[0].Value != "bar" || !unknown[0].HasValue {
		t.Fatalf("foo=%+v", unknown[0])
	}
	if unknown[1].Name != "baz" || unknown[1].Value != "qux" {
		t.Fatalf("baz=%+v", unknown[1])
	}
}

func TestPeelUnknownFlagsDoesNotEatNextFlag(t *testing.T) {
	_, unknown := peelUnknownFlags([]string{"--plan", "--verbose"})
	if len(unknown) != 1 || unknown[0].Name != "plan" || unknown[0].HasValue {
		t.Fatalf("unknown=%+v", unknown)
	}
}

func TestPeelUnknownFlagsSkipsSubcommands(t *testing.T) {
	rest, unknown := peelUnknownFlags([]string{"auth", "login", "--foo"})
	if len(unknown) != 0 {
		t.Fatalf("unknown=%+v", unknown)
	}
	if strings.Join(rest, ",") != "auth,login,--foo" {
		t.Fatalf("rest=%v", rest)
	}
}

func TestFormatUnclaimed(t *testing.T) {
	got := formatUnclaimed([]ext.UnknownFlag{{Name: "plan"}, {Name: "foo"}})
	if got != "unknown options: --plan, --foo" {
		t.Fatalf("%s", got)
	}
}
