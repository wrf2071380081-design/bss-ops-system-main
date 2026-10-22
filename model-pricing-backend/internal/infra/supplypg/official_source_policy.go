package supplypg

import (
	"context"
	"model_bss/internal/domain/supply"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

type OfficialSourcePolicy struct{ origins map[string]map[string]bool }

var _ supply.OfficialSourcePolicy = (*OfficialSourcePolicy)(nil)

func officialOrigin(raw string) (string, *url.URL, bool) {
	if strings.IndexFunc(raw, unicode.IsControl) >= 0 || strings.TrimSpace(raw) != raw {
		return "", nil, false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Opaque != "" || parsed.User != nil || parsed.Host == "" {
		return "", nil, false
	}
	host := strings.ToLower(parsed.Hostname())
	if net.ParseIP(host) != nil || !strings.Contains(host, ".") || strings.HasSuffix(host, ".") {
		return "", nil, false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", nil, false
		}
		for _, char := range label {
			if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-') {
				return "", nil, false
			}
		}
	}
	port := parsed.Port()
	if port == "" {
		port = "443"
	}
	numeric, err := strconv.Atoi(port)
	if err != nil || numeric < 1 || numeric > 65535 {
		return "", nil, false
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return "", nil, false
	}
	for key := range query {
		switch strings.ToLower(key) {
		case "key", "api_key", "apikey", "token", "access_token", "password", "secret", "authorization", "signature", "sig", "x-amz-signature", "credential":
			return "", nil, false
		}
	}
	return "https://" + host + ":" + strconv.Itoa(numeric), parsed, true
}
func NewOfficialSourcePolicy(origins map[string][]string) (*OfficialSourcePolicy, error) {
	if len(origins) == 0 {
		return nil, mappingError("official source configuration")
	}
	policy := &OfficialSourcePolicy{origins: map[string]map[string]bool{}}
	for company, list := range origins {
		if company == "" || strings.TrimSpace(company) != company || len(list) == 0 {
			return nil, mappingError("official source configuration")
		}
		allowed := map[string]bool{}
		for _, raw := range list {
			origin, parsed, ok := officialOrigin(raw)
			if !ok || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
				return nil, mappingError("official source configuration")
			}
			allowed[origin] = true
		}
		policy.origins[company] = allowed
	}
	return policy, nil
}
func (p *OfficialSourcePolicy) Validate(ctx context.Context, company string, sources []supply.Source) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil || p.origins == nil {
		return supply.ErrDependencyUnavailable
	}
	allowed := p.origins[company]
	if len(allowed) == 0 || len(sources) == 0 {
		return supply.ErrSourceRejected
	}
	for _, source := range sources {
		origin, _, ok := officialOrigin(source.URL)
		if !ok || !allowed[origin] {
			return supply.ErrSourceRejected
		}
	}
	return nil
}
