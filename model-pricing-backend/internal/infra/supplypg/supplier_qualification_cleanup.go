package supplypg

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"model_bss/internal/domain/supply"
)

const supplierQualificationOrphanGracePeriod = 24 * time.Hour

type supplierQualificationBlobLister interface {
	ListBefore(context.Context, time.Time) ([]string, error)
}

// SupplierQualificationAttachmentCleaner deletes old local files only after
// checking the authoritative supplier_qualification.file_ref column.
type SupplierQualificationAttachmentCleaner struct {
	db     *sql.DB
	schema string
	blobs  supplierQualificationBlobLister
	delete func(context.Context, string) error
}

func NewSupplierQualificationAttachmentCleaner(db *sql.DB, schema string, blobs supply.SupplierQualificationBlobStore) (*SupplierQualificationAttachmentCleaner, error) {
	if db == nil || !modelSchemaName.MatchString(schema) || blobs == nil {
		return nil, supply.ErrDependencyUnavailable
	}
	lister, ok := blobs.(supplierQualificationBlobLister)
	if !ok {
		return nil, supply.ErrDependencyUnavailable
	}
	return &SupplierQualificationAttachmentCleaner{db: db, schema: schema, blobs: lister, delete: blobs.Delete}, nil
}

func (c *SupplierQualificationAttachmentCleaner) CleanupUnreferenced(ctx context.Context) (int, error) {
	if c == nil || c.db == nil || c.blobs == nil || c.delete == nil || !modelSchemaName.MatchString(c.schema) {
		return 0, supply.ErrDependencyUnavailable
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	refs, err := c.blobs.ListBefore(ctx, time.Now().Add(-supplierQualificationOrphanGracePeriod))
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, ref := range refs {
		if err := ctx.Err(); err != nil {
			return removed, err
		}
		var referenced bool
		err := c.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM "`+c.schema+`"."supplier_qualification" WHERE file_ref=$1)`, strings.TrimSpace(ref)).Scan(&referenced)
		if err != nil {
			return removed, mapSupplierStorageError(err)
		}
		if referenced {
			continue
		}
		if err := c.delete(ctx, ref); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}
