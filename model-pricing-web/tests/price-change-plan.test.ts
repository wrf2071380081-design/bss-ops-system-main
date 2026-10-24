import test from "node:test";
import assert from "node:assert/strict";
import { buildPriceChangePlanCreate, buildPriceChangePlanRevision, validateCreatedPriceChangePlan, validateRevisedPriceChangePlan } from "../src/api/priceChangePlan.contract.ts";
import { buildPlanExecution } from "../src/api/planExecution.contract.ts";
import type { ResourceRow } from "../src/api/resources.types.ts";
import type { PlanSummary } from "../src/api/supplyWorkbench.types.ts";

const resource = (): ResourceRow => ({
  id: "31", supplier_id: "21", supplier_name: "供应商甲", standard_model_id: "11", model_name: "模型甲", official_id: "m-1", company: "厂商甲", model_type: "CHAT",
  region: "CN", supplier_model_id: "vendor-m1", coefficient: "0.6800", buyer_id: "7", state: "ADMITTED", ever_admitted: true, revision: "4", formal_config_revision: "2",
  test: { status: "COMPLETED", connectivity_passed: true, pressure_passed: true, valid: true, cache_stats: { status: "MISSING" }, metrics: { connection_ms: null, rps: null, tps: null } },
  quote_readiness: { eligible: true, blocked_reasons: [], resource_revision: "4", model_version_no: "1", formal_config_revision: "2" }, official_price_items: [],
  permissions: { can_edit: true }, created_at: "2026-10-20T01:00:00Z", updated_at: "2026-10-20T01:00:00Z",
});
const plan = (): PlanSummary => ({
  id: "81", resource_id: "31", region: "CN", kind: "PRICE_CHANGE", status: "PENDING", created_by: "7", assignee_id: "8", supplier_model_id: "vendor-m2", before_supplier_model_id: "vendor-m1", after_supplier_model_id: "vendor-m2", creator_name: "采购甲", revision: "3",
  expected_resource_revision: "4", before_coefficient: "0.6800", after_coefficient: "0.5000", current_resource_revision: "4", due_at: "2026-10-24T01:00:00Z", executed_at: null,
  created_at: "2026-10-20T01:00:00Z", reason: "采购调整", is_initiator: true, read_only: true, precondition_matches: true, blocked_reasons: [],
  permissions: { can_revise: true, can_cancel: true, can_execute: false },
});

test("internal price plan create sends normalized coefficient and exact assigned operator", () => {
  const body = buildPriceChangePlanCreate(resource(), "vendor-m2", "0.5", "8", " 成本调整 ", "2026-10-25T09:00:00+08:00");
  assert.deepEqual(body, { resource_id: "31", supplier_model_id: "vendor-m2", expected_resource_revision: "4", after_coefficient: "0.5000", assignee_id: "8", due_at: "2026-10-25T01:00:00.000Z", reason: "成本调整" });
  const receipt = validateCreatedPriceChangePlan({ id: "81", created_by: "7", assignee_id: "8", resource_id: "31", supplier_model_id: "vendor-m2", before_supplier_model_id: "vendor-m1", after_supplier_model_id: "vendor-m2", region: "CN", kind: "PRICE_CHANGE", status: "PENDING", revision: "1", expected_resource_revision: "4", before_coefficient: "0.6800", after_coefficient: "0.5000", reason: "成本调整", created_at: "2026-10-20T01:00:00Z", due_at: "2026-10-25T01:00:00.000Z" }, resource(), body);
  assert.equal(receipt.id, "81");
  assert.equal(receipt.supplier_model_id, "vendor-m2");
  assert.throws(() => buildPriceChangePlanCreate(resource(), "vendor-m1", "0.68", "8", "成本调整", "2026-10-25T09:00:00+08:00"));
  assert.throws(() => buildPriceChangePlanCreate({ ...resource(), permissions: { can_edit: false } }, "vendor-m1", "0.5", "8", "成本调整", "2026-10-25T01:00:00Z"));
});

test("internal price plan can change supplier model ID without changing coefficient", () => {
  const body = buildPriceChangePlanCreate(resource(), "vendor-m2", "0.68", "8", "仅修改接口模型ID", "2026-10-25T09:00:00+08:00");
  assert.equal(body.supplier_model_id, "vendor-m2");
  assert.equal(body.after_coefficient, "0.6800");
  assert.throws(() => buildPriceChangePlanCreate(resource(), "vendor-m1", "0.68", "8", "无改动", "2026-10-25T09:00:00+08:00"));
});

test("assigned executor records completion; initiator revises but cannot execute another person's plan", () => {
  const current = plan();
  const body = buildPriceChangePlanRevision(current, "vendor-m2", "0.45", "9", "调整办理安排", "2026-10-26T09:00:00+08:00");
  assert.deepEqual(body, { expected_revision: "3", supplier_model_id: "vendor-m2", after_coefficient: "0.4500", assignee_id: "9", due_at: "2026-10-26T01:00:00.000Z", reason: "调整办理安排" });
  const receipt = validateRevisedPriceChangePlan({ ...current, supplier_model_id: "vendor-m2", before_supplier_model_id: "vendor-m1", after_supplier_model_id: "vendor-m2", revision: "4", assignee_id: "9", after_coefficient: "0.4500", due_at: body.due_at, reason: body.reason }, current, body);
  assert.equal(receipt.revision, "4");
  assert.throws(() => buildPriceChangePlanRevision(current, "vendor-m1", "0.68", "9", "退回原配置", "2026-10-26T09:00:00+08:00"));
  assert.throws(() => buildPlanExecution(current, true, "外部已改", "登记"));
  const executorView = { ...current, is_initiator: false, permissions: { can_revise: false, can_cancel: false, can_execute: true } };
  assert.equal(buildPlanExecution(executorView, true, "外部已改", "登记").expected_revision, "3");
});

test("plan revision can change only supplier model ID", () => {
  const current = plan();
  const body = buildPriceChangePlanRevision(current, "vendor-m3", "0.5", "9", "仅改接口模型ID", "2026-10-26T09:00:00+08:00");
  assert.equal(body.supplier_model_id, "vendor-m3");
  assert.equal(body.after_coefficient, "0.5000");
  const receipt = validateRevisedPriceChangePlan({ ...current, supplier_model_id: "vendor-m3", after_supplier_model_id: "vendor-m3", before_supplier_model_id: "vendor-m1", after_coefficient: "0.5000", revision: "4", assignee_id: "9", due_at: body.due_at, reason: body.reason }, current, body);
  assert.equal(receipt.after_supplier_model_id, "vendor-m3");
});
