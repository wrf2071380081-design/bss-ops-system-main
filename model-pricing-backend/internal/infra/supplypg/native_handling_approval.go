package supplypg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"model_bss/internal/domain/supply"
	"strconv"
	"time"
)

// Immutable link between B's actual decision, assigned node and submitted row
// content. The approval log's real ID is the flow decision version reference;
// it is never a constant or a fabricated onboarding_flow column.
type NativeApproval struct {
	Contract       string              `json:"contract"`
	SubmissionID   string              `json:"submission_id"`
	SourceRevision uint64              `json:"source_revision,string"`
	SupplierID     string              `json:"supplier_id"`
	Region         string              `json:"region"`
	Kind           string              `json:"kind"`
	FlowID         string              `json:"flow_id"`
	NodeID         string              `json:"node_id"`
	AssigneeID     string              `json:"assignee_id"`
	Rows           []NativeApprovedRow `json:"rows"`
}
type NativeApprovedRow struct {
	ID               string `json:"id"`
	ModelID          string `json:"model_id"`
	Model            string `json:"model"`
	Coefficient      string `json:"coefficient"`
	Fingerprint      string `json:"fingerprint"`
	ResourceID       string `json:"resource_id"`
	ExpectedRevision uint64 `json:"expected_resource_revision,string"`
}

// RecordNativeApproval is called ONLY by B's successful, authorized approval
// transaction. It records safe submitted facts and materializes A control state
// after entry into handling. No credential or fabricated test result is stored.
func RecordNativeApproval(ctx context.Context, tx *sql.Tx, state *ResourceStateStore, submissionID, flowID, actor, reason, requestID string) error {
	if tx == nil || state == nil || state.log == nil {
		return supply.ErrDependencyUnavailable
	}
	a := NativeApproval{Contract: "supply.handling_approval", SubmissionID: submissionID, FlowID: flowID}
	if err := tx.QueryRowContext(ctx, `SELECT supplier_id::text,region,kind,revision FROM `+state.table("submission")+` WHERE id=$1 AND status='PROCESSING' FOR UPDATE`, submissionID).Scan(&a.SupplierID, &a.Region, &a.Kind, &a.SourceRevision); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT id::text,assignee_id::text FROM `+state.table("onboarding_node")+` WHERE flow_id=$1 AND node_key='CONFIG' AND status='IN_PROGRESS' AND enabled FOR UPDATE`, flowID).Scan(&a.NodeID, &a.AssigneeID); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id::text,standard_model_id::text,supplier_model_name,coefficient::text,coalesce(test_fingerprint,''),coalesce(resource_id::text,'') FROM `+state.table("submission_row")+` WHERE submission_id=$1 ORDER BY id FOR UPDATE`, submissionID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var r NativeApprovedRow
		if err := rows.Scan(&r.ID, &r.ModelID, &r.Model, &r.Coefficient, &r.Fingerprint, &r.ResourceID); err != nil {
			rows.Close()
			return err
		}
		a.Rows = append(a.Rows, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(a.Rows) == 0 {
		return supply.ErrRevisionConflict
	}
	meta := supply.WriteMeta{ActorID: actor, Reason: reason, RequestID: requestID}
	for i := range a.Rows {
		r := &a.Rows[i]
		if a.Kind == "PRICE_CHANGE" {
			if r.ResourceID == "" {
				if err := tx.QueryRowContext(ctx, `SELECT id::text FROM `+state.table("resource")+` WHERE supplier_id=$1 AND standard_model_id=$2 AND region=$3`, a.SupplierID, r.ModelID, a.Region).Scan(&r.ResourceID); err != nil {
					return err
				}
			}
			current, err := state.ForUpdate(ctx, tx, r.ResourceID)
			if err != nil {
				return err
			}
			if !current.EverAdmitted || current.SupplierID != a.SupplierID || current.StandardModelID != r.ModelID || current.Region != a.Region || current.SupplierModelID != r.Model {
				return supply.ErrRevisionConflict
			}
			r.ExpectedRevision = current.Revision
		} else {
			if r.Fingerprint == "" {
				return supply.ErrTestEvidenceRequired
			}
			buyer := ""
			if err := tx.QueryRowContext(ctx, `SELECT new_staff_id::text FROM `+state.table("assignment_log")+` WHERE entity_type='supplier' AND entity_id=$1 AND field='buyer_id' ORDER BY id DESC LIMIT 1`, a.SupplierID).Scan(&buyer); err != nil {
				return err
			}
			now := time.Now().UTC().Truncate(time.Microsecond)
			target := &supply.Resource{SupplierID: a.SupplierID, StandardModelID: r.ModelID, Region: a.Region, SupplierModelID: r.Model, Coefficient: r.Coefficient, BuyerID: buyer, State: "PENDING_TEST", Revision: 1, OriginRowID: r.ID, OriginRowVersion: a.SourceRevision, TargetVersion: 1, CreatedAt: now, UpdatedAt: now}
			if r.ResourceID == "" {
				target.ID, err = state.NextID(ctx, tx)
				if err != nil {
					return err
				}
				current, err := state.Insert(ctx, tx, target, meta, "CREATE")
				if err != nil {
					return err
				}
				r.ResourceID = current.ID
				r.ExpectedRevision = current.Revision
			} else {
				// B v42 may already have allocated a test-only resource for the draft.
				// Adopt it only once and only now that a real approval entered handling.
				target.ID = r.ResourceID
				var controls int
				if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM `+state.table("operation_log")+` WHERE entity_type='resource' AND entity_id=$1 AND action='RESOURCE_REVISION'`, r.ResourceID).Scan(&controls); err != nil {
					return err
				}
				if controls != 0 {
					return supply.ErrRevisionConflict
				}
				var admitted bool
				var supplier, model, region string
				if err := tx.QueryRowContext(ctx, `SELECT supplier_id::text,standard_model_id::text,region,ever_admitted,created_at FROM `+state.table("resource")+` WHERE id=$1 FOR UPDATE`, r.ResourceID).Scan(&supplier, &model, &region, &admitted, &target.CreatedAt); err != nil {
					return err
				}
				if admitted || supplier != a.SupplierID || model != r.ModelID || region != a.Region {
					return supply.ErrRevisionConflict
				}
				target, err = canonicalStoredResource(target)
				if err != nil {
					return err
				}
				if _, err := tx.ExecContext(ctx, `UPDATE `+state.table("resource")+` SET supplier_model_name=$2,coefficient=$3,buyer_id=$4,state='PENDING_TEST',latest_test_report_id=NULL,updated_at=$5,updated_by=$6,request_id=$7 WHERE id=$1 AND NOT ever_admitted`, r.ResourceID, r.Model, r.Coefficient, buyer, target.UpdatedAt, actor, requestID); err != nil {
					return err
				}
				if err := state.appendContext(ctx, tx, nil, target, meta, "CREATE", resourceWriteContext{}); err != nil {
					return err
				}
				r.ExpectedRevision = 1
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE `+state.table("submission_row")+` SET resource_id=$2 WHERE id=$1`, r.ID, r.ResourceID); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return err
	}
	return state.log(ctx, tx, ResourceStateLog{EntityType: "submission", EntityID: submissionID, Action: "APPROVE", ActorID: actor, Reason: reason, RequestID: requestID, After: raw, OccurredAt: time.Now().UTC()})
}

type NativeHandling struct {
	DB    *sql.DB
	State *ResourceStateStore
}

func (n *NativeHandling) approval(ctx context.Context, tx *sql.Tx, rowID string) (NativeApproval, NativeApprovedRow, uint64, error) {
	var a NativeApproval
	var r NativeApprovedRow
	var raw []byte
	var decision uint64
	err := tx.QueryRowContext(ctx, `SELECT l.id,l."after" FROM `+n.State.table("operation_log")+` l JOIN `+n.State.table("submission_row")+` sr ON sr.submission_id=l.entity_id WHERE sr.id=$1 AND l.entity_type='submission' AND l.action='APPROVE' AND l."after"->>'contract'='supply.handling_approval' ORDER BY l.id DESC LIMIT 1`, rowID).Scan(&decision, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return a, r, 0, supply.ErrForbidden
	}
	if err != nil {
		return a, r, 0, err
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return a, r, 0, supply.ErrReadContract
	}
	for _, v := range a.Rows {
		if v.ID == rowID {
			r = v
			break
		}
	}
	if a.Contract != "supply.handling_approval" || a.SourceRevision == 0 || r.ID == "" || !modelIdentity(a.NodeID) || !modelIdentity(a.FlowID) || !modelIdentity(a.AssigneeID) {
		return a, r, 0, supply.ErrReadContract
	}
	return a, r, decision, nil
}
func (n *NativeHandling) Proof(ctx context.Context, tx *sql.Tx, r *supply.Resource) (*supply.AdmissionProof, error) {
	a, row, version, err := n.approval(ctx, tx, r.OriginRowID)
	if err != nil {
		return nil, err
	}
	if a.Kind == "PRICE_CHANGE" || row.ResourceID != r.ID || row.ModelID != r.StandardModelID || row.Model != r.SupplierModelID || a.SupplierID != r.SupplierID || a.Region != r.Region || a.SourceRevision != r.OriginRowVersion {
		return nil, supply.ErrRevisionConflict
	}
	assigned, err := n.lockAssignment(ctx, tx, a, row)
	if err != nil {
		return nil, err
	}
	return &supply.AdmissionProof{FlowID: a.FlowID, NodeID: a.NodeID, SupplierID: r.SupplierID, ResourceID: r.ID, Region: r.Region, OriginRowID: r.OriginRowID, AssigneeID: assigned, FlowRevision: version, OriginRowVersion: a.SourceRevision, TargetVersion: r.TargetVersion, FlowInProgress: true, NodeInProgress: true, PrerequisitesReady: true}, nil
}
func (n *NativeHandling) lockAssignment(ctx context.Context, tx *sql.Tx, a NativeApproval, row NativeApprovedRow) (string, error) {
	var actor, model, name, amount, fp, resource, status string
	var revision uint64
	err := tx.QueryRowContext(ctx, `SELECT nd.assignee_id::text,sr.standard_model_id::text,sr.supplier_model_name,sr.coefficient::text,coalesce(sr.test_fingerprint,''),coalesce(sr.resource_id::text,''),sr.row_status,s.revision
 FROM `+n.State.table("submission_row")+` sr JOIN `+n.State.table("submission")+` s ON s.id=sr.submission_id JOIN `+n.State.table("onboarding_flow")+` f ON f.id=$3 JOIN `+n.State.table("onboarding_node")+` nd ON nd.id=$4 AND nd.flow_id=f.id
 WHERE sr.id=$1 AND s.id=$2 AND s.supplier_id=$5 AND s.region=$6 AND s.status='PROCESSING' AND f.kind='SUPPLIER' AND f.owner_id=s.supplier_id AND f.status='PROCESSING' AND f.current_node='CONFIG' AND nd.node_key='CONFIG' AND nd.status='IN_PROGRESS' AND nd.enabled FOR UPDATE OF sr,s,f,nd`, row.ID, a.SubmissionID, a.FlowID, a.NodeID, a.SupplierID, a.Region).Scan(&actor, &model, &name, &amount, &fp, &resource, &status, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return "", supply.ErrForbidden
	}
	if err != nil {
		return "", err
	}
	if !modelIdentity(actor) || revision != a.SourceRevision || model != row.ModelID || name != row.Model || amount != row.Coefficient || fp != row.Fingerprint || resource != row.ResourceID || status == "ADMITTED" || status == "COMPLETED" || status == "CANCELED" {
		return "", supply.ErrRevisionConflict
	}
	return actor, nil
}
func (n *NativeHandling) VerifyPrice(ctx context.Context, tx *sql.Tx, source supply.CoefficientSource, actor string, r *supply.Resource) (supply.CoefficientSourceProof, error) {
	var proof supply.CoefficientSourceProof
	a, row, _, err := n.approval(ctx, tx, source.ID)
	if err != nil {
		return proof, err
	}
	assigned, err := n.lockAssignment(ctx, tx, a, row)
	if err != nil {
		return proof, err
	}
	if assigned != actor {
		return proof, supply.ErrForbidden
	}
	if source.Kind != "SUBMISSION" || a.Kind != "PRICE_CHANGE" || source.Revision != a.SourceRevision || row.ResourceID != r.ID || row.ModelID != r.StandardModelID || a.SupplierID != r.SupplierID || a.Region != r.Region || row.ExpectedRevision != r.Revision || !r.EverAdmitted {
		return proof, supply.ErrRevisionConflict
	}
	return supply.CoefficientSourceProof{Authorized: true, ResourceID: r.ID, Coefficient: row.Coefficient, ExpectedResourceRevision: row.ExpectedRevision}, nil
}
func (n *NativeHandling) CompleteAdmission(ctx context.Context, tx *sql.Tx, e supply.ResourceAdmissionEffect) error {
	return n.complete(ctx, tx, e.After.OriginRowID, e.ActorID, "ADMITTED", e.CompletionNote)
}
func (n *NativeHandling) CancelAdmission(ctx context.Context, tx *sql.Tx, row, actor, note string) error {
	return n.complete(ctx, tx, row, actor, "CANCELED", note)
}
func (n *NativeHandling) CompletePrice(ctx context.Context, tx *sql.Tx, e supply.ResourceCoefficientOperation) error {
	return n.complete(ctx, tx, e.Source.ID, e.ActorID, "COMPLETED", approvedPriceCompletion(ctx))
}
func (n *NativeHandling) complete(ctx context.Context, tx *sql.Tx, rowID, actor, status, note string) error {
	a, row, _, err := n.approval(ctx, tx, rowID)
	if err != nil {
		return err
	}
	assigned, err := n.lockAssignment(ctx, tx, a, row)
	if err != nil {
		return err
	}
	if assigned != actor {
		return supply.ErrForbidden
	}
	if _, err := tx.ExecContext(ctx, `UPDATE `+n.State.table("submission_row")+` SET row_status=$2 WHERE id=$1`, rowID, status); err != nil {
		return err
	}
	var remaining int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM `+n.State.table("submission_row")+` WHERE submission_id=$1 AND row_status NOT IN ('ADMITTED','COMPLETED','CANCELED')`, a.SubmissionID).Scan(&remaining); err != nil {
		return err
	}
	if remaining > 0 {
		return nil
	}
	var admitted int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM `+n.State.table("submission_row")+` WHERE submission_id=$1 AND row_status IN ('ADMITTED','COMPLETED')`, a.SubmissionID).Scan(&admitted); err != nil {
		return err
	}
	if admitted == 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE `+n.State.table("onboarding_node")+` SET enabled=false,status='CANCELED',note=$2 WHERE flow_id=$1 AND node_key='CONTRACT' AND status='WAITING_PREDECESSOR'`, a.FlowID, note); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE `+n.State.table("onboarding_node")+` SET status='COMPLETED',completed_at=now(),note=$2 WHERE id=$1`, a.NodeID, note); err != nil {
		return err
	}
	var contract string
	err = tx.QueryRowContext(ctx, `UPDATE `+n.State.table("onboarding_node")+` SET status='IN_PROGRESS' WHERE flow_id=$1 AND enabled AND node_key='CONTRACT' AND status='WAITING_PREDECESSOR' RETURNING id::text`, a.FlowID).Scan(&contract)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if contract != "" {
		_, err = tx.ExecContext(ctx, `UPDATE `+n.State.table("onboarding_flow")+` SET current_node='CONTRACT',updated_at=now() WHERE id=$1`, a.FlowID)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE `+n.State.table("onboarding_flow")+` SET status='COMPLETED',updated_at=now() WHERE id=$1`, a.FlowID)
	}
	if err != nil {
		return err
	}
	if contract != "" {
		return nil
	}
	_, err = tx.ExecContext(ctx, `UPDATE `+n.State.table("submission")+` SET status='COMPLETED',completed_at=now(),revision=revision+1,updated_at=now() WHERE id=$1`, a.SubmissionID)
	return err
}
func versionText(n uint64) string { return strconv.FormatUint(n, 10) }
