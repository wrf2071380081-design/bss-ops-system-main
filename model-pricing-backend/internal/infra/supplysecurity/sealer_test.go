package supplysecurity

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"net"
	"testing"

	"model_bss/internal/domain/supply"
)

func TestAESGCMSealerAuthenticatesFormalConfigBinding(t *testing.T) {
	key := []byte("01234567890123456789012345678901")
	sealer, err := NewAESGCMSealer("key-v7", key)
	if err != nil {
		t.Fatal(err)
	}
	binding := supply.SecretBinding{Purpose: "SUPPLIER_FORMAL_API_KEY", SupplierID: "21", ConfigID: "42"}
	envelope, err := sealer.Seal(context.Background(), "sensitive-api-key", binding)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := aes.NewCipher(key)
	aead, _ := cipher.NewGCM(block)
	if envelope.KeyVersion != "key-v7" || len(envelope.Ciphertext) <= aead.NonceSize() {
		t.Fatalf("invalid sealed envelope: version=%q ciphertext bytes=%d", envelope.KeyVersion, len(envelope.Ciphertext))
	}
	plain, err := aead.Open(nil, envelope.Ciphertext[:aead.NonceSize()], envelope.Ciphertext[aead.NonceSize():], []byte("SUPPLIER_FORMAL_API_KEY\x0021\x0042"))
	if err != nil || string(plain) != "sensitive-api-key" {
		t.Fatalf("cannot open envelope with its binding: %q, %v", plain, err)
	}
	wrongBinding := []byte("SUPPLIER_FORMAL_API_KEY\x0022\x0042")
	if _, err := aead.Open(nil, envelope.Ciphertext[:aead.NonceSize()], envelope.Ciphertext[aead.NonceSize():], wrongBinding); err == nil {
		t.Fatal("envelope opened after supplier identity changed")
	}
}

func TestAESGCMSealerRejectsInvalidKey(t *testing.T) {
	if _, err := NewAESGCMSealer("key-v1", []byte("short")); err == nil {
		t.Fatal("expected a non-32-byte AES key to be rejected")
	}
}

func TestAESGCMSealerFromEnvRequiresVersionedBase64Key(t *testing.T) {
	t.Setenv("MODEL_BSS_FORMAL_CONFIG_KEY_VERSION", "key-v7")
	t.Setenv("MODEL_BSS_FORMAL_CONFIG_KEY", base64.StdEncoding.EncodeToString([]byte("01234567890123456789012345678901")))
	sealer, err := AESGCMSealerFromEnv()
	if err != nil || sealer == nil {
		t.Fatalf("expected environment sealer, got %v", err)
	}
	t.Setenv("MODEL_BSS_FORMAL_CONFIG_KEY", "not-base64")
	if _, err := AESGCMSealerFromEnv(); err == nil {
		t.Fatal("expected malformed environment key to be rejected")
	}
}

type fixedResolver map[string][]net.IPAddr

func (r fixedResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	return r[host], nil
}

func TestFormalURLPolicyRequiresHTTPSAndPublicResolvedAddresses(t *testing.T) {
	policy := NewFormalURLPolicy(fixedResolver{
		"api.vendor.example": {{IP: net.ParseIP("8.8.8.8")}},
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
