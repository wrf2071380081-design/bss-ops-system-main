package supplypg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"model_bss/internal/domain/supply"
	"os"
	"strings"
	"testing"
	"time"
)

func TestResourceConfigBindingPostgres34(t *testing.T) {
	name := os.Getenv("SUPPLY_SCHEMA_TEST_DB")
	if name == "" {
		t.Skip("explicit dedicated PostgreSQL test required")
	}
	if !strings.HasPrefix(name, "model_bss_a_schema34_") || os.Getenv("PGDATABASE") != name {
		t.Fatal("dedicated database required")
	}
	db, err := sql.Open("pgx", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var actual string
	if err := db.QueryRowContext(ctx, "SELECT current_database()").Scan(&actual); err != nil || actual != name {
		t.Fatal("wrong database")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, table := range []string{"standard_model", "standard_model_version", "supplier", "supplier_formal_config", "assignment_log", "operation_log", "resource"} {
		if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE `+table+` (LIKE public.`+table+` INCLUDING DEFAULTS INCLUDING IDENTITY INCLUDING CONSTRAINTS) ON COMMIT DROP`); err != nil {
			t.Fatal(err)
		}
	}
	for _, ddl := range []string{
		`ALTER TABLE pg_temp.resource ADD PRIMARY KEY(id),ADD UNIQUE(supplier_id,standard_model_id,region)`,
		`INSERT INTO pg_temp.supplier(id,name,region,created_by) OVERRIDING SYSTEM VALUE VALUES(21,'绑定供应商','CN',7)`,
		`INSERT INTO pg_temp.assignment_log(entity_type,entity_id,field,new_staff_id,changed_by) VALUES('supplier',21,'buyer_id',7,7)`,
	} {
		if _, err := tx.ExecContext(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	dropLog := false
	log := func(c context.Context, current *sql.Tx, e ResourceStateLog) error {
		if current != tx {
			t.Fatal("logger escaped shared transaction")
		}
		if dropLog {
			return nil
		}
		var before any
		if len(e.Before) > 0 {
			before = string(e.Before)
		}
		_, err := current.ExecContext(c, `INSERT INTO pg_temp.operation_log(entity_type,entity_id,action,operator_id,"before","after",reason,request_id,created_at) VALUES($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7,$8,$9)`, e.EntityType, e.EntityID, e.Action, e.ActorID, before, string(e.After), e.Reason, e.RequestID, e.OccurredAt)
		return err
	}
	state, err := NewResourceStateStore("pg_temp", log)
	if err != nil {
		t.Fatal(err)
	}
	configs, err := NewFormalConfigStore("pg_temp", func(c context.Context, tx *sql.Tx, e FormalConfigLog) error { return log(c, tx, ResourceStateLog(e)) })
	if err != nil {
		t.Fatal(err)
	}
	resolve := func(context.Context) (*sql.Tx, error) { return tx, nil }
	now := time.Date(2026, 10, 21, 7, 0, 0, 0, time.UTC)
	mw, err := NewModelUnitOfWork("pg_temp", resolve, func(context.Context, *sql.Tx, supply.ModelOperation) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	buyer := supply.WriteMeta{ActorID: "7", Reason: "外部配置已确认", RequestID: "binding-real-sql"}
	model, err := supply.NewModelService(mw, nil, func() time.Time { return now }).Create(ctx, buyer, supply.ModelData{Name: "绑定模型", Company: "Example", OfficialID: "binding", ModelType: "TEXT"})
	if err != nil {
		t.Fatal(err)
	}
	configService := supply.NewFormalConfigService(NewFormalConfigWork(configs, resolve), configPGURL{}, func() time.Time { return now })
	secret := "formal-key-not-from-submission"
	input := supply.FormalConfigInput{Scope: "全部模型", BaseURL: "https://formal.example/v1", APIKey: &secret, ExternalConfirmed: true}
	config, err := configService.Create(ctx, buyer, "21", input)
	if err != nil {
		t.Fatal(err)
	}
	resource := &supply.Resource{ID: "11", SupplierID: "21", StandardModelID: model.ID, BuyerID: "7", Region: "CN", SupplierModelID: "vendor-binding", Coefficient: "0.6800", State: "PENDING_TEST", Revision: 1, OriginRowID: "71", OriginRowVersion: 2, TargetVersion: 3, CreatedAt: now, UpdatedAt: now}
	if _, err := state.Insert(ctx, tx, resource, buyer, "CREATE"); err != nil {
		t.Fatal(err)
	}
	source := &supply.AdmissionProof{ResourceID: "11", SupplierID: "21", Region: "CN", OriginRowID: "71", OriginRowVersion: 2, TargetVersion: 3, FlowID: "81", NodeID: "82", FlowRevision: 4, AssigneeID: "8", FlowInProgress: true, NodeInProgress: true, PrerequisitesReady: true}
	work := NewResourceBindingWork(state, configs, resolve, func(c context.Context, current *sql.Tx, r *supply.Resource) (*supply.AdmissionProof, error) {
		if current != tx || r.ID != "11" {
			t.Fatal("proof snapshot changed")
		}
		copy := *source
		return &copy, nil
	})
	service := supply.NewResourceConfigBindingService(work, func() time.Time { return now })
	actor := supply.WriteMeta{ActorID: "8", Reason: "配置节点登记正式关联", RequestID: "bind-by-assignee"}
	cmd := supply.ResourceConfigBindingCommand{ResourceID: "11", ConfigID: config.ID, ExpectedRevision: 1, ExpectedConfigRevision: 1}
	for _, failure := range []string{"wrong_actor", "wrong_target", "stale_config", "empty_logger", "outer_rollback"} {
		t.Run(failure, func(t *testing.T) {
			if _, err := tx.ExecContext(ctx, "SAVEPOINT binding_failure"); err != nil {
				t.Fatal(err)
			}
			defer func() {
				dropLog = false
				source.TargetVersion = 3
				if _, err := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT binding_failure"); err != nil {
					t.Fatal(err)
				}
			}()
			meta := actor
			command := cmd
			switch failure {
			case "wrong_actor":
				meta.ActorID = "7"
			case "wrong_target":
				source.TargetVersion = 4
			case "stale_config":
				command.ExpectedConfigRevision = 2
			case "empty_logger":
				dropLog = true
			}
			got, err := service.BindFormalConfig(ctx, meta, command)
			switch failure {
			case "wrong_actor":
				if !errors.Is(err, supply.ErrForbidden) || got != nil {
					t.Fatal("buyer substituted for assignee", err)
				}
			case "wrong_target", "stale_config":
				if !errors.Is(err, supply.ErrRevisionConflict) || got != nil {
					t.Fatal("stale proof/config accepted", err)
				}
			case "empty_logger":
				if !errors.Is(err, supply.ErrReadContract) || got != nil {
					t.Fatal("empty logger accepted", err)
				}
			case "outer_rollback":
				if err != nil || got == nil || !got.Changed {
					t.Fatalf("real binding unavailable: %v", err)
				}
			}
		})
		current, err := state.Current(ctx, tx, "11")
		if err != nil || current.Revision != 1 || current.FormalConfigID != "" {
			t.Fatal("failed binding escaped rollback", err)
		}
	}
	bound, err := service.BindFormalConfig(ctx, actor, cmd)
	if err != nil || bound == nil {
		t.Fatalf("native binding unavailable: %v", err)
	}
	if !bound.Changed || bound.Resource.Revision != 2 || bound.Resource.State != "PENDING_TEST" || bound.Resource.BuyerID != "7" || bound.Resource.FormalConfigID != config.ID || bound.Resource.FormalConfigRevision != 1 {
		t.Fatalf("binding changed unrelated fields %+v", bound)
	}
	cmd.ExpectedRevision = 2
	repeat, err := service.BindFormalConfig(ctx, actor, cmd)
	if err != nil || repeat.Changed || repeat.Resource.Revision != 2 {
		t.Fatal("binding NOOP changed revision", err)
	}
	if _, err := configService.Disable(ctx, buyer, "21", config.ID, 1); !errors.Is(err, supply.ErrConfigInUse) {
		t.Fatal("pending resource reference did not block disable", err)
	}
	var raw []byte
	if err := tx.QueryRowContext(ctx, `SELECT "after" FROM pg_temp.operation_log WHERE entity_type='resource' AND entity_id=11 AND action='RESOURCE_REVISION' ORDER BY id DESC LIMIT 1`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var logged struct {
		Proof struct {
			FlowID       string `json:"flow_id"`
			NodeID       string `json:"node_id"`
			FlowRevision string `json:"flow_revision"`
		} `json:"proof"`
	}
	if err := json.Unmarshal(raw, &logged); err != nil || logged.Proof.FlowID != "81" || logged.Proof.NodeID != "82" || logged.Proof.FlowRevision != "4" || strings.Contains(string(raw), "formal.example") || strings.Contains(string(raw), secret) {
		t.Fatal("binding proof omitted or credentials leaked", err)
	}
	input.APIKey = nil
	input.Scope = "修改后的适用范围"
	now = now.Add(time.Second)
	if changed, err := configService.Update(ctx, buyer, "21", config.ID, 1, input); err != nil || changed.Revision != 2 {
		t.Fatal("config update failed", err)
	}
	repo, err := NewResourceRepository("pg_temp", func(c context.Context, run func(*sql.Tx) error) error { return run(tx) }, state.FactsLoader(nil, configs.ResourceView))
	if err != nil {
		t.Fatal(err)
	}
	view, err := supply.NewResourceReader(repo).Get(ctx, "11", "CN")
	if err != nil || !strings.Contains(strings.Join(view.QuoteReadiness.BlockedReasons, ","), "FORMAL_CONFIG_CHANGED") {
		t.Fatal("old binding version followed new config silently", err)
	}
	if _, err := tx.ExecContext(ctx, "SAVEPOINT missing_config"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM pg_temp.supplier_formal_config WHERE id=$1`, config.ID); err != nil {
		t.Fatal(err)
	}
	missing, err := supply.NewResourceReader(repo).Get(ctx, "11", "CN")
	if err != nil || missing == nil || !strings.Contains(strings.Join(missing.QuoteReadiness.BlockedReasons, ","), "FORMAL_CONFIG_UNAVAILABLE") {
		t.Fatalf("missing config hid resource instead of blocking quote: %+v %v", missing, err)
	}
	if _, err := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT missing_config"); err != nil {
		t.Fatal(err)
	}
	cmd.ExpectedConfigRevision = 2
	rebound, err := service.BindFormalConfig(ctx, actor, cmd)
	if err != nil || rebound.Resource.Revision != 3 || rebound.Resource.FormalConfigRevision != 2 {
		t.Fatal("confirmed new config could not be rebound", err)
	}
}
