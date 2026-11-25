import test from "node:test";
import assert from "node:assert/strict";
import { expandCatalogDifferences, catalogValue, catalogDifferenceLabel, catalogReviewRows } from "../src/api/catalogReviewPresentation.ts";

test('limit history matches parameters independently of evidence and displays only actual business changes',()=>{
  const input={field:'maxInput',value:'65536',unit:'TOKEN',semantics:'UNSPECIFIED',rawLabel:'最大输入长度 65,536',rawValue:'65,536',sha256:'old',evidenceId:'ev-old'};
  const context={...input,field:'contextLength'};
  const before=[input,context];
  const after=[context,input].map(row=>({...row,sha256:'new',evidenceId:'ev-new',rawLabel:'输入上限',rawValue:'65536'}));
  assert.deepEqual(expandCatalogDifferences([{field:'limits',old:before,new:after}]),[]);
  const fields=expandCatalogDifferences([{field:'limits',old:before,new:after.map(row=>row.field==='maxInput'?{...row,value:'32768'}:row)}]);
  assert.equal(fields.length,1);
  assert.equal(catalogDifferenceLabel(fields[0]!.field),'限制条件 / 最大输入');
  assert.equal(catalogValue(fields[0]!.old),'65,536 Token');
  assert.equal(catalogValue(fields[0]!.new),'32,768 Token');
  assert.doesNotMatch(catalogValue(fields[0]!.old),/sha256|evidenceId|UNSPECIFIED|rawLabel/);
  const conditioned=[{...input,condition:'标准模式'},{...input,value:'32768',condition:'思考模式'}];
  assert.deepEqual(expandCatalogDifferences([{field:'limits',old:conditioned,new:[...conditioned].reverse()}]),[]);
  assert.equal(expandCatalogDifferences([{field:'limits',old:conditioned,new:[conditioned[0],{...conditioned[1],condition:'扩展模式'}]}]).length,1);
});

test('unchanged model keeps the same field groups as a changed model', () => {
  const current = {name:'DeepSeek Flash', 'capacity.context_length':{key:'context_length',value:'1M'}};
  const rows = catalogReviewRows(current, current, []);
  assert.equal(rows.length, 2);
  assert.ok(rows.every(row => row.kind === 'UNCHANGED'));
  const changes = [{field:'name',old:'DeepSeek Flash',new:'DeepSeek Flash New',kind:'CHANGED'}];
  const changed = catalogReviewRows(current, {...current,name:'DeepSeek Flash New'}, changes);
  assert.equal(changed.filter(row => row.kind === 'CHANGED').length, 1);
  assert.equal(changed.find(row => row.field === 'capacity.context_length')?.kind, 'UNCHANGED');
});

test("history details expose changed nested values and preserve zero and false", () => {
  const fields = expandCatalogDifferences([{ field: "details", old: { concurrencyLimit: "10", enabled: true, rpm: 0 },
    new: { concurrencyLimit: "20", enabled: false, rpm: 5 } }]);
  assert.deepEqual(fields.map(row => [row.field, row.old, row.new]), [
    ["details.concurrencyLimit", "10", "20"], ["details.enabled", true, false], ["details.rpm", 0, 5],
  ]);
  assert.equal(catalogDifferenceLabel(fields[0]!.field), "访问与规格详情 / 并发上限");
  assert.equal(catalogValue(false), "不支持");
  assert.equal(catalogValue(0), "0");
});
test("added and removed details and structured arrays remain readable", () => {
  assert.deepEqual(expandCatalogDifferences([{ field: "details", old: null, new: { rpm: "100" } }]),
    [{ field: "details.rpm", old: null, new: "100" }]);
  assert.deepEqual(expandCatalogDifferences([{ field: "details", old: { rpm: "100" }, new: null }])[0]?.new, null);
  assert.equal(catalogValue([{ amount: "0.5", currency: "USD" }]).includes("0.5"), true);
  assert.equal(catalogValue([{ amount: "0.5" }]).includes("[object Object]"), false);
  assert.deepEqual(expandCatalogDifferences([{ field: "details", old: { rpm: "10" }, new: { rpm: "10" } }]), []);
});
test("checksum-only refresh has no business difference while real specifications remain visible", () => {
  const rows = [{ field: "details", old: { metadataSha256: "old", concurrencyLimit: "10" }, new: { metadataSha256: "new", concurrencyLimit: "20" } }];
  assert.deepEqual(expandCatalogDifferences(rows).map(row => row.field), ["details.concurrencyLimit"]);
  assert.equal(expandCatalogDifferences(rows, true).length, 2);
  assert.deepEqual(expandCatalogDifferences([{ field: "details", old: { metadataSha256: "old" }, new: { metadataSha256: "new" } }]), []);
});
