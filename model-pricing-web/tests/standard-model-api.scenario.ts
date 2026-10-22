import test from "node:test";
import assert from "node:assert/strict";
import { setupServer } from "msw/node";
import { http, HttpResponse } from "msw";
import { createPinia, setActivePinia } from "pinia";
import { standardModelsApi } from "../src/api/standardModels";

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
