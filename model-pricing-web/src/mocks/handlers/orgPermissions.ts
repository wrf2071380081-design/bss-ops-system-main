import { http, HttpResponse } from "msw";
import type { OrganizationNodeDTO, RoleSummaryDTO, StaffSummaryDTO } from "../../api/orgPermissions";
import { ApiError } from "../../domain/common";

const basePath = "/api/internal/org-permissions";
const staffPath = `${basePath}/staff`;
const readableIdentities = new Set(["ADMIN_OP", "VIEWER"]);
// 账号管理只允许平台管理员：与后端 perm.CanManageStaff 同口径（LEADER 持 M11:E 也不行）。
const manageableIdentities = new Set(["ADMIN_OP"]);

// 组织 id 用数字字符串，与真实后端一致（org_unit.id 是 bigint，DTO 里转字符串；code 恒等于 id）。
const organizations: OrganizationNodeDTO[] = [
  {
    id: "1", code: "1", name: "模型业务中心", type: "COMPANY", memberCount: 26, ownerName: "顾言",
    children: [
      { id: "2", code: "2", name: "模型运营部", type: "DEPARTMENT", memberCount: 6, ownerName: "林悦", children: [{ id: "3", code: "3", name: "主数据组", type: "TEAM", memberCount: 3, ownerName: "林悦" }] },
      { id: "4", code: "4", name: "采购与供应商部", type: "DEPARTMENT", memberCount: 7, ownerName: "陈昊" },
      { id: "5", code: "5", name: "定价与客户部", type: "DEPARTMENT", memberCount: 8, ownerName: "周婷" },
      { id: "6", code: "6", name: "财务运营部", type: "DEPARTMENT", memberCount: 4, ownerName: "许宁" },
    ],
  },
];

const roles: RoleSummaryDTO[] = [
  { id: "1", name: "平台管理员", description: "全平台权限，唯一可管理账号的角色。", portal: "internal", memberCount: 1, dataScope: "ALL", permissions: [{ code: "M11:V", module: "组织与权限", action: "查看" }, { code: "M11:E", module: "组织与权限", action: "编辑" }], fieldRestrictions: [] },
  { id: "2", name: "领导", description: "审批与配置权；不含账号管理。", portal: "internal", memberCount: 2, dataScope: "ALL", permissions: [{ code: "M11:V", module: "组织与权限", action: "查看" }, { code: "M1:A", module: "模型管理", action: "审批" }], fieldRestrictions: [] },
  { id: "3", name: "员工", description: "查看全量 + 业务编辑，无审批与账号管理。", portal: "internal", memberCount: 3, dataScope: "ALL", permissions: [{ code: "M1:V", module: "模型管理", action: "查看" }, { code: "M1:E", module: "模型管理", action: "编辑" }], fieldRestrictions: [] },
  { id: "4", name: "模型运营", description: "维护模型主数据并处理官方价格差异。", portal: "internal", memberCount: 6, dataScope: "ALL", permissions: [{ code: "M1:V", module: "模型管理", action: "查看" }, { code: "M1:E", module: "模型管理", action: "编辑" }, { code: "M2:V", module: "官方价格", action: "查看" }, { code: "M2:E", module: "官方价格", action: "处理" }], fieldRestrictions: ["不返回客户授信、押金和成本敏感字段"] },
  { id: "5", name: "采购", description: "查看供应商档案、审批报价并查询成本。", portal: "internal", memberCount: 7, dataScope: "DEPT_SUB", permissions: [{ code: "M3:V", module: "供应商管理", action: "查看" }, { code: "M4:V", module: "供应商报价", action: "查看" }, { code: "M5:E", module: "成本管理", action: "编辑" }], fieldRestrictions: ["不返回客户财务账户字段"] },
  { id: "6", name: "定价运营", description: "维护策略、价目表、客户与客户报价。", portal: "internal", memberCount: 8, dataScope: "ALL", permissions: [{ code: "M6:E", module: "定价策略", action: "编辑" }, { code: "M7:E", module: "价目表", action: "编辑" }], fieldRestrictions: ["不返回完整税号和非本域财务流水"] },
  { id: "7", name: "销售", description: "维护本人归属客户与报价。", portal: "internal", memberCount: 5, dataScope: "SELF", permissions: [{ code: "M8:V", module: "客户管理", action: "查看" }, { code: "M9:E", module: "客户报价", action: "编辑" }], fieldRestrictions: ["成本与毛利字段物理剔除"] },
  { id: "8", name: "财务", description: "查询财务账户、汇率、授信和押金。", portal: "internal", memberCount: 4, dataScope: "ALL", permissions: [{ code: "M10:V", module: "财务信息", action: "查看" }, { code: "M10:E", module: "财务信息", action: "维护" }], fieldRestrictions: ["联系方式默认脱敏"] },
  { id: "9", name: "供应商操作员", description: "维护本供应商报价和主体资料。", portal: "supplier", memberCount: 12, dataScope: "SELF", permissions: [], fieldRestrictions: ["仅返回所属供应商数据", "不返回成本、floor 和其他供应商报价"] },
  { id: "10", name: "客户操作员", description: "查看本客户价目表、报价和合同。", portal: "customer", memberCount: 15, dataScope: "SELF", permissions: [], fieldRestrictions: ["仅返回所属客户数据", "不返回成本与内部审批信息"] },
];

const roleById = (id: string) => roles.find((role) => role.id === id);
const roleRef = (id: string) => {
  const role = roleById(id);
  return role
    ? { id: role.id, code: role.name === "平台管理员" ? "PLATFORM_ADMIN" : role.name === "领导" ? "LEADER" : role.name === "员工" ? "STAFF" : role.name, name: role.name, dataScope: role.dataScope, portal: role.portal }
    : { id, code: id, name: `角色 ${id}`, dataScope: "ALL", portal: "internal" as const };
};
const orgNameOf = (id: string) => {
  let found = "";
  const walk = (nodes: OrganizationNodeDTO[]) => {
    for (const node of nodes) {
      if (node.id === id) found = node.name;
      if (node.children?.length) walk(node.children);
    }
  };
  walk(organizations);
  return found || "—";
};

let staffSeq = 100;
const staff: StaffSummaryDTO[] = [
  { id: "11", name: "许澄", mobile: "13900002002", orgUnitId: "1", orgName: "模型业务中心", status: "ACTIVE", account: { loginId: "xucheng", status: "ACTIVE" }, roles: [roleRef("2")] },
  { id: "12", name: "陈乐", mobile: "13900002003", orgUnitId: "2", orgName: "模型运营部", status: "ACTIVE", account: { loginId: "chenle", status: "ACTIVE" }, roles: [roleRef("3")] },
  { id: "13", name: "周宁", mobile: "13900002004", orgUnitId: "2", orgName: "模型运营部", status: "ACTIVE", account: { loginId: "zhouning", status: "ACTIVE" }, roles: [roleRef("3")] },
  { id: "14", name: "沈安", mobile: "13900002005", orgUnitId: "4", orgName: "采购与供应商部", status: "ACTIVE", account: { loginId: "shenan", status: "ACTIVE" }, roles: [roleRef("3")] },
];

const result = (data: unknown) => HttpResponse.json({ code: 0, message: "ok", data: structuredClone(data), requestId: `mock-${crypto.randomUUID()}` });
const failure = (error: unknown) => { const candidate = error as { httpStatus?: number; message?: string }; const status = candidate.httpStatus || 500; return HttpResponse.json({ code: `HTTP_${status}`, message: candidate.message || "Mock 服务异常", requestId: `mock-${crypto.randomUUID()}` }, { status }); };
const ensureReadable = (request: Request) => { const identity = request.headers.get("X-Mock-Identity") || "VIEWER"; if (!readableIdentities.has(identity)) throw new ApiError(403, "当前身份无权查看组织与权限"); };
const ensureManageable = (request: Request) => { const identity = request.headers.get("X-Mock-Identity") || "VIEWER"; if (!manageableIdentities.has(identity)) throw new ApiError(403, "角色不含 PLATFORM_ADMIN，不允许管理账号（M11:E 服务层角色收敛）"); };

const LOGIN_ID_RE = /^[a-z][a-z0-9_]{3,31}$/;
const MOBILE_RE = /^1[3-9]\d{9}$/;
const findStaff = (id: string) => staff.find((row) => row.id === id);

export const orgPermissionHandlers = [
  http.get(`${basePath}/organizations`, ({ request }) => { try { ensureReadable(request); return result(organizations); } catch (error) { return failure(error); } }),
  http.get(`${basePath}/roles`, ({ request }) => { try { ensureReadable(request); return result(roles); } catch (error) { return failure(error); } }),

  http.get(staffPath, ({ request }) => {
    try {
      ensureManageable(request);
      const url = new URL(request.url);
      const keyword = (url.searchParams.get("keyword") || "").trim().toLowerCase();
      const page = Number(url.searchParams.get("page") || "1") || 1;
      const size = Number(url.searchParams.get("size") || "20") || 20;
      const matched = keyword
        ? staff.filter((row) =>
            row.name.toLowerCase().includes(keyword) ||
            row.mobile.includes(keyword) ||
            (row.account?.loginId || "").toLowerCase().includes(keyword))
        : staff;
      const start = (page - 1) * size;
      return result({ list: matched.slice(start, start + size), total: matched.length, page, size });
    } catch (error) { return failure(error); }
  }),

  http.get(`${staffPath}/:id`, ({ request, params }) => {
    try {
      ensureManageable(request);
      const row = findStaff(String(params.id));
      if (!row) throw new ApiError(404, "员工不存在");
      return result(row);
    } catch (error) { return failure(error); }
  }),

  http.post(staffPath, async ({ request }) => {
    try {
      ensureManageable(request);
      const body = (await request.json()) as {
        name?: string; mobile?: string; org_unit_id?: number; login_id?: string; password?: string; role_ids?: number[];
      };
      const name = (body.name || "").trim();
      const mobile = (body.mobile || "").trim();
      const loginId = (body.login_id || "").trim();
      const password = body.password || "";
      const roleIds = (body.role_ids || []).map(String);
      if (!name) throw new ApiError(400, "姓名不能为空");
      if (!MOBILE_RE.test(mobile)) throw new ApiError(400, "手机号格式非法");
      if (!LOGIN_ID_RE.test(loginId)) throw new ApiError(400, "登录名须 4-32 位小写字母/数字/下划线且小写字母开头");
      if (password.length < 8 || password.length > 64) throw new ApiError(400, "口令长度须为 8-64 个字符");
      if (!roleIds.length) throw new ApiError(400, "新建员工必须至少授予一个角色");
      if (staff.some((row) => row.account?.loginId === loginId)) throw new ApiError(400, "登录名已存在");
      if (staff.some((row) => row.mobile === mobile)) throw new ApiError(400, "手机号已存在");
      const unknown = roleIds.find((id) => !roleById(id) || roleById(id)?.portal !== "internal");
      if (unknown) throw new ApiError(400, "角色不存在或为外部门户角色，不允许在此授予");
      const orgUnitId = String(body.org_unit_id ?? "");
      if (!orgNameOf(orgUnitId).replace("—", "")) throw new ApiError(400, "组织不存在");

      const created: StaffSummaryDTO = {
        id: String(++staffSeq), name, mobile, orgUnitId, orgName: orgNameOf(orgUnitId), status: "ACTIVE",
        account: { loginId, status: "ACTIVE" }, roles: roleIds.map(roleRef),
      };
      staff.unshift(created);
      return result(created);
    } catch (error) { return failure(error); }
  }),

  http.put(`${staffPath}/:id`, async ({ request, params }) => {
    try {
      ensureManageable(request);
      const row = findStaff(String(params.id));
      if (!row) throw new ApiError(404, "员工不存在");
      const body = (await request.json()) as { name?: string; mobile?: string; org_unit_id?: number; status?: string };
      const name = (body.name || "").trim();
      const mobile = (body.mobile || "").trim();
      if (!name) throw new ApiError(400, "姓名不能为空");
      if (!MOBILE_RE.test(mobile)) throw new ApiError(400, "手机号格式非法");
      if (staff.some((item) => item.id !== row.id && item.mobile === mobile)) throw new ApiError(400, "手机号已存在");
      const orgUnitId = String(body.org_unit_id ?? "");
      if (!orgNameOf(orgUnitId).replace("—", "")) throw new ApiError(400, "组织不存在");
      row.name = name;
      row.mobile = mobile;
      row.orgUnitId = orgUnitId;
      row.orgName = orgNameOf(orgUnitId);
      if (body.status) row.status = body.status;
      return result(row);
    } catch (error) { return failure(error); }
  }),

  http.post(`${staffPath}/:id/password`, async ({ request, params }) => {
    try {
      ensureManageable(request);
      const row = findStaff(String(params.id));
      if (!row) throw new ApiError(404, "员工不存在");
      const body = (await request.json()) as { password?: string };
      const password = body.password || "";
      if (password.length < 8 || password.length > 64) throw new ApiError(400, "口令长度须为 8-64 个字符");
      return result({ reset: true });
    } catch (error) { return failure(error); }
  }),

  http.post(`${staffPath}/:id/roles`, async ({ request, params }) => {
    try {
      ensureManageable(request);
      const row = findStaff(String(params.id));
      if (!row) throw new ApiError(404, "员工不存在");
      const body = (await request.json()) as { role_ids?: number[] };
      const roleIds = (body.role_ids || []).map(String);
      if (!roleIds.length) throw new ApiError(400, "至少选择一个角色");
      for (const id of roleIds) {
        const role = roleById(id);
        if (!role) throw new ApiError(400, "角色不存在");
        if (role.portal !== "internal") throw new ApiError(400, "外部门户角色不允许在此授予");
        if (row.roles.some((held) => held.id === id)) throw new ApiError(400, "该角色已授予，无需重复操作");
      }
      row.roles = [...row.roles, ...roleIds.map(roleRef)];
      return result(row);
    } catch (error) { return failure(error); }
  }),

  http.delete(`${staffPath}/:id/roles/:roleId`, ({ request, params }) => {
    try {
      ensureManageable(request);
      const row = findStaff(String(params.id));
      if (!row) throw new ApiError(404, "员工不存在");
      const roleId = String(params.roleId);
      if (!row.roles.some((held) => held.id === roleId)) throw new ApiError(400, "该角色未授予，无法回收");
      // 最后一个平台管理员保护：与后端 ErrLastPlatformAdmin 同口径。
      const isPlatformAdmin = (id: string) => roleRef(id).code === "PLATFORM_ADMIN";
      if (isPlatformAdmin(roleId)) {
        const remaining = staff.filter((item) =>
          item.id !== row.id && item.roles.some((held) => held.id === roleId));
        if (remaining.length === 0) throw new ApiError(400, "不允许回收最后一个平台管理员角色或停用唯一平台管理员");
      }
      row.roles = row.roles.filter((held) => held.id !== roleId);
      return result(row);
    } catch (error) { return failure(error); }
  }),
];
