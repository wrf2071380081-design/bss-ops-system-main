package supplypg

import (
	"context"
	"database/sql"
	"model_bss/internal/domain/supply"
)

type SupplierMergePreviewStore struct {
	schema    string
	read      ModelReadRunner
	suppliers *SupplierStore
}

var _ supply.SupplierMergePreviewStore = (*SupplierMergePreviewStore)(nil)

func NewSupplierMergePreviewStore(schema string, suppliers *SupplierStore, read ModelReadRunner) (*SupplierMergePreviewStore, error) {
	if !modelSchemaName.MatchString(schema) {
		return nil, mappingError("schema")
	}
	if suppliers == nil || suppliers.schema != schema || read == nil {
		return nil, supply.ErrDependencyUnavailable
	}
	return &SupplierMergePreviewStore{schema: schema, suppliers: suppliers, read: read}, nil
}

func (s *SupplierMergePreviewStore) table(name string) string {
	return `"` + s.schema + `"."` + name + `"`
}

func (s *SupplierMergePreviewStore) LoadSupplierMergeFacts(ctx context.Context, sourceID, targetID string) (*supply.SupplierMergeFacts, error) {
	if s == nil || s.read == nil || s.suppliers == nil || !modelIdentity(sourceID) || !modelIdentity(targetID) || sourceID == targetID {
		return nil, supply.ErrReadContract
	}
	facts := &supply.SupplierMergeFacts{}
	err := s.read(ctx, func(tx *sql.Tx) error {
		if tx == nil {
			return supply.ErrDependencyUnavailable
		}
		source, err := s.suppliers.loadSupplier(ctx, tx, sourceID, false)
		if err != nil {
			return err
		}
		target, err := s.suppliers.loadSupplier(ctx, tx, targetID, false)
		if err != nil {
			return err
		}
		facts.Source = supplierMergeParty(source)
		facts.Target = supplierMergeParty(target)
		facts.SourceReferences, err = s.referenceCounts(ctx, tx, sourceID)
		if err != nil {
			return err
		}
		facts.TargetReferences, err = s.referenceCounts(ctx, tx, targetID)
		if err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, `SELECT count(*) FROM `+s.table("resource")+` source JOIN `+s.table("resource")+` target ON source.standard_model_id=target.standard_model_id AND source.region=target.region WHERE source.supplier_id=$1 AND target.supplier_id=$2`, sourceID, targetID).Scan(&facts.DuplicateResourceKeys)
	})
	if err != nil {
		return nil, err
	}
	return facts, nil
}

func supplierMergeParty(profile *supply.Supplier) supply.SupplierMergePartySummary {
	return supply.SupplierMergePartySummary{ID: profile.ID, Name: profile.Data.Name, Region: profile.Data.Region, BuyerID: profile.BuyerID,
		SettlementCurrency: profile.Data.SettlementCurrency, Revision: profile.Revision, Status: profile.Status,
		Established: profile.Established, CreatedAt: profile.CreatedAt, UpdatedAt: profile.UpdatedAt}
}

func (s *SupplierMergePreviewStore) referenceCounts(ctx context.Context, tx *sql.Tx, supplierID string) (supply.SupplierMergeReferenceCounts, error) {
	if s == nil || tx == nil || !modelIdentity(supplierID) {
		return supply.SupplierMergeReferenceCounts{}, supply.ErrReadContract
	}
	r := supply.SupplierMergeReferenceCounts{}
	query := `SELECT
 (SELECT count(*) FROM ` + s.table("resource") + ` WHERE supplier_id=$1),
 (SELECT count(*) FROM ` + s.table("submission") + ` WHERE supplier_id=$1),
 (SELECT count(*) FROM ` + s.table("submission") + ` WHERE supplier_id=$1 AND status='PROCESSING'),
 (SELECT count(*) FROM ` + s.table("onboarding_flow") + ` WHERE kind='SUPPLIER' AND owner_id=$1),
 (SELECT count(*) FROM ` + s.table("contract") + ` WHERE subject_type='SUPPLIER' AND subject_id=$1),
 (SELECT count(*) FROM ` + s.table("credit_line") + ` WHERE subject_type='SUPPLIER' AND subject_id=$1),
 (SELECT count(*) FROM ` + s.table("supplier_formal_config") + ` WHERE supplier_id=$1),
 (SELECT count(*) FROM ` + s.table("supplier_qualification") + ` WHERE supplier_id=$1),
 (SELECT count(*) FROM ` + s.table("customer_link") + ` cl JOIN ` + s.table("resource") + ` r ON r.id=cl.resource_id WHERE r.supplier_id=$1),
 ((SELECT count(*) FROM ` + s.table("resource") + ` WHERE supplier_id=$1 AND (test_base_url IS NOT NULL OR test_api_key_enc IS NOT NULL))+
  (SELECT count(*) FROM ` + s.table("submission_row") + ` sr JOIN ` + s.table("submission") + ` ss ON ss.id=sr.submission_id WHERE ss.supplier_id=$1 AND (sr.test_base_url IS NOT NULL OR sr.test_api_key_enc IS NOT NULL))),
 (SELECT EXISTS(SELECT 1 FROM ` + s.table("supplier") + ` WHERE id=$1 AND (COALESCE(website,'')<>'' OR COALESCE(account,'')<>'' OR COALESCE(password_enc,'')<>'')))`
	if err := tx.QueryRowContext(ctx, query, supplierID).Scan(&r.Resources, &r.Submissions, &r.PendingSubmissions, &r.OnboardingFlows,
		&r.Contracts, &r.CreditLines, &r.FormalConfigs, &r.Qualifications, &r.CustomerRelations, &r.TestCredentialRecords, &r.WebsiteAccountConfigured); err != nil {
		return supply.SupplierMergeReferenceCounts{}, err
	}
	available, err := s.suppliers.supplierAliasTableAvailable(ctx, tx)
	if err != nil {
		return supply.SupplierMergeReferenceCounts{}, err
	}
	if available {
		var aliases int64
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM `+s.table("supplier_alias")+` WHERE supplier_id=$1`, supplierID).Scan(&aliases); err != nil {
			return supply.SupplierMergeReferenceCounts{}, err
		}
		r.Aliases = &aliases
	}
	return r, nil
}
