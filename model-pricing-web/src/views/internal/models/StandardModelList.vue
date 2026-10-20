<script setup lang="ts">
import { onBeforeUnmount, onMounted, reactive, ref } from "vue";
import { standardModelsApi } from "../../../api/standardModels";
import { abilityLabels, factLabels, formatStandardPrice, isOfficialURL, modelTypeLabels, standardFieldLabel } from "../../../api/standardModels.contract";
import type { StandardFact, StandardModelDTO, StandardModelPage, StandardModelQuery } from "../../../api/standardModels.types";
import StandardModelMaintainDialog from "./StandardModelMaintainDialog.vue";

const query = reactive<StandardModelQuery>({ page: 1, size: 20, keyword: "", company: "", model_type: "", verify_status: "", sort_by: "updated_at", order: "desc" });
const data = ref<StandardModelPage>(), loading = ref(false), error = ref(""), editorOpen = ref(false), editID = ref<string>();
const verificationLabels: Record<string, string> = { INCOMPLETE: "资料待补齐", UNVERIFIED: "待人工核验", VERIFIED: "已人工核验" };
let request: AbortController | undefined;
let active = true;
async function load(reset = false) {
  if (reset) query.page = 1;
  request?.abort(); const currentRequest = request = new AbortController();
  loading.value = true; error.value = "";
  try { const result = await standardModelsApi.list({ ...query }, currentRequest.signal); if (active && !currentRequest.signal.aborted) data.value = result; }
  catch (value) { if (active && !currentRequest.signal.aborted) { data.value = undefined; error.value = value instanceof Error ? value.message : "模型资料暂时无法加载，请重试。"; } }
  finally { if (active && !currentRequest.signal.aborted) loading.value = false; }
}
function reset() { Object.assign(query, { page: 1, size: 20, keyword: "", company: "", model_type: "", verify_status: "", sort_by: "updated_at", order: "desc" }); void load(); }
function sort(key: string) { query.order = query.sort_by === key && query.order === "asc" ? "desc" : "asc"; query.sort_by = key; void load(true); }
function open(model?: StandardModelDTO) { editID.value = model?.id; editorOpen.value = true; }
const factText = (fact: StandardFact) => fact.disclosure === "DISCLOSED" ? fact.value : factLabels[fact.disclosure] || "未核实";
onMounted(() => { void load(); });
onBeforeUnmount(() => { active = false; request?.abort(); });
</script>

<template>
  <section class="standard-library">
    <header class="library-heading"><div><h1>标准模型库</h1><p>官方资料与计费口径；新增、修改、手动补全均保留记录。</p></div><el-button type="primary" @click="open()">＋ 新增模型</el-button></header>
    <section class="catalog-panel">
      <header class="catalog-heading"><h2>官方模型参数表</h2><span>未核实 ≠ 不支持 · 价格币种和特殊单位逐项标注</span></header>
      <form class="catalog-filters" @submit.prevent="load(true)">
        <label>搜索<el-input v-model="query.keyword" clearable placeholder="模型名称 / 官方 ID" /></label>
        <label>模型公司<el-input v-model="query.company" clearable placeholder="输入公司名称" /></label>
        <label>模型类型<el-select v-model="query.model_type" clearable placeholder="全部类型"><el-option v-for="(text, key) in modelTypeLabels" :key="key" :label="text" :value="key" /></el-select></label>
        <label>资料状态<el-select v-model="query.verify_status" clearable placeholder="全部状态"><el-option v-for="(text, key) in verificationLabels" :key="key" :label="text" :value="key" /></el-select></label>
        <el-button native-type="submit" type="primary" :loading="loading">查询</el-button><el-button @click="reset">重置</el-button>
      </form>
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
.standard-library{color:#20313c}.library-heading{display:flex;justify-content:space-between;align-items:center;gap:20px;margin-bottom:22px}.library-heading h1{font-size:25px;margin:0 0 9px}.library-heading p{color:#79868f;font-size:14px;margin:0}.catalog-panel{border:1px solid #e6ebee;background:#fff;border-radius:10px;overflow:hidden}.catalog-heading{display:flex;align-items:center;gap:16px;flex-wrap:wrap;padding:18px 22px;border-bottom:1px solid #e6ebee}.catalog-heading h2{font-size:17px;margin:0}.catalog-heading>span,.catalog-note{font-size:12px;color:#79868f}.catalog-filters{display:flex;align-items:end;gap:12px;flex-wrap:wrap;padding:18px 22px;border-bottom:1px solid #e6ebee}.catalog-filters label{display:grid;gap:7px;width:180px;font-size:13px}.catalog-note{display:flex;justify-content:space-between;align-items:center;gap:14px;flex-wrap:wrap;padding:14px 22px}.catalog-sort{display:flex;gap:8px}.catalog-sort label{display:flex;align-items:center;gap:7px}.catalog-sort .el-select{width:120px}.official-scroll{overflow:auto;min-height:180px}.official-table{width:100%;border-collapse:collapse;font-size:13px}.official-table th,.official-table td{padding:15px 16px;text-align:left;vertical-align:top;border-bottom:1px solid #e4ecee;overflow-wrap:anywhere;min-width:145px}.official-table th{background:#f4f8f8;color:#526970;font-weight:600;white-space:nowrap}.official-table th:last-child,.official-table td:last-child{min-width:80px}.official-table td:nth-child(5){min-width:230px}.official-table strong{display:block;font-weight:600}.official-table code{display:block;margin:8px 0;word-break:break-all;font-size:12px}.official-table p{margin:0 0 12px;line-height:1.7}.meta{display:block;font-size:12px;color:#79868f;margin-top:6px}.source-link{display:block;color:#087c72;line-height:1.8}.unknown{color:#9a761d;font-size:12px}.sort-heading{border:0;background:none;padding:0;font:inherit;color:inherit;cursor:pointer}.official-table td.empty-row{text-align:center;padding:50px 20px;color:#79868f}.catalog-pagination{display:flex;justify-content:space-between;align-items:center;gap:12px;flex-wrap:wrap;padding:16px 22px}.catalog-pagination>span{font-size:13px;color:#79868f}:deep(.el-button--primary){--el-button-bg-color:#087c72;--el-button-border-color:#087c72}@media(max-width:700px){.library-heading{align-items:flex-start;flex-wrap:wrap}.catalog-filters label{width:calc(50% - 6px)}.catalog-note{align-items:flex-start}}
</style>
