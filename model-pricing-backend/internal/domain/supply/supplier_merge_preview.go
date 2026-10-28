package supply

import (
	"context"
	"math"
	"strings"
	"time"
)

type SupplierMergePartySummary struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	Region             string    `json:"region"`
	BuyerID            string    `json:"buyer_id"`
	SettlementCurrency string    `json:"settlement_currency"`
	Revision           uint64    `json:"revision,string"`
	Status             string    `json:"status"`
	Established        bool      `json:"established"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// Counts deliberately expose only whether references exist, never secrets,
// file paths, or raw credential values.
type SupplierMergeReferenceCounts struct {
	Resources                int64  `json:"resources"`
	Submissions              int64  `json:"submissions"`
	PendingSubmissions       int64  `json:"pending_submissions"`
	OnboardingFlows          int64  `json:"onboarding_flows"`
	Contracts                int64  `json:"contracts"`
	CreditLines              int64  `json:"credit_lines"`
	FormalConfigs            int64  `json:"formal_configs"`
	Qualifications           int64  `json:"qualifications"`
	CustomerRelations        int64  `json:"customer_relations"`
	TestCredentialRecords    int64  `json:"test_credential_records"`
	WebsiteAccountConfigured bool   `json:"website_account_configured"`
	Aliases                  *int64 `json:"aliases"`
}

type SupplierMergeFacts struct {
	Source                SupplierMergePartySummary
	Target                SupplierMergePartySummary
	SourceReferences      SupplierMergeReferenceCounts
	TargetReferences      SupplierMergeReferenceCounts
	DuplicateResourceKeys int64
}

type SupplierMergeReviewItem struct {
	Code        string `json:"code"`
	Title       string `json:"title"`
	SourceCount int64  `json:"source_count"`
	TargetCount int64  `json:"target_count"`
}

// SupplierMergePreview has no can_merge flag or mutation command. Conflict
// policy is still a business decision and this read-only result cannot grant it.
type SupplierMergePreview struct {
	Source                SupplierMergePartySummary    `json:"source"`
	Target                SupplierMergePartySummary    `json:"target"`
	SourceReferences      SupplierMergeReferenceCounts `json:"source_references"`
	TargetReferences      SupplierMergeReferenceCounts `json:"target_references"`
	DuplicateResourceKeys int64                        `json:"duplicate_resource_keys"`
	ReviewItems           []SupplierMergeReviewItem    `json:"review_items"`
}

type SupplierMergePreviewStore interface {
	LoadSupplierMergeFacts(context.Context, string, string) (*SupplierMergeFacts, error)
}

type SupplierMergePreviewReader struct{ store SupplierMergePreviewStore }

func NewSupplierMergePreviewReader(store SupplierMergePreviewStore) *SupplierMergePreviewReader {
	return &SupplierMergePreviewReader{store: store}
}

func validSupplierMergeParty(p SupplierMergePartySummary, expectedID string) bool {
	return p.ID == expectedID && validID(p.ID) && strings.TrimSpace(p.Name) != "" &&
		(p.Region == "CN" || p.Region == "OVERSEAS") && validID(p.BuyerID) &&
		(p.SettlementCurrency == "CNY" || p.SettlementCurrency == "USD") &&
		p.Revision > 0 && p.Revision <= math.MaxInt64 &&
		(p.Status == "ACTIVE" || p.Status == "FROZEN") && !p.CreatedAt.IsZero() && !p.UpdatedAt.Before(p.CreatedAt)
}

func validSupplierMergeReferences(r SupplierMergeReferenceCounts) bool {
	for _, count := range []int64{r.Resources, r.Submissions, r.PendingSubmissions, r.OnboardingFlows, r.Contracts, r.CreditLines, r.FormalConfigs, r.Qualifications, r.CustomerRelations, r.TestCredentialRecords} {
		if count < 0 {
			return false
		}
	}
	return r.PendingSubmissions <= r.Submissions && (r.Aliases == nil || *r.Aliases >= 0)
}

func appendMergeReview(items []SupplierMergeReviewItem, code, title string, sourceCount, targetCount int64) []SupplierMergeReviewItem {
	return append(items, SupplierMergeReviewItem{Code: code, Title: title, SourceCount: sourceCount, TargetCount: targetCount})
}

func (r *SupplierMergePreviewReader) Preview(ctx context.Context, sourceID, targetID string) (*SupplierMergePreview, error) {
	if r == nil || r.store == nil {
		return nil, ErrDependencyUnavailable
	}
	if !validID(sourceID) || !validID(targetID) || sourceID == targetID {
		return nil, invalid("target_supplier_id", "expected a different positive supplier id")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	facts, err := r.store.LoadSupplierMergeFacts(ctx, sourceID, targetID)
	if err != nil {
		return nil, err
	}
	if facts == nil || !validSupplierMergeParty(facts.Source, sourceID) || !validSupplierMergeParty(facts.Target, targetID) ||
		!validSupplierMergeReferences(facts.SourceReferences) || !validSupplierMergeReferences(facts.TargetReferences) || facts.DuplicateResourceKeys < 0 {
		return nil, ErrReadContract
	}
	result := &SupplierMergePreview{Source: facts.Source, Target: facts.Target, SourceReferences: facts.SourceReferences,
		TargetReferences: facts.TargetReferences, DuplicateResourceKeys: facts.DuplicateResourceKeys, ReviewItems: []SupplierMergeReviewItem{}}
	s, t := facts.SourceReferences, facts.TargetReferences
	if facts.Source.Region != facts.Target.Region {
		result.ReviewItems = appendMergeReview(result.ReviewItems, "REGION_MISMATCH", "主体区域不同", 1, 1)
	}
	if facts.Source.BuyerID != facts.Target.BuyerID {
		result.ReviewItems = appendMergeReview(result.ReviewItems, "DIFFERENT_PROCUREMENT_OWNERS", "归属采购人不同", 1, 1)
	}
	if facts.Source.Established || facts.Target.Established {
		result.ReviewItems = appendMergeReview(result.ReviewItems, "FIRST_ADMISSION_HISTORY", "存在首次接入历史", boolCount(facts.Source.Established), boolCount(facts.Target.Established))
	}
	if facts.Source.Status != facts.Target.Status {
		result.ReviewItems = appendMergeReview(result.ReviewItems, "SUPPLIER_STATUS_MISMATCH", "主体状态不同", 1, 1)
	}
	if facts.DuplicateResourceKeys > 0 {
		result.ReviewItems = appendMergeReview(result.ReviewItems, "DUPLICATE_RESOURCE_KEY", "相同模型与区域存在重复资源", facts.DuplicateResourceKeys, facts.DuplicateResourceKeys)
	}
	if s.PendingSubmissions+t.PendingSubmissions > 0 {
		result.ReviewItems = appendMergeReview(result.ReviewItems, "IN_PROGRESS_SUBMISSIONS", "存在待处理申报", s.PendingSubmissions, t.PendingSubmissions)
	}
	if s.OnboardingFlows+t.OnboardingFlows > 0 {
		result.ReviewItems = appendMergeReview(result.ReviewItems, "IN_PROGRESS_ONBOARDING", "存在供应商办理流程", s.OnboardingFlows, t.OnboardingFlows)
	}
	if s.FormalConfigs+t.FormalConfigs > 0 {
		result.ReviewItems = appendMergeReview(result.ReviewItems, "FORMAL_CONFIGS_PRESENT", "存在正式生产配置", s.FormalConfigs, t.FormalConfigs)
	}
	if s.TestCredentialRecords+t.TestCredentialRecords > 0 {
		result.ReviewItems = appendMergeReview(result.ReviewItems, "TEST_CREDENTIALS_PRESENT", "存在测试凭据引用", s.TestCredentialRecords, t.TestCredentialRecords)
	}
	if s.WebsiteAccountConfigured || t.WebsiteAccountConfigured {
		result.ReviewItems = appendMergeReview(result.ReviewItems, "WEBSITE_ACCOUNT_PRESENT", "存在网站账号配置", boolCount(s.WebsiteAccountConfigured), boolCount(t.WebsiteAccountConfigured))
	}
	if s.Contracts+s.CreditLines+t.Contracts+t.CreditLines > 0 {
		result.ReviewItems = appendMergeReview(result.ReviewItems, "CONTRACT_CREDIT_HISTORY", "存在合同或授信历史", s.Contracts+s.CreditLines, t.Contracts+t.CreditLines)
	}
	if s.Qualifications+t.Qualifications > 0 {
		result.ReviewItems = appendMergeReview(result.ReviewItems, "QUALIFICATION_HISTORY", "存在资质记录", s.Qualifications, t.Qualifications)
	}
	if s.CustomerRelations+t.CustomerRelations > 0 {
		result.ReviewItems = appendMergeReview(result.ReviewItems, "CUSTOMER_RELATIONS", "存在关联客户", s.CustomerRelations, t.CustomerRelations)
	}
	if s.Aliases == nil || t.Aliases == nil {
		result.ReviewItems = appendMergeReview(result.ReviewItems, "ALIAS_STRUCTURE_UNAVAILABLE", "曾用名结构尚未交付", 0, 0)
	}
	return result, nil
}

func boolCount(value bool) int64 {
	if value {
		return 1
	}
	return 0
}
