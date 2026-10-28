package api

import (
	"net"
	"net/http"
	"strings"
)

// trustedProxyMatcher reports whether an address is one of the configured
// trusted proxies. Entries are single IPs or CIDR ranges; unparsable entries
// are ignored, so a typo can only make the server trust less.
type trustedProxyMatcher struct {
	nets []*net.IPNet
}

func newTrustedProxyMatcher(entries []string) trustedProxyMatcher {
	var m trustedProxyMatcher
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if _, n, err := net.ParseCIDR(entry); err == nil {
			m.nets = append(m.nets, n)
			continue
		}
		if ip := net.ParseIP(entry); ip != nil {
			bits := 128
			if v4 := ip.To4(); v4 != nil {
				ip, bits = v4, 32
			}
			m.nets = append(m.nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
		}
	}
	return m
}

func (m trustedProxyMatcher) trusts(ip net.IP) bool {
	if ip == nil {
		return false
	}
	for _, n := range m.nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// remotePeerIP is the address of the immediate TCP peer.
func remotePeerIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.String()
	}
	return host
}

// resolveClientIP returns the client address used for rate limiting,
// lockouts and connection caps.
//
// X-Forwarded-For is honoured only when the immediate peer is a trusted
// proxy, and even then only the hops proxies appended are believed: the
// header is walked right to left, skipping trusted proxies, and the first
// address that is not one is the client. Anything left of it was supplied by
// that client and is ignored, so prepending fake entries cannot change the
// result. A malformed hop ends the walk at the last address a trusted proxy
// vouched for.
func resolveClientIP(r *http.Request, trusted trustedProxyMatcher) string {
	peer := remotePeerIP(r)
	if !trusted.trusts(net.ParseIP(peer)) {
		return peer
	}
	client := peer
	values := r.Header.Values("X-Forwarded-For")
	for i := len(values) - 1; i >= 0; i-- {
		hops := strings.Split(values[i], ",")
		for j := len(hops) - 1; j >= 0; j-- {
			hop := strings.TrimSpace(hops[j])
			if hop == "" {
				continue
			}
			ip := net.ParseIP(hop)
			if ip == nil {
				return client
			}
			client = ip.String()
			if !trusted.trusts(ip) {
				return client
			}
		}
	}
	return client
}

// requestIsHTTPS reports whether the client reached this server over HTTPS:
// TLS terminated here, or X-Forwarded-Proto: https set by a trusted proxy.
func requestIsHTTPS(r *http.Request, trusted trustedProxyMatcher) bool {
	if r.TLS != nil {
		return true
	}
	if !trusted.trusts(net.ParseIP(remotePeerIP(r))) {
		return false
	}
	proto := r.Header.Get("X-Forwarded-Proto")
	if i := strings.IndexByte(proto, ','); i >= 0 {
		proto = proto[:i]
	}
	return strings.EqualFold(strings.TrimSpace(proto), "https")
}

// ClientIPResolver returns the spoof-resistant client address resolver the
// REST server uses, for handlers mounted outside it (status pages).
func ClientIPResolver(trustedProxies []string) func(*http.Request) string {
	trusted := newTrustedProxyMatcher(trustedProxies)
	return func(r *http.Request) string { return resolveClientIP(r, trusted) }
}
