package api

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	"model_bss/internal/infra/supplypg"
)

// StandardModelSupplyOptions contains only the public resources needed to
// compose F1. The SQL pool must be the same pool owned by the idempotency
// repository so its GORM transaction and the native model/log writes commit
// together.
type StandardModelSupplyOptions struct {
	DB           *sql.DB
	Schema       string
	IdemStore    middleware.IdempotencyStore
	SourcePolicy supply.OfficialSourcePolicy
	Clock        func() time.Time
}

// SupplyRouteModuleOptions contains A-owned route adapters and transaction-local
// providers owned by the public/B integration boundary. It intentionally has
// no defaults for identity, logging, B proof, report, row completion,
// outbox, or pending-configuration checks.
type SupplyRouteModuleOptions struct {
	StandardModelSupplyOptions
	FormalURLPolicy       supply.FormalURLPolicy
	ResourceLog           supplypg.ResourceStateLogAppender
	FormalConfigLog       supplypg.FormalConfigLogAppender
	NoticeLog             supplypg.NoticeLogAppender
	Admission             supplypg.ResourceAdmissionHooks
	ResourceCreation      supplypg.ResourceCreationHooks
	TestEvidenceAuthority supplypg.TestEvidenceAuthority
	ResourceReports       SupplyResourceReportReader
	PendingConfiguration  func(context.Context, *sql.Tx, string) (bool, error)
}

// SupplyRouteModuleConfigError identifies every required adapter that was not
// supplied. Unwrap keeps callers' existing dependency-unavailable handling.
type SupplyRouteModuleConfigError struct{ Missing []string }

func (e *SupplyRouteModuleConfigError) Error() string {
	return "A supply route module is missing required providers: " + strings.Join(e.Missing, ", ")
}
func (e *SupplyRouteModuleConfigError) Unwrap() error { return supply.ErrDependencyUnavailable }

// SupplyRouteModule is the complete A-owned router composition. ResourceCreation
// and TestEvidence are exposed to the trusted B integration; HTTP callers do not
// receive these services directly.
type SupplyRouteModule struct {
	Deps             SupplyDeps
	ResourceCreation *supply.ResourceCreationService
	TestEvidence     *supply.TestEvidenceService
}

// NewSupplyRouteModule composes the handlers registered by RegisterSupplyRoutes
// plus the resource-creation and report-association services called by trusted B
// adapters. All writes borrow the public idempotency transaction. Public main,
// authentication, shared logs, B facts, and outbox implementations
// remain injected dependencies.
func NewSupplyRouteModule(options SupplyRouteModuleOptions) (*SupplyRouteModule, error) {
	missing := make([]string, 0, 18)
	if options.DB == nil {
		missing = append(missing, "database")
	}
	if options.Schema == "" {
		missing = append(missing, "schema")
	}
	if options.IdemStore == nil {
		missing = append(missing, "idempotency")
	}
	if options.SourcePolicy == nil {
		missing = append(missing, "official_source_policy")
	}
	if options.FormalURLPolicy == nil {
		missing = append(missing, "formal_url_policy")
	}
	if options.ResourceLog == nil {
		missing = append(missing, "resource_log")
	}
	if options.FormalConfigLog == nil {
		missing = append(missing, "formal_config_log")
	}
	if options.NoticeLog == nil {
		missing = append(missing, "notice_log")
	}
	if options.Admission.Proof == nil {
		missing = append(missing, "admission.proof")
	}
	if options.Admission.Report == nil {
		missing = append(missing, "admission.report")
	}
	if options.Admission.CompleteRow == nil {
		missing = append(missing, "admission.complete_row")
	}
	if options.Admission.Outbox == nil {
		missing = append(missing, "admission.outbox")
	}
	if options.ResourceCreation.LockSource == nil {
		missing = append(missing, "resource_creation.lock_source")
	}
	if options.ResourceCreation.AttachRow == nil {
		missing = append(missing, "resource_creation.attach_row")
	}
	if options.ResourceCreation.PrepareReport == nil {
		missing = append(missing, "resource_creation.prepare_report")
	}
	if options.TestEvidenceAuthority == nil {
		missing = append(missing, "test_evidence_authority")
	}
	if options.PendingConfiguration == nil {
		missing = append(missing, "pending_configuration")
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, &SupplyRouteModuleConfigError{Missing: missing}
	}
	if options.Schema != "public" {
		return nil, fmt.Errorf("%w: A notice adapter currently requires schema public", supply.ErrDependencyUnavailable)
	}
	idemGORM := options.IdemStore.DB()
	if idemGORM == nil {
		return nil, fmt.Errorf("%w: idempotency repository has no GORM database", supply.ErrDependencyUnavailable)
	}
	idemDB, err := idemGORM.DB()
	if err != nil || idemDB != options.DB {
		return nil, fmt.Errorf("%w: A supply stores and idempotency must share one SQL pool", supply.ErrDependencyUnavailable)
	}

	deps, err := NewStandardModelSupplyDeps(options.StandardModelSupplyOptions)
	if err != nil {
		return nil, err
	}
	supplierStore, err := supplypg.NewSupplierStore(options.Schema, options.DB, supplypg.GORMModelTxFromContext, options.ResourceLog)
	if err != nil {
		return nil, err
	}
	deps.SupplierReader = supply.NewSupplierReader(supplierStore)
	deps.SupplierWriter = supply.NewSupplierService(supplierStore, options.Clock)
	read := supplypg.ModelReadTransactions(options.DB)
	state, err := supplypg.NewResourceStateStore(options.Schema, options.ResourceLog)
	if err != nil {
		return nil, err
	}
	configs, err := supplypg.NewFormalConfigStore(options.Schema, options.FormalConfigLog)
	if err != nil {
		return nil, err
	}
	resourceStore, err := supplypg.NewResourceReadStore(options.Schema, read, state.FactsLoader(options.Admission.Report, configs.ResourceView))
	if err != nil {
		return nil, err
	}
	deps.Resources = supply.NewResourceReader(resourceStore)
	deps.ResourceReports = options.ResourceReports

	configWork := supplypg.NewFormalConfigWork(configs, supplypg.GORMModelTxFromContext)
	deps.FormalConfigs = supply.NewFormalConfigService(configWork, options.FormalURLPolicy, options.Clock)
	configReadStore, err := supplypg.NewFormalConfigReadStore(options.Schema, read, configs)
	if err != nil {
		return nil, err
	}
	deps.FormalConfigReader = supply.NewFormalConfigReader(configReadStore)

	admissionWork := supplypg.NewResourceAdmissionWork(state, configs, supplypg.GORMModelTxFromContext, options.Admission)
	deps.ResourceAdmitter = supply.NewResourceAdmissionService(admissionWork, options.Clock)
	bindingWork := supplypg.NewResourceBindingWork(state, configs, supplypg.GORMModelTxFromContext, options.Admission.Proof)
	deps.ResourceConfigBinder = supply.NewResourceConfigBindingService(bindingWork, options.Clock)

	planHooks := &supplypg.PlanHooks{
		Resolve: supplypg.GORMModelTxFromContext, PendingConfiguration: options.PendingConfiguration,
		Log: options.ResourceLog, Outbox: options.Admission.Outbox,
	}
	planStore, err := supplypg.NewPlanStore(options.Schema, read, state, planHooks)
	if err != nil {
		return nil, err
	}
	deps.Plans = supply.NewPlanReader(planStore)
	planWriter := supply.NewPlanMaintenanceService(planStore, options.Clock)
	deps.PlanWriter = planWriter
	deps.PricePlanWriter = planWriter
	planExecution := supplypg.NewPlanExecutionWork(planStore, configs, options.Admission.Report)
	deps.PlanExecutor = supply.NewPlanExecutionService(planExecution, options.Clock)

	deps.Notices = supply.NewNoticeReader(supplypg.NewNoticeReadStore(options.DB))
	noticeWork := supplypg.NewNoticeUnitOfWork(supplypg.GORMModelTxFromContext, options.NoticeLog)
	deps.NoticeWriter = supply.NewNoticeService(noticeWork, options.Clock)

	creationWork := supplypg.NewResourceCreationWork(state, supplypg.GORMModelTxFromContext, options.ResourceCreation)
	evidenceWork := supplypg.NewTestEvidenceWork(state, supplypg.GORMModelTxFromContext, options.TestEvidenceAuthority)
	return &SupplyRouteModule{
		Deps: deps, ResourceCreation: supply.NewResourceCreationService(creationWork, options.Clock),
		TestEvidence: supply.NewTestEvidenceService(evidenceWork, options.Clock),
	}, nil
}

// NewStandardModelSupplyDeps builds A's standard-model reader and writer over
// the native 33/34 schema. It does not register public routes or substitute
// missing authentication, logging, source-policy, or idempotency resources.
func NewStandardModelSupplyDeps(options StandardModelSupplyOptions) (SupplyDeps, error) {
	if options.DB == nil || options.IdemStore == nil || options.SourcePolicy == nil {
		return SupplyDeps{}, supply.ErrDependencyUnavailable
	}
	idemGORM := options.IdemStore.DB()
	if idemGORM == nil {
		return SupplyDeps{}, supply.ErrDependencyUnavailable
	}
	idemDB, err := idemGORM.DB()
	if err != nil || idemDB != options.DB {
		return SupplyDeps{}, supply.ErrDependencyUnavailable
	}
	repository, err := supplypg.NewModelRepository(options.Schema, supplypg.ModelReadTransactions(options.DB))
	if err != nil {
		return SupplyDeps{}, err
	}
	work, err := supplypg.NewModelUnitOfWork(options.Schema, supplypg.GORMModelTxFromContext, supplypg.AppendModelOperationLog)
	if err != nil {
		return SupplyDeps{}, err
	}
	return SupplyDeps{
		Reader:           supply.NewModelReader(repository),
		Writer:           supply.NewModelService(work, options.SourcePolicy, options.Clock),
		SourceOperations: supply.NewSourceOperationReader(repository),
		IdemStore:        options.IdemStore,
	}, nil
}
