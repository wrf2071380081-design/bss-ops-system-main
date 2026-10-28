package api

import (
	"context"
	"github.com/shopspring/decimal"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"model_bss/internal/domain/supply"
	"model_bss/internal/infra/supplypg"
	"model_bss/pkg/response"
)

// These are transaction-local B facts, never authority inferred from a buyer or
// supplied by the browser. Missing providers keep the write services closed.
type SupplyResourceWorkflowProviders struct {
	Evidence      SupplyResourceEvidenceWriter
	Admission     supplypg.ResourceAdmissionHooks
	ApprovedPrice supplypg.ApprovedPriceHooks
	Actions       SupplyResourceWorkflowReader
}
type ApprovedPriceOption struct {
	SourceRowID    string `json:"source_row_id"`
	SourceRevision string `json:"source_revision"`
	Coefficient    string `json:"coefficient"`
}
type ResourceWorkflowActions struct {
	CanRefreshTests bool                  `json:"can_refresh_tests"`
	CanBind         bool                  `json:"can_bind"`
	CanAdmit        bool                  `json:"can_admit"`
	ApprovedPrices  []ApprovedPriceOption `json:"approved_prices"`
}
type SupplyResourceWorkflowReader interface {
	GetResourceWorkflow(context.Context, string, string) (*ResourceWorkflowActions, error)
}
type SupplyApprovedPriceWriter interface {
	Change(context.Context, supply.WriteMeta, string, uint64, string, supply.CoefficientSource) (*supply.Resource, error)
}
type SupplyResourceEvidenceWriter interface {
	RefreshEvidence(context.Context, supply.WriteMeta, string, uint64) (*supply.Resource, error)
}

var workflowCoefficientPattern = regexp.MustCompile(`^(0|1)(\.[0-9]{1,4})?$`)

// WireSupplyResourceWorkflow joins the caller's idempotency transaction. The
// assembler supplies A persistence/outbox and B supplies authorization/evidence.
func WireSupplyResourceWorkflow(deps *SupplyDeps, state *supplypg.ResourceStateStore, configs *supplypg.FormalConfigStore, resolve supplypg.ModelTxResolver, report supplypg.ResourceReportRefLoader, outbox supplypg.AdmissionOutboxWriter, providers SupplyResourceWorkflowProviders) {
	admission := providers.Admission
	if admission.Report == nil {
		admission.Report = report
	}
	admission.Outbox = outbox
	deps.ResourceConfigBinder = supply.NewResourceConfigBindingService(supplypg.NewResourceBindingWork(state, configs, resolve, admission.Proof), time.Now)
	deps.ResourceAdmitter = supply.NewResourceAdmissionService(supplypg.NewResourceAdmissionWork(state, configs, resolve, admission), time.Now)
	price := providers.ApprovedPrice
	price.Outbox = outbox
	deps.ApprovedPriceWriter = supply.NewResourceCoefficientService(supplypg.NewApprovedPriceWork(state, resolve, price), time.Now)
	deps.ResourceEvidence = providers.Evidence
	// An action list cannot advertise a write whose transaction providers are absent.
	deps.ResourceWorkflow = &configuredWorkflowReader{reader: providers.Actions, bind: admission.Proof != nil, admit: admission.Proof != nil && admission.Report != nil && admission.CompleteRow != nil && outbox != nil, price: price.Verify != nil && price.Complete != nil && outbox != nil, refresh: providers.Evidence != nil}
}

type configuredWorkflowReader struct {
	reader             SupplyResourceWorkflowReader
	bind, admit, price bool
	refresh            bool
}

func (r *configuredWorkflowReader) GetResourceWorkflow(ctx context.Context, actor, id string) (*ResourceWorkflowActions, error) {
	if r.reader == nil {
		return nil, supply.ErrDependencyUnavailable
	}
	result, err := r.reader.GetResourceWorkflow(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, supply.ErrReadContract
	}
	seen := map[string]bool{}
	for _, option := range result.ApprovedPrices {
		_, idErr := planPositiveID(option.SourceRowID)
		_, revisionErr := planPositiveID(option.SourceRevision)
		amount, amountErr := decimal.NewFromString(option.Coefficient)
		if idErr != nil || revisionErr != nil || amountErr != nil || !workflowCoefficientPattern.MatchString(option.Coefficient) || !amount.GreaterThan(decimal.Zero) || amount.GreaterThan(decimal.NewFromInt(1)) || seen[option.SourceRowID] {
			return nil, supply.ErrReadContract
		}
		seen[option.SourceRowID] = true
	}
	copy := *result
	copy.CanBind = copy.CanBind && r.bind
	copy.CanAdmit = copy.CanAdmit && r.admit
	copy.CanRefreshTests = copy.CanRefreshTests && r.bind && r.refresh
	copy.ApprovedPrices = append([]ApprovedPriceOption{}, result.ApprovedPrices...)
	if !r.price {
		copy.ApprovedPrices = []ApprovedPriceOption{}
	}
	return &copy, nil
}
func resourceWorkflowHandler(reader SupplyResourceWorkflowReader) gin.HandlerFunc {
	return func(c *gin.Context) {
		if reader == nil {
			supplyHTTPError(c, supply.ErrDependencyUnavailable)
			return
		}
		if _, err := planPositiveID(c.Param("id")); err != nil {
			supplyHTTPError(c, err)
			return
		}
		actor := supplyActor(c)
		result, err := reader.GetResourceWorkflow(c.Request.Context(), actor, c.Param("id"))
		if err != nil {
			supplyHTTPError(c, err)
			return
		}
		if result == nil {
			supplyHTTPError(c, supply.ErrReadContract)
			return
		}
		response.Success(c, result)
	}
}
