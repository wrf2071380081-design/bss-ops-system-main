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

export const orgPermissionsApi = {
  getOrganizations: () => apiRequest<OrganizationNodeDTO[]>({ url: `${basePath}/organizations`, method: "GET" }),
  getRoles: () => apiRequest<RoleSummaryDTO[]>({ url: `${basePath}/roles`, method: "GET" }),
};
