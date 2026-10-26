package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"model_bss/internal/api/middleware"
	infra "model_bss/internal/infra/middleware"
	"model_bss/internal/infra/supplypg"
)

func TestPriceChangePlanHTTPPostgres36CommitsExecutionAndBlockedReplay(t *testing.T) {
	name := os.Getenv("SUPPLY_SCHEMA_TEST_DB")
	if name == "" {
		t.Skip("explicit dedicated PostgreSQL test required")
	}
	if !strings.HasPrefix(name, "model_bss_a_schema34_") || os.Getenv("PGDATABASE") != name {
		t.Fatal("dedicated A v36 database required")
	}
	options := newCompositionOptions(t)
	raw := options.DB
	ctx := t.Context()
	var actual string
	if err := raw.QueryRowContext(ctx, `SELECT current_database()`).Scan(&actual); err != nil || actual != name {
		t.Fatalf("wrong test database %q: %v", actual, err)
	}
	label := fmt.Sprintf("price-plan-http-%d", time.Now().UnixNano())
	var orgID, actorID, assigneeID, modelID string
	var supplierIDs, resourceIDs, planIDs []string
	cleanup := func(query string, args ...any) {
		if _, err := raw.ExecContext(ctx, query, args...); err != nil {
			t.Errorf("cleanup failed: %v", err)
		}
	}
	defer func() {
		for _, id := range resourceIDs {
			cleanup(`DELETE FROM notice WHERE target_type='RESOURCE' AND target_id=$1`, id)
			cleanup(`DELETE FROM operation_log WHERE entity_type='resource' AND entity_id=$1`, id)
			cleanup(`DELETE FROM plan WHERE resource_id=$1`, id)
		}
		for _, id := range planIDs {
			cleanup(`DELETE FROM operation_log WHERE entity_type='plan' AND entity_id=$1`, id)
		}
		cleanup(`DELETE FROM event_outbox WHERE request_id LIKE $1`, label+"%")
		cleanup(`DELETE FROM idempotency_key WHERE request_id LIKE $1`, label+"%")
		for _, id := range resourceIDs {
			cleanup(`DELETE FROM resource WHERE id=$1`, id)
		}
		for _, id := range supplierIDs {
			cleanup(`DELETE FROM supplier WHERE id=$1`, id)
		}
		if modelID != "" {
			cleanup(`DELETE FROM standard_model WHERE id=$1`, modelID)
		}
		for _, id := range []string{assigneeID, actorID} {
			if id != "" {
				cleanup(`DELETE FROM internal_staff WHERE id=$1`, id)
			}
		}
		if orgID != "" {
			cleanup(`DELETE FROM org_unit WHERE id=$1`, orgID)
		}
	}()
	if err := raw.QueryRowContext(ctx, `INSERT INTO org_unit(name,path) VALUES($1,$2) RETURNING id::text`, label, "/"+label+"/").Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	newStaff := func(suffix string) string {
		var id string
		nonce := fmt.Sprint(time.Now().UnixNano())
		if err := raw.QueryRowContext(ctx, `INSERT INTO internal_staff(org_unit_id,name,mobile) VALUES($1,$2,$3) RETURNING id::text`, orgID, label+" "+suffix, string(suffix[0])+nonce[len(nonce)-17:]).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	actorID, assigneeID = newStaff("buyer"), newStaff("executor")
	if err := raw.QueryRowContext(ctx, `INSERT INTO standard_model(company,model_type,name,official_id) VALUES('Example','TEXT',$1,$2) RETURNING id::text`, label+" model", label+"-official").Scan(&modelID); err != nil {
		t.Fatal(err)
	}
	resourceCreatedAt := time.Now().UTC().Truncate(time.Microsecond)
	seedResource := func(index int, coefficient string) string {
		var supplierID, resourceID string
		if err := raw.QueryRowContext(ctx, `INSERT INTO supplier(name,region) VALUES($1,'CN') RETURNING id::text`, fmt.Sprintf("%s supplier %d", label, index)).Scan(&supplierID); err != nil {
			t.Fatal(err)
		}
		supplierIDs = append(supplierIDs, supplierID)
		if err := raw.QueryRowContext(ctx, `INSERT INTO resource(standard_model_id,supplier_id,supplier_model_name,standard_model_name_snapshot,region,coefficient,state,ever_admitted,buyer_id,created_at,updated_at) VALUES($1,$2,$3,$4,'CN',$5,'ADMITTED',true,$6,$7,$7) RETURNING id::text`, modelID, supplierID, fmt.Sprintf("%s-vendor-%d", label, index), label+" model", coefficient, actorID, resourceCreatedAt).Scan(&resourceID); err != nil {
			t.Fatal(err)
		}
		resourceIDs = append(resourceIDs, resourceID)
		created := map[string]any{
			"id": resourceID, "supplier_id": supplierID, "standard_model_id": modelID, "region": "CN",
			"supplier_model_id": fmt.Sprintf("%s-vendor-%d", label, index), "coefficient": coefficient,
			"buyer_id": actorID, "state": "ADMITTED", "ever_admitted": true, "revision": "1",
			"formal_config_id": "", "formal_config_revision": "0", "origin_row_id": "1",
			"origin_row_version": "1", "target_version": "1", "latest_report_id": "",
			"latest_report_sequence": "0", "created_at": resourceCreatedAt, "updated_at": resourceCreatedAt,
		}
		insertResourceHead := func(snapshot map[string]any, requestID, reason string, occurredAt time.Time) {
			data, err := json.Marshal(map[string]any{"contract": "supply.resource", "schema_version": 1, "operation": "CREATE", "resource": snapshot})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := raw.ExecContext(ctx, `INSERT INTO operation_log(entity_type,entity_id,action,operator_id,"after",reason,request_id,created_at) VALUES('resource',$1,'RESOURCE_REVISION',$2,$3::jsonb,$4,$5,$6)`, resourceID, actorID, string(data), reason, requestID, occurredAt); err != nil {
				t.Fatal(err)
			}
		}
		insertResourceHead(created, label+fmt.Sprintf("-resource-%d", index), "isolated QA resource seed", resourceCreatedAt)
		return resourceID
	}
	resourceOK := seedResource(1, "0.7000")
	resourceConflict := seedResource(2, "0.7000")

	options.ResourceLog = func(c context.Context, tx *sql.Tx, e supplypg.ResourceStateLog) error {
		var before any
		if len(e.Before) != 0 {
			before = string(e.Before)
		}
		_, err := tx.ExecContext(c, `INSERT INTO operation_log(entity_type,entity_id,action,operator_id,"before","after",reason,request_id,created_at) VALUES($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7,$8,$9)`, e.EntityType, e.EntityID, e.Action, e.ActorID, before, string(e.After), e.Reason, e.RequestID, e.OccurredAt)
		return err
	}
	options.Admission.Outbox = func(c context.Context, tx *sql.Tx, e supplypg.AdmissionOutboxEntry) (string, error) {
		var id string
		err := tx.QueryRowContext(c, `INSERT INTO event_outbox(event_type,payload,created_at,updated_at,request_id,created_by,updated_by) VALUES($1,$2::jsonb,$3,$3,$4,$5,$5) RETURNING id::text`, e.EventType, string(e.Payload), e.OccurredAt, e.RequestID, e.ActorID).Scan(&id)
		return id, err
	}
	module, err := NewSupplyRouteModule(options)
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(infra.RequestID())
	var currentActor int64
	internal := router.Group("/api/internal")
	internal.Use(func(c *gin.Context) {
		c.Set("auth.operator", &middleware.Operator{PortalType: "INTERNAL", OperatorType: "STAFF", StaffID: currentActor, OperatorID: currentActor})
		c.Next()
	})
	RegisterSupplyRoutes(internal, module.Deps)
	actorNumber, _ := strconv.ParseInt(actorID, 10, 64)
	assigneeNumber, _ := strconv.ParseInt(assigneeID, 10, 64)
	call := func(actor int64, method, path, body, key string) *httptest.ResponseRecorder {
		currentActor = actor
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", key)
		req.Header.Set("X-Request-ID", key)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	createPlan := func(resourceID, suffix string) (string, *httptest.ResponseRecorder) {
		targetModelID := label + "-vendor-target-" + suffix
		body := fmt.Sprintf(`{"resource_id":%q,"supplier_model_id":%q,"expected_resource_revision":"1","after_coefficient":"0.7000","assignee_id":%q,"due_at":%q,"reason":"%s"}`, resourceID, targetModelID, assigneeID, time.Now().Add(time.Hour).UTC().Format(time.RFC3339), label+" supplier model ID change")
		key := label + "-create-" + suffix
		res := call(actorNumber, http.MethodPost, "/api/internal/plans/price-change", body, key)
		if res.Code != http.StatusOK {
			t.Fatalf("create price plan status=%d body=%s", res.Code, res.Body)
		}
		var envelope struct {
			Data struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &envelope); err != nil || envelope.Data.ID == "" {
			t.Fatalf("invalid create response %s: %v", res.Body, err)
		}
		planIDs = append(planIDs, envelope.Data.ID)
		return envelope.Data.ID, res
	}

	planOK, _ := createPlan(resourceOK, "success")
	executeBody := `{"expected_revision":"1","confirmed":true,"completion_note":"QA ONLY 已完成外部改价登记","reason":"隔离库改价执行"}`
	executeKey := label + "-execute-success"
	executed := call(assigneeNumber, http.MethodPost, "/api/internal/plans/"+planOK+"/execute", executeBody, executeKey)
	if executed.Code != http.StatusOK {
		t.Fatalf("successful execution status=%d body=%s", executed.Code, executed.Body)
	}
	var coefficient, supplierModelID string
	var resourceRevision int
	var nativePlanStatus string
	if err := raw.QueryRowContext(ctx, `SELECT coefficient::text FROM resource WHERE id=$1`, resourceOK).Scan(&coefficient); err != nil {
		t.Fatal(err)
	}
	if err := raw.QueryRowContext(ctx, `SELECT supplier_model_name FROM resource WHERE id=$1`, resourceOK).Scan(&supplierModelID); err != nil {
		t.Fatal(err)
	}
	if err := raw.QueryRowContext(ctx, `SELECT ("after"->'resource'->>'revision')::int FROM operation_log WHERE entity_type='resource' AND entity_id=$1 AND action='RESOURCE_REVISION' ORDER BY id DESC LIMIT 1`, resourceOK).Scan(&resourceRevision); err != nil {
		t.Fatal(err)
	}
	var targetVersion string
	if err := raw.QueryRowContext(ctx, `SELECT "after"->'resource'->>'target_version' FROM operation_log WHERE entity_type='resource' AND entity_id=$1 AND action='RESOURCE_REVISION' ORDER BY id DESC LIMIT 1`, resourceOK).Scan(&targetVersion); err != nil {
		t.Fatal(err)
	}
	if err := raw.QueryRowContext(ctx, `SELECT status FROM plan WHERE id=$1`, planOK).Scan(&nativePlanStatus); err != nil {
		t.Fatal(err)
	}
	var costEvents, costNotices int
	if err := raw.QueryRowContext(ctx, `SELECT count(*) FROM event_outbox WHERE event_type='RESOURCE_COEFFICIENT_CHANGED' AND request_id=$1`, executeKey).Scan(&costEvents); err != nil {
		t.Fatal(err)
	}
	if err := raw.QueryRowContext(ctx, `SELECT count(*) FROM notice WHERE target_type='RESOURCE' AND target_id=$1 AND type='COST_CHANGE'`, resourceOK).Scan(&costNotices); err != nil {
		t.Fatal(err)
	}
	if coefficient != "0.7000" || supplierModelID != label+"-vendor-target-success" || targetVersion != "2" || resourceRevision != 2 || nativePlanStatus != "DONE" || costEvents != 1 || costNotices != 1 {
		t.Fatalf("execution transaction incomplete coefficient=%s modelID=%s targetVersion=%s revision=%d status=%s events=%d notices=%d", coefficient, supplierModelID, targetVersion, resourceRevision, nativePlanStatus, costEvents, costNotices)
	}

	planConflict, _ := createPlan(resourceConflict, "blocked")
	staleAt := resourceCreatedAt.Add(2 * time.Second)
	if _, err := raw.ExecContext(ctx, `UPDATE resource SET coefficient=0.6500,updated_at=$2 WHERE id=$1`, resourceConflict, staleAt); err != nil {
		t.Fatal(err)
	}
	changed := map[string]any{
		"id": resourceConflict, "supplier_id": supplierIDs[1], "standard_model_id": modelID, "region": "CN",
		"supplier_model_id": label + "-vendor-2", "coefficient": "0.6500", "buyer_id": actorID,
		"state": "ADMITTED", "ever_admitted": true, "revision": "2", "formal_config_id": "",
		"formal_config_revision": "0", "origin_row_id": "1", "origin_row_version": "1",
		"target_version": "1", "latest_report_id": "", "latest_report_sequence": "0",
		"created_at": resourceCreatedAt, "updated_at": staleAt,
	}
	changedJSON, err := json.Marshal(map[string]any{"contract": "supply.resource", "schema_version": 1, "operation": "UPDATE", "resource": changed})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.ExecContext(ctx, `INSERT INTO operation_log(entity_type,entity_id,action,operator_id,"after",reason,request_id,created_at) VALUES('resource',$1,'RESOURCE_REVISION',$2,$3::jsonb,'isolated concurrent resource update',$4,$5)`, resourceConflict, actorID, string(changedJSON), label+"-stale-resource", staleAt); err != nil {
		t.Fatal(err)
	}
	blockedKey := label + "-execute-blocked"
	blocked := call(assigneeNumber, http.MethodPost, "/api/internal/plans/"+planConflict+"/execute", executeBody, blockedKey)
	if blocked.Code != http.StatusConflict {
		t.Fatalf("blocked execution status=%d body=%s", blocked.Code, blocked.Body)
	}
	replayed := call(assigneeNumber, http.MethodPost, "/api/internal/plans/"+planConflict+"/execute", executeBody, blockedKey)
	var blockedJSON, replayedJSON any
	firstJSONErr := json.Unmarshal(blocked.Body.Bytes(), &blockedJSON)
	replayJSONErr := json.Unmarshal(replayed.Body.Bytes(), &replayedJSON)
	if replayed.Code != http.StatusConflict || firstJSONErr != nil || replayJSONErr != nil || !reflect.DeepEqual(replayedJSON, blockedJSON) {
		t.Fatalf("409 replay changed response first=%s replay=%s", blocked.Body, replayed.Body)
	}
	if err := raw.QueryRowContext(ctx, `SELECT status FROM plan WHERE id=$1`, planConflict).Scan(&nativePlanStatus); err != nil {
		t.Fatal(err)
	}
	if err := raw.QueryRowContext(ctx, `SELECT coefficient::text FROM resource WHERE id=$1`, resourceConflict).Scan(&coefficient); err != nil {
		t.Fatal(err)
	}
	if nativePlanStatus != "BLOCKED" || coefficient != "0.6500" {
		t.Fatalf("blocked replay changed resource or failed to persist BLOCKED: status=%s coefficient=%s", nativePlanStatus, coefficient)
	}
}
