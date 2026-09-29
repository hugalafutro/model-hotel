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
// (RFC 6598), or cloud-metadata. A NAT64 address is judged by the IPv4
// addresses it embeds (see NAT64IPv4s). It is shared by the runtime SafeDialer and
// provider-URL validation so the two layers stay in lockstep.
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

// nat64Layouts are the byte positions of the embedded IPv4 address in the
// RFC 6052 section 2.2 layouts that fit inside a /48: prefix lengths 48, 56,
// 64 and 96. Byte 8 is the u-octet, which never carries address bits.
var nat64Layouts = [][4]int{
	{6, 7, 9, 10},
	{7, 9, 10, 11},
	{9, 10, 11, 12},
	{12, 13, 14, 15},
}

// NAT64IPv4s returns the IPv4 destinations a NAT64 translator could deliver ip
// to, or nil when ip is not under a NAT64 prefix. Each guard applies its own
// policy to every returned address and blocks ip when any of them is blocked.
//
// The well-known prefix 64:ff9b::/96 has one layout, so it yields one address.
// Under the local-use prefix 64:ff9b:1::/48 the operator picks the prefix
// length (/48 to /96), which the address alone does not reveal, so every
// layout's reading is returned and the guard fails closed across them: an
// attacker cannot pick an encoding whose reading under the gateway's actual
// layout differs from the one judged. A reading inside 0.0.0.0/8 is dropped,
// since the zero bytes of one layout (its suffix or subnet ID) land there under
// another and a translator does not forward to 0.0.0.0/8 ("this host", a
// source-only range per RFC 6890); if every reading is dropped, 0.0.0.0 is
// returned so the guard still blocks.
func NAT64IPv4s(ip net.IP) []net.IP {
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return nil
	}
	b := a.As16()
	if nat64WellKnown.Contains(a) {
		return []net.IP{net.IPv4(b[12], b[13], b[14], b[15])}
	}
	if !nat64LocalUse.Contains(a) {
		return nil
	}
	var out []net.IP
	for _, l := range nat64Layouts {
		if b[l[0]] != 0 {
			out = append(out, net.IPv4(b[l[0]], b[l[1]], b[l[2]], b[l[3]]))
		}
	}
	if out == nil {
		out = []net.IP{net.IPv4zero}
	}
	return out
}
