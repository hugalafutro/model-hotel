package config

import (
	"net"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// loadCIDRs reads a comma-separated CIDR list from an environment variable and
// returns the parsed networks, reporting each unparseable entry through logf
// and carrying on. Returns nil when the variable is unset or holds no valid
// CIDR.
func loadCIDRs(envKey string, logf func(string, ...any)) []*net.IPNet {
	parts := util.SplitAndTrim(EnvOr(envKey, ""))
	if len(parts) == 0 {
		return nil
	}
	nets := make([]*net.IPNet, 0, len(parts))
	for _, p := range parts {
		_, cidr, err := net.ParseCIDR(p)
		if err != nil {
			logf(envKey+": skipping invalid CIDR", "cidr", p, "error", err)
			continue
		}
		nets = append(nets, cidr)
	}
	return nets
}

// LoadTrustedProxies reads the TRUSTED_PROXIES env var (comma-separated CIDRs)
// and returns the parsed networks. A request whose TCP peer falls inside one of
// them is allowed to speak for its client through forwarded headers.
func LoadTrustedProxies() []*net.IPNet {
	return loadCIDRs("TRUSTED_PROXIES", debuglog.Warn)
}

// LoadKnownProxies reads the KNOWN_PROXIES env var (comma-separated CIDRs) and
// returns the parsed networks. IPs within these CIDRs bypass SafeDialer's
// private-IP restrictions on outbound connections.
//
// Invalid entries are reported at Error: debuglog.Error already reaches both
// stderr and the app-log ring buffer, so a separate os.Stderr write would just
// duplicate it.
func LoadKnownProxies() []*net.IPNet {
	return loadCIDRs("KNOWN_PROXIES", debuglog.Error)
}
