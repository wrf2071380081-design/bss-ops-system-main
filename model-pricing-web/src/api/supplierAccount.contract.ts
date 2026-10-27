import type {
  SupplierAccountDTO,
  SupplierLicenseDTO,
  SupplierOperationLogDTO,
  SupplierOperationLogPage,
  SupplierRegion,
  UpdateSupplierAccountRequest,
} from "./supplierAccount.types";

/** 供应商账户设置的响应契约校验（与后端 internal/api/supplier_account.go 逐字段对齐）。
 *
 * 为什么要有这层：
 *   - `regionLocked` 是后端算的，前端**不得**用本地规则推断；字段缺失必须直接报错，
 *     否则页面会静默把锁定态渲染成可编辑（等于越权入口）。
 *   - `region` 只有 CN / OVERSEAS 两个取值，脏值会让「统一社会信用代码 / 当地企业注册编号」
 *     的 label 落到错误分支。
 *   - 操作记录是分页的，page/size 与请求不一致说明后端换了分页口径，宁可报错也不显示错页数据。
 */

const REGIONS: readonly SupplierRegion[] = ["CN", "OVERSEAS"];
const NAME_MAX = 128;
const REG_NO_MAX = 64;
const CONTACT_MAX = 64;

function invalid(message: string): never {
  throw new Error(message);
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

/** 主体属性取值守卫。 */
export function isSupplierRegion(value: unknown): value is SupplierRegion {
  return typeof value === "string" && (REGIONS as readonly string[]).includes(value);
}

/** 注册编号的 label 由**已保存**的 region 决定（原型实测：下拉切换不即时联动）。 */
export function registrationLabelOf(region: SupplierRegion): string {
  return region === "OVERSEAS" ? "当地企业注册编号" : "统一社会信用代码";
}

/** 主体属性 label：锁定时追加原因（原型「主体属性（有上架历史，已锁定）」）。 */
export function regionFieldLabelOf(account: Pick<SupplierAccountDTO, "regionLocked" | "regionLockReason">): string {
  return account.regionLocked ? `主体属性（${account.regionLockReason || "已锁定"}）` : "主体属性";
}

function validateLicense(value: unknown): SupplierLicenseDTO | null {
  if (value === null || value === undefined) return null;
  if (!isPlainObject(value)) invalid("账户档案响应不符合约定：license 结构异常。");
  const { name, fileRef, uploadedAt } = value as Record<string, unknown>;
  if (typeof name !== "string" || typeof fileRef !== "string" || !name.trim() || !fileRef.trim()) {
    invalid("账户档案响应不符合约定：企业证明字段缺失。");
  }
  if (uploadedAt !== undefined && uploadedAt !== null && !Number.isFinite(Date.parse(String(uploadedAt)))) {
    invalid("账户档案响应不符合约定：企业证明上传时间格式异常。");
  }
  return { name, fileRef, ...(uploadedAt ? { uploadedAt: String(uploadedAt) } : {}) };
}

/** 校验 GET/PUT 的账户档案响应；字段做显式投影，避免把后端多加的内部字段带进页面。 */
export function validateSupplierAccount(value: unknown): SupplierAccountDTO {
  if (!isPlainObject(value)) invalid("账户档案响应不符合约定，请重新加载。");
  const row = value as Record<string, unknown>;
  if (typeof row.id !== "string" || !row.id) invalid("账户档案响应不符合约定：缺少 id。");
  if (typeof row.name !== "string") invalid("账户档案响应不符合约定：缺少企业名称。");
  for (const key of ["registrationNo", "contact", "phone"] as const) {
    if (typeof row[key] !== "string") invalid(`账户档案响应不符合约定：${key} 类型异常。`);
  }
  if (!isSupplierRegion(row.region)) invalid("账户档案响应不符合约定：主体属性取值异常。");
  if (typeof row.regionLocked !== "boolean") invalid("账户档案响应不符合约定：缺少锁定标志。");
  if (row.regionLockReason !== undefined && typeof row.regionLockReason !== "string") {
    invalid("账户档案响应不符合约定：锁定原因类型异常。");
  }
  if (typeof row.updatedAt !== "string" || !Number.isFinite(Date.parse(row.updatedAt))) {
    invalid("账户档案响应不符合约定：更新时间格式异常。");
  }
  const license = validateLicense(row.license);
  return {
    id: row.id,
    name: row.name,
    registrationNo: row.registrationNo as string,
    contact: row.contact as string,
    phone: row.phone as string,
    region: row.region,
    regionLocked: row.regionLocked,
    ...(row.regionLockReason ? { regionLockReason: row.regionLockReason as string } : {}),
    license,
    updatedAt: row.updatedAt,
  };
}

/** 保存前的入场校验（与后端 normalizeAccountUpdate 同口径），返回裁剪后的请求体。 */
export function validateSupplierAccountUpdate(input: UpdateSupplierAccountRequest): UpdateSupplierAccountRequest {
  const name = (input.name || "").trim();
  if (!name) throw new Error("企业资料必填");
  if ([...name].length > NAME_MAX) throw new Error("企业名称不能超过 128 个字符。");
  const registrationNo = (input.registrationNo || "").trim();
  if ([...registrationNo].length > REG_NO_MAX) throw new Error("注册编号不能超过 64 个字符。");
  const contact = (input.contact || "").trim();
  if ([...contact].length > CONTACT_MAX) throw new Error("对接负责人不能超过 64 个字符。");
  if (!isSupplierRegion(input.region)) throw new Error("主体属性取值不合法");
  return { name, registrationNo, contact, region: input.region };
}

function validateLogRow(value: unknown, seen: Set<string>): SupplierOperationLogDTO {
  if (!isPlainObject(value)) invalid("操作记录响应不符合约定，请重新加载。");
  const row = value as Record<string, unknown>;
  const id = row.id;
  if (typeof id !== "string" || !id) invalid("操作记录响应不符合约定：缺少 id。");
  if (seen.has(id)) invalid("操作记录响应不符合约定：记录重复。");
  seen.add(id);
  for (const key of ["action", "object"] as const) {
    if (typeof row[key] !== "string" || !(row[key] as string).trim()) {
      invalid(`操作记录响应不符合约定：${key} 字段缺失。`);
    }
  }
  // operator 只是写入时的名字快照，历史上可能为空（对接负责人未填 / 老数据）。
  // 这里做展示兜底而不是报错——一条空操作人不该让整页操作记录不可用（2026-10-27 实测踩到）。
  const operator = typeof row.operator === "string" && row.operator.trim() ? row.operator.trim() : "—";
  if (typeof row.operatedAt !== "string" || !Number.isFinite(Date.parse(row.operatedAt))) {
    invalid("操作记录响应不符合约定：时间格式异常。");
  }
  if (row.payload !== undefined && row.payload !== null && !isPlainObject(row.payload)) {
    invalid("操作记录响应不符合约定：payload 结构异常。");
  }
  return {
    id,
    action: row.action as string,
    object: row.object as string,
    operator,
    operatedAt: row.operatedAt,
    ...(isPlainObject(row.payload) ? { payload: row.payload } : {}),
  };
}

/** 校验操作记录分页：page/size 必须回显请求值，total 不得小于本页条数。 */
export function validateOperationLogPage(value: unknown, page: number, size: number): SupplierOperationLogPage {
  if (!isPlainObject(value)) invalid("操作记录响应不符合约定，请重新加载。");
  const data = value as Record<string, unknown>;
  if (!Array.isArray(data.list)) invalid("操作记录响应不符合约定：缺少列表。");
  if (data.page !== page || data.size !== size) invalid("操作记录分页与请求不一致，请重新加载。");
  if (!Number.isSafeInteger(data.total) || (data.total as number) < 0) invalid("操作记录响应不符合约定：总数异常。");
  if (data.list.length > size) invalid("操作记录响应不符合约定：返回条数超过页大小。");
  if ((data.total as number) < data.list.length) invalid("操作记录响应不符合约定：总数小于本页条数。");
  const seen = new Set<string>();
  const list = data.list.map((row) => validateLogRow(row, seen));
  return { list, total: data.total as number, page, size };
}
