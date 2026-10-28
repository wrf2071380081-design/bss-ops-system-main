package supply

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"unicode/utf8"
)

type SupplierTestConfigView struct {
	SupplierID string `json:"supplier_id"`
	Configured bool   `json:"configured"`
	BaseURL    string `json:"base_url"`
	HasAPIKey  bool   `json:"has_api_key"`
	Reference  string `json:"reference,omitempty"`
	Revision   uint64 `json:"revision,string"`
}

type SupplierTestConfigCommand struct {
	BaseURL           string
	APIKey            string
	ExpectedRevision  uint64
}

// SupplierTestConfigProvider is the A-side adapter port to Inspector, the
// single persistent owner of supplier test credentials. Implementations must
// authorize the authenticated actor against the supplier buyer assignment,
// enforce revision CAS, preserve a stored key when APIKey is empty, and never
// include key material in errors or logs.
type SupplierTestConfigProvider interface {
	GetSupplierTestConfig(context.Context, string, string) (*SupplierTestConfigView, error)
	SaveSupplierTestConfig(context.Context, WriteMeta, string, SupplierTestConfigCommand) (*SupplierTestConfigView, error)
}

type SupplierTestConfigService struct{ provider SupplierTestConfigProvider }

func NewSupplierTestConfigService(provider SupplierTestConfigProvider) *SupplierTestConfigService {
	return &SupplierTestConfigService{provider: provider}
}

func (s *SupplierTestConfigService) Get(ctx context.Context, actorID, supplierID string) (*SupplierTestConfigView, error) {
	if s == nil || s.provider == nil {
		return nil, ErrDependencyUnavailable
	}
	if !validID(actorID) || !validID(supplierID) {
		return nil, invalid("supplier_id", "expected positive bigint string IDs")
	}
	view, err := s.provider.GetSupplierTestConfig(ctx, actorID, supplierID)
	if err != nil {
		return nil, mapSupplierTestConfigError(err)
	}
	if err := validateSupplierTestConfigView(view, supplierID); err != nil {
		return nil, err
	}
	copy := *view
	return &copy, nil
}

func (s *SupplierTestConfigService) Save(ctx context.Context, meta WriteMeta, supplierID string, input SupplierTestConfigCommand) (*SupplierTestConfigView, error) {
	if s == nil || s.provider == nil {
		return nil, ErrDependencyUnavailable
	}
	if !validID(meta.ActorID) || !validID(supplierID) || validateWriteMetaStorage(meta) != nil {
		return nil, invalid("request", "authenticated actor, supplier, request ID and reason are required")
	}
	baseURL := strings.TrimSpace(input.BaseURL)
	parsed, err := url.ParseRequestURI(baseURL)
	if err != nil || parsed == nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" || hasCredentialURLQuery(parsed) {
		return nil, invalid("base_url", "expected an HTTP(S) URL without credentials or fragment")
	}
	if !utf8.ValidString(input.APIKey) || strings.ContainsAny(input.APIKey, "\r\n\x00") || len(input.APIKey) > 4096 {
		return nil, invalid("api_key", "key must be valid text without control delimiters")
	}
	if input.ExpectedRevision > 2147483647 {
		return nil, invalid("expected_revision", "revision exceeds supported range")
	}
	if input.ExpectedRevision == 0 && input.APIKey == "" {
		return nil, invalid("api_key", "first configuration requires a key")
	}
	input.BaseURL = baseURL
	view, err := s.provider.SaveSupplierTestConfig(ctx, meta, supplierID, input)
	if err != nil {
		return nil, mapSupplierTestConfigError(err)
	}
	if err := validateSupplierTestConfigView(view, supplierID); err != nil || view == nil || !view.Configured || !view.HasAPIKey || view.Revision <= input.ExpectedRevision {
		return nil, ErrReadContract
	}
	copy := *view
	return &copy, nil
}

func validateSupplierTestConfigView(view *SupplierTestConfigView, supplierID string) error {
	if view == nil || view.SupplierID != supplierID || view.Revision > 2147483647 {
		return ErrReadContract
	}
	if !view.Configured {
		if view.Revision != 0 || view.BaseURL != "" || view.HasAPIKey || view.Reference != "" {
			return ErrReadContract
		}
		return nil
	}
	parsed, err := url.ParseRequestURI(view.BaseURL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" || hasCredentialURLQuery(parsed) || !view.HasAPIKey || view.Revision == 0 || len(view.Reference) > 256 {
		return ErrReadContract
	}
	return nil
}

func hasCredentialURLQuery(parsed *url.URL) bool {
	if parsed == nil || parsed.RawQuery == "" {
		return false
	}
	values, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return true
	}
	separator := strings.NewReplacer("-", "", "_", "", ".", "")
	for name := range values {
		normalized := strings.ToLower(separator.Replace(name))
		if normalized == "key" || normalized == "auth" || strings.Contains(normalized, "apikey") || strings.Contains(normalized, "accesskey") || strings.Contains(normalized, "token") || strings.Contains(normalized, "secret") || strings.Contains(normalized, "password") || strings.Contains(normalized, "authorization") || strings.Contains(normalized, "credential") || strings.Contains(normalized, "signature") {
			return true
		}
	}
	return false
}

func mapSupplierTestConfigError(err error) error {
	if errors.Is(err, ErrForbidden) || errors.Is(err, ErrNotFound) || errors.Is(err, ErrRevisionConflict) || errors.Is(err, ErrReadContract) || errors.Is(err, ErrDependencyUnavailable) {
		return err
	}
	// Provider errors may contain request fragments; do not expose them to HTTP.
	return ErrDependencyUnavailable
}
