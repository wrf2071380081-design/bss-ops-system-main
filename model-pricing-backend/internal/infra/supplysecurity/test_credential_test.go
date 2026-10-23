package supplysecurity

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
)

func newTestSealer(t *testing.T) *AESGCMSealer {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	sealer, err := NewAESGCMSealer("v1", key)
	if err != nil {
		t.Fatalf("NewAESGCMSealer: %v", err)
	}
	return sealer
}

func TestTestCredentialRoundTrip(t *testing.T) {
	sealer := newTestSealer(t)
	ctx := context.Background()
	envelope, err := sealer.Seal(ctx, "sk-supplier-secret", TestCredentialBinding("1001"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	stored := EncodeTestCredential(envelope)
	if !strings.Contains(stored, ":") {
		t.Fatalf("stored format should be version:base64, got %q", stored)
	}
	plain, err := DecodeTestCredential(ctx, sealer, stored, "1001")
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if plain != "sk-supplier-secret" {
		t.Fatalf("plaintext = %q", plain)
	}
}

func TestDecodeRejectsOtherSupplier(t *testing.T) {
	sealer := newTestSealer(t)
	ctx := context.Background()
	envelope, err := sealer.Seal(ctx, "sk-supplier-secret", TestCredentialBinding("1001"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	// 绑定里带供应商 ID：换一个主体必须解不开（AAD 不匹配）。
	if _, err := DecodeTestCredential(ctx, sealer, EncodeTestCredential(envelope), "1002"); err == nil {
		t.Fatal("decrypting with another supplier id must fail")
	}
}

func TestDecodeRejectsTamperedCiphertext(t *testing.T) {
	sealer := newTestSealer(t)
	ctx := context.Background()
	envelope, err := sealer.Seal(ctx, "sk-supplier-secret", TestCredentialBinding("1001"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	envelope.Ciphertext[len(envelope.Ciphertext)-1] ^= 0xFF
	if _, err := DecodeTestCredential(ctx, sealer, EncodeTestCredential(envelope), "1001"); err == nil {
		t.Fatal("tampered ciphertext must fail")
	}
}

func TestDecodeRejectsVersionMismatch(t *testing.T) {
	sealer := newTestSealer(t)
	ctx := context.Background()
	envelope, err := sealer.Seal(ctx, "sk-supplier-secret", TestCredentialBinding("1001"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	// 换版本号：只解当前活动密钥，不做静默降级。
	stale := "v0:" + base64.StdEncoding.EncodeToString(envelope.Ciphertext)
	if _, err := DecodeTestCredential(ctx, sealer, stale, "1001"); err == nil {
		t.Fatal("version mismatch must fail")
	}
}

func TestDecodeRejectsMalformedInput(t *testing.T) {
	sealer := newTestSealer(t)
	ctx := context.Background()
	for _, stored := range []string{"", "no-colon", "v1:not-base64!!"} {
		if _, err := DecodeTestCredential(ctx, sealer, stored, "1001"); err == nil {
			t.Fatalf("stored %q should be rejected", stored)
		}
	}
	if _, err := DecodeTestCredential(ctx, nil, "v1:AAAA", "1001"); err == nil {
		t.Fatal("nil sealer must be rejected")
	}
}
