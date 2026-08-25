package prompt

import "testing"

func TestSubstituteArgsPiPatterns(t *testing.T) {
	args := []string{"one", "two", "three"}
	cases := []struct {
		in, want string
	}{
		{"use $1 and $2", "use one and two"},
		{"all $@", "all one two three"},
		{"all $ARGUMENTS", "all one two three"},
		{"${1:-fallback}", "one"},
		{"${9:-fallback}", "fallback"},
		{"${@:-none}", "one two three"},
		{"${@:2}", "two three"},
		{"${@:2:1}", "two"},
		{"missing $4", "missing "},
	}
	for _, c := range cases {
		got := SubstituteArgs(c.in, args)
		if got != c.want {
			t.Errorf("SubstituteArgs(%q) = %q want %q", c.in, got, c.want)
		}
	}
}

func TestParseCommandArgsQuotes(t *testing.T) {
	got := ParseCommandArgs(`foo "bar baz" 'x y'`)
	if len(got) != 3 || got[0] != "foo" || got[1] != "bar baz" || got[2] != "x y" {
		t.Fatalf("%q", got)
	}
}

func TestExpandTemplate(t *testing.T) {
	tpls := []Template{{Name: "review", Content: "Review $1"}}
	got, ok := ExpandTemplate("/review pkg/", tpls)
	if !ok || got != "Review pkg/" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
	if _, ok := ExpandTemplate("/nope", tpls); ok {
		t.Fatal("unknown template should not expand")
	}
}
