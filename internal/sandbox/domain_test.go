package sandbox

import "testing"

func TestHostAllowed(t *testing.T) {
	allow := []string{"github.com", "*.github.com", "example.com:443"}
	deny := []string{"evil.github.com"}
	if !HostAllowed("github.com:443", allow, deny) {
		t.Fatal("apex github.com")
	}
	if !HostAllowed("api.github.com:443", allow, deny) {
		t.Fatal("*.github.com should match api.github.com")
	}
	if HostAllowed("github.com.evil.com:443", allow, deny) {
		t.Fatal("suffix bypass")
	}
	if HostAllowed("evil.github.com:443", allow, deny) {
		t.Fatal("denied should win")
	}
	if HostAllowed("example.com:80", allow, deny) {
		t.Fatal("port 80 should miss example.com:443")
	}
	if !HostAllowed("example.com:443", allow, deny) {
		t.Fatal("example.com:443")
	}
	if HostAllowed("npmjs.org:443", allow, deny) {
		t.Fatal("not in allow list")
	}
	if HostAllowed("github.com:443", nil, nil) {
		t.Fatal("empty allow list is deny-all")
	}
}
