package main

import (
	"fmt"
	"net"
	"strings"
)

// parseIPRule accepts a single IP address or a CIDR range.
func parseIPRule(s string) (*net.IPNet, error) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "/") {
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			return nil, fmt.Errorf("ungültiger IP-Bereich %q", s)
		}
		return n, nil
	}
	ip := net.ParseIP(s)
	if ip == nil {
		return nil, fmt.Errorf("ungültige IP-Adresse %q", s)
	}
	if v4 := ip.To4(); v4 != nil {
		return &net.IPNet{IP: v4, Mask: net.CIDRMask(32, 32)}, nil
	}
	return &net.IPNet{IP: ip, Mask: net.CIDRMask(128, 128)}, nil
}

// normalizeIPRules validates rules and writes them in canonical form ("10.0.0.0/8", "203.0.113.7").
func normalizeIPRules(in []string) ([]string, error) {
	out := []string{}
	for _, r := range in {
		if r = strings.TrimSpace(r); r == "" {
			continue
		}
		n, err := parseIPRule(r)
		if err != nil {
			return nil, err
		}
		if ones, bits := n.Mask.Size(); ones == bits {
			out = append(out, n.IP.String())
		} else {
			out = append(out, n.String())
		}
	}
	return out, nil
}

func ipMatch(rules []string, ip string) bool {
	addr := net.ParseIP(strings.TrimSpace(ip))
	if addr == nil {
		return false
	}
	for _, r := range rules {
		if n, err := parseIPRule(r); err == nil && n.Contains(addr) {
			return true
		}
	}
	return false
}
