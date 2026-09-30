package main

import (
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/ext"
)

func TestPeelUnknownFlags(t *testing.T) {
	type flag struct {
		name, value string
		hasValue    bool
	}
	tests := []struct {
		name string
		args []string
		rest string
		want []flag
	}{
		{"keeps known and positional", []string{"--print", "hello", "--plan"}, "--print,hello", []flag{{"plan", "", false}}},
		{"equals and token", []string{"--foo=bar", "--name", "s1", "--baz", "qux"}, "--name,s1", []flag{{"foo", "bar", true}, {"baz", "qux", true}}},
		{"does not eat next flag", []string{"--plan", "--verbose"}, "--verbose", []flag{{"plan", "", false}}},
		{"skips auth subcommand", []string{"auth", "login", "--foo"}, "auth,login,--foo", nil},
		{"skips server subcommand", []string{"server", "--listen", "unix:///tmp/pigo.sock"}, "server,--listen,unix:///tmp/pigo.sock", nil},
		{"skips eval subcommand", []string{"eval", "--out", "r"}, "eval,--out,r", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rest, unknown := peelUnknownFlags(tt.args)
			if strings.Join(rest, ",") != tt.rest {
				t.Fatalf("rest=%v want %s", rest, tt.rest)
			}
			if len(unknown) != len(tt.want) {
				t.Fatalf("unknown=%+v", unknown)
			}
			for i, w := range tt.want {
				u := unknown[i]
				if u.Name != w.name || u.Value != w.value || u.HasValue != w.hasValue {
					t.Fatalf("unknown[%d]=%+v want %+v", i, u, w)
				}
			}
		})
	}
}

func TestFormatUnclaimed(t *testing.T) {
	got := formatUnclaimed([]ext.UnknownFlag{{Name: "plan"}, {Name: "foo"}})
	if got != "unknown options: --plan, --foo" {
		t.Fatalf("%s", got)
	}
}
