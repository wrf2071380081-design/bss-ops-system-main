import test from "node:test";
import assert from "node:assert/strict";
import { setupServer } from "msw/node";
import { http, HttpResponse } from "msw";
import { createPinia, setActivePinia } from "pinia";
import { useSessionStore } from "../src/stores/session";
import { resourceOperationsApi } from "../src/api/resourceOperations";

const resource = {
  id: "9007199254740993", supplier_id: "21", supplier_name: "供应商甲", standard_model_id: "11",
  model_name: "示例模型", official_id: "example-v1", company: "示例公司", model_type: "TEXT",
  region: "CN", supplier_model_id: "vendor-model-v1", coefficient: "0.6800", buyer_id: "7",
  state: "PENDING_ACCESS", ever_admitted: false, revision: "2", formal_config_id: "91", formal_config_revision: "4",
  test: { report_id: "51", status: "COMPLETED", connectivity_passed: true, pressure_passed: true, valid: true,
    cache_stats: { status: "NOT_PROVIDED" }, metrics: { connection_ms: "0.00", rps: "23.0", tps: "12.0" } },
  official_price_items: [], quote_readiness: { eligible: false, blocked_reasons: ["MODEL_NOT_READY"], resource_revision: "2", model_version_no: "1", formal_config_revision: "4", test_evidence_ref: "51" },
  permissions: { can_edit: false }, created_at: "2026-10-20T01:00:00Z", updated_at: "2026-10-20T01:00:00Z",
};

const config = {
  id: "91", supplier_id: "21", region: "CN", scope: "生产接口", base_url: "https://api.vendor.example/v1",
  has_api_key: true, api_key_mask: "********", status: "ACTIVE", revision: "4", created_by: "7", updated_by: "7",
  created_at: "2026-10-20T01:00:00Z", updated_at: "2026-10-20T01:00:00Z", confirmed_by: "7",
  confirmed_at: "2026-10-20T01:00:00Z", confirmed_revision: "4",
};

test("resource admission and formal-config binding use exact A wire contracts and retain unknown idempotent attempts", async () => {
  setActivePinia(createPinia());
  const session = useSessionStore();
  session.user = { id: "7", name: "办理人甲", roleLabel: "内部用户" };
  session.portal = "internal";
  session.token = "test-token";
  const storage = new Map<string, string>();
  Object.defineProperty(globalThis, "sessionStorage", { configurable: true, value: {
    getItem: (key: string) => storage.get(key) ?? null,
    setItem: (key: string, value: string) => storage.set(key, value),
    removeItem: (key: string) => storage.delete(key),
  } });
  let admissionAttempt = 0;
  const admissionRequests: Array<{ body: unknown; key: string | undefined }> = [];
  let bindingRequest: { body: unknown; key: string | undefined } | undefined;
  const server = setupServer(
    http.get("http://localhost/api/internal/suppliers/21/formal-configs", () => HttpResponse.json({
      code: 0, message: "ok", requestId: "formal-config-list", data: [config],
    })),
    http.post("http://localhost/api/internal/resources/9007199254740993/admit", async ({ request }) => {
      admissionRequests.push({ body: await request.json(), key: request.headers.get("Idempotency-Key") ?? undefined });
      admissionAttempt++;
      if (admissionAttempt === 1) return HttpResponse.json({ code: 10000, message: "temporarily unavailable" }, { status: 503 });
      return HttpResponse.json({ code: 0, message: "ok", requestId: "admission", data: {
        resource_id: resource.id, state: "ADMITTED", ever_admitted: true, revision: "3", updated_at: "2026-10-21T01:00:00Z",
      } });
    }),
    http.put("http://localhost/api/internal/resources/9007199254740993/formal-config", async ({ request }) => {
      bindingRequest = { body: await request.json(), key: request.headers.get("Idempotency-Key") ?? undefined };
      return HttpResponse.json({ code: 0, message: "ok", requestId: "binding", data: {
        resource_id: resource.id, formal_config_id: "91", formal_config_revision: "4", resource_revision: "3",
        changed: true, updated_at: "2026-10-21T01:00:00Z",
      } });
    }),
  );
  server.listen({ onUnhandledRequest: "error" });
  try {
    const configs = await resourceOperationsApi.formalConfigs(resource.supplier_id, resource.region);
    assert.equal(configs.length, 1);
    assert.equal(configs[0]?.id, "91");
    assert.equal("api_key" in (configs[0] as unknown as Record<string, unknown>), false);

    const admission = { externalConfirmed: true, completionNote: "已按计划完成外部配置", reason: "登记资源接入" };
    await assert.rejects(resourceOperationsApi.admit(resource, admission, "admit-attempt-1"));
    const receipt = await resourceOperationsApi.admit(resource, admission, "admit-attempt-1");
    assert.deepEqual(admissionRequests.map(item => item.body), [
      { expected_revision: "2", external_confirmed: true, completion_note: admission.completionNote, reason: admission.reason },
      { expected_revision: "2", external_confirmed: true, completion_note: admission.completionNote, reason: admission.reason },
    ]);
    assert.ok(admissionRequests[0]?.key);
    assert.equal(admissionRequests[0]?.key, admissionRequests[1]?.key);
    assert.equal(receipt.revision, "3");

    const binding = await resourceOperationsApi.bindFormalConfig(resource, configs[0]!, "绑定已核验配置", "bind-attempt-1");
    assert.deepEqual(bindingRequest?.body, {
      expected_revision: "2", formal_config_id: "91", expected_config_revision: "4", reason: "绑定已核验配置",
    });
    assert.ok(bindingRequest?.key);
    assert.equal(binding.resource_revision, "3");
  } finally {
    server.close();
  }
});
