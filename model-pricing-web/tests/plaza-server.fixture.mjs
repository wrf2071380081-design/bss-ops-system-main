import Decimal from "decimal.js";

// An HTTP fixture for server pagination; the browser must send every filter.
export function resourcePage(rows, query) {
  const dimensions = { companies: "company", model_types: "model_type", supplier_ids: "supplier_id", regions: "region", states: "state" };
  let filtered = rows.filter(row => {
    if (query.standard_model_id && row.standard_model_id !== query.standard_model_id) return false;
    for (const [plural, field] of Object.entries(dimensions)) {
      if (query[field] && row[field] !== query[field]) return false;
      if (query[plural] && !JSON.parse(query[plural]).includes(row[field])) return false;
    }
    if (query.keyword && ![row.model_name, row.official_id, row.supplier_name, row.supplier_model_id].join(" ").toLowerCase().includes(query.keyword.toLowerCase())) return false;
    for (const key of ["coefficient", "connection", "rps", "tps"]) {
      if (!query[`${key}_min`] && !query[`${key}_max`]) continue;
      const value = key === "coefficient" ? row.coefficient : row.test.status === "COMPLETED" ? row.test.metrics[key === "connection" ? "connection_ms" : key] : null;
      if (value == null || (query[`${key}_min`] && new Decimal(value).lt(query[`${key}_min`])) || (query[`${key}_max`] && new Decimal(value).gt(query[`${key}_max`]))) return false;
    }
    return true;
  });
  if (query.view === "grouped") {
    filtered = [...new Set(filtered.map(row => row.standard_model_id))].map(id => {
      const items = filtered.filter(row => row.standard_model_id === id), first = items[0];
      const coefficients = items.map(row => new Decimal(row.coefficient)).sort((a, b) => a.cmp(b));
      return { id, name: first.model_name, official_id: first.official_id, company: first.company, model_type: first.model_type, supplier_count: new Set(items.map(row => row.supplier_id)).size, resource_count: items.length, available_count: items.filter(row => row.quote_readiness.eligible).length, coefficient_min: coefficients[0].toFixed(), coefficient_max: coefficients.at(-1).toFixed(), regions: [...new Set(items.map(row => row.region))], state_counts: items.reduce((counts, row) => ({ ...counts, [row.state]: (counts[row.state] || 0) + 1 }), {}) };
    });
  }
  const key = query.sort_by || "id";
  filtered.sort((a, b) => {
    const x = a[key] ?? a.test?.metrics[key], y = b[key] ?? b.test?.metrics[key];
    if (x == null || y == null) return x == null && y == null ? 0 : x == null ? 1 : -1;
    const numeric = ["id", "coefficient", "coefficient_min", "coefficient_max", "supplier_count", "resource_count", "available_count", "connection_ms", "rps", "tps"].includes(key);
    return (numeric ? new Decimal(x).cmp(y) : String(x).localeCompare(String(y), "zh-CN")) * (query.order === "desc" ? -1 : 1);
  });
  const page = Number(query.page), size = Number(query.size), start = (page - 1) * size;
  return { view: query.view, list: filtered.slice(start, start + size), total: filtered.length, page, size };
}
export function resourceOptions(rows) {
  return { companies: [...new Set(rows.map(row => row.company))], suppliers: [...new Map(rows.map(row => [row.supplier_id, { id: row.supplier_id, name: row.supplier_name, region: row.region }])).values()], has_more: false };
}
