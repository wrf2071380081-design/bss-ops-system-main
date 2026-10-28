package supply

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type supplierTestConfigProviderFake struct {
	view    *SupplierTestConfigView
	command SupplierTestConfigCommand
	meta    WriteMeta
	err     error
	calls   int
}

func (f *supplierTestConfigProviderFake) GetSupplierTestConfig(_ context.Context, _, supplierID string) (*SupplierTestConfigView, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if f.view == nil {
		return &SupplierTestConfigView{SupplierID: supplierID}, nil
	}
	copy := *f.view
	return &copy, nil
}

func (f *supplierTestConfigProviderFake) SaveSupplierTestConfig(_ context.Context, meta WriteMeta, supplierID string, command SupplierTestConfigCommand) (*SupplierTestConfigView, error) {
	f.calls++
	f.meta, f.command = meta, command
	if f.err != nil {
		return nil, f.err
	}
	return &SupplierTestConfigView{SupplierID: supplierID, Configured: true, BaseURL: command.BaseURL, HasAPIKey: true, Reference: "inspector-config-1", Revision: command.ExpectedRevision + 1}, nil
}

func TestSupplierTestConfigSaveKeepsKeyOutOfViewAndPassesBlankForPreservation(t *testing.T) {
	provider := &supplierTestConfigProviderFake{}
	service := NewSupplierTestConfigService(provider)
	view, err := service.Save(context.Background(), WriteMeta{ActorID: "7", RequestID: "req-1", Reason: "维护配置"}, "10", SupplierTestConfigCommand{
		BaseURL: " https://vendor.example/v1 ", APIKey: "", ExpectedRevision: 3,
	})
	if err != nil || view == nil || !view.HasAPIKey || view.Revision != 4 || provider.command.APIKey != "" || provider.command.BaseURL != "https://vendor.example/v1" {
		t.Fatalf("save did not preserve key via provider and return safe state: view=%+v command=%+v err=%v", view, provider.command, err)
	}
	encoded, err := json.Marshal(view)
	var fields map[string]any
	if err == nil {
		err = json.Unmarshal(encoded, &fields)
	}
	_, exposesRawKey := fields["api_key"]
	_, exposesEncryptedKey := fields["api_key_enc"]
	if err != nil || exposesRawKey || exposesEncryptedKey || strings.Contains(strings.ToLower(string(encoded)), "secret") {
		t.Fatalf("safe view exposed key fields: json=%s err=%v", encoded, err)
	}
}

func TestSupplierTestConfigRejectsFirstSaveWithoutKeyAndUnsafeURL(t *testing.T) {
	provider := &supplierTestConfigProviderFake{}
	service := NewSupplierTestConfigService(provider)
	meta := WriteMeta{ActorID: "7", RequestID: "req-1", Reason: "配置"}
	for _, command := range []SupplierTestConfigCommand{
		{BaseURL: "https://vendor.example/v1", ExpectedRevision: 0},
		{BaseURL: "file:///etc/passwd", APIKey: "key", ExpectedRevision: 0},
		{BaseURL: "https://user:password@vendor.example/v1", APIKey: "key", ExpectedRevision: 0},
		{BaseURL: "https://vendor.example/v1?api_key=secret", APIKey: "key", ExpectedRevision: 0},
		{BaseURL: "https://vendor.example/v1?X-Access-Token=secret", APIKey: "key", ExpectedRevision: 0},
	} {
		if _, err := service.Save(context.Background(), meta, "10", command); err == nil {
			t.Fatalf("invalid config command was accepted: %+v", command)
		}
	}
	if provider.calls != 0 {
		t.Fatalf("invalid config reached Inspector provider %d times", provider.calls)
	}
}

func TestSupplierTestConfigRejectsProviderViewWithCredentialQuery(t *testing.T) {
	provider := &supplierTestConfigProviderFake{view: &SupplierTestConfigView{
		SupplierID: "10", Configured: true, BaseURL: "https://vendor.example/v1?token=secret-value",
		HasAPIKey: true, Reference: "inspector-config-1", Revision: 1,
	}}
	service := NewSupplierTestConfigService(provider)
	_, err := service.Get(context.Background(), "7", "10")
	if !errors.Is(err, ErrReadContract) || strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("provider credential-bearing URL was not rejected safely: %v", err)
	}
}

func TestSupplierTestConfigSuppressesProviderErrorsThatCouldContainKey(t *testing.T) {
	provider := &supplierTestConfigProviderFake{err: errors.New("Inspector rejected key=secret-value")}
	service := NewSupplierTestConfigService(provider)
	_, err := service.Save(context.Background(), WriteMeta{ActorID: "7", RequestID: "req-1", Reason: "配置"}, "10", SupplierTestConfigCommand{
		BaseURL: "https://vendor.example/v1", APIKey: "actual-secret", ExpectedRevision: 1,
	})
	if !errors.Is(err, ErrDependencyUnavailable) || strings.Contains(err.Error(), "secret-value") || strings.Contains(err.Error(), "actual-secret") {
		t.Fatalf("provider error leaked credential data: %v", err)
	}
}
