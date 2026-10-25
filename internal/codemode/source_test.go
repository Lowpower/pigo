package codemode

import (
	"strings"
	"testing"
)

func TestParsePlainSource(t *testing.T) {
	code, opt, err := Parse("host.echo(\"hi\")")
	if err != nil {
		t.Fatal(err)
	}
	if code != "host.echo(\"hi\")" || opt.TimeoutMS != nil || opt.MaxOutputTokens != nil {
		t.Fatalf("code %q opt %+v", code, opt)
	}
}

func TestParseOptionsFirstLine(t *testing.T) {
	code, opt, err := Parse("// @options: {\"timeout_ms\": 1500, \"max_output_tokens\": 20}\n1+1\n")
	if err != nil {
		t.Fatal(err)
	}
	if code != "\n1+1\n" {
		t.Fatalf("code %q", code)
	}
	if opt.TimeoutMS == nil || *opt.TimeoutMS != 1500 {
		t.Fatalf("timeout %+v", opt.TimeoutMS)
	}
	if opt.MaxOutputTokens == nil || *opt.MaxOutputTokens != 20 {
		t.Fatalf("max output %+v", opt.MaxOutputTokens)
	}
}

func TestParseOptionsLeadingSpace(t *testing.T) {
	_, opt, err := Parse("  // @options: {\"timeout_ms\": 5}\n1\n")
	if err != nil {
		t.Fatal(err)
	}
	if opt.TimeoutMS == nil || *opt.TimeoutMS != 5 {
		t.Fatalf("timeout %+v", opt.TimeoutMS)
	}
}

func TestParseRejects(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "  \n", want: "non-empty"},
		{name: "misplaced", in: "1\n// @options: {\"timeout_ms\": 5}\n", want: "line 2"},
		{name: "only options", in: "// @options: {\"timeout_ms\": 5}", want: "subsequent lines"},
		{name: "zero timeout", in: "// @options: {\"timeout_ms\": 0}\n1\n", want: "positive"},
		{name: "negative", in: "// @options: {\"timeout_ms\": -1}\n1\n", want: "positive"},
		{name: "fraction", in: "// @options: {\"timeout_ms\": 1.5}\n1\n", want: "integer"},
		{name: "too big", in: "// @options: {\"timeout_ms\": 2147483648}\n1\n", want: "2147483647"},
		{name: "unknown", in: "// @options: {\"nope\": 1}\n1\n", want: "nope"},
		{name: "array", in: "// @options: []\n1\n", want: "JSON object"},
		{name: "bad json", in: "// @options: {\n1\n", want: "valid JSON"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := Parse(tc.in)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err %v, want substring %q", err, tc.want)
			}
		})
	}
}
