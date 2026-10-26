import test from "node:test";
import assert from "node:assert/strict";
import { redactSensitive } from "../src/domain/sensitive.ts";

test("API error payloads redact a supplier test API key", () => {
  assert.deepEqual(redactSensitive({ test_api_key: "supplier-test-secret" }), {
    test_api_key: "************",
  });
});
