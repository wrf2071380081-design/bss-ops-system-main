// Package supplysecurity contains production security adapters for A's supply
// configuration. Secrets use versioned AES-GCM envelopes; URLs are restricted
// to public HTTPS endpoints before they can be stored.
package supplysecurity

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"model_bss/internal/domain/supply"
)

const (
	keyVersionEnv = "MODEL_BSS_FORMAL_CONFIG_KEY_VERSION"
	keyBytesEnv   = "MODEL_BSS_FORMAL_CONFIG_KEY"
)

var errSecurityConfig = errors.New("formal configuration security settings are invalid")

// AESGCMSealer encrypts credentials with one active 256-bit key. The envelope
// carries the configured key version separately, while AAD binds the ciphertext
// to its purpose, supplier, and configuration identity.
type AESGCMSealer struct {
	version string
	aead    cipher.AEAD
}

func NewAESGCMSealer(version string, key []byte) (*AESGCMSealer, error) {
	if strings.TrimSpace(version) != version || version == "" || len(version) > 64 || !utf8.ValidString(version) || strings.ContainsRune(version, 0) {
		return nil, errSecurityConfig
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errSecurityConfig
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errSecurityConfig
	}
	return &AESGCMSealer{version: version, aead: aead}, nil
}

// AESGCMSealerFromEnv returns (nil, nil) when neither setting is configured,
// allowing other application modules to start while formal-config writes stay
// disabled. Partial or malformed settings fail closed.
func AESGCMSealerFromEnv() (*AESGCMSealer, error) {
	version, hasVersion := os.LookupEnv(keyVersionEnv)
	encoded, hasKey := os.LookupEnv(keyBytesEnv)
	if !hasVersion && !hasKey {
		return nil, nil
	}
	if !hasVersion || !hasKey || strings.TrimSpace(version) == "" || strings.TrimSpace(encoded) != encoded {
		return nil, errSecurityConfig
	}
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(key) != 32 {
		return nil, errSecurityConfig
	}
	return NewAESGCMSealer(version, key)
}

func (s *AESGCMSealer) Seal(ctx context.Context, plaintext string, binding supply.SecretBinding) (supply.SecretEnvelope, error) {
	if s == nil || s.aead == nil || s.version == "" || ctx == nil || ctx.Err() != nil || plaintext == "" || !utf8.ValidString(plaintext) || strings.ContainsRune(plaintext, 0) || !validBinding(binding) {
		return supply.SecretEnvelope{}, supply.ErrDependencyUnavailable
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return supply.SecretEnvelope{}, fmt.Errorf("seal formal configuration secret: %w", err)
	}
	aad := []byte(binding.Purpose + "\x00" + binding.SupplierID + "\x00" + binding.ConfigID)
	sealed := s.aead.Seal(nonce, nonce, []byte(plaintext), aad)
	return supply.SecretEnvelope{KeyVersion: s.version, Ciphertext: sealed}, nil
}

func validBinding(binding supply.SecretBinding) bool {
	for _, value := range []string{binding.Purpose, binding.SupplierID, binding.ConfigID} {
		if value == "" || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
			return false
		}
	}
	return true
}

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

var _ supply.SecretSealer = (*AESGCMSealer)(nil)
var _ supply.FormalURLPolicy = (*FormalURLPolicy)(nil)
