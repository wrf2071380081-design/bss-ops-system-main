package supplypg

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"model_bss/internal/domain/supply"
	"strings"
	"testing"
)

func TestFormalConfigStoreRejectsUnsafeSchemaAndMissingDependencies(t *testing.T) {
	if s, err := NewFormalConfigStore(`public;SELECT 1`, nil); s != nil || err == nil {
		t.Fatal("unsafe schema accepted")
	}
	s, err := NewFormalConfigStore("public", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := s.Current(ctx, nil, "21", "1 OR TRUE"); !errors.Is(err, supply.ErrReadContract) {
		t.Fatal("invalid identity", err)
	}
	if _, err := s.Current(ctx, nil, "21", "1"); !errors.Is(err, supply.ErrDependencyUnavailable) {
		t.Fatal("missing transaction", err)
	}
	calls := 0
	work := NewFormalConfigWork(s, func(context.Context) (*sql.Tx, error) { calls++; return nil, nil })
	if err := work.Within(ctx, func(supply.FormalConfigTransaction) error { return nil }); !errors.Is(err, supply.ErrDependencyUnavailable) || calls != 0 {
		t.Fatal("missing log dependency reached SQL", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.Current(canceled, nil, "21", "1"); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation hidden", err)
	}
}

func TestFormalCredentialWireKeepsNativeCapacityAndOpaqueBytes(t *testing.T) {
	secret := supply.SecretEnvelope{KeyVersion: "v1", Ciphertext: bytes.Repeat([]byte{0xfa}, 378)}
	wire, err := encodeFormalSecret(secret)
	if err != nil || len(wire) != 512 {
		t.Fatalf("native capacity boundary %d %v", len(wire), err)
	}
	decoded, err := decodeFormalSecret(wire)
	if err != nil || decoded.KeyVersion != secret.KeyVersion || !bytes.Equal(decoded.Ciphertext, secret.Ciphertext) {
		t.Fatal("opaque credential round trip", err)
	}
	decoded.Ciphertext[0] = 0
	if secret.Ciphertext[0] != 0xfa {
		t.Fatal("decoded envelope aliases original")
	}
	secret.Ciphertext = append(secret.Ciphertext, 0xfa)
	if got, err := encodeFormalSecret(secret); err == nil || got != "" {
		t.Fatal("credential was truncated to fit")
	}
	for _, bad := range []string{"legacy-secret", wire + "=", "se1..AA", "se1.djE.", "se1.djE.%%%", strings.Replace(wire, "se1.", "se2.", 1)} {
		if _, err := decodeFormalSecret(bad); !errors.Is(err, supply.ErrReadContract) {
			t.Fatal("unknown or malformed envelope accepted")
		}
	}
}
