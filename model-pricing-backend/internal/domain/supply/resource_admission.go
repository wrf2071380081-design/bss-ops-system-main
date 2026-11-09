package supply

import (
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

// AdmissionProof is a server-side B projection obtained within the shared
// transaction, never a client-supplied admission/approval authorization.
type AdmissionProof struct {
	FlowID, NodeID, SupplierID, ResourceID, Region, OriginRowID, AssigneeID string
	FlowRevision, OriginRowVersion, TargetVersion                           uint64
	FlowInProgress, NodeInProgress, PrerequisitesReady                      bool
}
type ResourceAdmissionCommand struct {
	ResourceID, CompletionNote string
	ExpectedRevision           uint64
	ExternalConfirmed          bool
}

func ProposeResourceAdmission(record *ResourceRecord, proof *AdmissionProof, actor string, cmd ResourceAdmissionCommand, now time.Time) (*Resource, error) {
	if !validID(actor) || !validID(cmd.ResourceID) || cmd.ExpectedRevision == 0 || cmd.ExpectedRevision >= math.MaxInt64 || now.IsZero() {
		return nil, invalid("admission", "valid identity, incrementable revision and time required")
	}
	if !cmd.ExternalConfirmed || strings.TrimSpace(cmd.CompletionNote) == "" {
		return nil, invalid("completion", "actual configuration confirmation and evidence required")
	}
	if !utf8.ValidString(cmd.CompletionNote) || strings.ContainsRune(cmd.CompletionNote, 0) {
		return nil, invalid("completion_note", "valid UTF-8 evidence without NUL required")
	}
	if record == nil || record.Resource == nil {
		return nil, ErrNotFound
	}
	if proof == nil {
		return nil, ErrDependencyUnavailable
	}
	r := record.Resource
	if r.ID != cmd.ResourceID {
		return nil, ErrReadContract
	}
	if !validID(proof.AssigneeID) {
		return nil, ErrReadContract
	}
	if proof.AssigneeID != actor {
		return nil, ErrForbidden
	}
	if !validID(proof.FlowID) || !validID(proof.NodeID) || proof.FlowRevision == 0 || proof.FlowRevision > math.MaxInt64 {
		return nil, ErrReadContract
	}
	if r.Revision != cmd.ExpectedRevision || (r.State != "PENDING_TEST" && r.State != "PENDING_ACCESS") || r.EverAdmitted {
		return nil, ErrRevisionConflict
	}
	if proof.ResourceID != r.ID || proof.SupplierID != r.SupplierID || proof.Region != r.Region || proof.OriginRowID != r.OriginRowID || proof.OriginRowVersion != r.OriginRowVersion || proof.TargetVersion != r.TargetVersion || !proof.FlowInProgress || !proof.NodeInProgress || !proof.PrerequisitesReady {
		return nil, ErrRevisionConflict
	}
	copy := *r
	copy.State = "ADMITTED"
	copy.EverAdmitted = true
	proposed := *record
	proposed.Resource = &copy
	snapshot, err := BuildResourceSnapshot(&proposed)
	if err != nil {
		return nil, err
	}
	for _, reason := range snapshot.QuoteReadiness.BlockedReasons {
		// 官方资料核验与测试证据都独立于「技术接入」这个动作：
		//   - MODEL_NOT_READY：官方资料是否核验完，是报价口径，不拦接入；
		//   - TESTS_*：测试是质量评估，供应商在申报阶段已经跑过，
		//     内部复测只是可选动作，不能变成接入的前置条件（原型：填完 URL/Key 即可接入）。
		// 真正拦接入的只有「主体不可用」和「正式配置没绑对」两类。
		switch reason {
		case "MODEL_NOT_READY", "TESTS_REQUIRED", "TEST_EVIDENCE_INVALID", "TEST_TARGET_CHANGED", "TESTS_NOT_PASSED":
			continue
		}
		return nil, ErrRevisionConflict
	}
	copy.Revision++
	copy.UpdatedAt = now.UTC()
	return &copy, nil
}
