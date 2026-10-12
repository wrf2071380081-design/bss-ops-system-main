# 财务中心 M10（内部门户）

> **状态：已实现。批次 4（fx-rates / accounts / account detail 三个只读接口）+ 批次 6（汇率锁定读写）均已落地并 E2E 验收。**
> 依据：批次 4 任务书、批次 6 任务书、设计文档 §8 / §11。
> 通用约定（响应包、分页、幂等、错误码、字段剔除、数据域）见 `README.md`。
> 本文件是前端对接的**唯一契约源头**。前端 `model-pricing-web/src/api/finance.ts` / `finance.types.ts`
> 已按本文件对齐（原 `PROVISIONAL` 注释已升级为正式契约）。

---

## 0. 数据字典（现有表，本批不新增迁移）

### 0.1 `fx_rate_lock`（月度汇率锁定）

表 `migrations/000004_quote_cost.up.sql:105-119`：

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | bigint | 主键 |
| `ym` | char(7) | 月份，如 `2026-09`；`uk_fx_ym UNIQUE(ym)` —— **一个月最多一条** |
| `fx_tier` | numeric(6,3) | 档位，必须属于固定 **8 档** |
| `locked_by` | bigint | 锁定人（`internal_staff.id`），联 `internal_staff.name` 回显 |
| `locked_at` | timestamptz | 锁定时间 |
| `adjust_reason` | varchar(256) | 月中调整理由（首次锁定为 `null`） |

**固定 8 档**：`6.5 / 6.7 / 6.75 / 6.8 / 6.85 / 6.9 / 6.95 / 7.0`（与 `05-quotes.md §0.4` 同一集合，
代码单一来源在 `pkg/fxtier`，供应商报价与财务共用）。

> **币种对是固定常量 `USD/CNY`**：本表按业务定义只承载「USD 折算 CNY 的月度档位」，
> 不存在多币种行、也没有 `source`（来源）列。响应中的 `baseCurrency/quoteCurrency`
> 是**由表语义决定的常量**，不是从行里取的字段；`source` 已从契约中移除。

### 0.2 `customer_profile` + `legal_subject`（客户财务账户）

表 `migrations/000002_subject.up.sql:58-80` + `:1-17`：

| 契约字段 | 来源 |
|---|---|
| `customerId` | `customer_profile.id`（字符串传输） |
| `customerCode` | `legal_subject.uscc`（统一社会信用代码；个人主体 `uscc` 为 `null` → 空串） |
| `customerName` | `legal_subject.legal_name` |
| `levelCode` | `customer_profile.level_code` |
| `status` | `customer_profile.credit_status`（`NORMAL/WARNING/FROZEN`） |
| `creditLimit` / `usedCredit` | `credit_limit` / `credit_used` |
| `availableCredit` | **服务层 decimal 计算**：`credit_limit − credit_used` |
| `depositBalance` | `deposit_amount` |
| `paymentTerms` | `payment_type`（`PREPAID/MONTHLY/PAYG`，原值直通，不加中文标签） |
| `currency` | 常量 `CNY`（表内金额列无币种，客户授信/押金按人民币计） |
| `billingEntity` / `invoiceTitle` | `legal_subject.legal_name` |
| `taxNoMasked` | `legal_subject.uscc` 脱敏：保留前 4 + 后 4，中间 `****`；无则空串 |
| `settlementCycle` | `customer_profile.billing_cycle`（天数）→ 展示串 `"{n} 天"` |
| `updatedAt` | `customer_profile.updated_at` |

### 0.3 流水（合并 `credit_txn` + `deposit_txn`）

`account_detail.recentTransactions` 合并两张 append-only 表，按 `created_at DESC` 取最近 **10** 条。

| 来源 | 方向原值 | 契约 `type` |
|---|---|---|
| `credit_txn.direction` | `CONSUME` | `CREDIT_USAGE` |
| `credit_txn.direction` | `REPAY` | `CREDIT_RELEASE` |
| `credit_txn.direction` | `ADJUST` | `CREDIT_USAGE`（额度人工调整，视作占用变动；口径待业务复核，见 §7） |
| `deposit_txn.direction` | `PAY` | `DEPOSIT_IN` |
| `deposit_txn.direction` | `DEDUCT` | `DEPOSIT_OUT` |
| `deposit_txn.direction` | `REFUND` | `DEPOSIT_OUT` |

> 押金表按 `subject_side='CUSTOMER' AND profile_id = customerId` 过滤（`deposit_txn` 无 `customer_id` 列）。
> 映射规则不是字段直通，已写进代码注释（`internal/domain/finance/finance.go`）。
> 合并流水 `id` 加来源前缀（授信 `C`、押金 `D`）避免两表 id 撞号。

### 0.4 不返回的字段（无数据源，已从 DTO 移除）

`source`（汇率来源）、`warningThresholdRate`（预警阈值）、`notes`（备注）**在库中无对应列**，
按「不返回假字段」原则从 DTO 删除；前端已同步删除相应展示。

---

## 1. 汇率快照列表

```
GET /api/internal/finance/fx-rates?page=1&size=20&month=&status=&search=
```

- 权限：`M10:V`
- 行级过滤：**无**（汇率是全局主数据）
- 查询参数：

  | 参数 | 说明 |
  |---|---|
  | `page` / `size` | 分页，`size` 上限 200，默认 20 |
  | `month` | 精确匹配 `ym`（`YYYY-MM`） |
  | `status` | `LOCKED` / `PENDING_LOCK` / `EXPIRED` |
  | `search` | 模糊匹配**月份**或**锁定人姓名**（币种固定、无来源，搜索不针对币种/来源） |

### 1.1 `status` 三态推导规则

> 设 `cur` = 当前月（`Asia/Shanghai`）
> - 表中有该月记录且 `ym >= cur` → `LOCKED`（含未来月：未来月已锁定仍是 LOCKED）
> - 表中有该月记录且 `ym < cur` → `EXPIRED`（早于当月的历史锁定已失效）
> - **当月无记录** → 合成一条 `PENDING_LOCK` **占位行**（见 1.2）

### 1.2 `PENDING_LOCK` 占位行

当月尚无锁定时，响应中**补一条占位行**，让前端能看到「本月待锁定」：

| 字段 | 取值 |
|---|---|
| `id` | 当月 `ym`（如 `"2026-10"`），非数据库 id |
| `month` | 当月 `ym` |
| `baseCurrency` / `quoteCurrency` | `USD` / `CNY` |
| `rate` | 空串 `""` |
| `status` | `PENDING_LOCK` |
| `lockedAt` / `lockedBy` / `updatedAt` | 省略（占位行不是真实记录，不伪造时间） |

> 占位行是「本月未完成事项」的**状态标记**，不是演示数据；不写库、只影响读响应。
> 分页 `total` 计入占位行；`month` 筛选为其他月时不出现；`status=PENDING_LOCK` 筛选时只返回它。

### 1.3 响应 `data`

```json
{
  "list": [
    { "id": "12", "month": "2027-01", "baseCurrency": "USD", "quoteCurrency": "CNY",
      "rate": "6.9", "status": "LOCKED",
      "lockedAt": "2026-10-09T17:42:56+08:00", "lockedBy": "财务冒烟",
      "updatedAt": "2026-10-09T17:42:56+08:00" },
    { "id": "2026-10", "month": "2026-10", "baseCurrency": "USD", "quoteCurrency": "CNY",
      "rate": "", "status": "PENDING_LOCK" }
  ],
  "total": 2, "page": 1, "size": 20
}
```

- 排序：`ym DESC`
- `rate` 回显**档位规范形式**（`6.8` 而非 `6.800`），与报价侧口径一致
- 错误：401 未认证 / 403 无 `M10:V`

---

## 2. 客户财务账户列表

```
GET /api/internal/finance/accounts?page=1&size=20&search=&status=
```

- 权限：`M10:V`
- **行级过滤**：按 `customer_profile.owner_sales_operator_id` 做 `SELF/DEPT/DEPT_SUB/ALL` 四档
  （与 `internal/repo/customer.go` 的 `applyCustomerOwnerScope` 同形态）
- 查询参数：

  | 参数 | 说明 |
  |---|---|
  | `page` / `size` | 分页，`size` 上限 200，默认 20 |
  | `search` | 模糊匹配客户名称（`legal_name ILIKE`）或编码（`uscc ILIKE`） |
  | `status` | `NORMAL` / `WARNING` / `FROZEN`（= `credit_status`） |

### 2.1 响应 `data`

```json
{
  "list": [
    { "customerId": "1", "customerCode": "91310000MA1FL0001A", "customerName": "示例科技有限公司",
      "levelCode": "PRO", "status": "WARNING", "currency": "CNY",
      "creditLimit": "100000.00", "usedCredit": "85000.00", "availableCredit": "15000.00",
      "depositBalance": "20000.00", "paymentTerms": "MONTHLY",
      "updatedAt": "2026-10-09T18:03:27+08:00" }
  ],
  "total": 1, "page": 1, "size": 20
}
```

- 金额一律 decimal 字符串；`availableCredit = creditLimit − usedCredit`（服务层算，禁 float64）
- 错误：401 / 403（无 `M10:V`）

---

## 3. 客户财务账户详情

```
GET /api/internal/finance/accounts/{customerId}
```

- 权限：`M10:V` + 同一套行级过滤
- 越域 → **403**；不存在 → **404**
- `{customerId}` = `customer_profile.id`

### 3.1 响应 `data`（在 §2.1 基础上追加）

```json
{
  "customerId": "1", "customerCode": "91310000MA1FL0001A", "customerName": "示例科技有限公司",
  "levelCode": "PRO", "status": "WARNING", "currency": "CNY",
  "creditLimit": "100000.00", "usedCredit": "85000.00", "availableCredit": "15000.00",
  "depositBalance": "20000.00", "paymentTerms": "MONTHLY",
  "billingEntity": "示例科技有限公司", "invoiceTitle": "示例科技有限公司",
  "taxNoMasked": "9131****001A", "settlementCycle": "30 天",
  "recentTransactions": [
    { "id": "C3", "occurredAt": "2026-10-08T18:03:27+08:00", "type": "CREDIT_USAGE",
      "amount": "500.00", "currency": "CNY", "referenceNo": "MANUAL", "note": "" },
    { "id": "D1", "occurredAt": "2026-10-08T06:03:27+08:00", "type": "DEPOSIT_IN",
      "amount": "20000.00", "currency": "CNY", "referenceNo": "", "note": "预付押金" }
  ],
  "updatedAt": "2026-10-09T18:03:27+08:00"
}
```

- `id`：合并流水，授信前缀 `C`、押金前缀 `D`（避免两表 id 撞号）
- `referenceNo`：`credit_txn` = `ref_type`（有 `ref_id` 时 `ref_type:ref_id`）；`deposit_txn` = 空串
- `note`：`deposit_txn.reason`；`credit_txn` = 空串
- 错误：401 / 403（无权限或越域） / 404（客户不存在）

---

## 4. 汇率锁定 · 当月状态

```
GET /api/internal/finance/fx-lock
```

- 权限：`M10:V`
- 锁定目标月 = **当前月**（`Asia/Shanghai`）
- 响应 `data`：

```json
{
  "month": "2026-10", "status": "PENDING_LOCK", "fxTier": "",
  "lockedAt": null, "lockedBy": "", "adjustReason": "",
  "tiers": ["6.5","6.7","6.75","6.8","6.85","6.9","6.95","7.0"]
}
```

- `status`：`LOCKED`（当月已锁）/ `PENDING_LOCK`（当月未锁）
- 已锁定时 `fxTier/lockedAt/lockedBy/adjustReason` 回填；未锁定时 `fxTier=""`
- `tiers` 恒返回 8 档可选值，供前端下拉选择

---

## 5. 汇率锁定 · 锁定/调整

```
POST /api/internal/finance/fx-lock
Idempotency-Key: <uuid>
{ "fx_tier": "7.0", "adjust_reason": "" }
```

- 权限：**两层**
  1. 中间件 `M10:E`
  2. **服务层二道校验** `perm.AnyRoleCan(op.Roles, perm.CanLockFx)` —— 只放行 `FINANCE`。
     这道不可省：即使有人给非财务角色配了 `M10:E`，也锁不了汇率。
- 幂等：必挂 `Idempotency-Key`（`middleware.Idempotency`），与仓库其他写接口一致
- 业务规则：
  1. `fx_tier` 必须 ∈ 8 档（数值等价），否则 **400**
  2. 当月**无记录** → `INSERT`（首次锁定，`adjust_reason` 可空）
  3. 当月**已有记录** → 视为**调整**，`adjust_reason` **必填**，否则 **400**；`UPDATE` 该行
     （`uk_fx_ym` 保证一个月最多一条）
  4. **审计**：写操作同事务落 `audit_log`，`action=FX_LOCK`（首次）/ `FX_LOCK_ADJUST`（调整），
     含档位新旧值 / 理由 / 操作人 / 时间
  5. 月中调整走**简化版**：直接写 + 强制理由 + 审计留痕；**审批链不在本批实现**（见 §7）
- 响应 `data`：

```json
{ "month": "2026-10", "fxTier": "7.0", "status": "LOCKED",
  "adjusted": false, "lockedAt": "2026-10-10T16:40:00+08:00", "lockedBy": "财务冒烟" }
```

- 错误：400（档位非法 / 调整缺理由）/ 401 / 403（无 `M10:E` 或非 FINANCE）/ 409（幂等冲突）

---

## 6. 错误码

沿用 `README.md` §7：400/10001 参数校验、401/10002 未认证、403/10003 无权限（含越域）、
404/10004 记录不存在、409/10005 幂等或状态冲突、500/10000 系统错误。

---

## 7. 遗留（不在批次 4/6 范围）

1. 汇率月中调整的**审批链**（`change_request` + `approval_step` 双人审批）未实现——批次 6 已裁决本批做简化版，
   仅登记为遗留，后续批次再补。
2. 「每月 1 号 09:00 推送汇率锁定待办 + 超时告警」未实现（设计文档提及，批次 6 登记为未实现项）。
3. `internal/domain/workbench` 里 FINANCE 工作台「汇率锁定」卡片（`fin_fx_pending_month`）仍是 `"0"` 占位，
   批次 6 列为「可考虑接真实数据（若做需单独说明）」，本批未做。
4. `credit_txn.direction=ADJUST` 与 `deposit_txn.direction=REFUND` 的归类口径待业务复核。
