# 12 · 组织与权限 M11（批次 5）

> 本文件是组织与权限接口的前端对接契约源头。
> 实现见 `internal/domain/orgperm/` + `internal/repo/orgperm.go` + `internal/api/org_permissions.go`。
> `openapi/swagger.json` 是代码注解生成的产物，不是源头。

---

## 0. 范围

本批**只实现两个只读接口**。创建角色、编辑角色、改授权、改组织树**一律未实现**，
返回 404（路由未注册），不要前端先做写入口。

---

## 1. 接口清单

- `GET /api/internal/org-permissions/organizations` —— 组织树，权限点 `M11:V`
- `GET /api/internal/org-permissions/roles` —— 角色权限矩阵，权限点 `M11:V`

两个接口都在**内部门户**（`/api/internal/*`）下，供应商/客户门户没有对应路由。

鉴权与错误：

- 未带 token → `401`（code 10002 未认证）
- 已认证但角色不含 `M11:V` → `403`（code 10003 无权限）
- 服务异常 → `500`（code 10000 系统错误）

路径前缀 `/internal/org-permissions` 由前端写死在 `src/api/orgPermissions.ts`，不要改。

---

## 2. GET organizations

`data` 是**组织树根节点数组**（不是 `{list,total}` 分页结构，也不分页）。

节点字段：

- `id` string —— `org_unit.id` 转字符串
- `code` string —— **org_unit 表没有 code 列**，后端用 `id` 转字符串填充，
  即 `code` 恒等于 `id`，**不是业务编码**。不要把 code 当「组织编号」给运营做人工比对
- `name` string —— 组织名称
- `type` string —— `COMPANY` / `DEPARTMENT` / `TEAM` 三值。
  DB 的 `unit_type` 当前只有 `COMPANY` 和 `DEPT`，`DEPT` 映射为 `DEPARTMENT`；
  未知类型兜底成 `DEPARTMENT`
- `memberCount` number —— **含全部下级**的在职人数，不是直属人数。
  父节点必然 ≥ 各子节点之和
- `ownerName` string —— `leader_staff_id` → `internal_staff.name`；无负责人为空串 `""`
- `children` array —— 子节点，结构与本节点相同；**叶子节点不输出该键**

口径：

- 只返回 `status='ACTIVE'` 的组织节点
- 成员只数 `status='ACTIVE'` 的员工
- 建树按 `parent_id`，**不用 `path`**（库里 `path` 存在脏值）
- `parent_id` 为 NULL、或指向不存在/非 ACTIVE 的节点、或自引用时，
  该节点**降为根节点**（不丢数据，不会死循环）

样例：

```json
{
  "code": 0,
  "message": "ok",
  "data": [
    { "id": "1", "code": "1", "name": "根公司", "type": "COMPANY",
      "memberCount": 7, "ownerName": "",
      "children": [
        { "id": "2", "code": "2", "name": "平台部", "type": "DEPARTMENT",
          "memberCount": 5, "ownerName": "" }
      ] }
  ],
  "requestId": "0c61857b-..."
}
```

---

## 3. GET roles

`data` 是**角色数组**（不分页），每个角色展开到权限点明细。

角色字段：

- `id` string —— `role.id` 转字符串
- `name` string —— 角色名
- `description` string —— **恒为空串**。`role` 表没有该列，后端不编造文案，
  前端展示时自行兜底（当前页面用 `v-if` 隐藏空描述）
- `portal` string —— `internal` / `supplier` / `customer`。
  `role` 表没有 portal 列，由后端按 `role.code` 推导：
  `SUPPLIER`→`supplier`、`CUSTOMER`→`customer`、其余→`internal`
- `memberCount` number —— `role_grant` 里该角色**未过期**的授权人数
  （`expires_at IS NULL OR expires_at > now()`），与登录时能否拿到该角色同口径
- `dataScope` string —— `role.data_scope` 原值透传：`SELF` / `DEPT` / `DEPT_SUB` / `ALL`
- `permissions` array —— 权限点明细，**恒为非 null 数组**（空则 `[]`）。
  排序按 `pkg/perm` 的 M1..M12 × V/E/A/C/P 语义顺序，**不是字典序**
  - `code` string —— 形如 `M11:V`，即 `module_code || ':' || action_code`
  - `module` string —— `permission_point.name` 按「·」（U+00B7）拆分的前半，如 `成本管理`
  - `action` string —— 拆分后半，如 `编辑`
- `fieldRestrictions` array —— `role.field_mask` 里 `{"hide":[...]}` 的数组元素，
  **元素是字段名**（如 `unit_cost`、`margin_before`），不是可读文案。
  为 NULL 时返回 `[]`，**恒为非 null 数组**

口径补充：

- 外部门户角色（`SUPPLIER` / `CUSTOMER`）不参与 M1–M12 权限点矩阵，
  其 `role_permission` 为空是预期行为 ⇒ `permissions` 为 `[]`，不是缺陷
- 角色列表**不做数据域过滤**：返回的是全系统权限配置，靠 `M11:V` 一道门收口

样例（截断）：

```json
{
  "code": 0,
  "message": "ok",
  "data": [
    { "id": "23", "name": "平台管理员", "description": "", "portal": "internal",
      "memberCount": 1, "dataScope": "ALL",
      "permissions": [
        { "code": "M1:V", "module": "模型管理", "action": "查看" },
        { "code": "M1:E", "module": "模型管理", "action": "编辑" }
      ],
      "fieldRestrictions": [] },
    { "id": "27", "name": "销售", "description": "", "portal": "internal",
      "memberCount": 2, "dataScope": "SELF",
      "permissions": [ "... 20 项 ..." ],
      "fieldRestrictions": ["unit_cost","unit_cost_basis","cost_min","cost_max",
        "cost_weighted","supplier_cost","calc_snapshot","margin_before","margin_after"] },
    { "id": "32", "name": "供应商", "description": "", "portal": "supplier",
      "memberCount": 0, "dataScope": "SELF",
      "permissions": [], "fieldRestrictions": ["... 8 项 ..."] }
  ],
  "requestId": "..."
}
```

---

## 4. 安全约束

- 两个接口严格挂 `RequirePerm(M11:V)`，不因为「只是查看」而放宽
- 响应**不含** `password_hash`、任何 token、`login_session.role_snapshot`
- 权限点编码词汇的唯一事实源是 `pkg/perm`（`perm.All()` 60 点），
  模块中文名来自 `permission_point` 表，**后端不硬编码中文模块名列表**
- `router.go` 里 `M11:E` / `M11:A` / `M11:C` / `M11:P` **零挂载**
  （本批不实现写操作），由 `router_orgperm_perm_test.go` 钉住

---

## 5. 已知口径缺口（前端需知）

- `description` 恒空串：`role` 表无该列
- `code` 等于 `id`：组织表无业务编码列
- 组织树不含「成员清单 / 岗位 / 人员分配」——那些接口尚未开放
- `fieldRestrictions` 是字段名而非说明文案，前端若要展示可读文案需自行映射
