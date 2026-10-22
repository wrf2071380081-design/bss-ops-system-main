package supply

import (
	"context"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

type F12ContractQuery struct {
	SubjectType, SubjectID, Status string
	Page, Size                     int
}

type F12CreditQuery struct {
	SubjectType, SubjectID, Direction, Currency string
	AsOf                                        *time.Time
	Page, Size                                  int
}

type F12ContractRecord struct {
	ID            string     `json:"id"`
	SubjectType   string     `json:"subject_type"`
	SubjectID     string     `json:"subject_id"`
	Status        string     `json:"status"`
	Version       uint64     `json:"version"`
	HasAttachment bool       `json:"has_attachment"`
	SignedAt      *time.Time `json:"signed_at"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type F12CreditRecord struct {
	ID          string     `json:"id"`
	SubjectType string     `json:"subject_type"`
	SubjectID   string     `json:"subject_id"`
	Direction   string     `json:"direction"`
	Amount      string     `json:"amount"`
	Currency    string     `json:"currency"`
	Version     uint64     `json:"version"`
	EffectiveAt *time.Time `json:"effective_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

type F12CurrentCredit struct {
	Direction string           `json:"direction"`
	Currency  string           `json:"currency"`
	Status    string           `json:"status"`
	Record    *F12CreditRecord `json:"record,omitempty"`
}

type F12ContractPage struct {
	List  []*F12ContractRecord `json:"list"`
	Total int64                `json:"total"`
	Page  int                  `json:"page"`
	Size  int                  `json:"size"`
}

type F12CreditPage struct {
	List    []*F12CreditRecord  `json:"list"`
	Current []*F12CurrentCredit `json:"current_credit"`
	Total   int64               `json:"total"`
	Page    int                 `json:"page"`
	Size    int                 `json:"size"`
}

// F12ReadStore exposes only the compatibility fields present in native v36.
// The store must never copy contract.file_ref or contract.note into these DTOs.
type F12ReadStore interface {
	ListSupplierContracts(context.Context, F12ContractQuery) ([]*F12ContractRecord, int64, error)
	ListSupplierCredits(context.Context, F12CreditQuery) ([]*F12CreditRecord, int64, []*F12CreditRecord, error)
}

type F12Reader struct{ store F12ReadStore }

func NewF12Reader(store F12ReadStore) *F12Reader { return &F12Reader{store: store} }

func normalizeF12Subject(subjectType, subjectID string, page, size int) (int, int, error) {
	if subjectType != "SUPPLIER" {
		return 0, 0, invalid("subject_type", "v36 compatibility reads currently support SUPPLIER only")
	}
	if !validID(subjectID) {
		return 0, 0, invalid("subject_id", "expected a positive bigint string")
	}
	return readPagination(page, size)
}

func (r *F12Reader) ListContracts(ctx context.Context, q F12ContractQuery) (*F12ContractPage, error) {
	if r == nil || r.store == nil {
		return nil, ErrDependencyUnavailable
	}
	var err error
	q.Page, q.Size, err = normalizeF12Subject(q.SubjectType, q.SubjectID, q.Page, q.Size)
	if err != nil {
		return nil, err
	}
	if q.Status != "" && q.Status != "PROCESSING" && q.Status != "ARCHIVED" {
		return nil, invalid("status", "expected PROCESSING or ARCHIVED")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rows, total, err := r.store.ListSupplierContracts(ctx, q)
	if err != nil {
		return nil, err
	}
	if total < int64(len(rows)) || len(rows) > q.Size {
		return nil, ErrReadContract
	}
	seen := map[string]bool{}
	for _, row := range rows {
		if row == nil || !validID(row.ID) || row.SubjectType != q.SubjectType || row.SubjectID != q.SubjectID || (row.Status != "PROCESSING" && row.Status != "ARCHIVED") || (q.Status != "" && row.Status != q.Status) || row.Version == 0 || row.Version > math.MaxInt32 || row.CreatedAt.IsZero() || row.UpdatedAt.Before(row.CreatedAt) || seen[row.ID] {
			return nil, ErrReadContract
		}
		seen[row.ID] = true
	}
	if rows == nil {
		rows = []*F12ContractRecord{}
	}
	return &F12ContractPage{List: rows, Total: total, Page: q.Page, Size: q.Size}, nil
}

var f12AmountText = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]{1,2})?$`)

func validF12CreditRecord(row *F12CreditRecord, q F12CreditQuery) bool {
	if row == nil || !validID(row.ID) || row.SubjectType != q.SubjectType || row.SubjectID != q.SubjectID || row.Direction != "SUPPLIER_TO_US" || (q.Direction != "" && row.Direction != q.Direction) || !currencyCode.MatchString(row.Currency) || (q.Currency != "" && row.Currency != q.Currency) || row.Version == 0 || row.Version > math.MaxInt32 || row.CreatedAt.IsZero() || !f12AmountText.MatchString(row.Amount) {
		return false
	}
	parts := strings.SplitN(strings.TrimPrefix(row.Amount, "-"), ".", 2)
	if len(parts[0]) > 18 {
		return false
	}
	_, err := decimal.NewFromString(row.Amount)
	return err == nil
}

func f12CreditDimension(row *F12CreditRecord) string { return row.Direction + "\x00" + row.Currency }

func (r *F12Reader) ListCredits(ctx context.Context, q F12CreditQuery) (*F12CreditPage, error) {
	if r == nil || r.store == nil {
		return nil, ErrDependencyUnavailable
	}
	var err error
	q.Page, q.Size, err = normalizeF12Subject(q.SubjectType, q.SubjectID, q.Page, q.Size)
	if err != nil {
		return nil, err
	}
	if q.Direction != "" && q.Direction != "SUPPLIER_TO_US" {
		return nil, invalid("direction", "supplier credit direction must be SUPPLIER_TO_US")
	}
	if q.Currency != "" && !currencyCode.MatchString(q.Currency) {
		return nil, invalid("currency", "expected a three-letter uppercase currency")
	}
	if q.AsOf != nil {
		asOf := q.AsOf.UTC()
		if asOf.IsZero() {
			return nil, invalid("as_of", "expected a valid RFC3339 timestamp")
		}
		q.AsOf = &asOf
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rows, total, latest, err := r.store.ListSupplierCredits(ctx, q)
	if err != nil {
		return nil, err
	}
	if total < int64(len(rows)) || len(rows) > q.Size {
		return nil, ErrReadContract
	}
	seen := map[string]bool{}
	for _, row := range rows {
		if !validF12CreditRecord(row, q) || seen[row.ID] || (q.AsOf != nil && row.CreatedAt.After(*q.AsOf)) {
			return nil, ErrReadContract
		}
		seen[row.ID] = true
	}
	groups := map[string][]*F12CreditRecord{}
	maxVersions := map[string]uint64{}
	for _, row := range latest {
		if !validF12CreditRecord(row, q) || (q.AsOf != nil && row.CreatedAt.After(*q.AsOf)) || seen["latest:"+row.ID] {
			return nil, ErrReadContract
		}
		seen["latest:"+row.ID] = true
		key := f12CreditDimension(row)
		if row.Version < maxVersions[key] {
			return nil, ErrReadContract
		}
		if row.Version > maxVersions[key] {
			maxVersions[key] = row.Version
			groups[key] = nil
		}
		groups[key] = append(groups[key], row)
	}
	for _, row := range rows {
		if maxVersions[f12CreditDimension(row)] < row.Version {
			return nil, ErrReadContract
		}
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	current := make([]*F12CurrentCredit, 0, len(keys))
	for _, key := range keys {
		candidates := groups[key]
		row := candidates[0]
		projection := &F12CurrentCredit{Direction: row.Direction, Currency: row.Currency}
		switch {
		case len(candidates) > 1:
			projection.Status = "AMBIGUOUS_VERSION"
		case !decimal.RequireFromString(row.Amount).IsPositive():
			projection.Status = "INVALID_LEGACY_AMOUNT"
		default:
			projection.Status = "AVAILABLE"
			copy := *row
			projection.Record = &copy
		}
		current = append(current, projection)
	}
	if rows == nil {
		rows = []*F12CreditRecord{}
	}
	return &F12CreditPage{List: rows, Current: current, Total: total, Page: q.Page, Size: q.Size}, nil
}
