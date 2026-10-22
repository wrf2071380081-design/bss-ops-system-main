package supply

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

var ErrSupplierRegionLocked = errors.New("supplier region is locked by historical business use")

// SupplierData contains only ordinary profile fields. Assignment, onboarding,
// website credentials, credit and contract records have separate write paths.
type SupplierData struct {
	Name               string `json:"name"`
	Region             string `json:"region"`
	RegistrationNo     string `json:"registration_no"`
	Contact            string `json:"contact"`
	Phone              string `json:"phone"`
	TaxRate            string `json:"tax_rate"`
	SettlementCurrency string `json:"settlement_currency"`
}
type Supplier struct {
	ID          string       `json:"id"`
	BuyerID     string       `json:"buyer_id"`
	Data        SupplierData `json:"data"`
	Status      string       `json:"status"`
	Established bool         `json:"established"`
	Revision    uint64       `json:"revision,string"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

func (s *Supplier) Clone() *Supplier {
	if s == nil {
		return nil
	}
	copy := *s
	return &copy
}
func canonicalSupplierData(data SupplierData) (SupplierData, error) {
	data.Name = strings.TrimSpace(data.Name)
	data.RegistrationNo = strings.TrimSpace(data.RegistrationNo)
	data.Contact = strings.TrimSpace(data.Contact)
	data.Phone = strings.TrimSpace(data.Phone)
	if data.Name == "" {
		return data, invalid("name", "supplier name is required")
	}
	if data.Region != "CN" && data.Region != "OVERSEAS" {
		return data, invalid("region", "expected CN or OVERSEAS")
	}
	if !currencyCode.MatchString(data.SettlementCurrency) {
		return data, invalid("settlement_currency", "expected explicit three-letter uppercase currency")
	}
	rate, err := monetaryDecimal(data.TaxRate, "tax_rate", false)
	if err != nil {
		return data, err
	}
	if rate.GreaterThan(decimal.NewFromInt(1)) {
		return data, invalid("tax_rate", "expected ratio between zero and one")
	}
	data.TaxRate = rate.StringFixed(8)
	return data, nil
}
func NewSupplier(id, buyerID string, data SupplierData, now time.Time) (*Supplier, error) {
	if !validID(id) {
		return nil, invalid("id", "expected positive bigint string")
	}
	if !validID(buyerID) {
		return nil, invalid("buyer_id", "expected valid staff bigint string")
	}
	canonical, err := canonicalSupplierData(data)
	if err != nil {
		return nil, err
	}
	return &Supplier{ID: id, BuyerID: buyerID, Data: canonical, Status: "ACTIVE", Revision: 1, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}, nil
}

// Usage includes historical relations, even withdrawn submissions, retired
// resources or archived contracts. All relation writers must share the supplier lock.
type SupplierUsage struct{ HasSubmissions, HasResources, HasContracts bool }

func (u SupplierUsage) LocksRegion() bool {
	return u.HasSubmissions || u.HasResources || u.HasContracts
}

type SupplierOperation struct {
	EntityType string
	EntityID   string
	Action     string
	ActorID    string
	Reason     string
	RequestID  string
	Before     *Supplier
	After      *Supplier
	OccurredAt time.Time
}
type SupplierUnitOfWork interface {
	// Join an existing transaction before opening a new top-level transaction.
	Within(context.Context, func(SupplierTransaction) error) error
}
type SupplierListQuery struct {
	Page, Size int
	Keyword    string
	Region     string
}
type SupplierPage struct {
	List  []Supplier `json:"list"`
	Total int64      `json:"total"`
	Page  int        `json:"page"`
	Size  int        `json:"size"`
}
type SupplierReadStore interface {
	Get(context.Context, string) (*Supplier, error)
	List(context.Context, SupplierListQuery) (*SupplierPage, error)
}
type SupplierTransaction interface {
	NextID(context.Context) (string, error)
	SupplierForUpdate(context.Context, string) (*Supplier, error)
	CreateSupplier(context.Context, *Supplier) error
	CreateSupplierAssignment(context.Context, string, string, string, time.Time) error
	UpdateSupplier(context.Context, *Supplier, uint64) error
	SupplierUsage(context.Context, string) (SupplierUsage, error)
	CurrencyEnabled(context.Context, string) (bool, error)
	StaffActive(context.Context, string) (bool, error)
	AppendSupplierOperation(context.Context, SupplierOperation) error
}
