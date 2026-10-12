package bom

import "testing"

func TestStrip(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "absent", in: `{"theme":"dark"}`, want: `{"theme":"dark"}`},
		{name: "only", in: "\ufeff", want: ""},
		{name: "json", in: "\ufeff{\"ok\":true}", want: `{"ok":true}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := string(Strip([]byte(tc.in)))
			if got != tc.want {
				t.Fatalf("Strip(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
