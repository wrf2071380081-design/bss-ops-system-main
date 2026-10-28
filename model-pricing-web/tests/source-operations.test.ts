import test from "node:test";
import assert from "node:assert/strict";
import { assertPlanContext, sourceOperationLocation, validateSourceOperations, supplierOperationLabel } from "../src/api/sourceOperations.contract.ts";
import type { PlanSummary } from "../src/api/supplyWorkbench.types.ts";
const row = { id:"9",entity_type:"plan" as const,entity_id:"9007199254740993",resource_id:"2",supplier_id:"3",supplier_name:"供应商",supplier_model_id:"api-model",region:"CN" as const,action:"CREATE",operator_name:"操作人",reason:"维护",created_at:"2026-10-22T01:00:00Z" };
test('resource stage logs use their actual business action labels',()=>{
 assert.match(supplierOperationLabel({...row,entity_type:'resource',action:'CONFIG_COMPLETE'}),/登记配置完成/);
 assert.match(supplierOperationLabel({...row,entity_type:'resource',action:'CANCEL_ACCESS'}),/取消接入/);
});
test("source log projection omits snapshots and links exact plan without losing bigint IDs", () => {
  const result = validateSourceOperations({ list:[{...row,after:{api_key:"private"}}],total:1,page:1,size:20 },1,20);
  assert.equal("after" in result.list[0]!,false);
  assert.equal(sourceOperationLocation(result.list[0]!).query.plan_id,"9007199254740993");
  assert.throws(() => validateSourceOperations({list:[row,row],total:2,page:1,size:20},1,20));
});
test("exact plan context rejects another resource or region before showing operations", () => {
  const plan = {resource_id:"2",region:"CN"} as PlanSummary;
  assert.equal(assertPlanContext(plan,"2","CN"),plan);
  assert.throws(() => assertPlanContext(plan,"4","CN"));
  assert.throws(() => assertPlanContext(plan,"2","OVERSEAS"));
});

test("supplier history includes non-resource categories without inventing a resource link", () => {
  for (const entityType of ["supplier", "supplier_qualification", "supplier_formal_config", "contract", "credit_line"] as const) {
    const subjectRow = { ...row, entity_type: entityType, entity_id: entityType === "supplier" ? "3" : "55", resource_id: "", supplier_model_id: "", action: "UPDATE", after: { api_key: "secret" } };
    const result = validateSourceOperations({ list: [subjectRow], total: 1, page: 1, size: 20 }, 1, 20, "3");
    assert.equal("after" in result.list[0]!, false);
    assert.equal(sourceOperationLocation(result.list[0]!).path, "/internal/suppliers/3/models");
    assert.equal(sourceOperationLocation(result.list[0]!).query.section, entityType);
    assert.match(supplierOperationLabel(result.list[0]!), /维护/);
    assert.throws(() => validateSourceOperations({ list: [subjectRow], total: 1, page: 1, size: 20 }, 1, 20));
    assert.throws(() => validateSourceOperations({ list: [subjectRow], total: 1, page: 1, size: 20 }, 1, 20, "4"));
  }
});
