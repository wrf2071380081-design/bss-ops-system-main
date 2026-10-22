package supply

import (
	"math"
	"time"
)

// ResourceFormalConfig is a credential-free availability reference. It cannot
// carry an endpoint, secret envelope or a submission test credential.
type ResourceFormalConfig struct {
	ID, SupplierID, Region, Status string
	Revision, ConfirmedRevision    uint64
	HasAPIKey                      bool
}
type ResourceConfigBindingCommand struct {
	ResourceID, ConfigID                     string
	ExpectedRevision, ExpectedConfigRevision uint64
}
type ResourceConfigBindingChange struct {
	Resource *Resource
	Changed  bool
}

func ProposeResourceConfigBinding(r *Resource, cfg *ResourceFormalConfig, proof *AdmissionProof, actor string, cmd ResourceConfigBindingCommand, now time.Time) (*ResourceConfigBindingChange, error) {
	if r == nil {
		return nil, ErrNotFound
	}
	if cfg == nil || proof == nil {
		return nil, ErrDependencyUnavailable
	}
	if !validID(actor) || !validID(cmd.ResourceID) || !validID(cmd.ConfigID) || now.IsZero() {
		return nil, invalid("binding", "valid actor, resource/config references and time required")
	}
	for _, version := range []uint64{r.Revision, r.OriginRowVersion, r.TargetVersion, cmd.ExpectedRevision, cmd.ExpectedConfigRevision, cfg.Revision, proof.FlowRevision} {
		if version == 0 || version > math.MaxInt64 {
			return nil, ErrReadContract
		}
	}
	if r.ID != cmd.ResourceID || cfg.ID != cmd.ConfigID || !validID(r.SupplierID) || !validID(r.StandardModelID) || !validID(r.OriginRowID) || !validID(proof.AssigneeID) || !validID(proof.FlowID) || !validID(proof.NodeID) || (r.Region != "CN" && r.Region != "OVERSEAS") {
		return nil, ErrReadContract
	}
	if proof.AssigneeID != actor {
		return nil, ErrForbidden
	}
	if r.Revision != cmd.ExpectedRevision || r.EverAdmitted || (r.State != "PENDING_TEST" && r.State != "PENDING_ACCESS") {
		return nil, ErrRevisionConflict
	}
	if proof.ResourceID != r.ID || proof.SupplierID != r.SupplierID || proof.Region != r.Region || proof.OriginRowID != r.OriginRowID || proof.OriginRowVersion != r.OriginRowVersion || proof.TargetVersion != r.TargetVersion || !proof.FlowInProgress || !proof.NodeInProgress || !proof.PrerequisitesReady {
		return nil, ErrRevisionConflict
	}
	if cfg.SupplierID != r.SupplierID || cfg.Region != r.Region || cfg.Status != "ACTIVE" || !cfg.HasAPIKey || cfg.Revision != cmd.ExpectedConfigRevision || cfg.ConfirmedRevision != cfg.Revision {
		return nil, ErrRevisionConflict
	}
	if (r.FormalConfigID == "") != (r.FormalConfigRevision == 0) || (r.FormalConfigID != "" && !validID(r.FormalConfigID)) || r.FormalConfigRevision > math.MaxInt64 {
		return nil, ErrReadContract
	}
	copy := *r
	change := &ResourceConfigBindingChange{Resource: &copy}
	if r.FormalConfigID == cfg.ID && r.FormalConfigRevision == cfg.Revision {
		return change, nil
	}
	if r.Revision >= math.MaxInt64 {
		return nil, ErrRevisionConflict
	}
	copy.FormalConfigID = cfg.ID
	copy.FormalConfigRevision = cfg.Revision
	copy.Revision++
	copy.UpdatedAt = now.UTC()
	change.Changed = true
	return change, nil
}
