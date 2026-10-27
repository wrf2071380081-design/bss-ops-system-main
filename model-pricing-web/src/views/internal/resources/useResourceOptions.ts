import { computed, onBeforeUnmount, ref, watch, type Ref } from "vue";
import { resourcesApi } from "../../../api/resources";
import type { ResourceFilterOptions } from "../../../api/resources.types";
import type { SourceFilters } from "../../../domain/modelPlaza";
export function useResourceOptions(filters: Ref<SourceFilters>) {
  const options = ref<ResourceFilterOptions>({ companies: [], suppliers: [], has_more: false }), error = ref("");
  const selectedNames = new Map<string, ResourceFilterOptions["suppliers"][number]>(); let controller: AbortController | undefined;
  const choices = computed(() => ({ ...options.value, suppliers: [...filters.value.supplier_ids.filter(id => !options.value.suppliers.some(supplier => supplier.id === id)).flatMap(id => selectedNames.has(id) ? [selectedNames.get(id)!] : []), ...options.value.suppliers] }));
  async function loadOptions(keyword = "") {
    controller?.abort(); const request = controller = new AbortController(); error.value = "";
    try { const result = await resourcesApi.options(keyword, filters.value.regions.length === 1 ? filters.value.regions[0]! : "", request.signal); if (!request.signal.aborted) { options.value = result; for (const supplier of result.suppliers) selectedNames.set(supplier.id, supplier); } }
    catch (value) { if (!request.signal.aborted) error.value = value instanceof Error ? value.message : "筛选选项加载失败。"; }
  }
  watch(() => JSON.stringify(filters.value.regions), () => { void loadOptions(); });
  onBeforeUnmount(() => controller?.abort());
  return { choices, optionError: error, loadOptions };
}
