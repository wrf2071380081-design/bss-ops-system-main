import { http, HttpResponse } from "msw";
import type {
  SupplierSubmissionDTO,
  SupplierSubmissionRowDTO,
  SupplierSubmissionTestDTO,
} from "../../api/supplierSubmissions.types";
import type { SupplierTestCredentialDTO } from "../../api/supplierTestCredential";

/**
 * 供应商申报 + 主体级测试凭据的 Mock（000044，原型对齐）。
 *
 * 之前 Mock 里完全没有 /supplier/submissions：mock 模式下点开「申报记录」就 401
 * 被踢回登录页，等于这条链路没法在本地验证。这里补一份内存实现，
 * 规则与后端保持一致（门禁看覆盖不看通过、凭据主体级、Key 不回显），
 * 只用来跑页面，不追求边界完备。
 */

const requestId = () => `mock-${crypto.randomUUID()}`;
const result = (data: unknown) => HttpResponse.json({ code: 0, message: "ok", data, requestId: requestId() });
const fail = (status: number, message: string) => HttpResponse.json({ code: 10005, message, data: null, requestId: requestId() }, { status });

function rowFixture(id: string, model: string, official: string, overrides: Partial<SupplierSubmissionRowDTO> = {}): SupplierSubmissionRowDTO {
  return {
    id,
    interface_model_id: `${official}-supplier`,
    standard_model_id: id,
    standard_model_name: model,
    official_id: official,
    coefficient: "0.68",
    test_base_url: "demo",
    test_api_key_configured: true,
    row_status: "UNTESTED",
    participate_test: true,
    connectivity_tested: false,
    connectivity_passed: false,
    stress_tested: false,
    stress_passed: false,
    ...overrides,
  };
}

const passedJob = (category: "CONNECTIVITY" | "STRESS"): SupplierSubmissionTestDTO => ({
  category,
  status: "SUCCEEDED",
  tested: true,
  passed: category === "CONNECTIVITY",
  verdict: category === "CONNECTIVITY" ? "pass" : "",
  connection_ms: category === "CONNECTIVITY" ? "87.00" : null,
  rps: category === "STRESS" ? "28.40" : null,
  tps: category === "STRESS" ? "24500.00" : null,
});

const now = () => new Date().toISOString();

const submissions: SupplierSubmissionDTO[] = [
  {
    id: "101",
    submission_no: "S00000101",
    submitter_name: "陈经理",
    region: "CN",
    kind: "MODEL_NEW",
    status: "PENDING_APPROVAL",
    revision: "2",
    reason: "",
    row_count: 1,
    model_names: ["GPT-4.1"],
    pending_row_count: 0,
    rows: [rowFixture("201", "GPT-4.1", "gpt-4.1", {
      row_status: "PASSED", connectivity_tested: true, connectivity_passed: true,
      stress_tested: true, stress_passed: true, connectivity_job: passedJob("CONNECTIVITY"), stress_job: passedJob("STRESS"),
    })],
    permissions: { can_edit: false, can_submit: false, can_withdraw: true },
    created_at: "2026-10-26T10:00:00+08:00",
    updated_at: "2026-10-26T10:00:00+08:00",
    submitted_at: "2026-10-26T10:00:00+08:00",
  },
  {
    id: "102",
    submission_no: "S00000102",
    submitter_name: "陈经理",
    region: "CN",
    kind: "MODEL_NEW",
    status: "REJECTED",
    revision: "3",
    reason: "补充说明：已按驳回意见调整系数",
    row_count: 2,
    model_names: ["Qwen3-32B", "DeepSeek-V3"],
    pending_row_count: 1,
    rows: [
      rowFixture("202", "Qwen3-32B", "Qwen3-32B", {
        row_status: "PASSED", connectivity_tested: true, connectivity_passed: true,
        stress_tested: true, stress_passed: true, connectivity_job: passedJob("CONNECTIVITY"), stress_job: passedJob("STRESS"),
      }),
      rowFixture("203", "DeepSeek-V3", "deepseek-chat", {
        participate_test: false, row_status: "FAILED", connectivity_tested: true, connectivity_passed: false,
        connectivity_job: { category: "CONNECTIVITY", status: "SUCCEEDED", tested: true, passed: false, verdict: "fail", connection_ms: "320.00" },
      }),
    ],
    permissions: { can_edit: true, can_submit: true, can_withdraw: false },
    created_at: "2026-10-24T09:00:00+08:00",
    updated_at: "2026-10-25T09:00:00+08:00",
    submitted_at: "2026-10-24T09:00:00+08:00",
  },
  {
    id: "103",
    submission_no: "S00000103",
    submitter_name: "陈经理",
    region: "CN",
    kind: "MODEL_NEW",
    status: "DRAFT",
    revision: "1",
    reason: "",
    row_count: 1,
    model_names: ["GPT Image 1"],
    pending_row_count: 0,
    rows: [rowFixture("204", "GPT Image 1", "gpt-image-1")],
    permissions: { can_edit: true, can_submit: true, can_withdraw: true },
    created_at: "2026-10-27T09:00:00+08:00",
    updated_at: "2026-10-27T09:00:00+08:00",
    submitted_at: null,
  },
  {
    id: "104",
    submission_no: "S00000104",
    submitter_name: "陈经理",
    region: "CN",
    kind: "MODEL_NEW",
    status: "WITHDRAWN",
    revision: "4",
    reason: "报价口径调整，撤回重报",
    row_count: 1,
    model_names: ["Gemini 2.5 Pro"],
    pending_row_count: 0,
    rows: [rowFixture("205", "Gemini 2.5 Pro", "gemini-2.5-pro", {
      row_status: "PASSED", connectivity_tested: true, connectivity_passed: true,
      stress_tested: true, stress_passed: true, connectivity_job: passedJob("CONNECTIVITY"), stress_job: passedJob("STRESS"),
    })],
    permissions: { can_edit: true, can_submit: true, can_withdraw: false },
    created_at: "2026-10-23T14:00:00+08:00",
    updated_at: "2026-10-23T16:00:00+08:00",
    submitted_at: "2026-10-23T14:00:00+08:00",
  },
  {
    id: "105",
    submission_no: "S00000105",
    submitter_name: "陈经理",
    region: "OVERSEAS",
    kind: "MODEL_NEW",
    status: "APPROVED",
    revision: "2",
    reason: "",
    row_count: 1,
    model_names: ["GPT-4o Mini TTS"],
    pending_row_count: 0,
    rows: [rowFixture("206", "GPT-4o Mini TTS", "gpt-4o-mini-tts", {
      row_status: "PASSED", connectivity_tested: true, connectivity_passed: true,
      stress_tested: true, stress_passed: true, connectivity_job: passedJob("CONNECTIVITY"), stress_job: passedJob("STRESS"),
    })],
    permissions: { can_edit: false, can_submit: false, can_withdraw: false },
    created_at: "2026-10-20T11:00:00+08:00",
    updated_at: "2026-10-21T11:00:00+08:00",
    submitted_at: "2026-10-20T11:00:00+08:00",
  },
];

let credential: SupplierTestCredentialDTO = {
  base_url: "demo",
  has_api_key: true,
  configured: true,
  updated_at: "2026-10-27T09:00:00+08:00",
};

const modelOptions = [
  { id: "11", name: "GPT-4.1", official_id: "gpt-4.1", company: "OpenAI" },
  { id: "12", name: "Qwen3-32B", official_id: "Qwen3-32B", company: "阿里云" },
  { id: "13", name: "DeepSeek-V3", official_id: "deepseek-chat", company: "DeepSeek" },
  { id: "14", name: "GPT Image 1", official_id: "gpt-image-1", company: "OpenAI" },
  { id: "15", name: "Gemini 2.5 Pro", official_id: "gemini-2.5-pro", company: "Google" },
  { id: "16", name: "GPT-4o Mini TTS", official_id: "gpt-4o-mini-tts", company: "OpenAI" },
];

function credentialDTO(): SupplierTestCredentialDTO {
  return { ...credential };
}

/** 模型管理列表的演示数据（结构与 SupplierModelResourceDTO 一致）。 */
const supplierModelFixtures = [
  {
    id: "3", standard_model_id: "15", model_name: "Claude Sonnet 4", official_id: "claude-sonnet-4",
    supplier_model_id: "demo/deepseek-v4", region: "CN", coefficient: "0.6000", state: "ADMITTED",
    ever_admitted: true, revision: "1",
    test: { report_id: "51", status: "COMPLETED", connectivity_tested: true, connectivity_passed: true, stress_tested: true, stress_passed: true, valid: true, connection_ms: "87.00", rps: "28.40", tps: "24500.00" },
    quote_readiness: { eligible: true, blocked_reasons: [] },
  },
  {
    id: "8", standard_model_id: "12", model_name: "Qwen3 Max", official_id: "qwen3-max",
    supplier_model_id: "demo/rowB", region: "CN", coefficient: "0.7000", state: "PENDING_TEST",
    ever_admitted: false, revision: "1",
    test: { report_id: "52", status: "COMPLETED", connectivity_tested: true, connectivity_passed: true, stress_tested: true, stress_passed: false, valid: true, connection_ms: "96.00", rps: "0", tps: "0" },
    quote_readiness: { eligible: false, blocked_reasons: ["RESOURCE_NOT_ADMITTED", "TESTS_NOT_PASSED"] },
  },
  {
    id: "9", standard_model_id: "14", model_name: "GPT-4o mini", official_id: "gpt-4o-mini",
    supplier_model_id: "demo-model-1", region: "OVERSEAS", coefficient: "1.0000", state: "PENDING_TEST",
    ever_admitted: false, revision: "1",
    test: { status: "NOT_TESTED", connectivity_tested: false, connectivity_passed: false, stress_tested: false, stress_passed: false, valid: false },
    quote_readiness: { eligible: false, blocked_reasons: ["RESOURCE_NOT_ADMITTED", "TESTS_REQUIRED"] },
  },
];

function findSubmission(id: string) { return submissions.find(item => item.id === id); }

function rowReady(row: SupplierSubmissionRowDTO) {
  return Boolean(row.connectivity_tested);
}

function matches(row: SupplierSubmissionRowDTO, filters: URLSearchParams) {
  const values = (key: string) => (filters.get(key) || "").split(",").map(item => item.trim()).filter(Boolean);
  const company = values("company");
  const type = values("model_type");
  const option = modelOptions.find(item => item.name === row.standard_model_name);
  if (company.length && !company.includes(option?.company || "")) return false;
  if (type.length && !type.includes("TEXT")) return false;
  return true;
}

export const supplierSubmissionHandlers = [
  // 供应商模型管理（2026-10-28）：按原型 7 列所需字段造数据，测试结论覆盖
  // 已接入可报价 / 待接入有报告 / 待接入无报告 三种态。
  http.get("/api/supplier/models", () =>
    result({
      total: supplierModelFixtures.length,
      page: 1,
      size: 100,
      list: structuredClone(supplierModelFixtures),
    }),
  ),

  http.get("/api/supplier/test-credential", () => result(credentialDTO())),

  http.put("/api/supplier/test-credential", async ({ request }) => {
    const body = (await request.json()) as { base_url?: string; api_key?: string };
    const url = (body.base_url || "").trim();
    if (!url) return fail(400, "请填写测试接口 URL");
    if (!/^https?:\/\//i.test(url) && url !== "demo") return fail(400, "测试接口 URL 必须是 http/https 地址");
    credential = {
      base_url: url,
      has_api_key: body.api_key?.trim() ? true : credential.has_api_key,
      configured: true,
      updated_at: now(),
    };
    return result(credentialDTO());
  }),

  http.get("/api/supplier/submissions/model-options", ({ request }) => {
    const keyword = (new URL(request.url).searchParams.get("keyword") || "").toLowerCase();
    const list = keyword
      ? modelOptions.filter(item => `${item.name}${item.official_id}`.toLowerCase().includes(keyword))
      : modelOptions;
    return result({ list });
  }),

  http.get("/api/supplier/submissions/filter-options", () =>
    result({
      companies: [...new Set(modelOptions.map(item => item.company))],
      model_types: ["TEXT"],
      regions: ["CN"],
      submitters: ["陈经理"],
      statuses: ["DRAFT", "PENDING_TEST", "PENDING_APPROVAL", "APPROVED", "REJECTED", "WITHDRAWN"],
      kinds: ["MODEL_NEW", "PRICE_CHANGE"],
    }),
  ),

  http.get("/api/supplier/submissions", ({ request }) => {
    const url = new URL(request.url);
    const page = Number(url.searchParams.get("page") || 1);
    const size = Number(url.searchParams.get("size") || 100);
    const values = (key: string) => (url.searchParams.get(key) || "").split(",").map(item => item.trim()).filter(Boolean);
    const status = values("status");
    const kind = values("kind");
    const submitter = values("submitter");
    const region = values("region");
    const filtered = submissions.filter(item => {
      if (status.length && !status.includes(item.status)) return false;
      if (kind.length && !kind.includes(item.kind)) return false;
      if (submitter.length && !submitter.includes(item.submitter_name || "")) return false;
      if (region.length && !region.includes(item.region || "CN")) return false;
      return item.rows.some(row => matches(row, url.searchParams));
    });
    return result({ list: filtered.slice((page - 1) * size, page * size), total: filtered.length, page, size });
  }),

  http.get("/api/supplier/submissions/:id", ({ params }) => {
    const record = findSubmission(String(params.id));
    return record ? result(structuredClone(record)) : fail(404, "数据不存在或当前账号不可见");
  }),

  http.post("/api/supplier/submissions", async ({ request }) => {
    const body = (await request.json()) as { kind: "MODEL_NEW" | "PRICE_CHANGE"; reason?: string; rows: Array<Partial<SupplierSubmissionRowDTO>> };
    const id = String(200 + submissions.length + 1);
    const rows = body.rows.map((row, index) => rowFixture(String(300 + index), modelOptions.find(item => item.id === row.standard_model_id)?.name || "标准模型", modelOptions.find(item => item.id === row.standard_model_id)?.official_id || "model", {
      interface_model_id: row.interface_model_id || "",
      standard_model_id: row.standard_model_id || "",
      standard_model_name: modelOptions.find(item => item.id === row.standard_model_id)?.name || "",
      coefficient: row.coefficient || "1",
      participate_test: !!row.participate_test,
      test_base_url: credential.base_url,
    }));
    const record: SupplierSubmissionDTO = {
      id, submission_no: `S${id.padStart(8, "0")}`, submitter_name: "", region: "CN",
      kind: body.kind, status: "DRAFT", revision: "1", reason: body.reason || "",
      row_count: rows.length, model_names: rows.map(row => row.standard_model_name), pending_row_count: 0, rows,
      permissions: { can_edit: true, can_submit: true, can_withdraw: true },
      created_at: now(), updated_at: now(), submitted_at: null,
    };
    submissions.unshift(record);
    return result(structuredClone(record));
  }),

  http.put("/api/supplier/submissions/:id", async ({ params, request }) => {
    const record = findSubmission(String(params.id));
    if (!record) return fail(404, "数据不存在");
    const body = (await request.json()) as { reason?: string; rows: Array<Partial<SupplierSubmissionRowDTO>> };
    record.reason = body.reason || "";
    record.rows = body.rows.map((row, index) => {
      const existing = record.rows.find(item => item.id === row.id) || rowFixture(String(400 + index), "", "");
      const option = modelOptions.find(item => item.id === row.standard_model_id);
      return {
        ...existing,
        interface_model_id: row.interface_model_id || "",
        standard_model_id: row.standard_model_id || "",
        standard_model_name: option?.name || existing.standard_model_name,
        official_id: option?.official_id || existing.official_id,
        coefficient: row.coefficient || "1",
        participate_test: !!row.participate_test,
      };
    });
    record.row_count = record.rows.length;
    record.model_names = record.rows.map(row => row.standard_model_name);
    record.revision = String(Number(record.revision) + 1);
    record.updated_at = now();
    return result(structuredClone(record));
  }),

  http.post("/api/supplier/submissions/:id/tests", ({ params }) => {
    const record = findSubmission(String(params.id));
    if (!record) return fail(404, "数据不存在");
    let started = 0;
    for (const row of record.rows) {
      if (!row.connectivity_job) { row.connectivity_job = passedJob("CONNECTIVITY"); row.connectivity_tested = true; row.connectivity_passed = true; started++; }
      if (row.participate_test && !row.stress_job) { row.stress_job = passedJob("STRESS"); row.stress_tested = true; row.stress_passed = true; started++; }
      row.row_status = "PASSED";
    }
    return result({ categories: ["CONNECTIVITY", "STRESS"], items: [], started, skipped: 0, retried: 0, not_selected: 0, failed: 0 });
  }),

  http.post("/api/supplier/submissions/:id/submit", ({ params }) => {
    const record = findSubmission(String(params.id));
    if (!record) return fail(404, "数据不存在");
    const missing = record.rows.filter(row => !row.connectivity_job).length;
    if (missing > 0) return fail(409, `还有 ${missing} 个模型没有连通性测试结果，请先发起测试`);
    const selected = record.rows.filter(row => row.participate_test);
    if (!selected.length) return fail(409, "请至少勾选一个压力测试模型后再提交");
    const pendingStress = selected.filter(row => !row.stress_job).length;
    if (pendingStress > 0) return fail(409, `勾选的模型里还有 ${pendingStress} 个没有压力测试结果，请先发起测试`);
    record.status = "PENDING_APPROVAL";
    record.submitted_at = now();
    record.submitter_name = "陈经理";
    record.permissions = { can_edit: false, can_submit: false, can_withdraw: true };
    for (const row of record.rows) {
      row.row_status = row.connectivity_passed ? "PASSED" : "SUPPLEMENT";
      row.supplement_required = !row.connectivity_passed;
    }
    record.pending_row_count = record.rows.filter(row => row.supplement_required).length;
    return result({ id: record.id, revision: record.revision, status: record.status });
  }),

  http.post("/api/supplier/submissions/:id/withdraw", ({ params }) => {
    const record = findSubmission(String(params.id));
    if (!record) return fail(404, "数据不存在");
    record.status = "WITHDRAWN";
    record.revision = String(Number(record.revision) + 1);
    record.permissions = { can_edit: true, can_submit: true, can_withdraw: false };
    return result({ id: record.id, revision: record.revision, status: record.status });
  }),
];
