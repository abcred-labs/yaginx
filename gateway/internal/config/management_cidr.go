package config

import (
	"fmt"
	"net"
)

func DefaultManagementCIDRs() []string {
	return []string{"127.0.0.1/32", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "100.64.0.0/10"}
}

// ClientIPAllowed reports whether the client address belongs to one of the
// configured management networks. It accepts both IPv4 and IPv6 CIDRs and
// rejects malformed entries instead of silently widening access.
func ClientIPAllowed(clientIP string, cidrs []string) (bool, error) {
	ip := net.ParseIP(clientIP)
	if ip == nil {
		return false, fmt.Errorf("invalid client IP %q", clientIP)
	}
	for _, value := range cidrs {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return false, fmt.Errorf("invalid management CIDR %q: %w", value, err)
		}
		if network.Contains(ip) {
			return true, nil
		}
	}
	return false, nil
}
