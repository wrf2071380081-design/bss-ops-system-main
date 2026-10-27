<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from "vue";
import { standardModelsApi } from "../../../api/standardModels";
import { factLabels, modelTypeLabels, standardFieldLabel } from "../../../api/standardModels.contract";
import type { ModelFilterOptions, StandardFact, StandardModelDTO, StandardModelQuery } from "../../../api/standardModels.types";
import StandardModelMaintainDialog from "./StandardModelMaintainDialog.vue";
import StandardModelOperationRecords from "./StandardModelOperationRecords.vue";
import CatalogFilterSelect from "./CatalogFilterSelect.vue";
import { modelParameterLabel, modelPriceGroups } from "../../../api/standardModelPresentation";
const activeTab = ref("models");
const modelNames = ref<Record<string, string>>({});
const companyOptions = computed(() => options.value.companies.map(value => ({ label: value, value })));
const typeOptions = Object.entries(modelTypeLabels).map(([value, label]) => ({ label, value }));
const regionOptions = [{ label: "国内", value: "CN" }, { label: "海外", value: "OVERSEAS" }];
let keywordTimer: ReturnType<typeof setTimeout> | undefined;

const query = reactive<StandardModelQuery>({ page: 1, size: 100, keyword: "", companies: [], model_types: [], supplier_ids: [], regions: [], verify_status: "", sort_by: "updated_at", order: "desc" });
const data = ref<{ list: StandardModelDTO[]; total: number }>(), loading = ref(false), error = ref(""), editorOpen = ref(false), editID = ref<string>();
const editorPanel = ref<'profile' | 'history' | 'records' | 'advanced'>('profile');
let request: AbortController | undefined;
let active = true;
const options = ref<ModelFilterOptions>({ companies: [], suppliers: [], has_more: false }), optionsLoading = ref(false), optionsError = ref("");
let optionsRequest: AbortController | undefined;
async function loadOptions(keyword = "") {
  optionsRequest?.abort(); const current = optionsRequest = new AbortController();
  optionsLoading.value = true; optionsError.value = "";
  try { const result = await standardModelsApi.options(keyword, [...(query.regions || [])], current.signal); if (active && !current.signal.aborted) options.value = result; }
  catch (value) { if (active && !current.signal.aborted) { options.value = { companies: [], suppliers: [], has_more: false }; optionsError.value = value instanceof Error ? value.message : "筛选选项暂时无法加载。"; } }
  finally { if (active && !current.signal.aborted) optionsLoading.value = false; }
}
function regionChanged() { query.supplier_ids = []; void loadOptions(); void load(true); }
function filtersChanged() { void load(true); }
function keywordChanged() { clearTimeout(keywordTimer); keywordTimer = setTimeout(() => { void load(true); }, 250); }
async function load(reset = false) {
  if (reset) query.page = 1;
  request?.abort(); const currentRequest = request = new AbortController();
  loading.value = true; error.value = "";
  const params = { ...query, companies: [...(query.companies || [])], model_types: [...(query.model_types || [])], supplier_ids: [...(query.supplier_ids || [])], regions: [...(query.regions || [])] };
  try {
    const list: StandardModelDTO[] = [], ids = new Set<string>(); let total: number | undefined;
    for (let page = 1; ; page++) {
      const result = await standardModelsApi.list({ ...params, page }, currentRequest.signal);
      if (!active || currentRequest.signal.aborted) return;
      if (result.page !== page || result.size !== params.size || result.list.length > params.size || (total !== undefined && total !== result.total)) throw new Error("模型资料已变化，请重新加载。");
      total = result.total;
      for (const model of result.list) { if (ids.has(model.id)) throw new Error("模型资料已变化，请重新加载。"); ids.add(model.id); list.push(model); }
      if (list.length === total) break;
      if (!result.list.length || list.length > total) throw new Error("模型资料分页不完整，请重新加载。");
    }
    data.value = { list, total: total! }; for (const model of list) modelNames.value[model.id] = model.name;
  }
  catch (value) { if (active && !currentRequest.signal.aborted) { data.value = undefined; error.value = value instanceof Error ? value.message : "模型资料暂时无法加载，请重试。"; } }
  finally { if (active && !currentRequest.signal.aborted) loading.value = false; }
}
function reset() { clearTimeout(keywordTimer); Object.assign(query, { page: 1, size: 100, keyword: "", companies: [], model_types: [], supplier_ids: [], regions: [], verify_status: "", sort_by: "updated_at", order: "desc" }); void load(); void loadOptions(); }
function open(model?: StandardModelDTO, panel: 'profile' | 'history' | 'records' | 'advanced' = 'profile') { editID.value = model?.id; editorPanel.value = panel; editorOpen.value = true; }
function openMore(model: StandardModelDTO, panel: unknown) { if (panel === 'history' || panel === 'records' || panel === 'advanced') open(model, panel); }
const factText = (fact: StandardFact) => fact.disclosure === "DISCLOSED" ? fact.value : factLabels[fact.disclosure] || "未核实";
onMounted(() => { void load(); void loadOptions(); });
onBeforeUnmount(() => { active = false; clearTimeout(keywordTimer); request?.abort(); optionsRequest?.abort(); });
</script>

<template>
  <section class="standard-library">
    <header class="library-heading"><div><div class="eyebrow">TOKEN OPS / CATALOG</div><h1>标准模型库</h1><p>官方模型资料与独立更新结果，维护操作完整留痕。</p></div><el-button type="primary" @click="open()">＋ 新增模型</el-button></header>
    <el-tabs v-model="activeTab" class="catalog-tabs"><el-tab-pane label="标准模型" name="models" /><el-tab-pane label="操作记录" name="records" /></el-tabs>
    <section v-if="activeTab === 'records'" class="catalog-panel"><header class="catalog-heading"><h2>操作记录</h2></header><StandardModelOperationRecords  /></section>
    <template v-else>
      <div class="crawl-status" role="status"><span>上次成功抓取：暂无真实成功记录</span><span>最近尝试：尚未执行</span><span class="crawl-unavailable">自动抓取尚未接入</span><el-tooltip content="官方资料自动拉取属于后续增强，当前没有可用的抓取接口。"><span><el-button size="small" disabled>手动更新</el-button></span></el-tooltip></div>
      <section class="catalog-panel">
      <header class="catalog-heading"><h2>官方模型参数表</h2></header>
      <form class="catalog-filters" @submit.prevent="load(true)">
        <CatalogFilterSelect v-model="query.companies!" label="模型公司" :options="companyOptions" :loading="optionsLoading" remote @search="loadOptions" @change="filtersChanged" />
        <CatalogFilterSelect v-model="query.model_types!" label="模型类型" :options="typeOptions" @change="filtersChanged" />
        <CatalogFilterSelect v-model="query.regions!" label="部署节点" :options="regionOptions" @change="regionChanged" />
        <el-input v-model="query.keyword" class="catalog-search" clearable aria-label="搜索模型名称或官方ID" placeholder="搜索模型名称 / 官方 ID" @input="keywordChanged" />
        <el-button @click="reset">重置</el-button>
      </form>
      <el-alert v-if="optionsError" :title="optionsError" type="warning" :closable="false"><el-button text @click="loadOptions()">重新加载筛选选项</el-button></el-alert>
      <p v-if="options.has_more" class="options-hint">还有更多选项，请输入名称缩小搜索范围。</p>
      <div class="catalog-note"><span>{{ data ? `${data.total} 个模型 · 官方参数快照` : '官方参数快照' }}</span><span>价格币种和特殊单位逐项标注 · 未核实 ≠ 不支持 · 内容随页面宽度排列，窄屏支持横向滚动</span></div>
      <el-alert v-if="error" :title="error" type="error" show-icon :closable="false"><el-button text @click="load()">重新加载</el-button></el-alert>
      <div v-loading="loading" class="official-scroll" tabindex="0" role="region" aria-label="官方模型参数表，可横向滚动">
        <table class="official-table"><thead><tr>
          <th scope="col">模型名称 / 官方 ID / 厂商</th><th scope="col">模型类型 / 输入输出</th>
          <th scope="col">容量规格</th><th scope="col">核心能力</th><th scope="col">官方价格 / 计费单位</th><th scope="col">API / 限流 / 访问条件</th><th scope="col">发布 / 版本 / 生命周期</th><th scope="col">最近更新结果 / 上次成功</th><th scope="col">操作</th>
        </tr></thead><tbody>
          <tr v-for="model in data?.list || []" :key="model.id">
            <td><strong>{{ model.name || '待补充名称' }}</strong><span v-if="model.missing_fields.length" class="meta" :title="model.missing_fields.map(standardFieldLabel).join('、')">资料待补齐 · 暂不可报价</span><span v-else-if="!model.quote_readiness.ready" class="meta">待人工核验 · 暂不可报价</span><code>{{ model.official_id || '官方 ID 待补齐' }}</code><span class="meta">{{ model.company || '公司待补齐' }}</span><span v-for="source in model.sources.slice(1)" :key="source.url" class="meta">{{ source.label }}</span></td>
            <td><span class="model-type">{{ modelTypeLabels[model.model_type] || '类型待补齐' }}</span><p class="spec-line">输入：{{ model.input_modalities.map(value => modelTypeLabels[value]).join('、') || '待补齐' }}</p><p class="spec-line">输出：{{ model.output_modalities.map(value => modelTypeLabels[value]).join('、') || '待补齐' }}</p></td>
            <td><p v-for="fact in model.capacity" :key="fact.key" class="spec-line" :class="{ unknown: fact.disclosure !== 'DISCLOSED' }">{{ modelParameterLabel(fact.key) }}：{{ factText(fact) }}</p><span v-if="!model.capacity.length" class="unknown">待补齐</span></td>
            <td><p v-for="ability in model.abilities" :key="ability.key" class="spec-line" :class="{ unknown: !['SUPPORTED', 'UNSUPPORTED'].includes(ability.state) }">{{ modelParameterLabel(ability.key) }} {{ ability.state === 'SUPPORTED' ? '✓' : ability.state === 'UNSUPPORTED' ? '×' : ability.state === 'UNDISCLOSED' ? '：未披露' : '：未核实' }}<span v-if="ability.description">（{{ ability.description }}）</span></p><span v-if="!model.abilities.length" class="unknown">待补齐</span></td>
            <td><div v-for="group in modelPriceGroups(model.price_items)" :key="group.key" class="price-group"><p v-for="line in group.lines" :key="line" class="spec-line">{{ line }}</p><p class="spec-line">{{ group.unit }}</p><p v-if="group.condition" class="spec-line price-condition">{{ group.condition }}</p><p v-for="value in group.dimensions" :key="value" class="spec-line">{{ value }}</p></div><span v-if="!model.price_items.length" class="unknown">未知价格 · 待补齐</span></td>
            <td><p v-for="fact in model.api_access" :key="fact.key" class="spec-line" :class="{ unknown: fact.disclosure !== 'DISCLOSED' }">{{ modelParameterLabel(fact.key) }}：{{ factText(fact) }}</p><span v-if="!model.api_access.length" class="unknown">待补齐</span></td>
            <td><p v-for="fact in model.lifecycle" :key="fact.key" class="spec-line" :class="{ unknown: fact.disclosure !== 'DISCLOSED' }">{{ modelParameterLabel(fact.key) }}：{{ factText(fact) }}</p><span v-if="!model.lifecycle.length" class="unknown">待补齐</span></td>
            <td><span class="update-state">尚未更新</span><span class="meta">最近尝试：未记录</span><span class="meta">上次成功：暂无成功记录</span><span class="meta">未连接真实抓取</span><span class="meta">资料核验：{{ model.checked_at || '未记录' }}</span></td>
            <td><el-button size="small" @click="open(model)">维护</el-button><el-dropdown trigger="click" @command="openMore(model, $event)"><el-button text size="small" aria-label="更多模型操作">更多</el-button><template #dropdown><el-dropdown-menu><el-dropdown-item command="history">历史版本</el-dropdown-item><el-dropdown-item command="records">操作记录</el-dropdown-item><el-dropdown-item command="advanced">补充资料</el-dropdown-item></el-dropdown-menu></template></el-dropdown></td>
          </tr>
          <tr v-if="!loading && !error && !data?.list.length"><td colspan="9" class="empty-row">当前筛选暂无模型</td></tr>
        </tbody></table>
      </div>

      </section>
    </template>
    <StandardModelMaintainDialog v-if="editorOpen" :id="editID" :initial-panel="editorPanel" @close="editorOpen = false" @saved="load()" />
  </section>
</template>

<style scoped>
.standard-library{color:#1c1d1f;--el-color-primary:#315ee8}.library-heading{display:flex;justify-content:space-between;align-items:center;gap:20px;margin-bottom:16px}.library-heading h1{font-size:22px;margin:0 0 7px}.library-heading p{color:#8a9099;font-size:12px;margin:0}.catalog-tabs{margin-bottom:16px}.catalog-tabs:deep(.el-tabs__header){margin:0}.catalog-tabs:deep(.el-tabs__item){font-size:13px;height:44px;font-weight:500}.catalog-tabs:deep(.el-tabs__nav-wrap:after){height:1px}.crawl-status{display:flex;align-items:center;gap:12px;flex-wrap:wrap;padding:12px 14px;border:1px solid #e3e6eb;border-radius:6px;background:#fafbfc;font-size:12px;margin-bottom:16px}.crawl-unavailable{color:#9a761d}.catalog-panel{border:1px solid #e3e6eb;background:#fff;border-radius:8px;overflow:hidden}.catalog-heading{padding:14px 16px;border-bottom:1px solid #e3e6eb}.catalog-heading h2{font-size:15px;font-weight:600;margin:0;line-height:20px}.catalog-filters{display:flex;align-items:center;gap:8px;flex-wrap:wrap;padding:12px 16px;border-bottom:1px solid #e3e6eb}.catalog-search{flex:1;min-width:180px;width:220px}.catalog-filters>.el-button{margin:0}.options-hint{margin:0;padding:8px 16px;font-size:12px;color:#8d929a}.catalog-note{display:flex;justify-content:space-between;align-items:center;gap:8px;flex-wrap:wrap;padding:10px 16px;font-size:12px;color:#8d929a}.official-scroll{width:100%;overflow:auto;min-height:180px;overscroll-behavior:contain}.official-table{width:100%;min-width:1150px;border-collapse:collapse;table-layout:fixed;font-size:12px;line-height:1.5;font-variant-numeric:tabular-nums}.official-table th,.official-table td{padding:12px;text-align:left;vertical-align:top;border-bottom:1px solid #eceef1;overflow-wrap:anywhere;white-space:normal}.official-table th{background:#edf3f3;color:#536a70;font-size:12px;font-weight:500}.official-table th:first-child{width:12%}.official-table th:nth-child(5){width:15%}.official-table th:nth-last-child(2){width:13%}.official-table th:last-child{width:76px}.official-table th:first-child,.official-table td:first-child{position:sticky;left:0;z-index:1;border-right:1px solid #eceef1}.official-table td{background:white}.official-table tbody tr:nth-child(even) td{background:#f8fafb}.official-table tbody tr:hover td{background:#f4f7fa}.official-table strong{display:block;font-size:13px;font-weight:500}.official-table code{display:block;margin:5px 0;font-size:11px;color:#617a82;word-break:break-word}.spec-line{margin:3px 0;line-height:1.5;white-space:pre-line}.model-type,.update-state{display:inline-block;font-size:11px;padding:2px 7px;border-radius:5px;background:#f0f1f4;color:#727985;margin-bottom:4px;line-height:18px}.meta{display:block;font-size:12px;color:#8b9098;margin-top:3px;line-height:16px}.unknown,.price-condition{color:#8b9299}.price-group+.price-group{margin-top:10px}.official-table td.empty-row{text-align:center;padding:40px 20px;color:#8b9098}.catalog-pagination{display:flex;justify-content:space-between;align-items:center;gap:12px;flex-wrap:wrap;padding:10px 16px;font-size:12px;color:#91969f}.catalog-pagination :deep(.el-pagination){font-weight:400}:deep(.el-button--primary){--el-button-bg-color:#315ee8;--el-button-border-color:#315ee8}@media(max-width:900px){.official-table{min-width:1100px}}@media(max-width:700px){.library-heading{align-items:flex-start;flex-wrap:wrap}.catalog-search{flex-basis:100%}.catalog-note{align-items:flex-start}.catalog-filters:deep(.catalog-select){width:158px}}
</style>
