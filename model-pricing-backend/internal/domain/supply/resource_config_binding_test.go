package supply

import (
	"errors"
	"testing"
	"time"
)

func configBindingFixture(t *testing.T) (*Resource, *ResourceFormalConfig, *AdmissionProof, ResourceConfigBindingCommand) {
	record, p, _ := admissionFixture(t)
	r := record.Resource
	r.FormalConfigID = ""
	r.FormalConfigRevision = 0
	cfg := &ResourceFormalConfig{ID: "2199", SupplierID: "21", Region: "CN", Status: "ACTIVE", Revision: 3, ConfirmedRevision: 3, HasAPIKey: true}
	return r, cfg, p, ResourceConfigBindingCommand{ResourceID: r.ID, ExpectedRevision: r.Revision, ConfigID: cfg.ID, ExpectedConfigRevision: 3}
}
func TestResourceConfigBindingLinksConfirmedReferenceOnly(t *testing.T) {
	r, cfg, p, cmd := configBindingFixture(t)
	now := modelNow.Add(time.Hour)
	change, err := ProposeResourceConfigBinding(r, cfg, p, "8", cmd, now)
	if err != nil {
		t.Fatal(err)
	}
	after := change.Resource
	if !change.Changed || after.FormalConfigID != cfg.ID || after.FormalConfigRevision != 3 || after.Revision != 2 || after.State != r.State || after.LatestReportID != r.LatestReportID || after.TargetVersion != r.TargetVersion || after.EverAdmitted || r.FormalConfigID != "" {
		t.Fatalf("bad binding %+v", change)
	}
	again, err := ProposeResourceConfigBinding(after, cfg, p, "8", ResourceConfigBindingCommand{ResourceID: r.ID, ExpectedRevision: 2, ConfigID: cfg.ID, ExpectedConfigRevision: 3}, now.Add(time.Hour))
	if err != nil || again.Changed || *again.Resource != *after {
		t.Fatalf("NOOP changed resource %+v %v", again, err)
	}
}
func TestResourceConfigBindingUsesNodeAssigneeAndRejectsInactiveOrStaleFacts(t *testing.T) {
	r, cfg, p, cmd := configBindingFixture(t)
	if _, err := ProposeResourceConfigBinding(r, cfg, p, "7", cmd, modelNow); !errors.Is(err, ErrForbidden) {
		t.Fatalf("buyer bypassed assigned config executor %v", err)
	}
	for _, change := range []func(*Resource, *ResourceFormalConfig, *AdmissionProof, *ResourceConfigBindingCommand){
		func(r *Resource, c *ResourceFormalConfig, p *AdmissionProof, cmd *ResourceConfigBindingCommand) {
			c.SupplierID = "22"
		},
		func(r *Resource, c *ResourceFormalConfig, p *AdmissionProof, cmd *ResourceConfigBindingCommand) {
			c.Region = "OVERSEAS"
		},
		func(r *Resource, c *ResourceFormalConfig, p *AdmissionProof, cmd *ResourceConfigBindingCommand) {
			c.HasAPIKey = false
		},
		func(r *Resource, c *ResourceFormalConfig, p *AdmissionProof, cmd *ResourceConfigBindingCommand) {
			c.Status = "INACTIVE"
		},
		func(r *Resource, c *ResourceFormalConfig, p *AdmissionProof, cmd *ResourceConfigBindingCommand) {
			c.ConfirmedRevision = 2
		},
		func(r *Resource, c *ResourceFormalConfig, p *AdmissionProof, cmd *ResourceConfigBindingCommand) {
			cmd.ExpectedConfigRevision = 2
		},
		func(r *Resource, c *ResourceFormalConfig, p *AdmissionProof, cmd *ResourceConfigBindingCommand) {
			cmd.ExpectedRevision = 2
		},
		func(r *Resource, c *ResourceFormalConfig, p *AdmissionProof, cmd *ResourceConfigBindingCommand) {
			p.NodeInProgress = false
		},
		func(r *Resource, c *ResourceFormalConfig, p *AdmissionProof, cmd *ResourceConfigBindingCommand) {
			p.PrerequisitesReady = false
		},
		func(r *Resource, c *ResourceFormalConfig, p *AdmissionProof, cmd *ResourceConfigBindingCommand) {
			p.TargetVersion = 2
		},
		func(r *Resource, c *ResourceFormalConfig, p *AdmissionProof, cmd *ResourceConfigBindingCommand) {
			p.OriginRowVersion = 2
		},
		func(r *Resource, c *ResourceFormalConfig, p *AdmissionProof, cmd *ResourceConfigBindingCommand) {
			r.State = "ADMITTED"
			r.EverAdmitted = true
		},
	} {
		r, cfg, p, cmd := configBindingFixture(t)
		change(r, cfg, p, &cmd)
		if _, err := ProposeResourceConfigBinding(r, cfg, p, "8", cmd, modelNow); err == nil {
			t.Fatal("invalid binding accepted")
		}
	}
}
