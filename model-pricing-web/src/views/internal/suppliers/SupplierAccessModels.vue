<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from "vue";
import { ElMessage, ElMessageBox, type UploadFile } from "element-plus";
import { useRoute, useRouter } from "vue-router";
import { supplierEntitiesApi } from "../../../api/supplierEntities";
import type { SupplierEntityDTO, SupplierEntityForm, SupplierQualificationDTO } from "../../../api/supplierEntities.contract";
import { resourcesApi } from "../../../api/resources";
import { resourceOperationsApi } from "../../../api/resourceOperations";
import type { FormalConfigOption } from "../../../api/resourceOperations.contract";
import { buildResourceQuery, resourceCacheDisplay, resourceMetricDisplay, resourceRegionLabels, resourceStateLabels } from "../../../api/resources.contract";
import {submissionFlowsApi,type SubmissionFlowView,type SupplierProgressNode} from '../../../api/submissionFlows';
import { supplierSourceOperationsApi } from "../../../api/supplierSourceOperations";
import type { SourceOperation } from "../../../api/sourceOperations.contract";
import { sourceOperationLocation, supplierOperationCategories, supplierOperationCategoryLabels, supplierOperationLabel, type SourceOperationType } from "../../../api/sourceOperations.contract";
import { modelTypeLabels, standardModelVersion } from "../../../api/standardModels.contract";
import type { ResourceRow } from "../../../api/resources.types";
import { ApiError } from "../../../domain/common";
import { formatDateTime } from "../../../domain/date";
import ResourceDetailDrawer from "../resources/ResourceDetailDrawer.vue";
import ResourceOperationsPanel from "../resources/ResourceOperationsPanel.vue";
import ResourceReportsPanel from "../resources/ResourceReportsPanel.vue";
import F12SubjectPanel from "../subjects/F12SubjectPanel.vue";
import SupplierAccessBatchPanel from './SupplierAccessBatchPanel.vue';
const batchSelected=ref<ResourceRow[]>([]);
// 原型「接入模型」的四个筛选：模型公司 / 模型类型 / 状态 + 关键词（平台模型名称或接口模型 ID）。
const modelFilters=reactive({ company: "", modelType: "", state: "", keyword: "" });
const modelCompanies=computed(()=>[...new Set(rows.value.map(row=>row.company).filter(Boolean))].sort((a,b)=>a.localeCompare(b,"zh-CN")));
const filteredRows=computed(()=>{
  const keyword=modelFilters.keyword.trim().toLowerCase();
  return rows.value.filter(row=>
    (!modelFilters.company||row.company===modelFilters.company)
    &&(!modelFilters.modelType||row.model_type===modelFilters.modelType)
    &&(!modelFilters.state||row.state===modelFilters.state)
    &&(!keyword||`${row.model_name} ${row.supplier_model_id} ${row.official_id||""}`.toLowerCase().includes(keyword)));
});
function resetModelFilters(){ Object.assign(modelFilters,{company:"",modelType:"",state:"",keyword:""}); }
// 系数按有效位展示（1.0000 → 1），与模型广场一致。
const coefficientText = (raw: string) => raw && raw.includes(".") ? raw.replace(/0+$/, "").replace(/\.$/, "") : raw;

const route = useRoute();
const router = useRouter();
const supplierID = computed(() => String(route.params.supplierId || ""));
const rows = ref<ResourceRow[]>([]);
const selectedID = ref("");
const loading = ref(false);
const error = ref("");
const activeTab = ref("models");
// 测试列表弹窗：每次测试单独留档，仅在打开时按资源 ID 拉取报告。
const reportsVisible = ref(false), reportsRow = ref<ResourceRow>();
function openReports(row: ResourceRow) { reportsRow.value = row; reportsVisible.value = true; }
const profile = ref<SupplierEntityDTO>();
const profileFormOpen = ref(false), profileSaving = ref(false), reloadingProfileConflict = ref(false), profileSaveError = ref(""), profileSaveConflict = ref(false), profileReloadKey = ref(0);
// 档案改成页面内表单（原型样式）：编辑内容先落在草稿里，点「保存修改」才入库并写审计。
const profileDraft = ref<SupplierEntityForm>({ name: "", region: "CN", registration_no: "", contact: "", phone: "", tax_rate_percent: "", settlement_currency: "CNY", remark: "" });
watch(() => profile.value, (value) => {
  if (!value) return;
  profileDraft.value = {
    name: value.name,
    region: value.region,
    registration_no: value.registration_no,
    contact: value.contact,
    phone: value.phone,
    // 库里是比率（0.06），表单按百分数展示（6）。
    tax_rate_percent: value.tax_rate === null || value.tax_rate === undefined || value.tax_rate === ""
      ? "" : String(Number(value.tax_rate) * 100),
    settlement_currency: value.settlement_currency,
    remark: value.remark ?? "",
  };
}, { immediate: true, deep: true });
const profileLoading = ref(false), profileError = ref("");
const qualifications = ref<SupplierQualificationDTO[]>([]), qualificationTotal = ref(0), qualificationPage = ref(1), qualificationLoading = ref(false), qualificationError = ref(""), qualificationMetadataSupported = ref<boolean | null>(null);
const qualificationDialogOpen = ref(false), qualificationSaving = ref(false), qualificationSubmittingError = ref(""), qualificationDownloading = ref("");
const qualificationConflict = ref(false), qualificationConflictReady = ref(false), qualificationReloading = ref(false);
const qualificationName = ref(""), qualificationType = ref(""), qualificationValidFrom = ref(""), qualificationValidTo = ref(""), qualificationFileDraft = ref<File>(), qualificationReason = ref("补充供应商资质"), editingQualification = ref<SupplierQualificationDTO>();
const operationRows = ref<SourceOperation[]>([]), operationPage = ref(1), operationTotal = ref(0), operationLoading = ref(false), operationError = ref("");
const operationCategory = ref<SourceOperationType | "">("");
const operationDetail = ref<SourceOperation>(), operationDetailOpen = ref(false);
const profileAnchor = ref<HTMLElement>(), qualificationsAnchor = ref<HTMLElement>(), f12Anchor = ref<HTMLElement>();
// 两张表两份数据：待审核走单状态接口且不分页（待办必须一次看全），
// 审批记录走分页接口并排除待审批（否则总数与每页条数都对不上）。
const pendingQuotes = ref<SubmissionFlowView[]>([]), quoteRows = ref<SubmissionFlowView[]>([]);
const quotePage = ref(1), quoteTotal = ref(0), quoteLoading = ref(false), quoteError = ref("");
const progress=ref<SupplierProgressNode[]>([]),progressError=ref('');
const nodeLabels:Record<string,string>={CONFIG:'测试与资源配置',CONTRACT:'合同与授信'};
let progressSequence=0;
const nodeStatusLabels:Record<string,string>={WAITING_PREDECESSOR:'等待前序',IN_PROGRESS:'办理中',COMPLETED:'已完成',CANCELED:'已取消',CANCELLED:'已取消'};
const submissionStatusLabels:Record<string,string>={DRAFT:'草稿',PENDING_APPROVAL:'待审批',REJECTED:'已驳回',PROCESSING:'办理中',COMPLETED:'已完成',WITHDRAWN:'已撤回'};
// 原型「申报与审批记录」：申报类型 / 处理状态 / 提交时间 三个筛选，
// 并按状态拆成「待审核」与「审批记录」两张表。
// 原型「操作记录」的筛选：操作人 + 时间区间（左闭右开）。
const operationOperator = ref(""), operationRange = ref<string[]>([]);
function operationRangeParams() {
  const params: { from?: string; to?: string } = {};
  if (operationRange.value[0]) params.from = new Date(`${operationRange.value[0]}T00:00:00`).toISOString();
  if (operationRange.value[1]) { const end = new Date(`${operationRange.value[1]}T00:00:00`); end.setDate(end.getDate() + 1); params.to = end.toISOString(); }
  return params;
}
function resetOperationFilters() { operationOperator.value = ""; operationRange.value = []; operationPage.value = 1; void loadOperations(); }
const quoteFilters = reactive({ kind: "", status: "", range: [] as string[] });
const quoteKindLabel = (kind: string) => kind === "PRICE_CHANGE" ? "价格变更" : "模型新增";
// 状态下拉用固定全集：拆分数据源后单看某一页会缺状态选项。
const quoteStatusOptions = Object.keys(submissionStatusLabels);
function matchQuoteFilters(row: SubmissionFlowView) {
  if (quoteFilters.kind && quoteKindLabel(row.kind) !== quoteFilters.kind) return false;
  if (quoteFilters.status && row.status !== quoteFilters.status) return false;
  const submitted = row.submitted_at ? row.submitted_at.slice(0, 10) : "";
  if (quoteFilters.range[0] && (!submitted || submitted < quoteFilters.range[0])) return false;
  if (quoteFilters.range[1] && (!submitted || submitted > quoteFilters.range[1])) return false;
  return true;
}
// 待审核只放真正等审批的；草稿与已撤回连同已出结论的一起进「审批记录」，
// 否则草稿会把待审核列表占满（原型也是这么分的）。
const pendingQuoteRows = computed(() => pendingQuotes.value.filter(matchQuoteFilters));
const decidedQuoteRows = computed(() => quoteRows.value.filter(matchQuoteFilters));
const quoteStatusLabel = (status: string) => submissionStatusLabels[status] || status || "未记录";
/** 原型只展示一份企业证明：取最近上传的那条。 */
const latestQualification = computed(() => {
  const rows = [...qualifications.value].sort((a, b) => (Date.parse(b.uploaded_at) || 0) - (Date.parse(a.uploaded_at) || 0));
  return rows[0];
});
const latestQualificationText = computed(() => {
  const row = latestQualification.value;
  if (!row) return "未上传企业证明";
  const extra = qualifications.value.length > 1 ? `（共 ${qualifications.value.length} 份，仅显示最新）` : "";
  return `${row.file_name || row.name} · ${formatDateTime(row.uploaded_at)}${extra}`;
});
function resetQuoteFilters() { Object.assign(quoteFilters, { kind: "", status: "", range: [] }); }
function resourceNodes(id:string){return progress.value.filter(n=>n.resource_id===id);}
// 原型「接口配置」是页内弹窗（接口模型 ID / Base URL / API Key + 复制 + 历史版本与证据），
// 不再跳走单条资源的详情抽屉——那样看不到同批其他模型，也没法对照办理进度。
const ifaceOpen = ref(false), ifaceRow = ref<ResourceRow>(), ifaceConfigs = ref<FormalConfigOption[]>([]), ifaceLoading = ref(false);
const ifaceConfig = computed(() => (ifaceRow.value ? ifaceConfigs.value.find(item => item.id === ifaceRow.value?.formal_config_id) : undefined));
async function openInterfaceConfig(row: ResourceRow) {
  ifaceRow.value = row; ifaceOpen.value = true; ifaceConfigs.value = []; ifaceLoading.value = true;
  try { ifaceConfigs.value = await resourceOperationsApi.formalConfigs(supplierID.value, row.region); }
  catch { ifaceConfigs.value = []; }
  finally { ifaceLoading.value = false; }
}
async function copyText(value: string, label: string) {
  if (!value) { ElMessage.info(`${label}尚未登记`); return; }
  try { await navigator.clipboard.writeText(value); ElMessage.success(`${label}已复制`); }
  catch { ElMessage.warning("浏览器未授权剪贴板，请手动选择复制"); }
}
/** 配置登记/接入完成后刷新列表与进度（弹窗不关，方便接着看下一项）。 */
function afterInterfaceChange() { void load(); }
/** API Key 一律不回显：库里只有密文，界面只能告诉你"设没设"。 */
function copyKey() { ElMessage.info("API Key 不回显；需要更换请在下方正式配置里重置。"); }
// 原型那条进度：技术完成 x/y · 已取消 · 尚待配置 · 当前可办理 · 商务待完成 · 办理人
const flowSummary=computed(()=>{
  const total=rows.value.length;
  const admitted=rows.value.filter(r=>r.state==='ADMITTED').length;
  const canceled=rows.value.filter(r=>rowHandling(r.id)==='本次办理已取消').length;
  const pending=rows.value.filter(r=>r.state==='PENDING_TEST'||r.state==='PENDING_ACCESS').length;
  const contractNode=progress.value.find(n=>n.key==='CONTRACT');
  return {total,admitted,canceled,pending,contractDone:contractNode?['COMPLETED','DONE'].includes(contractNode.status):false,contractAssignee:contractNode?.assignee_name||'',configAssignee:progress.value.find(n=>n.key==='CONFIG')?.assignee_name||''};
});
function rowHandling(id:string){const state=resourceNodes(id).find(n=>n.key==='CONFIG')?.row_status;return state==='CANCELED'?'本次办理已取消':state==='CONFIGURED'?'配置已完成，待确认开通':'';}
async function loadProgress(){const id=supplierID.value,epoch=supplierEpoch,sequence=++progressSequence;progressError.value='';try{const result=await submissionFlowsApi.supplierProgress(id);if(isCurrentSupplier(id,epoch)&&sequence===progressSequence){if(result.supplier_id!==id)throw new Error('办理主体不匹配');progress.value=result.list;}}catch(e){if(isCurrentSupplier(id,epoch)&&sequence===progressSequence){progress.value=[];progressError.value=e instanceof Error?e.message:'办理进度加载失败';}}}
let request: AbortController | undefined;
let profileRequest: AbortController | undefined, qualificationRequest: AbortController | undefined, operationRequest: AbortController | undefined, quoteRequest: AbortController | undefined;
let active = true;
let supplierEpoch = 0;

function isCurrentSupplier(id: string, epoch: number) {
  return active && supplierEpoch === epoch && supplierID.value === id;
}

const selected = computed(() => rows.value.find(row => row.id === selectedID.value));
const supplierName = computed(() => rows.value[0]?.supplier_name || profile.value?.name || queryString(route.query.supplier_name) || `供应商 ${supplierID.value}`);
const backLabel = computed(() => queryString(route.query.return_to) === "suppliers"
  ? "← 返回供应商列表"
  : queryString(route.query.from_model_id) ? "← 返回模型详情" : "← 返回模型广场");
const stateLabel = (state: string) => resourceStateLabels[state] || state;
const regionLabel = (region: string) => resourceRegionLabels[region] || region;

function queryString(value: unknown) {
  if (typeof value === "string") return value;
  if (Array.isArray(value) && typeof value[0] === "string") return value[0];
  return "";
}

const returnContextKeys = [
  "return_filters", "source_filters", "source_page", "source_size",
  "return_keyword", "return_company", "return_model_type", "return_supplier_id", "return_supplier_name", "return_supplier_region",
  "return_region", "return_state", "return_page", "return_size", "return_sort_by", "return_order",
  "source_keyword", "source_state", "source_coefficient_min", "source_coefficient_max", "source_connection_max", "source_rps_min", "source_tps_min", "source_sort_by", "source_order",
];
function preservedReturnContext() {
  return Object.fromEntries(returnContextKeys.flatMap(key => {
    const value = queryString(route.query[key]);
    return value ? [[key, value]] : [];
  }));
}

function back() {
  if (queryString(route.query.return_to) === "suppliers") {
    const supplierFilters: Record<string, string> = {
      keyword: "supplier_keyword", region: "supplier_region", contract: "supplier_contract",
      stage: "supplier_stage", handling: "supplier_handling",
      page: "supplier_page", size: "supplier_size", sort_by: "supplier_sort_by", sort_order: "supplier_sort_order",
    };
    const query = Object.fromEntries(Object.entries(supplierFilters).flatMap(([target, source]) => {
      const value = queryString(route.query[source]);
      return value ? [[target, value]] : [];
    }));
    void router.push({ path: "/internal/suppliers", query });
    return;
  }
  const modelID = queryString(route.query.from_model_id);
  if (modelID) {
    try { void router.push({ path: `/internal/model-detail/${standardModelVersion(modelID)}`, query: preservedReturnContext() }); return; }
    catch { /* invalid return context falls back to model plaza */ }
  }
  void router.push({ path: "/internal/model-plaza", query: preservedReturnContext() });
}

function focus(row: ResourceRow) {
  selectedID.value = row.id;
  void router.replace({ query: { ...route.query, resource_id: row.id, model_id: row.standard_model_id, region: row.region } });
}

async function loadProfile() {
  const id = supplierID.value, epoch = supplierEpoch;
  profileRequest?.abort(); const current = profileRequest = new AbortController();
  profileLoading.value = true; profileError.value = "";
  try {
    const entityID = standardModelVersion(id);
    const entity = await supplierEntitiesApi.get(entityID, current.signal);
    if (!isCurrentSupplier(id, epoch) || current.signal.aborted) return;
    if (entity.id !== entityID) throw new Error("档案与当前供应商不匹配。");
    profile.value = entity;
  } catch (value) { if (isCurrentSupplier(id, epoch) && !current.signal.aborted) profileError.value = value instanceof Error ? value.message : "供应商档案暂时无法加载。"; }
  finally { if (isCurrentSupplier(id, epoch) && !current.signal.aborted) profileLoading.value = false; }
}

async function loadQualifications(page = qualificationPage.value) {
  const id = supplierID.value, epoch = supplierEpoch;
  qualificationRequest?.abort(); const current = qualificationRequest = new AbortController();
  qualificationPage.value = page;
  qualificationLoading.value = true; qualificationError.value = ""; qualificationMetadataSupported.value = null;
  try {
    const result = await supplierEntitiesApi.qualifications(id, page, 20, current.signal);
    if (isCurrentSupplier(id, epoch) && !current.signal.aborted) { qualifications.value = result.list; qualificationTotal.value = result.total; qualificationMetadataSupported.value = result.metadata_supported; }
  } catch (value) {
    if (isCurrentSupplier(id, epoch) && !current.signal.aborted) {
      qualifications.value = []; qualificationTotal.value = 0; qualificationMetadataSupported.value = null;
      qualificationError.value = value instanceof Error ? value.message : "供应商资质暂时无法加载。";
    }
  } finally { if (isCurrentSupplier(id, epoch) && !current.signal.aborted) qualificationLoading.value = false; }
}

function openQualificationUpload() {
  if (qualificationMetadataSupported.value === null || !profile.value?.can_edit || qualificationSaving.value || qualificationReloading.value) return;
  qualificationConflict.value = false; qualificationConflictReady.value = false;
  editingQualification.value = undefined;
  qualificationName.value = "";
  qualificationType.value = ""; qualificationValidFrom.value = ""; qualificationValidTo.value = "";
  qualificationFileDraft.value = undefined;
  qualificationReason.value = "补充供应商资质";
  qualificationSubmittingError.value = "";
  qualificationDialogOpen.value = true;
}

function openQualificationEdit(row: SupplierQualificationDTO) {
  if (!qualificationMetadataSupported.value || !profile.value?.can_edit || !row.revision || qualificationSaving.value || qualificationReloading.value) return;
  qualificationConflict.value = false; qualificationConflictReady.value = false;
  editingQualification.value = row;
  qualificationName.value = row.name;
  qualificationType.value = row.qualification_type || "";
  qualificationValidFrom.value = row.valid_from || "";
  qualificationValidTo.value = row.valid_to || "";
  qualificationFileDraft.value = undefined;
  qualificationReason.value = "维护供应商资质资料";
  qualificationSubmittingError.value = "";
  qualificationDialogOpen.value = true;
}

function chooseQualificationFile(upload: UploadFile) {
  const file = upload.raw;
  if (!file) return;
  if (file.size < 1 || file.size > 2 * 1024 * 1024) {
    qualificationFileDraft.value = undefined;
    qualificationSubmittingError.value = "资质文件大小须为 1 字节至 2 MiB。";
    return;
  }
  if (!/\.(pdf|png|jpe?g)$/i.test(file.name)) {
    qualificationFileDraft.value = undefined;
    qualificationSubmittingError.value = "仅支持 PDF、PNG 或 JPEG 文件。";
    return;
  }
  qualificationFileDraft.value = file;
  qualificationSubmittingError.value = "";
}

async function saveQualification() {
  if (!profile.value?.can_edit || qualificationSaving.value || qualificationReloading.value || qualificationConflict.value && !qualificationConflictReady.value) return;
  const id = supplierID.value, epoch = supplierEpoch;
  if (qualificationMetadataSupported.value === null) { qualificationSubmittingError.value = "资质结构支持状态尚未确认，请先重新加载资质列表。"; return; }
  if (editingQualification.value && qualificationMetadataSupported.value !== true) { qualificationSubmittingError.value = "资质资料维护暂不可用，请重新加载列表确认。"; return; }
  if (!qualificationName.value.trim() || (!editingQualification.value && !qualificationFileDraft.value) || !qualificationReason.value.trim() || (qualificationMetadataSupported.value && !qualificationType.value.trim())) {
    qualificationSubmittingError.value = editingQualification.value ? "请填写资质名称、类型和操作说明。" : (qualificationMetadataSupported.value ? "请填写资质名称、类型、选择文件并填写操作说明。" : "请填写资质名称、选择文件并填写操作说明。");
    return;
  }
  if (qualificationValidFrom.value && qualificationValidTo.value && qualificationValidFrom.value > qualificationValidTo.value) {
    qualificationSubmittingError.value = "有效期截止日期不能早于起始日期。";
    return;
  }
  qualificationSaving.value = true;
  qualificationSubmittingError.value = "";
  try {
    if (editingQualification.value) {
      await supplierEntitiesApi.updateQualification(id, editingQualification.value.id, {
        name: qualificationName.value, qualification_type: qualificationType.value, valid_from: qualificationValidFrom.value,
        valid_to: qualificationValidTo.value, expected_revision: editingQualification.value.revision!, reason: qualificationReason.value,
      }, crypto.randomUUID());
    } else {
      const input = { name: qualificationName.value, file: qualificationFileDraft.value!, reason: qualificationReason.value,
        ...(qualificationMetadataSupported.value ? { metadata: { qualification_type: qualificationType.value, valid_from: qualificationValidFrom.value, valid_to: qualificationValidTo.value } } : {}) };
      await supplierEntitiesApi.uploadQualification(id, input, crypto.randomUUID());
    }
    if (!isCurrentSupplier(id, epoch)) return;
    qualificationDialogOpen.value = false;
    ElMessage.success(editingQualification.value ? "资质资料已保存为新修订。" : "资质资料已保存。");
    editingQualification.value = undefined;
    await loadQualifications(1);
    if (!isCurrentSupplier(id, epoch)) return;
  } catch (value) {
    if (isCurrentSupplier(id, epoch)) {
      qualificationSubmittingError.value = value instanceof Error ? value.message : "资质保存失败，请重试。";
      qualificationConflict.value = value instanceof ApiError && value.httpStatus === 409;
      qualificationConflictReady.value = false;
    }
  } finally {
    if (isCurrentSupplier(id, epoch)) qualificationSaving.value = false;
  }
}

async function refreshQualificationConflict() {
  if (qualificationSaving.value || qualificationReloading.value || !editingQualification.value) return;
  const id = supplierID.value, epoch = supplierEpoch, qualificationID = editingQualification.value.id;
  qualificationReloading.value = true; qualificationConflictReady.value = false;
  try {
    await Promise.all([loadProfile(), loadQualifications()]);
    if (!isCurrentSupplier(id, epoch)) return;
    if (profileError.value || qualificationError.value) throw new Error(profileError.value || qualificationError.value);
    if (!profile.value?.can_edit) throw new Error("当前账号已无维护权限，请关闭表单后核对采购归属。");
    const latest = qualifications.value.find(row => row.id === qualificationID);
    if (qualificationMetadataSupported.value !== true || !latest?.revision) throw new Error("当前页未找到可维护的最新资质，请关闭表单后在列表核对。草稿仍保留。");
    editingQualification.value = latest;
    qualificationConflictReady.value = true;
    qualificationSubmittingError.value = `已读取修订 R${latest.revision}：${latest.name}；类型：${latest.qualification_type || '未记录'}；有效期：${latest.valid_from || '未记录'} 至 ${latest.valid_to || '未记录'}。草稿保留，请核对差异后重试。`;
  } catch (value) {
    if (isCurrentSupplier(id, epoch)) qualificationSubmittingError.value = value instanceof Error ? value.message : "刷新失败，草稿仍保留。";
  } finally { if (isCurrentSupplier(id, epoch)) qualificationReloading.value = false; }
}

async function downloadQualification(row: SupplierQualificationDTO) {
  if (!profile.value?.can_edit || !row.can_download || !row.file_name || qualificationDownloading.value) return;
  const id = supplierID.value, epoch = supplierEpoch;
  qualificationDownloading.value = row.id;
  try {
    const blob = await supplierEntitiesApi.qualificationFile(id, row.id);
    if (!isCurrentSupplier(id, epoch)) return;
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = row.file_name;
    link.rel = "noopener";
    link.click();
    window.setTimeout(() => URL.revokeObjectURL(url), 1000);
  } catch (value) {
    if (isCurrentSupplier(id, epoch)) ElMessage.error(value instanceof Error ? value.message : "资质文件暂时无法下载。");
  } finally {
    if (isCurrentSupplier(id, epoch)) qualificationDownloading.value = "";
  }
}

/** 页面内保存：先收操作说明（审计要求），再走既有的档案保存接口。 */
async function saveProfileInline() {
  if (!profile.value?.can_edit || profileSaving.value) return;
  let reason = "";
  try {
    const result = await ElMessageBox.prompt("说明本次档案调整原因，会写入操作记录。", "保存档案", {
      confirmButtonText: "保存", cancelButtonText: "取消",
      inputPlaceholder: "例如：更新联系人电话",
      inputValidator: (value: string) => (value && value.trim() ? true : "请填写操作说明"),
    });
    reason = String(result.value || "").trim();
  } catch { return; }
  await saveProfile({ ...profileDraft.value }, reason);
  if (profileSaveError.value) ElMessage.error(profileSaveError.value);
  else ElMessage.success("档案已保存");
}

async function saveProfile(form: SupplierEntityForm, reason: string) {
  if (!profile.value?.can_edit || profileSaving.value || reloadingProfileConflict.value || profileSaveConflict.value) return;
  const id = supplierID.value, epoch = supplierEpoch;
  profileSaving.value = true;
  profileSaveError.value = "";
  profileSaveConflict.value = false;
  try {
    const saved = await supplierEntitiesApi.save(form, reason, crypto.randomUUID(), profile.value);
    if (!isCurrentSupplier(id, epoch)) return;
    profile.value = saved;
    profileFormOpen.value = false;
    await loadProfile();
  } catch (value) {
    if (isCurrentSupplier(id, epoch)) {
      profileSaveError.value = value instanceof Error ? value.message : "供应商档案保存失败，请稍后重试。";
      profileSaveConflict.value = value instanceof ApiError && value.httpStatus === 409;
    }
  } finally {
    if (isCurrentSupplier(id, epoch)) profileSaving.value = false;
  }
}

async function reloadProfileAfterConflict() {
  if (!profile.value || reloadingProfileConflict.value) return;
  const id = supplierID.value, epoch = supplierEpoch;
  reloadingProfileConflict.value = true;
  try {
    const latest = await supplierEntitiesApi.get(profile.value.id);
    if (!isCurrentSupplier(id, epoch)) return;
    if (latest.id !== id) throw new Error("刷新结果与当前供应商不匹配。");
    profile.value = latest;
    if (!latest.can_edit) { profileFormOpen.value = false; profileSaveConflict.value = false; return; }
    profileReloadKey.value += 1;
    profileSaveError.value = ""; profileSaveConflict.value = false;
  } catch (value) { if (isCurrentSupplier(id, epoch)) profileSaveError.value = value instanceof Error ? value.message : "无法读取最新供应商档案。"; }
  finally { if (isCurrentSupplier(id, epoch)) reloadingProfileConflict.value = false; }
}

async function loadOperations() {
  const id = supplierID.value, epoch = supplierEpoch;
  operationRequest?.abort(); const current = operationRequest = new AbortController();
  operationLoading.value = true; operationError.value = "";
  try {
    const result = await supplierSourceOperationsApi.list(id, operationPage.value, 20, current.signal, operationCategory.value || undefined,
      { operator: operationOperator.value.trim() || undefined, ...operationRangeParams() });
    if (isCurrentSupplier(id, epoch) && !current.signal.aborted) { operationRows.value = result.list; operationTotal.value = result.total; }
  } catch (value) { if (isCurrentSupplier(id, epoch) && !current.signal.aborted) { operationRows.value = []; operationTotal.value = 0; operationError.value = value instanceof Error ? value.message : "供应商操作记录暂时无法加载。"; } }
  finally { if (isCurrentSupplier(id, epoch) && !current.signal.aborted) operationLoading.value = false; }
}

async function loadQuotes() {
  const routeID = supplierID.value, epoch = supplierEpoch;
  quoteRequest?.abort(); const current = quoteRequest = new AbortController();
  quoteLoading.value = true; quoteError.value = "";
  try {
    const id = standardModelVersion(routeID);
    const [decided, pending] = await Promise.all([
      submissionFlowsApi.supplierHistory(id,quotePage.value,20,current.signal,{exclude_status:"PENDING_APPROVAL"}),
      submissionFlowsApi.supplierHistory(id,1,20,current.signal,{status:"PENDING_APPROVAL"}),
    ]);
    if (!isCurrentSupplier(routeID, epoch) || current.signal.aborted) return;
    for (const item of [...decided.list, ...pending.list]) {
      if (String(item.supplier_id) !== id) throw new Error("报价结果包含其他供应商数据，已停止显示。");
    }
    quoteRows.value = decided.list; quoteTotal.value = decided.total;
    pendingQuotes.value = pending.list;
  } catch (value) {
    if (isCurrentSupplier(routeID, epoch) && !current.signal.aborted) {
      quoteRows.value = [];
      pendingQuotes.value = [];
      quoteTotal.value = 0;
      quoteError.value = value instanceof Error ? value.message : "该供应商报价暂时无法加载。";
    }
  }
  finally { if (isCurrentSupplier(routeID, epoch) && !current.signal.aborted) quoteLoading.value = false; }
}

function onTabChange(tab: string | number) {
  if (tab === "profile") { void loadProfile(); void loadQualifications(); void loadProgress(); }
  else if (tab === "operations") void loadOperations();
  else if (tab === "quotes") void loadQuotes();
}

const operationLabel = supplierOperationLabel;
function changeOperationCategory() { operationPage.value = 1; void loadOperations(); }

// 原型里「详情」是一张独立页（可筛选模型、逐模型勾选审批）。
function openQuote(quote: SubmissionFlowView) { void router.push({ path: `/internal/submissions/${quote.id}` }); }
function openOperation(row: SourceOperation) {
  operationDetail.value = row; operationDetailOpen.value = true;
}
async function openOperationBusiness(row: SourceOperation) {
  operationDetailOpen.value = false;
  const location = sourceOperationLocation(row);
  if (row.resource_id) { void router.push(location); return; }
  await router.replace({ query: { ...route.query, ...location.query } });
  await showBusinessSection(row.entity_type);
}
async function showBusinessSection(section: string) {
  const id = supplierID.value, epoch = supplierEpoch;
  activeTab.value = "profile";
  await nextTick();
  if (!isCurrentSupplier(id, epoch)) return;
  await Promise.all([loadProfile(), loadQualifications(),loadProgress()]);
  if (!isCurrentSupplier(id, epoch)) return;
  await nextTick();
  const anchor = section === "supplier_qualification" ? qualificationsAnchor.value : section === "contract" || section === "credit_line" ? f12Anchor.value : profileAnchor.value;
  anchor?.scrollIntoView({ behavior: "smooth", block: "start" });
}

async function load() {
  request?.abort();
  const current = request = new AbortController();
  loading.value = true;
  error.value = "";
  rows.value = [];
  batchSelected.value=[];
  selectedID.value = "";
  try {
    const supplier = standardModelVersion(supplierID.value);
    const resourceValue = queryString(route.query.resource_id);
    const modelValue = queryString(route.query.model_id);
    const region = queryString(route.query.region);
    const hasResourceContext = Boolean(resourceValue || modelValue || region);
    const query = buildResourceQuery({ view: "flat", page: 1, size: 100, sort_by: "supplier_name", order: "asc", supplier_id: supplier, region });
    const result = await resourcesApi.allRows(query, current.signal);
    if (!active || current.signal.aborted) return;
    if (hasResourceContext) {
      const resource = standardModelVersion(resourceValue);
      const model = standardModelVersion(modelValue);
      const selectedRow = result.find(row => row.id === resource);
      if (!selectedRow || selectedRow.supplier_id !== supplier || selectedRow.standard_model_id !== model || selectedRow.region !== region) {
        throw new Error("当前供应来源已变化或与供应商不匹配，请从模型广场重新打开。");
      }
      selectedID.value = selectedRow.id;
    }
    rows.value = result;
    // 从工作台「供应资源接入」进来时带 ?highlight=<资源 ID>：落到对应行，省得再找。
    // 注意：参数缺省时不能拿空串去解析（standardModelVersion("") 会直接抛校验错）。
    const highlightRaw = queryString(route.query.highlight);
    if (highlightRaw) {
      const highlight = standardModelVersion(highlightRaw);
      if (result.some(row => row.id === highlight)) selectedID.value = highlight;
    }
    void loadProgress();
  } catch (value) {
    if (!active || current.signal.aborted) return;
    error.value = value instanceof Error ? value.message : "供应商模型列表暂时无法加载，请重试。";
  } finally {
    if (active && !current.signal.aborted) loading.value = false;
  }
}

function clearSelected() { selectedID.value = ""; }
function onSupplierChanged() {
  ++supplierEpoch;
  request?.abort(); profileRequest?.abort(); qualificationRequest?.abort(); operationRequest?.abort(); quoteRequest?.abort();
  profile.value = undefined; profileLoading.value = false; profileError.value = "";
  progress.value=[];progressError.value='';
  profileFormOpen.value = false; profileSaving.value = false; reloadingProfileConflict.value = false; profileSaveError.value = ""; profileSaveConflict.value = false;
  qualifications.value = []; qualificationTotal.value = 0; qualificationPage.value = 1; qualificationLoading.value = false; qualificationError.value = ""; qualificationMetadataSupported.value = null;
  qualificationDialogOpen.value = false; qualificationSaving.value = false; qualificationSubmittingError.value = ""; qualificationDownloading.value = "";
  qualificationConflict.value = false; qualificationConflictReady.value = false; qualificationReloading.value = false;
  qualificationName.value = ""; qualificationType.value = ""; qualificationValidFrom.value = ""; qualificationValidTo.value = ""; qualificationFileDraft.value = undefined; qualificationReason.value = "补充供应商资质"; editingQualification.value = undefined;
  operationRows.value = []; operationPage.value = 1; operationTotal.value = 0; operationLoading.value = false; operationError.value = "";
  operationCategory.value = ""; operationDetail.value = undefined; operationDetailOpen.value = false;
  pendingQuotes.value = []; quoteRows.value = []; quotePage.value = 1; quoteTotal.value = 0; quoteLoading.value = false; quoteError.value = "";
  void load();
  if (activeTab.value === "profile") { void loadProfile(); void loadQualifications(1); }
  else if (activeTab.value === "operations") void loadOperations();
  else if (activeTab.value === "quotes") void loadQuotes();
}
onMounted(() => {
  // 审批页审批完会回到 ?tab=quotes：这里要把页签落到「申报与审批记录」并把数据拉起来，
  // 否则用户回到供应商页看到的是「接入模型」，会以为审批没生效。
  const tab = queryString(route.query.tab);
  if (["models", "profile", "quotes", "operations"].includes(tab)) {
    activeTab.value = tab;
    if (tab === "quotes") void loadQuotes();
    if (tab === "profile") void loadProfile();
    if (tab === "operations") void loadOperations();
  }
  void load();
  if (tab === "profile") void showBusinessSection(queryString(route.query.section));
});
watch(supplierID, onSupplierChanged);
onBeforeUnmount(() => { active = false; request?.abort(); profileRequest?.abort(); qualificationRequest?.abort(); operationRequest?.abort(); quoteRequest?.abort(); });
</script>

<template>
  <section class="supplier-access-page">
    <el-button text class="back-button" @click="back">{{ backLabel }}</el-button>
    <header class="page-heading">
      <div><div class="eyebrow">SUPPLIER / ACCESS MODELS</div><h1>{{ supplierName }}</h1><p>供应商接入模型与当前来源详情</p></div>
      <el-button :loading="loading" @click="load">刷新</el-button>
    </header>

    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false"><el-button text @click="load">重新加载</el-button></el-alert>

    <section class="panel supplier-model-panel" v-loading="loading">
      <el-tabs v-model="activeTab" @tab-change="onTabChange">
        <el-tab-pane label="接入模型" name="models">
          <div class="model-context" v-if="selected">
            <div><span class="muted">当前选中来源</span><strong>{{ selected.model_name }} · {{ selected.supplier_model_id }}</strong></div>
            <el-tag>{{ regionLabel(selected.region) }}</el-tag>
          </div>
          <p v-if="rows.length" class="flow-summary">
            <span>技术完成 {{ flowSummary.admitted }}/{{ flowSummary.total }}</span>
            <span v-if="flowSummary.canceled">已取消 {{ flowSummary.canceled }} 个</span>
            <span>尚待配置 {{ flowSummary.pending }} 个</span>
            <span>当前可办理 {{ flowSummary.pending }} 个</span>
            <span>商务：{{ flowSummary.contractDone ? '已完成' : '待完成' }}<template v-if="!flowSummary.contractDone && flowSummary.contractAssignee"> · {{ flowSummary.contractAssignee }}</template></span>
            <span v-if="flowSummary.configAssignee">技术办理 · {{ flowSummary.configAssignee }}</span>
          </p>
          <div class="model-filters">
            <label>模型公司<el-select v-model="modelFilters.company" clearable placeholder="全部" aria-label="模型公司" style="width:150px"><el-option v-for="company in modelCompanies" :key="company" :label="company" :value="company"/></el-select></label>
            <label>模型类型<el-select v-model="modelFilters.modelType" clearable placeholder="全部" aria-label="模型类型" style="width:130px"><el-option v-for="(name,key) in modelTypeLabels" :key="key" :label="name" :value="key"/></el-select></label>
            <label>状态<el-select v-model="modelFilters.state" clearable placeholder="全部" aria-label="资源状态" style="width:130px"><el-option v-for="(name,key) in resourceStateLabels" :key="key" :label="name" :value="key"/></el-select></label>
            <el-input v-model="modelFilters.keyword" clearable placeholder="搜索平台模型名称 / 供应商接口模型 ID" aria-label="搜索模型名称或接口模型 ID" style="width:280px" />
            <el-button @click="resetModelFilters">重置</el-button>
            <span v-if="rows.length" class="muted">共 {{ filteredRows.length }} / {{ rows.length }} 条</span>
          </div>
          <SupplierAccessBatchPanel :supplier-id="supplierID" :selected="batchSelected" @changed="load();loadProgress()"/>
          <el-table :data="filteredRows" row-key="id" highlight-current-row :current-row-key="selectedID" style="width:100%" @selection-change="batchSelected=$event" @row-click="focus">
            <el-table-column type="selection" width="45"/>
            <el-table-column label="标准模型 / 官方 ID" min-width="190"><template #default="{ row }"><strong>{{ row.model_name }}</strong><span class="muted">{{ row.official_id }}</span></template></el-table-column>
            <el-table-column label="供应商接口模型 ID" min-width="180"><template #default="{ row }"><code>{{ row.supplier_model_id }}</code></template></el-table-column>
            <el-table-column label="采购系数" width="110"><template #default="{ row }">{{ coefficientText(row.coefficient) }}</template></el-table-column>
            <el-table-column label="连通耗时(ms)" width="120"><template #default="{ row }">{{ resourceMetricDisplay(row.test.metrics.connection_ms, row.test.status) }}</template></el-table-column>
            <el-table-column label="RPS" width="100"><template #default="{ row }">{{ resourceMetricDisplay(row.test.metrics.rps, row.test.status) }}</template></el-table-column>
            <el-table-column label="TPS" width="100"><template #default="{ row }">{{ resourceMetricDisplay(row.test.metrics.tps, row.test.status) }}</template></el-table-column>
            <el-table-column label="输入缓存命中率" width="130"><template #default="{ row }">{{ resourceCacheDisplay(row.test.cache_stats, row.test.status) }}</template></el-table-column>
            <el-table-column label="最近上架时间" width="170"><template #default="{ row }">{{ row.admitted_at ? formatDateTime(row.admitted_at) : "未记录" }}</template></el-table-column>
            <el-table-column label="状态" min-width="150"><template #default="{ row }"><el-tag :type="row.state === 'ADMITTED' ? 'success' : 'info'" size="small">{{ stateLabel(row.state) }}</el-tag><span class="muted">{{rowHandling(row.id)}}</span></template></el-table-column>
            <el-table-column label="测试与报告" width="112" fixed="right"><template #default="{ row }"><el-button size="small" text type="primary" @click.stop="openReports(row)">测试与报告</el-button></template></el-table-column>
            <!-- 原型末列「资源操作」里就是「接口配置」；行点击仍会选中行看下方详情，不再单占一列。 -->
            <el-table-column label="资源操作" width="110" fixed="right"><template #default="{ row }"><el-button size="small" text type="primary" @click.stop="openInterfaceConfig(row)">接口配置</el-button></template></el-table-column>
            <template #empty><el-empty description="该供应商暂无接入模型" :image-size="64" /></template>
          </el-table>
        </el-tab-pane>
        <el-tab-pane label="档案与办理" name="profile" lazy>
          <div v-loading="profileLoading" class="supplier-tab-content">
            <el-alert v-if="profileError" :title="profileError" type="error" :closable="false"><el-button text @click="loadProfile">重试</el-button></el-alert>
            <template v-if="profile">
              <!-- 基础资料按原型做成两列表单：字段直接可编辑，保存时才入库（并记操作说明）。 -->
              <div ref="profileAnchor" class="supplier-profile-heading">
                <h3>基础资料</h3>
                <el-button v-if="profile.can_edit" type="primary" plain size="small" :loading="profileSaving" @click="saveProfileInline">保存修改</el-button>
              </div>
              <el-alert v-if="!profile.can_edit" type="info" :closable="false" show-icon title="当前账号不是该供应商的归属采购人，也没有供应商维护权限，档案为只读。" />
              <el-form label-position="top" class="profile-grid" :disabled="!profile.can_edit || profileSaving">
                <el-form-item label="公司名称"><el-input v-model="profileDraft.name" maxlength="128" /></el-form-item>
                <el-form-item label="统一社会信用代码"><el-input v-model="profileDraft.registration_no" maxlength="64" /></el-form-item>
                <el-form-item label="联系人"><el-input v-model="profileDraft.contact" maxlength="64" /></el-form-item>
                <el-form-item label="联系电话"><el-input v-model="profileDraft.phone" maxlength="32" /></el-form-item>
                <el-form-item label="国内／海外"><el-select v-model="profileDraft.region"><el-option label="国内" value="CN" /><el-option label="海外" value="OVERSEAS" /></el-select></el-form-item>
                <el-form-item label="税率 %"><el-input v-model="profileDraft.tax_rate_percent" inputmode="decimal" placeholder="可留空，例如 6 或 6.25" /></el-form-item>
                <el-form-item label="结算币种"><el-select v-model="profileDraft.settlement_currency"><el-option label="人民币 CNY" value="CNY" /><el-option label="美元 USD" value="USD" /></el-select></el-form-item>
                <el-form-item label="备注" class="span-2"><el-input v-model="profileDraft.remark" type="textarea" :rows="2" maxlength="500" placeholder="选填，例如合作背景或特殊约定" /></el-form-item>
              </el-form>
              <el-alert v-if="profileSaveError" :title="profileSaveError" type="error" :closable="false" />
              <!-- 营业执照／企业证明：原型就是一个文件控件 + 当前状态，不放表格。 -->
              <div ref="qualificationsAnchor" class="supplier-qualification-heading"><h3>营业执照／企业证明</h3></div>
              <div v-loading="qualificationLoading" class="qualification-inline">
                <el-button v-if="profile.can_edit" size="small" :disabled="qualificationLoading" @click="openQualificationUpload">选择文件</el-button>
                <span class="qualification-file">{{ latestQualificationText }}</span>
                <el-button v-if="latestQualification?.can_download" link type="primary" :loading="qualificationDownloading === latestQualification.id" @click="downloadQualification(latestQualification)">下载</el-button>
                <span v-else-if="latestQualification?.has_attachment" class="muted">{{ profile.can_edit ? '附件暂不可下载（文件不可用或已归档）' : '需归属采购人或维护人下载' }}</span>
              </div>
              <el-alert v-if="qualificationError" :title="qualificationError" type="error" :closable="false"><el-button text @click="loadQualifications">重试</el-button></el-alert>
              <!-- 合同与授信紧跟基础资料（原型顺序）；接入模型与办理状态在「接入模型」页签，这里不再重复。 -->
              <div ref="f12Anchor"><F12SubjectPanel v-if="profile" subject-type="SUPPLIER" :subject-id="profile.id" :can-manage="profile.can_edit" layout="merged" @changed="loadProfile" /></div>
            </template>
          </div>
        </el-tab-pane>
        <el-tab-pane label="申报与审批记录" name="quotes" lazy>
          <div v-loading="quoteLoading" class="supplier-tab-content">
            <p class="muted">当前主体的全部模型申报与改价记录，包含草稿、待审批、驳回、办理及已完成状态。</p>
            <div class="quote-record-filters">
              <label>申报类型<el-select v-model="quoteFilters.kind" clearable placeholder="全部" aria-label="申报类型" style="width:130px"><el-option label="模型新增" value="模型新增" /><el-option label="价格变更" value="价格变更" /></el-select></label>
              <label>处理状态<el-select v-model="quoteFilters.status" clearable placeholder="全部" aria-label="处理状态" style="width:150px"><el-option v-for="status in quoteStatusOptions" :key="status" :label="quoteStatusLabel(status)" :value="status" /></el-select></label>
              <label>提交时间<el-date-picker v-model="quoteFilters.range" type="daterange" value-format="YYYY-MM-DD" start-placeholder="开始" end-placeholder="结束" aria-label="提交时间" style="width:250px" /></label>
              <el-button @click="resetQuoteFilters">重置</el-button>
            </div>
            <el-alert v-if="quoteError" :title="quoteError" type="error" :closable="false"><el-button text @click="loadQuotes">重试</el-button></el-alert>
            <div v-else>
              <h3>待审核</h3>
              <!-- 列宽用 min-width：容器变宽时由各列分摊剩余空间，避免右侧留一大片空白。 -->
              <el-table :data="pendingQuoteRows" empty-text="暂无待审核申报" style="width:100%">
                <el-table-column prop="id" label="申报编号" min-width="120" />
                <el-table-column label="类型" min-width="120"><template #default="{ row }">{{ quoteKindLabel(row.kind) }}</template></el-table-column>
                <el-table-column label="模型数量" min-width="90"><template #default="{ row }">{{ row.rows.length }}</template></el-table-column>
                <el-table-column label="提交时间" min-width="160"><template #default="{ row }">{{ formatDateTime(row.submitted_at) }}</template></el-table-column>
                <el-table-column label="状态" min-width="120"><template #default="{ row }">{{ quoteStatusLabel(row.status) }}</template></el-table-column>
                <el-table-column label="审批结果" min-width="120"><template #default>—</template></el-table-column>
                <el-table-column label="操作" width="90" fixed="right"><template #default="{ row }"><el-button link type="primary" @click="openQuote(row)">详情</el-button></template></el-table-column>
              </el-table>
              <h3>审批记录</h3>
              <el-table :data="decidedQuoteRows" empty-text="暂无审批记录" style="width:100%">
                <el-table-column prop="id" label="申请编号" min-width="120" />
                <el-table-column label="类型" min-width="110"><template #default="{ row }">{{ quoteKindLabel(row.kind) }}</template></el-table-column>
                <el-table-column label="模型数量" min-width="90"><template #default="{ row }">{{ row.rows.length }}</template></el-table-column>
                <el-table-column label="申请时间" min-width="160"><template #default="{ row }">{{ formatDateTime(row.submitted_at) }}</template></el-table-column>
                <el-table-column label="审批时间" min-width="160"><template #default="{ row }">{{ row.approved_at ? formatDateTime(row.approved_at) : '未记录' }}</template></el-table-column>
                <el-table-column label="审批结果" min-width="120"><template #default="{ row }">{{ row.approval_result || (["PROCESSING", "COMPLETED"].includes(row.status) ? "未记录" : "—") }}</template></el-table-column>
                <el-table-column label="审批人" min-width="110"><template #default="{ row }">{{ row.approver || '未记录' }}</template></el-table-column>
                <el-table-column label="操作" width="90" fixed="right"><template #default="{ row }"><el-button link type="primary" @click="openQuote(row)">详情</el-button></template></el-table-column>
              </el-table>
            </div>
            <el-pagination v-if="quoteTotal" v-model:current-page="quotePage" :page-size="20" :total="quoteTotal" layout="total, prev, pager, next" @current-change="loadQuotes" />
          </div>
        </el-tab-pane>
        <el-tab-pane label="操作记录" name="operations" lazy>
          <div v-loading="operationLoading" class="supplier-tab-content">
            <p class="muted">汇总主体、资质、正式配置、合同、授信、资源及计划的操作摘要。</p>
            <div class="operation-filters">
              <label>操作人<el-select v-model="operationOperator" clearable filterable allow-create default-first-option placeholder="全部" aria-label="操作人" style="width:150px" @change="operationPage = 1; loadOperations()"><el-option v-for="name in [...new Set(operationRows.map(row => row.operator_name).filter(Boolean))]" :key="name" :label="name" :value="name" /></el-select></label>
              <label>操作类型<el-select v-model="operationCategory" clearable placeholder="全部" aria-label="操作记录业务分类" style="width:160px" @change="changeOperationCategory"><el-option v-for="category in supplierOperationCategories" :key="category" :value="category" :label="supplierOperationCategoryLabels[category]" /></el-select></label>
              <label>操作时间<el-date-picker v-model="operationRange" type="daterange" value-format="YYYY-MM-DD" start-placeholder="开始" end-placeholder="结束" aria-label="操作时间" style="width:250px" @change="operationPage = 1; loadOperations()" /></label>
              <el-button @click="resetOperationFilters">重置</el-button><el-button :loading="operationLoading" @click="loadOperations">刷新</el-button>
            </div>
            <el-alert v-if="operationError" :title="operationError" type="error" :closable="false"><el-button text @click="loadOperations">重试</el-button></el-alert>
            <el-table v-else :data="operationRows" empty-text="暂无操作记录"><el-table-column label="操作" min-width="170"><template #default="{ row }">{{ operationLabel(row) }}</template></el-table-column><el-table-column label="关联对象" min-width="210"><template #default="{ row }"><template v-if="row.resource_id"><strong>{{ row.supplier_model_id }}</strong><div class="muted">资源 {{ row.resource_id }} · {{ regionLabel(row.region) }}</div></template><template v-else><strong>{{ supplierOperationCategoryLabels[row.entity_type as SourceOperationType] }}</strong><div class="muted">对象 {{ row.entity_id }} · {{ row.supplier_name }}</div></template></template></el-table-column><el-table-column prop="operator_name" label="操作人" min-width="120"><template #default="{ row }">{{ row.operator_name || '未记录姓名' }}</template></el-table-column><el-table-column label="时间" min-width="170"><template #default="{ row }">{{ formatDateTime(row.created_at) }}</template></el-table-column><el-table-column prop="reason" label="说明" min-width="180"/><el-table-column label="详情" width="90"><template #default="{ row }"><el-button link type="primary" @click="openOperation(row)">查看</el-button></template></el-table-column></el-table>
            <el-pagination v-if="operationTotal" v-model:current-page="operationPage" :page-size="20" :total="operationTotal" layout="total, prev, pager, next" @current-change="loadOperations" />
          </div>
        </el-tab-pane>
      </el-tabs>
    </section>

  <el-dialog v-model="operationDetailOpen" append-to-body title="操作记录详情" width="min(600px, 94vw)">
    <el-descriptions v-if="operationDetail" :column="1" border>
      <el-descriptions-item label="操作">{{ operationLabel(operationDetail) }}</el-descriptions-item>
      <el-descriptions-item label="供应商">{{ operationDetail.supplier_name }} · {{ operationDetail.supplier_id }}</el-descriptions-item>
      <el-descriptions-item label="业务对象">{{ operationDetail.entity_id }}</el-descriptions-item>
      <el-descriptions-item label="操作人">{{ operationDetail.operator_name || '未记录姓名' }}</el-descriptions-item>
      <el-descriptions-item label="时间">{{ formatDateTime(operationDetail.created_at) }}</el-descriptions-item>
      <el-descriptions-item label="说明">{{ operationDetail.reason || '未记录' }}</el-descriptions-item>
    </el-descriptions>
    <p v-if="operationDetail?.entity_type === 'supplier_formal_config'" class="muted">正式配置维护请从接入模型的配置入口进入。</p>
    <template #footer><el-button @click="operationDetailOpen = false">关闭</el-button><el-button v-if="operationDetail && operationDetail.entity_type !== 'supplier_formal_config'" type="primary" @click="openOperationBusiness(operationDetail)">{{ operationDetail.resource_id ? '查看关联资源 / 计划' : '查看业务资料' }}</el-button></template>
  </el-dialog>
  <el-dialog v-model="qualificationDialogOpen" append-to-body :title="editingQualification ? '维护供应商资质' : '上传供应商资质'" width="min(560px, 94vw)" :close-on-click-modal="false" :close-on-press-escape="!qualificationSaving && !qualificationReloading" :show-close="!qualificationSaving && !qualificationReloading">
    <el-form label-position="top" :disabled="qualificationSaving || qualificationReloading">
      <el-form-item label="资质名称"><el-input v-model="qualificationName" maxlength="128" :disabled="qualificationSaving" /></el-form-item>
      <template v-if="qualificationMetadataSupported">
        <el-form-item label="资质类型"><el-input v-model="qualificationType" maxlength="128" placeholder="按产品/审核口径填写" :disabled="qualificationSaving" /></el-form-item>
        <div class="qualification-validity"><el-form-item label="有效期起始"><el-input v-model="qualificationValidFrom" type="date" :disabled="qualificationSaving" /></el-form-item><el-form-item label="有效期截止"><el-input v-model="qualificationValidTo" type="date" :disabled="qualificationSaving" /></el-form-item></div>
        <el-alert title="审核状态由审核能力维护，此表单不会将新上传资料标记为通过。维护只追加修订号，不替换历史附件。" type="info" :closable="false" />
      </template>
      <el-form-item v-if="!editingQualification" label="资质附件"><el-upload accept=".pdf,.png,.jpg,.jpeg" :auto-upload="false" :show-file-list="false" :disabled="qualificationSaving" :on-change="chooseQualificationFile"><el-button :disabled="qualificationSaving">选择文件</el-button></el-upload><span class="muted">{{ qualificationFileDraft ? `${qualificationFileDraft.name} · ${(qualificationFileDraft.size / 1024).toFixed(0)} KiB` : "尚未选择文件" }}</span></el-form-item>
      <el-form-item label="操作说明"><el-input v-model="qualificationReason" maxlength="500" :disabled="qualificationSaving" /></el-form-item>
      <el-alert v-if="qualificationSubmittingError" :title="qualificationSubmittingError" :type="qualificationConflict ? 'warning' : 'error'" :closable="false">
        <el-button v-if="qualificationConflict && editingQualification" text :loading="qualificationReloading" :disabled="qualificationSaving" @click="refreshQualificationConflict">刷新最新修订并保留草稿</el-button>
      </el-alert>
    </el-form>
    <template #footer><el-button :disabled="qualificationSaving || qualificationReloading" @click="qualificationDialogOpen = false">取消</el-button><el-button type="primary" :loading="qualificationSaving" :disabled="qualificationSaving || qualificationReloading || qualificationConflict && !qualificationConflictReady" @click="saveQualification">{{ qualificationConflictReady ? '按最新修订重试' : '保存资质' }}</el-button></template>
  </el-dialog>

    <!-- 原型「接口配置 · 模型名」：接口模型 ID / Base URL / API Key + 复制，下面接登记与接入。 -->
    <el-dialog v-if="ifaceRow" v-model="ifaceOpen" class="iface-dialog" :title="`接口配置 · ${ifaceRow.model_name}`" width="min(940px, 96vw)" top="6vh" append-to-body :close-on-click-modal="false">
      <el-alert type="info" :closable="false" show-icon title="生产配置待登记；申报时用的准入测试 Key 不会自动带入，生产 Key 需要单独登记。" />
      <el-form label-position="top" class="iface-form" v-loading="ifaceLoading">
        <el-form-item label="接口模型 ID">
          <el-input :model-value="ifaceRow.supplier_model_id" readonly>
            <template #append><el-button @click="copyText(ifaceRow.supplier_model_id, '接口模型 ID')">复制</el-button></template>
          </el-input>
        </el-form-item>
        <el-form-item label="Base URL">
          <el-input :model-value="ifaceConfig?.base_url || ''" readonly placeholder="未登记正式配置">
            <template #append><el-button @click="copyText(ifaceConfig?.base_url || '', 'Base URL')">复制</el-button></template>
          </el-input>
        </el-form-item>
        <el-form-item label="API Key">
          <el-input :model-value="ifaceConfig ? (ifaceConfig.has_api_key ? '已登记（不回显）' : '未设置') : ''" readonly placeholder="未登记正式配置">
            <template #append><el-button @click="copyKey">复制</el-button></template>
          </el-input>
        </el-form-item>
      </el-form>
      <ResourceOperationsPanel :resource="ifaceRow" @updated="afterInterfaceChange" />
      <template #footer>
        <el-button @click="ifaceOpen = false">关闭</el-button>
        <el-button @click="openReports(ifaceRow)">历史版本与证据</el-button>
      </template>
    </el-dialog>

    <el-dialog v-if="reportsVisible && reportsRow" v-model="reportsVisible" :title="`${reportsRow.model_name} · 资源 #${reportsRow.id}`" width="min(1120px, 96vw)" append-to-body>
      <p class="muted">{{ reportsRow.supplier_model_id }} · 每次测试单独留档</p>
      <ResourceReportsPanel :resource-i-d="reportsRow.id" :current-report-i-d="reportsRow.test.report_id" />
      <template #footer><el-button @click="reportsVisible = false">关闭测试列表</el-button></template>
    </el-dialog>
    <ResourceDetailDrawer v-if="selected && activeTab === 'models'" :key="`${selected.id}:${selected.region}`" :id="selected.id" :region="selected.region" presentation="page" :expected-supplier-i-d="selected.supplier_id" :expected-model-i-d="selected.standard_model_id" initial-tab="profile" @close="clearSelected" @changed="load" />
  </section>
</template>

<style scoped>
.flow-summary{display:flex;flex-wrap:wrap;gap:14px;margin:0 0 12px;padding:10px 12px;border:1px solid #e6ebee;border-radius:6px;background:#f8fafb;font-size:12px;color:#5b6270}
.supplier-access-page{color:var(--app-text-primary)}.back-button{margin-bottom:10px}.page-heading{display:flex;justify-content:space-between;align-items:flex-start;gap:20px;margin-bottom:18px}.page-heading h1{margin:7px 0;font-size:26px}.page-heading p,.muted{color:var(--app-text-muted);font-size:12px}.eyebrow{color:var(--app-text-muted);font-size:11px;letter-spacing:.14em}.supplier-model-panel{padding:18px 20px}.model-context{display:flex;justify-content:space-between;align-items:center;gap:16px;padding:12px 14px;margin-bottom:12px;background:#f6f8fb;border:1px solid #e6e8ed;border-radius:8px}.model-context strong,.model-context .muted{display:block}.model-context strong{margin-top:4px}.muted{display:block;margin-top:4px}.supplier-tab-content{min-height:220px;padding:12px 0}.supplier-tab-content h3{margin:20px 0 10px;font-size:16px}.supplier-tab-content h4{margin:18px 0 8px}.supplier-profile{display:grid;grid-template-columns:160px minmax(0,1fr);gap:10px 20px;padding:12px 0}.supplier-profile dt{color:var(--app-text-muted)}.supplier-profile dd{margin:0;overflow-wrap:anywhere}.el-pagination{margin-top:18px;justify-content:flex-end}code{font-family:ui-monospace,SFMono-Regular,Consolas,monospace}@media(max-width:640px){.page-heading{align-items:flex-start}.supplier-model-panel{padding:12px}.supplier-profile{grid-template-columns:1fr;gap:6px}.supplier-profile dd{margin-bottom:10px}}
.supplier-profile-heading{display:flex;align-items:center;justify-content:space-between;gap:12px}.supplier-profile-heading h3{margin:20px 0 2px}
/* 基础资料：原型是两列表单，字段宽度随容器收缩。 */
.profile-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:0 24px;padding:10px 0 0}
.profile-grid :deep(.el-form-item){margin-bottom:14px}
.profile-grid :deep(.el-select),.profile-grid :deep(.el-input){width:100%}
.profile-grid .span-2{grid-column:1 / -1}
@media(max-width:820px){.profile-grid{grid-template-columns:1fr}}.qualification-validity{display:grid;grid-template-columns:1fr 1fr;gap:12px}
.supplier-qualification-heading{display:flex;align-items:baseline;justify-content:space-between;gap:12px;margin-top:24px}.supplier-qualification-heading h3{margin:0 0 10px}.qualification-list{min-height:90px}
.iface-form { margin-top: 14px; }
.qualification-inline{display:flex;align-items:center;gap:12px;flex-wrap:wrap;min-height:44px;padding:4px 0}
/* 合同与授信是独立区块，和上方营业执照留出间距（原型也是分开的两块）。 */
.supplier-access-page :deep(.f12-subject-panel){margin-top:28px}
.qualification-file{color:var(--app-text-muted);font-size:13px}
.operation-filters{display:flex;align-items:center;gap:10px;flex-wrap:wrap;margin:12px 0}.operation-filters label{display:flex;align-items:center;gap:8px;font-size:12px;white-space:nowrap}.quote-record-filters{display:flex;align-items:center;gap:10px;flex-wrap:wrap;margin:12px 0}.quote-record-filters label{display:flex;align-items:center;gap:8px;font-size:12px;white-space:nowrap}.model-filters{display:flex;align-items:center;gap:10px;flex-wrap:wrap;margin:0 0 12px}.model-filters label{display:flex;align-items:center;gap:8px;font-size:12px;white-space:nowrap}.operation-filters .el-select{max-width:220px}
</style>

<style>
/* 接口配置弹窗内嵌了配置与接入面板：限制高度、内部滚动（弹窗被 teleport 到 body，需全局选择器）。 */
.iface-dialog .el-dialog__body { max-height: 68vh; overflow-y: auto; }
</style>
