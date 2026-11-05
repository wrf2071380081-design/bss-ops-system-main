import Decimal from "decimal.js";

/**
 * 税率展示：原型口径是「未填写」而不是占位符，且保留小数位（6.25% 不能显示成 6%）。
 * 历史数据可能是 0.0625 这样的比率，统一乘 100。
 */
export function formatSupplierTaxRate(rate: string | null | undefined): string {
  if (rate == null || rate.trim() === "") return "未填写";
  const percent = new Decimal(rate).mul(100).toFixed(2);
  const [integer, decimal] = percent.split(".");
  const trimmed = decimal ? `${integer}.${decimal.replace(/0+$/, "")}` : percent;
  return `${trimmed.replace(/\.$/, "")}%`;
}
