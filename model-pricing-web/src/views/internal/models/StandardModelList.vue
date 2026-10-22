<script setup lang="ts">
import { onBeforeUnmount, onMounted, reactive, ref } from "vue";
import { standardModelsApi } from "../../../api/standardModels";
import { abilityLabels, factLabels, formatStandardPrice, isOfficialURL, modelTypeLabels, standardFieldLabel } from "../../../api/standardModels.contract";
import type { ModelFilterOptions, StandardFact, StandardModelDTO, StandardModelPage, StandardModelQuery } from "../../../api/standardModels.types";
import StandardModelMaintainDialog from "./StandardModelMaintainDialog.vue";
import ResourceOperationRecords from "../resources/ResourceOperationRecords.vue";
const activeTab = ref("models");
const advancedFilters = ref(false);

const query = reactive<StandardModelQuery>({ page: 1, size: 20, keyword: "", companies: [], model_types: [], supplier_ids: [], regions: [], verify_status: "", sort_by: "updated_at", order: "desc" });
const data = ref<StandardModelPage>(), loading = ref(false), error = ref(""), editorOpen = ref(false), editID = ref<string>();
const verificationLabels: Record<string, string> = { INCOMPLETE: "资料待补齐", UNVERIFIED: "待人工核验", VERIFIED: "已人工核验" };
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
function regionChanged() { query.supplier_ids = []; void loadOptions(); }
async function load(reset = false) {
  if (reset) query.page = 1;
  request?.abort(); const currentRequest = request = new AbortController();
  loading.value = true; error.value = "";
  try { const result = await standardModelsApi.list({ ...query }, currentRequest.signal); if (active && !currentRequest.signal.aborted) data.value = result; }
  catch (value) { if (active && !currentRequest.signal.aborted) { data.value = undefined; error.value = value instanceof Error ? value.message : "模型资料暂时无法加载，请重试。"; } }
  finally { if (active && !currentRequest.signal.aborted) loading.value = false; }
}
function reset() { Object.assign(query, { page: 1, size: 20, keyword: "", companies: [], model_types: [], supplier_ids: [], regions: [], verify_status: "", sort_by: "updated_at", order: "desc" }); void load(); void loadOptions(); }
function sort(key: string) { query.order = query.sort_by === key && query.order === "asc" ? "desc" : "asc"; query.sort_by = key; void load(true); }
function open(model?: StandardModelDTO) { editID.value = model?.id; editorOpen.value = true; }
const factText = (fact: StandardFact) => fact.disclosure === "DISCLOSED" ? fact.value : factLabels[fact.disclosure] || "未核实";
onMounted(() => { void load(); void loadOptions(); });
onBeforeUnmount(() => { active = false; request?.abort(); optionsRequest?.abort(); });
</script>

<template>
  <section class="standard-library">
    <header class="library-heading"><div><div class="eyebrow">TOKEN OPS / CATALOG</div><h1>标准模型库</h1><p>官方模型资料与独立更新结果，维护操作完整留痕。</p></div><el-button type="primary" @click="open()">＋ 新增模型</el-button></header>
    <el-tabs v-model="activeTab" class="catalog-tabs"><el-tab-pane label="标准模型" name="models" /><el-tab-pane label="操作记录" name="records" /></el-tabs>
    <section v-if="activeTab === 'records'" class="catalog-panel"><header class="catalog-heading"><h2>操作记录</h2></header><ResourceOperationRecords entity-type="standard_model" /></section>
    <section v-else class="catalog-panel">
      <header class="catalog-heading"><h2>官方模型参数表</h2><span>未核实 ≠ 不支持 · 价格币种和特殊单位逐项标注</span></header>
      <form class="catalog-filters" @submit.prevent="load(true)">
        <label>模型公司<el-select v-model="query.companies" multiple collapse-tags collapse-tags-tooltip filterable remote :remote-method="loadOptions" :loading="optionsLoading" clearable aria-label="模型公司" placeholder="全部公司"><el-option v-for="company in options.companies" :key="company" :label="company" :value="company" /></el-select></label>
        <label>模型类型<el-select v-model="query.model_types" multiple collapse-tags collapse-tags-tooltip aria-label="模型类型" clearable placeholder="全部类型"><el-option v-for="(text, key) in modelTypeLabels" :key="key" :label="text" :value="key" /></el-select></label>
        <label>部署节点<el-select v-model="query.regions" multiple clearable aria-label="部署节点" placeholder="全部节点" @change="regionChanged"><el-option label="国内" value="CN" /><el-option label="海外" value="OVERSEAS" /></el-select></label>
        <label class="catalog-search"><el-input v-model="query.keyword" clearable aria-label="搜索模型名称或官方ID" placeholder="搜索模型名称 / 官方 ID" /></label>
        <label v-if="advancedFilters">供应商<el-select v-model="query.supplier_ids" multiple collapse-tags collapse-tags-tooltip filterable remote :remote-method="loadOptions" :loading="optionsLoading" clearable aria-label="供应商" placeholder="全部供应商"><el-option v-for="supplier in options.suppliers" :key="supplier.id" :label="`${supplier.name} · ${supplier.region === 'CN' ? '国内' : '海外'}`" :value="supplier.id" /></el-select></label>
        <label v-if="advancedFilters">资料状态<el-select v-model="query.verify_status" clearable placeholder="全部状态"><el-option v-for="(text, key) in verificationLabels" :key="key" :label="text" :value="key" /></el-select></label>
        <el-button native-type="submit" type="primary" :loading="loading">查询</el-button><el-button @click="reset">重置</el-button>
        <el-button text :aria-expanded="advancedFilters" @click="advancedFilters = !advancedFilters">{{ advancedFilters ? '收起筛选' : '更多筛选' }}</el-button>
      </form>
      <el-alert v-if="optionsError" :title="optionsError" type="warning" :closable="false"><el-button text @click="loadOptions()">重新加载筛选选项</el-button></el-alert>
      <p v-if="options.has_more" class="catalog-note">还有更多选项，请输入名称缩小搜索范围。</p>
      <div class="catalog-note"><span>{{ data ? `${data.total} 个模型 · 官方参数快照` : '官方参数快照' }}</span><div class="catalog-sort"><label>排序<el-select v-model="query.sort_by" aria-label="排序字段" @change="load(true)"><el-option label="最近更新" value="updated_at" /><el-option label="模型名称" value="name" /><el-option label="模型公司" value="company" /><el-option label="模型类型" value="model_type" /><el-option label="核对日期" value="checked_at" /></el-select></label><el-select v-model="query.order" aria-label="排序方向" @change="load(true)"><el-option label="降序" value="desc" /><el-option label="升序" value="asc" /></el-select></div><span>空值末尾，窄屏可横向滚动</span></div>
      <el-alert v-if="error" :title="error" type="error" show-icon :closable="false"><el-button text @click="load()">重新加载</el-button></el-alert>
      <div v-loading="loading" class="official-scroll" tabindex="0" role="region" aria-label="官方模型参数表，可横向滚动">
        <table class="official-table"><thead><tr>
          <th scope="col" :aria-sort="query.sort_by === 'name' ? query.order === 'asc' ? 'ascending' : 'descending' : 'none'"><button type="button" class="sort-heading" @click="sort('name')">模型名称 / 官方 ID / 厂商 ↕</button></th>
          <th scope="col" :aria-sort="query.sort_by === 'model_type' ? query.order === 'asc' ? 'ascending' : 'descending' : 'none'"><button type="button" class="sort-heading" @click="sort('model_type')">模型类型 / 输入输出 ↕</button></th>
          <th scope="col">容量规格</th><th scope="col">核心能力</th><th scope="col">官方价格 / 计费单位</th><th scope="col">API / 限流 / 访问条件</th><th scope="col">发布 / 版本 / 生命周期</th><th scope="col">官方来源 / 核对日期</th><th scope="col">操作</th>
        </tr></thead><tbody>
          <tr v-for="model in data?.list || []" :key="model.id">
            <td><strong>{{ model.name || '待补充名称' }}</strong><code>{{ model.official_id || '官方 ID 待补齐' }}</code><span class="meta">{{ model.company || '公司待补齐' }}</span><el-tag size="small" :type="model.verify_status === 'VERIFIED' ? 'success' : 'warning'">{{ verificationLabels[model.verify_status] }}</el-tag><span v-if="model.missing_fields.length" class="meta">{{ model.missing_fields.map(standardFieldLabel).join('、') }} 待补齐</span></td>
            <td><strong>{{ modelTypeLabels[model.model_type] || '类型待补齐' }}</strong><p>输入：{{ model.input_modalities.map(value => modelTypeLabels[value]).join('、') || '待补齐' }}</p><p>输出：{{ model.output_modalities.map(value => modelTypeLabels[value]).join('、') || '待补齐' }}</p></td>
            <td><p v-for="fact in model.capacity" :key="fact.key"><span class="meta">{{ fact.key }}</span>{{ factText(fact) }}</p><span v-if="!model.capacity.length" class="unknown">待补齐</span></td>
            <td><p v-for="ability in model.abilities" :key="ability.key"><span class="meta">{{ ability.key }}</span>{{ abilityLabels[ability.state] }}<span v-if="ability.description" class="meta">{{ ability.description }}</span></p><span v-if="!model.abilities.length" class="unknown">待补齐</span></td>
            <td><p v-for="price in model.price_items" :key="price.item_key" class="price-item"><strong>{{ formatStandardPrice(price) }}</strong><span class="meta">{{ price.condition }}</span><span v-for="(value, key) in price.dimensions" :key="key" class="meta">{{ key }}：{{ value }}</span></p><span v-if="!model.price_items.length" class="unknown">未知价格 · 待补齐</span></td>
            <td><p v-for="fact in model.api_access" :key="fact.key"><span class="meta">{{ fact.key }}</span>{{ factText(fact) }}</p><span v-if="!model.api_access.length" class="unknown">待补齐</span></td>
            <td><p v-for="fact in model.lifecycle" :key="fact.key"><span class="meta">{{ fact.key }}</span>{{ factText(fact) }}</p><span v-if="!model.lifecycle.length" class="unknown">待补齐</span></td>
            <td><template v-for="source in model.sources" :key="source.url"><a v-if="isOfficialURL(source.url)" :href="source.url" target="_blank" rel="noopener noreferrer" class="source-link">{{ source.label }} ↗</a><span v-else class="unknown">来源链接无效</span></template><span v-if="!model.sources.length" class="unknown">官方来源待补齐</span><span class="meta">核对：{{ model.checked_at || '未记录' }}</span><span class="meta">{{ model.quote_readiness.ready ? '可用于报价' : '暂不可用于报价' }}</span></td>
            <td><el-button size="small" @click="open(model)">维护</el-button></td>
          </tr>
          <tr v-if="!loading && !error && !data?.list.length"><td colspan="9" class="empty-row">当前筛选暂无模型</td></tr>
        </tbody></table>
      </div>
      <footer v-if="data" class="catalog-pagination"><span>共 {{ data.total }} 个模型</span><el-pagination v-model:current-page="query.page" v-model:page-size="query.size" :page-sizes="[10, 20, 50, 100]" :total="data.total" layout="sizes, prev, pager, next" @current-change="load()" @size-change="load(true)" /></footer>
    </section>
    <StandardModelMaintainDialog v-if="editorOpen" :id="editID" @close="editorOpen = false" @saved="load()" />
  </section>
</template>

<style scoped>
.library-heading h1{font-size:22px!important}.catalog-filters{align-items:center!important;gap:8px!important;padding:14px 16px!important}.catalog-filters label{width:165px!important;display:flex!important;align-items:center;gap:8px;white-space:nowrap;font-size:12px!important}.catalog-filters label>.el-select{flex:1;min-width:0}.catalog-filters .catalog-search{flex:1;min-width:210px}.official-table{font-size:12px!important}.official-table th,.official-table td{padding:12px!important}.catalog-panel>.resource-records{padding:0 18px 18px}

.standard-library{color:#1c1d1f}.library-heading{display:flex;justify-content:space-between;align-items:center;gap:20px;margin-bottom:22px}.library-heading h1{font-size:25px;margin:0 0 9px}.library-heading p{color:#8a9099;font-size:14px;margin:0}.catalog-panel{border:1px solid #e6e8ed;background:#fff;border-radius:10px;overflow:hidden}.catalog-heading{display:flex;align-items:center;gap:16px;flex-wrap:wrap;padding:18px 22px;border-bottom:1px solid #e6e8ed}.catalog-heading h2{font-size:17px;margin:0}.catalog-heading>span,.catalog-note{font-size:12px;color:#8a9099}.catalog-filters{display:flex;align-items:end;gap:12px;flex-wrap:wrap;padding:18px 22px;border-bottom:1px solid #e6e8ed}.catalog-filters label{display:grid;gap:7px;width:180px;font-size:13px}.catalog-note{display:flex;justify-content:space-between;align-items:center;gap:14px;flex-wrap:wrap;padding:14px 22px}.catalog-sort{display:flex;gap:8px}.catalog-sort label{display:flex;align-items:center;gap:7px}.catalog-sort .el-select{width:120px}.official-scroll{overflow:auto;min-height:180px}.official-table{width:100%;border-collapse:collapse;font-size:13px}.official-table th,.official-table td{padding:15px 16px;text-align:left;vertical-align:top;border-bottom:1px solid #e4ecee;overflow-wrap:anywhere;min-width:95px}.official-table th{background:#f8f9fb;color:#526970;font-weight:500;white-space:normal}.official-table th:last-child,.official-table td:last-child{min-width:65px}.official-table td:nth-child(5){min-width:140px}.official-table strong{display:block;font-weight:600}.official-table code{display:block;margin:8px 0;word-break:break-all;font-size:12px}.official-table p{margin:0 0 12px;line-height:1.7}.meta{display:block;font-size:12px;color:#8a9099;margin-top:6px}.source-link{display:block;color:#315ee8;line-height:1.8}.unknown{color:#9a761d;font-size:12px}.sort-heading{border:0;background:none;padding:0;font:inherit;color:inherit;cursor:pointer}.official-table td.empty-row{text-align:center;padding:50px 20px;color:#8a9099}.catalog-pagination{display:flex;justify-content:space-between;align-items:center;gap:12px;flex-wrap:wrap;padding:16px 22px}.catalog-pagination>span{font-size:13px;color:#8a9099}:deep(.el-button--primary){--el-button-bg-color:#315ee8;--el-button-border-color:#315ee8}@media(max-width:700px){.library-heading{align-items:flex-start;flex-wrap:wrap}.catalog-filters label{width:calc(50% - 6px)}.catalog-note{align-items:flex-start}}
</style>
