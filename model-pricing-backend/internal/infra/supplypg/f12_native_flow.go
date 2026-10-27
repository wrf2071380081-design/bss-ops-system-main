package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"model_bss/internal/domain/supply"
	appdb "model_bss/internal/infra/db"
)

// NativeSupplierF12 consumes the immutable B approval association and its real
// CONTRACT node. CUSTOMER flow identity requires a C mapping; numeric equality
// with customer_profile is never treated as that mapping.
type NativeSupplierF12 struct {
	db     *sql.DB
	schema string
}

var _ F12FlowWriteAuthorizer = (*NativeSupplierF12)(nil)
var _ supply.F12BusinessContextProvider = (*NativeSupplierF12)(nil)

func NewNativeSupplierF12(db *sql.DB, schema string) (*NativeSupplierF12, error) {
	if db == nil || !modelSchemaName.MatchString(schema) {
		return nil, supply.ErrDependencyUnavailable
	}
	return &NativeSupplierF12{db: db, schema: schema}, nil
}
func (n *NativeSupplierF12) table(name string) string { return `"` + n.schema + `"."` + name + `"` }

// All readiness reads borrow the public write transaction when one exists.
// GETs keep the existing read-only snapshot runner.
func F12ReadTransactions(db *sql.DB) ModelReadRunner {
	fallback := ModelReadTransactions(db)
	return func(ctx context.Context, run func(*sql.Tx) error) error {
		if appdb.FromContext(ctx) != nil {
			tx, err := GORMModelTxFromContext(ctx)
			if err != nil {
				return err
			}
			return run(tx)
		}
		return fallback(ctx, run)
	}
}

type nativeF12Flow struct {
	decision                                                                  uint64
	assignee, nodeStatus, flowStatus, currentNode, submissionStatus, currency string
}

func (n *NativeSupplierF12) flow(ctx context.Context, tx *sql.Tx, subjectType, subjectID, flowID, module string, lock bool) (nativeF12Flow, error) {
	var facts nativeF12Flow
	if subjectType != "SUPPLIER" {
		return facts, supply.ErrDependencyUnavailable
	}
	if !modelIdentity(subjectID) || !modelIdentity(flowID) || module != "CONTRACT" {
		return facts, supply.ErrReadContract
	}
	suffix := ""
	if lock {
		suffix = " FOR SHARE OF f,nd,s"
	}
	err := tx.QueryRowContext(ctx, `SELECT l.id,coalesce(nd.assignee_id::text,''),nd.status,f.status,f.current_node,s.status,coalesce(sp.settlement_currency,'')
 FROM `+n.table("supplier")+` sp JOIN `+n.table("onboarding_flow")+` f ON f.owner_id=sp.id AND f.kind='SUPPLIER'
 JOIN `+n.table("onboarding_node")+` nd ON nd.flow_id=f.id AND nd.node_key='CONTRACT' AND (NOT $3::boolean OR nd.enabled)
 JOIN `+n.table("operation_log")+` l ON l.entity_type='submission' AND l.action='APPROVE' AND l."after"->>'contract'='supply.handling_approval' AND l."after"->>'flow_id'=f.id::text AND l."after"->>'supplier_id'=sp.id::text
 JOIN `+n.table("submission")+` s ON s.id=l.entity_id AND s.supplier_id=sp.id
 WHERE sp.id=$1 AND f.id=$2 ORDER BY l.id DESC LIMIT 1`+suffix, subjectID, flowID, lock).Scan(&facts.decision, &facts.assignee, &facts.nodeStatus, &facts.flowStatus, &facts.currentNode, &facts.submissionStatus, &facts.currency)
	if errors.Is(err, sql.ErrNoRows) {
		return facts, supply.ErrForbidden
	}
	if err != nil {
		return facts, err
	}
	if facts.decision == 0 || facts.decision > math.MaxInt32 || !modelIdentity(facts.assignee) {
		return facts, supply.ErrReadContract
	}
	return facts, nil
}
func (n *NativeSupplierF12) AuthorizeF12FlowWrite(ctx context.Context, tx *sql.Tx, actor, typ, id, flow, module string, expected uint64) (uint64, error) {
	f, err := n.flow(ctx, tx, typ, id, flow, module, true)
	if err != nil {
		return 0, err
	}
	if actor != f.assignee {
		return 0, supply.ErrForbidden
	}
	if expected != f.decision {
		return 0, supply.ErrRevisionConflict
	}
	// The designated commercial handler may prepare documents in parallel with
	// CONFIG, but completing the flow still requires the predecessor to finish.
	if f.flowStatus != "PROCESSING" || f.submissionStatus != "PROCESSING" || (f.nodeStatus != "IN_PROGRESS" && f.nodeStatus != "WAITING_PREDECESSOR") || (f.currentNode != "CONFIG" && f.currentNode != "CONTRACT") {
		return 0, supply.ErrRevisionConflict
	}
	return f.decision, nil
}
func (n *NativeSupplierF12) AuthorizeF12FlowRead(ctx context.Context, tx *sql.Tx, actor, typ, id, flow, module string) error {
	f, err := n.flow(ctx, tx, typ, id, flow, module, false)
	if err != nil {
		return err
	}
	if actor == f.assignee {
		return nil
	}
	// Read permission also belongs to the current procurement owner. Resolve
	// the real assignment chain; this never grants flow write/completion rights.
	owner, err := f12CurrentSupplierBuyer(ctx, tx, n.table("assignment_log"), id)
	if err != nil {
		return err
	}
	if owner != actor {
		return supply.ErrForbidden
	}
	return nil
}
func (n *NativeSupplierF12) AuthorizeF12OrdinaryWrite(ctx context.Context, tx *sql.Tx, actor, typ, id string) error {
	if typ != "SUPPLIER" {
		return supply.ErrDependencyUnavailable
	}
	if !modelIdentity(actor) || !modelIdentity(id) {
		return supply.ErrReadContract
	}
	var pending bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+n.table("onboarding_flow")+` f JOIN `+n.table("onboarding_node")+` nd ON nd.flow_id=f.id WHERE f.kind='SUPPLIER' AND f.owner_id=$1 AND f.status='PROCESSING' AND nd.enabled AND nd.node_key='CONTRACT' AND nd.status<>'COMPLETED')`, id).Scan(&pending); err != nil {
		return err
	}
	if pending {
		return supply.ErrForbidden
	}
	return nil // The store still checks the real subject owner.
}
func (n *NativeSupplierF12) ResolveF12BusinessContext(ctx context.Context, q supply.F12ReadinessRequest) (*supply.F12BusinessContext, error) {
	var result *supply.F12BusinessContext
	err := F12ReadTransactions(n.db)(ctx, func(tx *sql.Tx) error {
		f, err := n.flow(ctx, tx, q.SubjectType, q.SubjectID, q.FlowID, q.ModuleKey, false)
		if err != nil {
			return err
		}
		if (f.flowStatus != "PROCESSING" && f.flowStatus != "COMPLETED") || (f.submissionStatus != "PROCESSING" && f.submissionStatus != "COMPLETED") {
			return supply.ErrRevisionConflict
		}
		if !f12AttachmentCurrency.MatchString(f.currency) {
			return &supply.FieldError{Path: "settlement_currency", Reason: "supplier settlement currency is required for first contract and credit handling"}
		}
		result = &supply.F12BusinessContext{SubjectType: q.SubjectType, SubjectID: q.SubjectID, FlowID: q.FlowID, ModuleKey: q.ModuleKey, FlowRevision: f.decision, RequiredItems: []supply.F12ReadinessRequirement{{Item: supply.F12RequiredContract}, {Item: supply.F12RequiredCredit, Currency: f.currency}}}
		return nil
	})
	return result, err
}
