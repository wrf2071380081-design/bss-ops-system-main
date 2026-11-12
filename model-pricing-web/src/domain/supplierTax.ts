import Decimal from "decimal.js";

/**
 * 税率展示：原型口径是「未填写」而不是占位符，且保留有效小数位（6.25% 不能显示成 6%）。
 *
 * 库里存在两种口径：0.0625（比率）和 6.25（百分数）。之前统一乘 100，
 * 导致 6.00 显示成 600%；按「绝对值 ≤ 1 视为比率」判断，两种都能显示对。
 */
export function formatSupplierTaxRate(rate: string | null | undefined): string {
  if (rate == null || rate.trim() === "") return "未填写";
  const value = new Decimal(rate);
  const percent = value.abs().lte(1) ? value.mul(100) : value;
  const text = percent.toFixed(2).replace(/\.?0+$/, "");
  return `${text === "" ? "0" : text}%`;
}
