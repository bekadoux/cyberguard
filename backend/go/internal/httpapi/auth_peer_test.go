package httpapi

import (
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestLoginPeerTrustBoundary(t *testing.T) {
	t.Parallel()
	proxy := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")}
	for _, test := range []struct {
		name, remote, forwarded, want string
		trusted                       []netip.Prefix
	}{
		{"default ignores headers", "192.0.2.1:80", "203.0.113.1", "192.0.2.1", nil},
		{"untrusted peer", "192.0.2.1:80", "203.0.113.1", "192.0.2.1", proxy},
		{"trusted ingress", "10.0.0.2:80", "203.0.113.1", "203.0.113.1", proxy},
		{"trusted chain", "10.0.0.2:80", "203.0.113.1,10.0.0.3", "203.0.113.1", proxy},
		{"spoofed first IP", "10.0.0.2:80", "198.51.100.99,203.0.113.1", "203.0.113.1", proxy},
		{"bad chain", "10.0.0.2:80", "garbage,203.0.113.1", "10.0.0.2", proxy},
		{"missing chain", "10.0.0.2:80", "", "10.0.0.2", proxy},
		{"all trusted", "10.0.0.2:80", "10.0.0.3", "10.0.0.2", proxy},
		{"IPv4 mapped", "[::ffff:192.0.2.1]:80", "203.0.113.1", "192.0.2.1", nil},
		{"invalid peer", "not-an-ip", "203.0.113.1", "unknown", proxy},
		{"too many hops", "10.0.0.2:80", strings.Repeat("203.0.113.1,", 16) + "203.0.113.1", "10.0.0.2", proxy},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest("POST", "/auth/login", nil)
			r.RemoteAddr = test.remote
			r.Header.Set("X-Forwarded-For", test.forwarded)
			r.Header.Set("X-Real-IP", "198.51.100.22")
			if actual := loginPeer(r, test.trusted); actual != test.want {
				t.Fatalf("got peer %s, want %s", actual, test.want)
			}
		})
	}
}
