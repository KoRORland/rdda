package singboxconf

import "testing"

func TestSplitHostPort(t *testing.T) {
	h, p := splitHostPort("www.microsoft.com:8443", 443)
	if h != "www.microsoft.com" || p != 8443 {
		t.Fatalf("got %s:%d", h, p)
	}
	h, p = splitHostPort("example.com", 443)
	if h != "example.com" || p != 443 {
		t.Fatalf("default port not applied: %s:%d", h, p)
	}
}
