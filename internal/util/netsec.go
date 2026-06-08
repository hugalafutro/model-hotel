package util

import "net"

// cgnatNet is the carrier-grade NAT range (RFC 6598), 100.64.0.0/10.
// Go's net.IP.IsPrivate does not cover it, so we check it explicitly.
// Parsed from the CIDR string so the IP and mask widths stay consistent
// (a literal net.IPv4 is 16 bytes while net.CIDRMask(10, 32) is 4).
var _, cgnatNet, _ = net.ParseCIDR("100.64.0.0/10")

// IsBlockedIP reports whether an IP falls into a range that must never be
// dialled by the proxy or accepted as a provider base URL: unspecified,
// loopback, private (RFC 1918 + IPv6 ULA), link-local, carrier-grade NAT
// (RFC 6598), or cloud-metadata. It is shared by the runtime SafeDialer and
// provider-URL validation so the two layers stay in lockstep.
func IsBlockedIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsUnspecified() {
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
