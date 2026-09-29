package util

import (
	"net"
	"net/netip"
	"slices"
)

// cgnatNet is the carrier-grade NAT range (RFC 6598), 100.64.0.0/10.
// Go's net.IP.IsPrivate does not cover it, so we check it explicitly.
// Parsed from the CIDR string so the IP and mask widths stay consistent
// (a literal net.IPv4 is 16 bytes while net.CIDRMask(10, 32) is 4).
var _, cgnatNet, _ = net.ParseCIDR("100.64.0.0/10")

// IsBlockedIP reports whether an IP falls into a range that must never be
// dialled by the proxy or accepted as a provider base URL: unspecified,
// loopback, private (RFC 1918 + IPv6 ULA), link-local, carrier-grade NAT
// (RFC 6598), or cloud-metadata. A well-known-prefix NAT64 address is judged
// by the IPv4 address it embeds; a local-use one is refused (see NAT64IPv4s).
// It is shared by the runtime SafeDialer and provider-URL validation so the
// two layers stay in lockstep.
func IsBlockedIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if v4s := NAT64IPv4s(ip); v4s != nil {
		return slices.ContainsFunc(v4s, IsBlockedIP)
	}
	// IsUnspecified covers only 0.0.0.0 and ::, but a dial to any address in
	// 0.0.0.0/8 ("this host", RFC 1122) lands on the local machine.
	if ip.IsUnspecified() || (ip.To4() != nil && ip.To4()[0] == 0) {
		return true
	}
	if ip.IsLoopback() {
		return true
	}
	if ip.IsPrivate() {
		return true
	}
	if ip.IsLinkLocalUnicast() {
		return true
	}
	if ip.IsLinkLocalMulticast() {
		return true
	}
	// Carrier-grade NAT (RFC 6598): 100.64.0.0/10. Not covered by IsPrivate.
	if cgnatNet.Contains(ip) {
		return true
	}
	// 169.254.169.254 is link-local unicast (caught above), but explicitly
	// check the string form for defence-in-depth against cloud metadata.
	if ip.String() == "169.254.169.254" {
		return true
	}
	return false
}

// NAT64 prefixes (RFC 6052 well-known, RFC 8215 local-use).
var (
	nat64WellKnown = netip.MustParsePrefix("64:ff9b::/96")
	nat64LocalUse  = netip.MustParsePrefix("64:ff9b:1::/48")
)

// NAT64IPv4s returns the IPv4 destinations a NAT64 translator could deliver ip
// to, or nil when ip is not under a NAT64 prefix. Each guard applies its own
// policy to every returned address and blocks ip when any of them is blocked.
//
// The well-known prefix 64:ff9b::/96 has one layout, so its embedded address
// is exact. The local-use prefix 64:ff9b:1::/48 is refused outright (0.0.0.0,
// which both guards block): its operator picks the prefix length, which the
// address alone does not reveal, so no reading of it is certain to be the one
// the gateway delivers to, and every rule that tries to pick one either lets a
// hidden destination through or blocks most real deployments. A provider on a
// local-use NAT64 network is reached through ALLOWED_PROVIDER_HOSTS, which the
// proxy dialer and provider base_url validation honour. netguard's clients (SSO
// identity providers, apprise, Front Desk members) have no such list, so a
// local-use NAT64 address stays unreachable for them.
func NAT64IPv4s(ip net.IP) []net.IP {
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return nil
	}
	switch {
	case nat64WellKnown.Contains(a):
		b := a.As16()
		return []net.IP{net.IPv4(b[12], b[13], b[14], b[15])}
	case nat64LocalUse.Contains(a):
		return []net.IP{net.IPv4zero}
	default:
		return nil
	}
}
