package supplysecurity

import (
	"context"
	"net"
	"testing"
)

type fixedResolver map[string][]net.IPAddr

func (r fixedResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	return r[host], nil
}

func TestFormalURLPolicyRequiresHTTPSAndPublicResolvedAddresses(t *testing.T) {
	policy := NewFormalURLPolicy(fixedResolver{
		"api.vendor.example":     {{IP: net.ParseIP("8.8.8.8")}},
		"private.vendor.example": {{IP: net.ParseIP("10.0.0.8")}},
	})
	for _, value := range []string{"http://api.vendor.example/v1", "https://127.0.0.1/v1", "https://private.vendor.example/v1", "https://user:pass@api.vendor.example/v1", "https://api.vendor.example/?key=secret"} {
		if err := policy.ValidateFormalURL(context.Background(), value); err == nil {
			t.Errorf("expected URL policy to reject %q", value)
		}
	}
	if err := policy.ValidateFormalURL(context.Background(), "https://api.vendor.example/v1"); err != nil {
		t.Fatalf("expected public HTTPS URL to pass: %v", err)
	}
}
