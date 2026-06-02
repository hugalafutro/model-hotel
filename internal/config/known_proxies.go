package config

import (
	"net"
	"os"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// LoadKnownProxies reads the KNOWN_PROXIES env var (comma-separated CIDRs)
// and returns a slice of parsed *net.IPNet. IPs within these CIDRs bypass
// SafeDialer's private-IP restrictions on outbound connections. Returns
// empty slice if the env var is empty or contains no valid CIDRs.
func LoadKnownProxies() []*net.IPNet {
	raw := os.Getenv("KNOWN_PROXIES")
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
			debuglog.Warn("KNOWN_PROXIES: skipping invalid CIDR", "cidr", p, "error", err)
			continue
		}
		nets = append(nets, cidr)
	}
	return nets
}
