package httpapi

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

func loginPeer(r *http.Request, trusted []netip.Prefix) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return "unknown"
	}
	peer = peer.Unmap()
	isTrusted := func(ip netip.Addr) bool {
		for _, network := range trusted {
			if network.Contains(ip) {
				return true
			}
		}
		return false
	}
	if !isTrusted(peer) {
		return peer.String()
	}
	// Validate the complete chain before using it. Walking right-to-left avoids
	// trusting a caller-supplied leftmost IP when an ingress appends its peer.
	forwarded := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	if len(forwarded) > 16 {
		return peer.String()
	}
	ips := make([]netip.Addr, 0, len(forwarded))
	for _, value := range forwarded {
		ip, err := netip.ParseAddr(strings.TrimSpace(value))
		if err != nil || ip.Zone() != "" {
			return peer.String()
		}
		ips = append(ips, ip.Unmap())
	}
	for index := len(ips) - 1; index >= 0; index-- {
		if !isTrusted(ips[index]) {
			return ips[index].String()
		}
	}
	return peer.String()
}
