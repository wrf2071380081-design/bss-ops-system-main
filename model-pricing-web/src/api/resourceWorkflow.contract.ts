export type ApprovedPriceOption = { source_row_id: string; source_revision: string; coefficient: string };
export type ResourceWorkflowActions = { can_bind: boolean; can_admit: boolean; can_refresh_tests: boolean; approved_prices: ApprovedPriceOption[] };
const positive = (v: unknown): v is string => typeof v === 'string' && /^[1-9]\d*$/.test(v) && BigInt(v) <= 9223372036854775807n;
const coefficient = (v: unknown): v is string => typeof v === 'string' && /^(?:0(?:\.\d{1,4})?|1(?:\.0{1,4})?)$/.test(v) && Number(v) > 0;
export function parseResourceWorkflow(input: unknown): ResourceWorkflowActions {
 const v = input as ResourceWorkflowActions;
 if (!v || typeof v.can_bind !== 'boolean' || typeof v.can_admit !== 'boolean' || !Array.isArray(v.approved_prices)) throw new Error('办理信息格式不正确，请刷新后重试。');
 const seen = new Set<string>();
 const options = v.approved_prices.map(p => {
  if (!p || !positive(p.source_row_id) || !positive(p.source_revision) || !coefficient(p.coefficient) || seen.has(p.source_row_id)) throw new Error('批准改价来源格式不正确，请刷新后重试。');
  seen.add(p.source_row_id);return {source_row_id:p.source_row_id,source_revision:p.source_revision,coefficient:p.coefficient};
 });
 if (v.can_refresh_tests!==undefined&&typeof v.can_refresh_tests!=='boolean')throw new Error('测试同步权限格式不正确。');
 return {can_bind:v.can_bind,can_admit:v.can_admit,can_refresh_tests:v.can_refresh_tests===true,approved_prices:options};
}
export function buildApprovedPrice(revision: string, source: ApprovedPriceOption, confirmed: boolean, note: string, reason: string) {
 if (!positive(revision) || BigInt(revision) >= 9223372036854775807n || !positive(source?.source_row_id) || !positive(source?.source_revision) || !coefficient(source?.coefficient)) throw new Error('请刷新资源及批准改价来源。');
 if (!confirmed || !note.trim() || !reason.trim()) throw new Error('请确认外部改价完成，填写完成依据和操作原因。');
 return {expected_revision:revision,source_row_id:source.source_row_id,source_revision:source.source_revision,coefficient:source.coefficient,external_confirmed:true,completion_note:note.trim(),reason:reason.trim()};
}
