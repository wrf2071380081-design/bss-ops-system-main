// Package supplysecurity contains environment URL policy for A's formal
// supplier configuration. API keys are stored as entered and are not encrypted.
package supplysecurity

import (
	"context"
	"net"
	"net/url"
	"strconv"
	"strings"

	"model_bss/internal/domain/supply"
)

type IPResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type systemResolver struct{}

func (systemResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return net.DefaultResolver.LookupIPAddr(ctx, host)
}

type FormalURLPolicy struct{ resolver IPResolver }

func NewFormalURLPolicy(resolver IPResolver) *FormalURLPolicy {
	if resolver == nil {
		resolver = systemResolver{}
	}
	return &FormalURLPolicy{resolver: resolver}
}

func (p *FormalURLPolicy) ValidateFormalURL(ctx context.Context, raw string) error {
	if p == nil || p.resolver == nil || ctx == nil || ctx.Err() != nil {
		return supply.ErrDependencyUnavailable
	}
	u, err := url.ParseRequestURI(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Opaque != "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.Host == "" {
		return supply.ErrForbidden
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return supply.ErrForbidden
		}
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "" || strings.Contains(host, "%") {
		return supply.ErrForbidden
	}
	if ip := net.ParseIP(host); ip != nil {
		if !isPublicAddress(ip) {
			return supply.ErrForbidden
		}
		return nil
	}
	addresses, err := p.resolver.LookupIPAddr(ctx, host)
	if err != nil || len(addresses) == 0 {
		return supply.ErrForbidden
	}
	for _, address := range addresses {
		if !isPublicAddress(address.IP) {
			return supply.ErrForbidden
		}
	}
	return nil
}

func isPublicAddress(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	// These ranges are not globally routable even though Go reports some of
	// them as global unicast.
	blocked := []string{"100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "2001:db8::/32"}
	for _, value := range blocked {
		_, network, _ := net.ParseCIDR(value)
		if network.Contains(ip) {
			return false
		}
	}
	return true
}

var _ supply.FormalURLPolicy = (*FormalURLPolicy)(nil)
