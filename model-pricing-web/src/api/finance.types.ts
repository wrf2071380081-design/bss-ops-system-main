import type { IsoDateTime } from "../domain/common";
import type { Money } from "../domain/money";
import type { FinanceAccountStatus, FxRateStatus } from "../domain/status";

// 正式契约（2026-10-10 批次 4 实现后由 PROVISIONAL 升级），与后端
// model-pricing-backend/docs/api/12-finance.md 对齐。
// 关键口径：
//   - 汇率固定 USD/CNY（无 source 字段）；rate 为 8 档之一，"PENDING_LOCK" 占位行 rate 为空串、无 lockedAt/updatedAt。
//   - 客户编码 customerCode 取统一社会信用代码（个人主体为空）；currency 固定 CNY。
//   - 账户详情无 warningThresholdRate / notes（库中无对应列，按"不返回假字段"移除）。

export interface FxRateListParams {
  page: number;
  size: number;
  /** 模糊匹配月份或锁定人姓名 */
  search?: string;
  month?: string;
  status?: FxRateStatus | "";
}

export interface FxRateDTO {
  id: string;
  month: string;
  baseCurrency: string;
  quoteCurrency: string;
  /** 档位规范形式（如 "7.0"）；PENDING_LOCK 占位行为空串 */
  rate: string;
  status: FxRateStatus;
  lockedAt?: IsoDateTime;
  lockedBy?: string;
  /** PENDING_LOCK 占位行不含该字段 */
  updatedAt?: IsoDateTime;
}

export interface FinanceAccountListParams {
  page: number;
  size: number;
  search?: string;
  status?: FinanceAccountStatus | "";
}

export interface FinanceAccountSummaryDTO {
  customerId: string;
  /** = 统一社会信用代码（uscc）；个人主体为空 */
  customerCode: string;
  customerName: string;
  levelCode: string;
  status: FinanceAccountStatus;
  currency: string;
  creditLimit: Money;
  usedCredit: Money;
  /** 后端 decimal 计算：creditLimit − usedCredit */
  availableCredit: Money;
  depositBalance: Money;
  /** payment_type 原值（PREPAID / MONTHLY / PAYG） */
  paymentTerms: string;
  updatedAt: IsoDateTime;
}

export interface FinanceTransactionDTO {
  /** 合并流水 id（C=授信、D=押金 前缀，避免两表 id 撞号） */
  id: string;
  occurredAt: IsoDateTime;
  type: "CREDIT_USAGE" | "CREDIT_RELEASE" | "DEPOSIT_IN" | "DEPOSIT_OUT";
  amount: Money;
  currency: string;
  referenceNo: string;
  note: string;
}

export interface FinanceAccountDetailDTO extends FinanceAccountSummaryDTO {
  billingEntity: string;
  invoiceTitle: string;
  /** uscc 脱敏（保留前 4 + 后 4） */
  taxNoMasked: string;
  /** billing_cycle → "N 天" */
  settlementCycle: string;
  recentTransactions: FinanceTransactionDTO[];
}
