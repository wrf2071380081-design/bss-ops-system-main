import { apiRequest } from "./http";

/** 组织树节点。后端契约（批次 5 定稿，原 PROVISIONAL 注释已作废）。
 * - id / code：**后端 org_unit 表没有 code 列**，code 用 id 转字符串填充，二者恒等，
 *   **不是业务编码**，不要当成「组织编号」展示给运营做人工比对。
 * - memberCount：**含全部下级**的在职人数，不是直属人数；父节点必然 ≥ 各子节点之和。
 * - ownerName：来自 org_unit.leader_staff_id；该列当前为空时返回空串。
 * - 只返回 status='ACTIVE' 的节点。 */
export interface OrganizationNodeDTO {
  id: string;
  code: string;
  name: string;
  type: "COMPANY" | "DEPARTMENT" | "TEAM";
  memberCount: number;
  ownerName?: string;
  children?: OrganizationNodeDTO[];
}

export interface RolePermissionDTO {
  code: string;
  module: string;
  action: string;
}

/** 角色矩阵一行。后端契约（批次 5 定稿，原 PROVISIONAL 注释已作废）。
 * - description：**后端 role 表没有该列，恒返回空串**（不编造文案），前端需自行兜底展示。
 * - portal：role 表也没有 portal 列，由后端按 role.code 推导（SUPPLIER/CUSTOMER/其余→internal）。
 * - dataScope：role.data_scope 原值透传。
 * - permissions：按 pkg/perm 的 M1..M12 × V/E/A/C/P 语义顺序排序（不是字典序）；
 *   module / action 由 permission_point.name 按「·」拆分。
 * - fieldRestrictions：role.field_mask 的 hide 数组，元素是**字段名**（如 unit_cost），
 *   不是可读文案；为 NULL 时后端返回 []。
 * - permissions 与 fieldRestrictions 恒为非 null 数组，可直接取 .length。 */
export interface RoleSummaryDTO {
  id: string;
  name: string;
  description: string;
  portal: "internal" | "supplier" | "customer";
  memberCount: number;
  dataScope: "SELF" | "DEPT" | "DEPT_SUB" | "ALL";
  permissions: RolePermissionDTO[];
  fieldRestrictions: string[];
}

const basePath = "/internal/org-permissions";
const staffPath = `${basePath}/staff`;

/** 员工持有的角色（后端 StaffRoleRef）。
 * portal 由后端按 role.code 推导：SUPPLIER→supplier、CUSTOMER→customer、其余→internal。
 * 账号管理只允许授予 internal 角色，SUPPLIER/CUSTOMER 必须走各自入驻流程（后端会 400）。 */
export interface StaffRoleRefDTO {
  id: string;
  code: string;
  name: string;
  dataScope: string;
  portal: "internal" | "supplier" | "customer";
}

/** 员工账号（后端 StaffAccountRef）。**绝不包含 password_hash / token**。 */
export interface StaffAccountRefDTO {
  loginId: string;
  status: string;
}

/** 员工列表行 / 详情（后端 StaffSummary；详情 StaffDetail 结构与列表行一致）。 */
export interface StaffSummaryDTO {
  id: string;
  name: string;
  mobile: string;
  orgUnitId: string;
  orgName: string;
  /** ACTIVE / DISABLED。注意：本期 UI 不提供停用入口，但编辑资料时必须原样回传。 */
  status: string;
  /** 无账号时为 null（理论不出现）。 */
  account: StaffAccountRefDTO | null;
  /** 恒为非 null 数组。 */
  roles: StaffRoleRefDTO[];
}

export type StaffDetailDTO = StaffSummaryDTO;

export interface StaffPageDTO {
  list: StaffSummaryDTO[];
  total: number;
  page: number;
  size: number;
}

export interface StaffQuery {
  /** 命中 name / mobile / login_id（模糊）。 */
  keyword?: string;
  /** ACTIVE / DISABLED，空 = 全部。 */
  status?: string;
  page?: number;
  size?: number;
}

/** ⚠️ 请求体是 snake_case，响应体是 camelCase——后端 DTO 现状，不要「顺手统一」。 */
export interface CreateStaffPayload {
  name: string;
  mobile: string;
  org_unit_id: number;
  login_id: string;
  password: string;
  role_ids: number[];
}

export interface UpdateStaffPayload {
  name: string;
  mobile: string;
  org_unit_id: number;
  /** 必须原样回传当前值，否则有被清空的风险。 */
  status: string;
}

export const orgPermissionsApi = {
  getOrganizations: () => apiRequest<OrganizationNodeDTO[]>({ url: `${basePath}/organizations`, method: "GET" }),
  getRoles: () => apiRequest<RoleSummaryDTO[]>({ url: `${basePath}/roles`, method: "GET" }),

  // ---- 账号管理（M11:E + PLATFORM_ADMIN 双层鉴权）。

  listStaff: (query: StaffQuery = {}) =>
    apiRequest<StaffPageDTO>({ url: staffPath, method: "GET", params: query }),

  getStaff: (id: string) =>
    apiRequest<StaffDetailDTO>({ url: `${staffPath}/${id}`, method: "GET" }),

  createStaff: (payload: CreateStaffPayload) =>
    apiRequest<StaffDetailDTO>({
      url: staffPath,
      method: "POST",
      data: payload,
      idempotency: { scope: "staff.create", payload },
    }),

  updateStaff: (id: string, payload: UpdateStaffPayload) =>
    apiRequest<StaffDetailDTO>({
      url: `${staffPath}/${id}`,
      method: "PUT",
      data: payload,
      idempotency: { scope: `staff.update:${id}`, payload },
    }),

  resetStaffPassword: (id: string, password: string) =>
    apiRequest<{ reset: boolean }>({
      url: `${staffPath}/${id}/password`,
      method: "POST",
      data: { password },
      // 口令参与指纹：连续两次同值重置若指纹相同会被幂等重放，第二次实际不生效。
      idempotency: { scope: `staff.password:${id}`, payload: { password } },
    }),

  grantStaffRoles: (id: string, roleIds: number[]) =>
    apiRequest<StaffDetailDTO>({
      url: `${staffPath}/${id}/roles`,
      method: "POST",
      data: { role_ids: roleIds },
      idempotency: { scope: `staff.roles.grant:${id}`, payload: { role_ids: roleIds } },
    }),

  revokeStaffRole: (id: string, roleId: string) =>
    apiRequest<StaffDetailDTO>({
      url: `${staffPath}/${id}/roles/${roleId}`,
      method: "DELETE",
      idempotency: { scope: `staff.roles.revoke:${id}:${roleId}` },
    }),
};
