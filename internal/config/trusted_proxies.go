package config

import (
	"net"
	"os"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// LoadTrustedProxies reads the TRUSTED_PROXIES env var (comma-separated CIDRs)
// and returns a slice of parsed *net.IPNet. Returns empty slice if the env var
// is empty or contains no valid CIDRs.
func LoadTrustedProxies() []*net.IPNet {
	raw := os.Getenv("TRUSTED_PROXIES")
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	nets := make([]*net.IPNet, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		_, cidr, err := net.ParseCIDR(p)
		if err != nil {
			debuglog.Warn("TRUSTED_PROXIES: skipping invalid CIDR", "cidr", p, "error", err)
			continue
		}
		nets = append(nets, cidr)
	}
	return nets
}

// IsTrustedProxy checks if the given remoteAddr (in "ip:port" format) belongs
// to any of the trusted CIDR networks. Returns false if trustedNets is empty.
// If remoteAddr has no port, it is treated as a bare IP.
func IsTrustedProxy(remoteAddr string, trustedNets []*net.IPNet) bool {
	if len(trustedNets) == 0 {
		return false
	}
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, n := range trustedNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
