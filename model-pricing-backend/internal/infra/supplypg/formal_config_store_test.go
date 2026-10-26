package supplypg

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
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

func TestFormalAPIKeyUsesPlaintextAndHonorsNativeCapacity(t *testing.T) {
	key := strings.Repeat("k", 512)
	if err := validateFormalAPIKey(key); err != nil {
		t.Fatalf("native varchar capacity rejected: %v", err)
	}
	if err := validateFormalAPIKey(key + "k"); !errors.Is(err, supply.ErrReadContract) {
		t.Fatal("over-capacity key accepted")
	}
	for _, bad := range []string{"", "  ", "bad\x00key", string([]byte{0xff})} {
		if err := validateFormalAPIKey(bad); !errors.Is(err, supply.ErrReadContract) {
			t.Fatalf("invalid key accepted: %q", bad)
		}
	}
}

func TestFormalStoreDetectsLegacySealedKeyWithoutTreatingItAsPlaintext(t *testing.T) {
	legacy := "se1." + base64.RawURLEncoding.EncodeToString([]byte("old-key-v1")) + "." + base64.RawURLEncoding.EncodeToString(make([]byte, 28))
	if !isLegacySealedFormalAPIKey(legacy) {
		t.Fatal("valid legacy envelope was not recognized")
	}
	if isLegacySealedFormalAPIKey("se1.not-an-envelope") || isLegacySealedFormalAPIKey("se1."+base64.RawURLEncoding.EncodeToString([]byte("v1"))+"."+base64.RawURLEncoding.EncodeToString([]byte("too-short"))) {
		t.Fatal("plain or malformed key was incorrectly classified as legacy encryption")
	}
}

func TestFormalConfigLogWritesKeyFingerprintAndReadsLegacyField(t *testing.T) {
	key := "provider-secret-for-fingerprint-test"
	entry := formalConfigEnvelope{Contract: "supply.formal_config", SchemaVersion: 1, Operation: "CREATE", APIKeyFingerprint: formalFingerprint(key)}
	encoded, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), key) || !strings.Contains(string(encoded), `"api_key_fingerprint"`) || strings.Contains(string(encoded), `"sealed_fingerprint"`) {
		t.Fatalf("new audit snapshot has an unsafe or stale fingerprint field: %s", encoded)
	}
	var legacy formalConfigEnvelope
	if err := json.Unmarshal([]byte(`{"contract":"supply.formal_config","schema_version":1,"operation":"CREATE","sealed_fingerprint":"`+formalFingerprint(key)+`"}`), &legacy); err != nil || legacy.keyFingerprint() != formalFingerprint(key) {
		t.Fatalf("legacy audit snapshot can no longer be read: %v", err)
	}
	entry.LegacySealedFingerprint = entry.APIKeyFingerprint
	if entry.keyFingerprint() != "" {
		t.Fatal("ambiguous fingerprint field combination was accepted")
	}
}
