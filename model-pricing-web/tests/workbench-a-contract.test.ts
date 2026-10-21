import test from "node:test";
import assert from "node:assert/strict";
import { noticeImpactText, validateNoticeView, validateNoticePage, validateReadReceipt, validatePlanSummary } from "../src/api/supplyWorkbench.contract.ts";

export const noticeFixture = () => ({ notice: { id: "91", resource_id: "9007199254740993", resource_name: "示例模型", region: "CN", kind: "OFFLINE", title: "供应资源已下架", content: "原客户配置已停止，保留历史；不会自动切换供应商。", reason: "供应商维护", affected_status: "PENDING", published_at: "2026-10-20T01:00:00Z" }, staff_id: "7", staff_name: "采购甲", read_status: "UNREAD", read_at: null, permissions: { can_mark_read: true } });
export const planFixture = () => ({ id: "81", resource_id: "9007199254740993", region: "CN", kind: "OFFLINE", status: "PENDING", created_by: "8", creator_name: "采购乙", revision: "1", expected_resource_revision: "2", due_at: "2027-01-01T01:00:00Z", executed_at: null, created_at: "2026-10-20T01:00:00Z", reason: "计划维护", is_initiator: false, read_only: true, precondition_matches: false, current_resource_revision: "3", blocked_reasons: ["RESOURCE_CHANGED"] });

test("notice projection keeps selected person and unread state without mutation", () => {
  const fixture = noticeFixture();
  const result = validateNoticeView(fixture, "91", "7");
  result.notice.title = "changed";
  assert.equal(fixture.notice.title, "供应资源已下架");
  assert.equal(fixture.read_at, null);
  assert.throws(() => validateNoticeView(fixture, "91", "8"));
  assert.throws(() => validateNoticeView({ ...fixture, read_status: "READ", read_at: "2026-10-20T02:00:00Z" }));
  assert.throws(() => validateNoticeView({ ...fixture, staff_id: 7 }));
});

test("impact count distinguishes unknown from exact zero and preserves bigint", () => {
  const fixture = noticeFixture();
  assert.equal(noticeImpactText(validateNoticeView(fixture).notice), "影响范围待计算");
  for (const count of ["0", "9007199254740993"]) {
    const result = validateNoticeView({ ...fixture, notice: { ...fixture.notice, affected_status: "READY", affected_customer_count: count } });
    assert.equal(noticeImpactText(result.notice), `${count} 个关联客户`);
  }
  assert.throws(() => validateNoticeView({ ...fixture, notice: { ...fixture.notice, affected_status: "READY" } }));
  assert.throws(() => validateNoticeView({ ...fixture, notice: { ...fixture.notice, affected_customer_count: "0" } }));
});

test("notice pages validate resource, pagination and unique notice/person pairs", () => {
  const row = noticeFixture(), query = { resource_id: row.notice.resource_id, region: "CN", page: 1, size: 20 };
  assert.equal(validateNoticePage({ list: [row], total: 1, page: 1, size: 20 }, query).total, 1);
  assert.throws(() => validateNoticePage({ list: [row, row], total: 2, page: 1, size: 20 }, query));
  assert.throws(() => validateNoticePage({ list: [row], total: 1, page: 2, size: 20 }, query));
  assert.throws(() => validateNoticePage({ list: [row], total: 1, page: 1, size: 20 }, { ...query, resource_id: "12" }));
});

test("read receipt cannot acknowledge another person or notice", () => {
  const receipt = { notice_id: "91", staff_id: "7", read_at: "2026-10-20T02:00:00Z", created: false };
  assert.equal(validateReadReceipt(receipt, "91", "7").read_at, receipt.read_at);
  assert.throws(() => validateReadReceipt(receipt, "91", "8"));
  assert.throws(() => validateReadReceipt(receipt, "92", "7"));
  assert.throws(() => validateReadReceipt({ ...receipt, read_at: "invalid" }, "91", "7"));
});

test("plan remains readonly with future date and consistent revision hint", () => {
  const fixture = planFixture();
  assert.equal(validatePlanSummary(fixture, "81").due_at, fixture.due_at);
  assert.throws(() => validatePlanSummary({ ...fixture, read_only: false }, "81"));
  assert.throws(() => validatePlanSummary({ ...fixture, precondition_matches: true }, "81"));
  assert.throws(() => validatePlanSummary({ ...fixture, status: "EXECUTED" }, "81"));
  assert.throws(() => validatePlanSummary(fixture, "82"));
});
