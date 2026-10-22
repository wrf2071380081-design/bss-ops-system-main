package supplypg

import (
	"context"
	"errors"
	"model_bss/internal/domain/supply"
	"strings"
	"testing"
)

func TestOfficialSourcePolicyExactOriginsAndImmutableConfiguration(t *testing.T) {
	config := map[string][]string{"Example": {"https://Vendor.Example", "https://docs.vendor.example:8443"}}
	policy, err := NewOfficialSourcePolicy(config)
	if err != nil {
		t.Fatal(err)
	}
	config["Example"][0] = "https://evil.example"
	delete(config, "Example")
	for _, url := range []string{"https://vendor.example/pricing", "https://VENDOR.EXAMPLE:443/docs#section", "https://docs.vendor.example:8443/api"} {
		if err := policy.Validate(context.Background(), "Example", []supply.Source{{URL: url}}); err != nil {
			t.Fatalf("allowed origin rejected %v", err)
		}
	}
	for _, url := range []string{"https://vendor.example.evil.example/pricing", "https://sub.vendor.example/pricing", "https://evil.example/?next=https://vendor.example", "http://vendor.example/pricing", "https://vendor.example:8443/pricing", "https://user:secret@vendor.example/pricing", "https://vendor.example/pricing?api_key=secret", "https://127.0.0.1/pricing", "https://vendor.example/pricing?token=secret"} {
		err := policy.Validate(context.Background(), "Example", []supply.Source{{URL: url}})
		if !errors.Is(err, supply.ErrSourceRejected) || strings.Contains(err.Error(), "secret") {
			t.Fatalf("unexpected source result %v", err)
		}
	}
	if err := policy.Validate(context.Background(), "Other", []supply.Source{{URL: "https://vendor.example"}}); !errors.Is(err, supply.ErrSourceRejected) {
		t.Fatal("unknown company")
	}
	if err := policy.Validate(context.Background(), "Example", nil); !errors.Is(err, supply.ErrSourceRejected) {
		t.Fatal("empty sources")
	}
	if err := policy.Validate(context.Background(), "Example", []supply.Source{{URL: "https://vendor.example"}, {URL: "https://evil.example"}}); !errors.Is(err, supply.ErrSourceRejected) {
		t.Fatal("only first source checked")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := policy.Validate(ctx, "Example", nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation hidden")
	}
}

func TestOfficialSourcePolicyRejectsMalformedConfiguration(t *testing.T) {
	for _, origins := range []map[string][]string{nil, {}, {"": {"https://vendor.example"}}, {"Example": {}}, {"Example": {"https://vendor.example/path"}}, {"Example": {"https://vendor.example?key=x"}}, {"Example": {"https://*.vendor.example"}}, {"Example": {"https://127.0.0.1"}}, {"Example": {"http://vendor.example"}}, {"Example": {"https://vendor.example#docs"}}} {
		if policy, err := NewOfficialSourcePolicy(origins); err == nil || policy != nil {
			t.Fatalf("malformed config accepted %+v", origins)
		}
	}
}
