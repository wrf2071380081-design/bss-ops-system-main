import assert from "node:assert/strict";
import test from "node:test";
import { formatSupplierTaxRate } from "../src/domain/supplierTax.ts";

test("supplier tax ratios display as percentages without floating point artifacts", () => {
  for (const [ratio, expected] of [["0.06", "6%"], ["0.0600", "6%"], ["0.0625", "6.25%"], ["0.29", "29%"], ["1", "100%"]]) {
    assert.equal(formatSupplierTaxRate(ratio), expected);
  }
});

test("missing supplier tax stays distinct from a real zero", () => {
  for (const missing of [null, undefined, "", " "]) assert.equal(formatSupplierTaxRate(missing), "—");
  for (const zero of ["0", "0.0000"]) assert.equal(formatSupplierTaxRate(zero), "0%");
});
