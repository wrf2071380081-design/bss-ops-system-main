import Decimal from "decimal.js";

export function formatSupplierTaxRate(rate: string | null | undefined): string {
  if (rate == null || rate.trim() === "") return "—";
  return `${new Decimal(rate).mul(100).toFixed()}%`;
}
