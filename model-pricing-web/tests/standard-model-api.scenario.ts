import test from "node:test";
import assert from "node:assert/strict";
import { setupServer } from "msw/node";
import { http, HttpResponse } from "msw";
import { createPinia, setActivePinia } from "pinia";
import { standardModelsApi } from "../src/api/standardModels";
import { emptyStandardModel } from "../src/api/standardModels.contract";
import { useSessionStore } from "../src/stores/session";

const idempotencyStorage = new Map<string, string>();
Object.defineProperty(globalThis, "sessionStorage", { configurable: true, value: {
  getItem: (key: string) => idempotencyStorage.get(key) ?? null,
  setItem: (key: string, value: string) => idempotencyStorage.set(key, value),
  removeItem: (key: string) => idempotencyStorage.delete(key),
} });

test("standard model operation history uses the authenticated workbench operation contract", async () => {
  setActivePinia(createPinia());
  let query: URLSearchParams | undefined;
  const server = setupServer(http.get("http://localhost/api/internal/workbench/operations", ({ request }) => {
    query = new URL(request.url).searchParams;
    return HttpResponse.json({
      code: 0,
      message: "ok",
      requestId: "standard-model-operation-history",
      data: {
        list: [{ id: 1, action: "VERIFY", entity_type: "standard_model", entity_id: 42, operator_name: "操作员甲", reason: "人工核验", created_at: "2026-10-20T01:00:00Z" }],
        total: 1,
        page: 1,
        size: 100,
      },
    });
  }));
  server.listen({ onUnhandledRequest: "error" });
  try {
    const result = await standardModelsApi.records("9007199254740993");
    assert.deepEqual(Object.fromEntries(query ?? []), {
      entity_type: "standard_model",
      entity_id: "9007199254740993",
      page: "1",
      size: "100",
    });
    assert.deepEqual(result, [{
      action: "VERIFY",
      reason: "人工核验",
      operator_name: "操作员甲",
      created_at: "2026-10-20T01:00:00Z",
    }]);
  } finally {
    server.close();
  }
});

test("manual completion uses one versioned save endpoint for create and update", async () => {
  setActivePinia(createPinia());
  const session = useSessionStore();
  session.user = { id: "7", name: "维护人", roleLabel: "内部用户" };
  session.portal = "internal";
  const requests: { method: string; path: string; body: Record<string, unknown>; key: string | undefined }[] = [];
  const draft = { ...emptyStandardModel(), name: "待补齐模型" };
  const response = {
    ...draft,
    id: "17", maintainer_id: "7", revision: "2", version_no: "2", status: "ACTIVE", verify_status: "INCOMPLETE", checked_by: "",
    created_at: "2026-10-20T01:00:00Z", updated_at: "2026-10-20T01:00:00Z", missing_fields: ["company"],
    quote_readiness: { ready: false, reasons: ["MODEL_INCOMPLETE", "MODEL_UNVERIFIED"], standard_model_version_no: "2" },
    permissions: { can_edit: true, can_verify: false },
  };
  const handler = async ({ request }: { request: Request }) => {
    requests.push({ method: request.method, path: new URL(request.url).pathname, body: await request.json() as Record<string, unknown>, key: request.headers.get("idempotency-key") ?? undefined });
    return HttpResponse.json({ code: 0, message: "ok", requestId: "manual-save", data: response });
  };
  const server = setupServer(http.post("http://localhost/api/internal/standard-models/manual-completion", handler), http.put("http://localhost/api/internal/standard-models/17/manual-completion", handler));
  server.listen({ onUnhandledRequest: "error" });
  try {
    await standardModelsApi.manualSave(draft, "  补充资料  ", "new-draft");
    await standardModelsApi.manualSave(draft, "补充资料", "edit-draft", { ...response, revision: "1", version_no: "1", quote_readiness: { ...response.quote_readiness, standard_model_version_no: "1" } });
    assert.equal(requests.length, 2);
    assert.deepEqual(requests.map(item => [item.method, item.path]), [
      ["POST", "/api/internal/standard-models/manual-completion"],
      ["PUT", "/api/internal/standard-models/17/manual-completion"],
    ]);
    assert.equal(requests[0]!.body.reason, "补充资料");
    assert.equal("expected_revision" in requests[0]!.body, false);
    assert.equal(requests[1]!.body.expected_revision, "1");
    assert.ok(requests.every(item => item.key));
  } finally {
    server.close();
  }
});
