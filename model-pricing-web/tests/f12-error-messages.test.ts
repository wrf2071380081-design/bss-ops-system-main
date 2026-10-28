import assert from "node:assert/strict";
import test from "node:test";
import { f12ErrorMessage } from "../src/domain/f12Errors.ts";

function apiError(status: number, code: string | number, message: string) {
  return Object.assign(new Error(message), { name: "ApiError", httpStatus: status, code });
}

test("F12 write dependency failure explains the blocker and preserved draft", () => {
  const error = apiError(503, 10000, "服务暂时不可用，请稍后重试。");
  const message = f12ErrorMessage(error, "合同资料保存失败。");
  assert.match(message, /结构或办理授权依赖可能尚未就绪/);
  assert.match(message, /表单和已选附件已保留/);
});

test("F12 attachment download dependency failure has a file-specific message", () => {
  const error = apiError(503, "10000", "服务暂时不可用，请稍后重试。");
  assert.equal(f12ErrorMessage(error, "附件下载失败。", "download"), "合同与授信附件服务暂不可用，请稍后重试。");
});

test("F12 keeps conflict and ordinary server errors actionable without rewriting them", () => {
  const conflict = apiError(409, 10005, "当前数据状态已发生变化，请刷新后重新操作。");
  assert.equal(f12ErrorMessage(conflict, "合同资料保存失败。"), conflict.message);
  assert.equal(f12ErrorMessage(new Error("网络错误"), "合同资料保存失败。"), "网络错误");
  assert.equal(f12ErrorMessage(null, "合同资料保存失败。"), "合同资料保存失败。");
});
