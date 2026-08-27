package ratelimit

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

const unknownKey = "unknown"

const v6BucketBits = 64

type IPResolver struct {
	trusted []netip.Prefix
}

func NewIPResolver(cidrs []string) (*IPResolver, error) {
	var out []netip.Prefix
	for _, c := range cidrs {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if addr, err := netip.ParseAddr(c); err == nil {
			addr = addr.Unmap()
			out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
			continue
		}
		p, err := netip.ParsePrefix(c)
		if err != nil {
			return nil, fmt.Errorf("ratelimit: invalid trusted proxy %q: %w", c, err)
		}
		out = append(out, p.Masked())
	}
	return &IPResolver{trusted: out}, nil
}

func (r *IPResolver) isTrusted(a netip.Addr) bool {
	a = a.Unmap()
	for _, p := range r.trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

func (r *IPResolver) ClientIP(req *http.Request) (netip.Addr, bool) {
	peer, ok := peerAddr(req.RemoteAddr)
	if !ok {
		return netip.Addr{}, false
	}
	if !r.isTrusted(peer) {
		return peer, true
	}

	hops := forwardedFor(req)
	for i := len(hops) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			return netip.Addr{}, false
		}
		a = a.Unmap()
		if r.isTrusted(a) {
			continue
		}
		return a, true
	}
	return peer, true
}

func (r *IPResolver) Key(req *http.Request) string {
	a, ok := r.ClientIP(req)
	if !ok {
		return unknownKey
	}
	if a.Is4() {
		return a.String()
	}
	p, err := a.Prefix(v6BucketBits)
	if err != nil {
		return a.String()
	}
	return p.String()
}

func peerAddr(remoteAddr string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	host = strings.Trim(host, "[]")
	if i := strings.IndexByte(host, '%'); i >= 0 {
		host = host[:i]
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return a.Unmap(), true
}

func forwardedFor(req *http.Request) []string {
	var hops []string
	for _, h := range req.Header.Values("X-Forwarded-For") {
		for _, part := range strings.Split(h, ",") {
			if part = strings.TrimSpace(part); part != "" {
				hops = append(hops, part)
			}
		}
	}
	return hops
}
