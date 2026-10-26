package supply

import (
	"context"
	"errors"
	"time"
)

var (
	ErrConfigInUse    = errors.New("formal configuration is referenced by resources")
	ErrConfigInactive = errors.New("formal configuration is inactive")
)

// FormalURLPolicy is a local environment policy, not an HTTP connectivity test.
type FormalURLPolicy interface {
	ValidateFormalURL(context.Context, string) error
}

type FormalConfigInput struct {
	Scope             string  `json:"scope"`
	BaseURL           string  `json:"base_url"`
	APIKey            *string `json:"-"`
	ExternalConfirmed bool    `json:"external_confirmed"`
}

// FormalConfig is a persistence aggregate. Transport and audit use View, never
// the aggregate. APIKey is stored as entered for internal use and must never be
// included in a response or audit log.
type FormalConfig struct {
	ID                string
	SupplierID        string
	Region            string
	Scope             string
	BaseURL           string
	APIKey            string `json:"-"`
	LegacyEncrypted   bool   `json:"-"`
	Status            string
	Revision          uint64
	CreatedBy         string
	UpdatedBy         string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	ConfirmedBy       string
	ConfirmedAt       time.Time
	ConfirmedRevision uint64
}

func (c *FormalConfig) Clone() *FormalConfig {
	if c == nil {
		return nil
	}
	copy := *c
	return &copy
}

type FormalConfigView struct {
	ID                string    `json:"id"`
	SupplierID        string    `json:"supplier_id"`
	Region            string    `json:"region"`
	Scope             string    `json:"scope"`
	BaseURL           string    `json:"base_url"`
	HasAPIKey         bool      `json:"has_api_key"`
	APIKeyMask        string    `json:"api_key_mask"`
	Status            string    `json:"status"`
	Revision          uint64    `json:"revision,string"`
	CreatedBy         string    `json:"created_by"`
	UpdatedBy         string    `json:"updated_by"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	ConfirmedBy       string    `json:"confirmed_by"`
	ConfirmedAt       time.Time `json:"confirmed_at"`
	ConfirmedRevision uint64    `json:"confirmed_revision,string"`
}

func (c *FormalConfig) View() *FormalConfigView {
	if c == nil {
		return nil
	}
	hasKey := c.APIKey != "" && !c.LegacyEncrypted
	mask := ""
	if hasKey {
		mask = "********"
	}
	return &FormalConfigView{ID: c.ID, SupplierID: c.SupplierID, Region: c.Region, Scope: c.Scope, BaseURL: c.BaseURL,
		HasAPIKey: hasKey, APIKeyMask: mask, Status: c.Status, Revision: c.Revision,
		CreatedBy: c.CreatedBy, UpdatedBy: c.UpdatedBy, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
		ConfirmedBy: c.ConfirmedBy, ConfirmedAt: c.ConfirmedAt, ConfirmedRevision: c.ConfirmedRevision}
}

type FormalConfigSupplier struct{ ID, BuyerID, Region, Status string }
type FormalConfigOperation struct {
	EntityType string
	EntityID   string
	SupplierID string
	Action     string
	ActorID    string
	Reason     string
	RequestID  string
	Before     *FormalConfigView
	After      *FormalConfigView
	OccurredAt time.Time
}
type FormalConfigUnitOfWork interface {
	// Join an existing transaction when present; do not independently commit
	// inside the shared idempotency or B onboarding transaction.
	Within(context.Context, func(FormalConfigTransaction) error) error
}
type FormalConfigTransaction interface {
	// Binding, resource linking and disabling must obey the same lock order.
	SupplierForUpdate(context.Context, string) (FormalConfigSupplier, error)
	NextID(context.Context) (string, error)
	ConfigForUpdate(context.Context, string, string) (*FormalConfig, error)
	CreateConfig(context.Context, *FormalConfig) error
	UpdateConfig(context.Context, *FormalConfig, uint64) error
	HasResourceReferences(context.Context, string) (bool, error)
	AppendConfigOperation(context.Context, FormalConfigOperation) error
}
