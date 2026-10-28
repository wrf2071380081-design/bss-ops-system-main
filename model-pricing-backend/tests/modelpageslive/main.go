package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	_ "github.com/jackc/pgx/v5/stdlib"
	"golang.org/x/crypto/bcrypt"
	"model_bss/internal/domain/supply"
	"model_bss/internal/infra/supplypg"
	"model_bss/pkg/perm"
	"os"
	"strings"
	"time"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func main() {
	ctx := context.Background()
	const expected = "model_bss_a_schema34_20261026_models_backend"
	if os.Getenv("PGDATABASE") != expected || len(os.Getenv("BSS_TEST_PASSWORD")) < 16 {
		panic("dedicated database/password required")
	}
	db, err := sql.Open("pgx", "")
	must(err)
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	must(err)
	defer tx.Rollback()
	exec := func(q string, args ...any) { _, err := tx.ExecContext(ctx, q, args...); must(err) }
	scalar := func(q string, args ...any) string {
		var v string
		must(tx.QueryRowContext(ctx, q, args...).Scan(&v))
		return v
	}
	if scalar("SELECT current_database()") != expected || scalar("SELECT count(*)::text FROM account") != "0" {
		panic("empty dedicated database required")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(os.Getenv("BSS_TEST_PASSWORD")), bcrypt.MinCost)
	must(err)
	exec(`INSERT INTO org_unit(id,name,unit_type,path) OVERRIDING SYSTEM VALUE VALUES(1,'A页面真实联调','COMPANY','/1/')`)
	exec(`INSERT INTO internal_staff(id,org_unit_id,name,mobile) OVERRIDING SYSTEM VALUE VALUES(1,1,'A联调维护人','19900000101'),(2,1,'A联调只读人','19900000102')`)
	role := scalar(`INSERT INTO role(code,name,data_scope,is_functional) VALUES('PLATFORM_ADMIN','仅隔离库联调','ALL',true) RETURNING id::text`)
	for _, code := range perm.All() {
		pair := strings.Split(code, ":")
		id := scalar(`INSERT INTO permission_point(module_code,action_code,name) VALUES($1,$2,$3) RETURNING id::text`, pair[0], pair[1], "联调权限"+code)
		exec(`INSERT INTO role_permission(role_id,permission_point_id) VALUES($1,$2)`, role, id)
	}
	for i, login := range []string{"a_models_live", "a_models_reader"} {
		exec(`INSERT INTO account(portal_type,owner_type,owner_id,login_id,password_hash) VALUES('INTERNAL','STAFF',$1,$2,$3)`, i+1, login, string(hash))
		exec(`INSERT INTO role_grant(staff_id,role_id,granted_by) VALUES($1,$2,1)`, i+1, role)
	}
	subject := scalar(`INSERT INTO legal_subject(subject_type,legal_name,uscc,verification_status) VALUES('COMPANY','A联调供应商门户','QA20261026MODEL001','VERIFIED') RETURNING id::text`)
	operator := scalar(`INSERT INTO subject_operator(subject_id,name,mobile,is_primary) VALUES($1,'A联调供应商账号','19900000103',true) RETURNING id::text`, subject)
	exec(`INSERT INTO supplier_profile(subject_id,channel_type,qual_status,status,owner_procurement_operator_id) VALUES($1,'OFFICIAL','NORMAL','ACTIVE',1)`, subject)
	exec(`INSERT INTO account(portal_type,owner_type,owner_id,login_id,password_hash) VALUES('SUPPLIER','OPERATOR',$1,'a_models_supplier',$2)`, operator, string(hash))
	exec(`SELECT setval(pg_get_serial_sequence('standard_model','id'),9007199254741000,false)`)
	exec(`SELECT setval(pg_get_serial_sequence('operation_log','id'),9007199254742000,false)`)
	work, err := supplypg.NewModelUnitOfWork("public", func(context.Context) (*sql.Tx, error) { return tx, nil }, supplypg.AppendModelOperationLog)
	must(err)
	policy, err := supplypg.NewOfficialSourcePolicy(map[string][]string{"OpenAI": {"https://openai.com"}, "Anthropic": {"https://anthropic.com"}})
	must(err)
	svc := supply.NewModelService(work, policy, time.Now)
	meta := supply.WriteMeta{ActorID: "1", Reason: "隔离库真实接口联调测试资料，不代表B办理验收", RequestID: "a-model-pages-live-seed"}
	models := []*supply.StandardModel{}
	for i := 0; i < 25; i++ {
		company, site := "OpenAI", "https://openai.com/pricing"
		if i == 1 {
			company, site = "Anthropic", "https://anthropic.com/pricing"
		}
		p := supply.PriceItem{ItemKey: "input", Label: "输入", TaskType: "TEXT_INPUT", Amount: "0.00000000", Currency: "USD", UnitCode: "TOKEN", UnitQuantity: "1000000", Condition: "标准API", SourceURL: site}
		out := p
		out.ItemKey = "output"
		out.Label = "输出"
		out.TaskType = "TEXT_OUTPUT"
		out.Amount = "8.12345678"
		name := fmt.Sprintf("A联调-%02d模型", i)
		if i == 0 {
			name = "A联调-00历史名称"
		}
		data := supply.ModelData{Company: company, Name: name, OfficialID: fmt.Sprintf("a-models-live-%02d", i), ModelType: "TEXT", InputModalities: []string{"TEXT"}, OutputModalities: []string{"TEXT"}, Capacity: []supply.OfficialFact{{Key: "context", Disclosure: "DISCLOSED", Value: "128000 tokens"}}, Abilities: []supply.Ability{{Key: "tool_call", State: "SUPPORTED", Description: "测试资料"}}, PriceItems: []supply.PriceItem{p, out}, APIAccess: []supply.OfficialFact{{Key: "protocol", Disclosure: "DISCLOSED", Value: "HTTPS API"}}, Lifecycle: []supply.OfficialFact{{Key: "release", Disclosure: "UNDISCLOSED"}}, Sources: []supply.Source{{Label: "官方价格", URL: site, Kind: "OFFICIAL_PRICING"}}, CheckedAt: time.Now().Format("2006-01-02")}
		m, err := svc.SaveManual(ctx, meta, "", 0, data)
		must(err)
		models = append(models, m)
	}
	firstLog := scalar(`SELECT min(id)::text FROM operation_log WHERE entity_type='standard_model' AND entity_id=$1`, models[0].ID)
	for i := 0; i < 78; i++ {
		m := models[0]
		d := m.Clone().Data
		d.Name = "A联调-00主模型"
		d.APIAccess[0].Value = fmt.Sprintf("HTTPS API 联调版本%d", i)
		n, err := svc.SaveManual(ctx, meta, m.ID, m.Revision, d)
		must(err)
		models[0] = n
	}
	exec(`UPDATE operation_log SET "after"=jsonb_set("after"||'{"api_key":"QA_SENTINEL_DO_NOT_RETURN"}'::jsonb,'{data,password}','"QA_SENTINEL_DO_NOT_RETURN"'::jsonb) WHERE id=$1`, firstLog)
	suppliers := []string{}
	resourceIDs := []string{}
	reports := []string{}
	for i := 0; i < 25; i++ {
		region := "CN"
		if i >= 20 {
			region = "OVERSEAS"
		}
		id := scalar(`INSERT INTO supplier(name,region,created_by,updated_by) VALUES($1,$2,1,1) RETURNING id::text`, fmt.Sprintf("A联调供应商%02d", i+1), region)
		suppliers = append(suppliers, id)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	for i := 0; i < 49; i++ {
		modelIndex, supplierIndex := 0, i
		if i >= 25 {
			modelIndex = i - 24
			supplierIndex = 0
		}
		supplier := suppliers[supplierIndex]
		region := "CN"
		if supplierIndex >= 20 {
			region = "OVERSEAS"
		}
		state, ever := "PENDING_TEST", false
		if i == 0 || i == 1 {
			state, ever = "ADMITTED", true
		}
		if i == 2 {
			state, ever = "OFFLINE", true
		}
		if i == 3 {
			state = "PENDING_ACCESS"
		}
		coefficient := "0.8000"
		if i == 0 {
			coefficient = "0.5000"
		}
		if i == 1 {
			coefficient = "0.7000"
		}
		if i == 2 {
			coefficient = "0.9000"
		}
		r := map[string]any{"supplier_id": supplier, "standard_model_id": models[modelIndex].ID, "region": region, "supplier_model_id": fmt.Sprintf("a-live-vendor-%02d", i), "coefficient": coefficient, "buyer_id": "1", "state": state, "ever_admitted": ever, "revision": "1", "formal_config_id": "", "formal_config_revision": "0", "origin_row_id": fmt.Sprint(10000 + i), "origin_row_version": "1", "target_version": "1", "latest_report_id": "", "latest_report_sequence": "0", "created_at": now, "updated_at": now}
		native := state
		if state == "PENDING_ACCESS" {
			native = "PENDING_ADMIT"
		}
		id := scalar(`INSERT INTO resource(standard_model_id,supplier_id,supplier_model_name,standard_model_name_snapshot,region,coefficient,state,ever_admitted,buyer_id,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,1,$9,$9) RETURNING id::text`, models[modelIndex].ID, supplier, r["supplier_model_id"], models[modelIndex].Data.Name, region, coefficient, native, ever, now)
		r["id"] = id
		envelope := map[string]any{"contract": "supply.resource", "schema_version": 1, "operation": "READ_FIXTURE", "resource": r}
		if i < 3 {
			ms, rps, tps := "0", "0", "120000"
			if i == 1 {
				ms, rps, tps = "12", "200", "345"
			}
			if i == 2 {
				ms, rps, tps = "30", "300", "456"
			}
			job := scalar(`INSERT INTO test_job(resource_id,category,status,created_by,finished_at) VALUES($1,'stress','SUCCEEDED',1,$2) RETURNING id::text`, id, now)
			report := scalar(`INSERT INTO test_report(test_job_id,resource_id,connection_ms,rps,tps,connection_passed,stress_passed,cache_hit_tokens,cache_input_tokens,scene,tested_by,tested_at) VALUES($1,$2,$3,$4,$5,true,true,0,2000,'隔离数据库读取联调报告',1,$6) RETURNING id::text`, job, id, ms, rps, tps, now)
			r["latest_report_id"] = report
			r["latest_report_sequence"] = "1"
			envelope["test_evidence"] = map[string]any{"report_id": report, "sequence": "1", "origin_row_version": "1", "target_version": "1", "outcome": "SELECTED", "valid": true}
			exec(`UPDATE resource SET latest_test_report_id=$1 WHERE id=$2`, report, id)
			reports = append(reports, report)
		}
		if i == 0 {
			envelope["operation"] = "ADMIT"
			envelope["proof"] = map[string]any{"flow_id": "1", "node_id": "1", "assignee_id": "1", "flow_revision": "1"}
			envelope["completion_note"] = "隔离读取夹具，不代表实际B办理"
		}
		raw, err := json.Marshal(envelope)
		must(err)
		exec(`INSERT INTO operation_log(entity_type,entity_id,action,operator_id,operator_name_snapshot,"after",reason,request_id,created_at) VALUES('resource',$1,'RESOURCE_REVISION',1,'A联调维护人',$2::jsonb,$3,$4,$5)`, id, string(raw), meta.Reason, meta.RequestID, now)
		resourceIDs = append(resourceIDs, id)
	}
	must(tx.Commit())
	modelIDs := []string{}
	for _, m := range models {
		modelIDs = append(modelIDs, m.ID)
	}
	result := map[string]any{"database": expected, "model_ids": modelIDs, "main_model_id": models[0].ID, "historical_log_id": firstLog, "supplier_ids": suppliers, "resource_ids": resourceIDs, "report_ids": reports, "seed_models": 25, "seed_resources": 49, "seed_model_logs": 103}
	must(json.NewEncoder(os.Stdout).Encode(result))
}
