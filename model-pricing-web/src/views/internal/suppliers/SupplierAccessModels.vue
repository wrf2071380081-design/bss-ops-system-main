<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { ElMessage, type UploadFile } from "element-plus";
import { useRoute, useRouter } from "vue-router";
import { supplierEntitiesApi } from "../../../api/supplierEntities";
import type { SupplierEntityDTO, SupplierEntityForm, SupplierQualificationDTO } from "../../../api/supplierEntities.contract";
import { resourcesApi } from "../../../api/resources";
import { buildResourceQuery, resourceRegionLabels, resourceStateLabels } from "../../../api/resources.contract";
import {submissionFlowsApi,type SubmissionFlowView,type SupplierProgressNode} from '../../../api/submissionFlows';
import SubmissionFlowDialog from '../workbench/SubmissionFlowDialog.vue';
import { supplierSourceOperationsApi } from "../../../api/supplierSourceOperations";
import type { SourceOperation } from "../../../api/sourceOperations.contract";
import { sourceOperationLocation, supplierOperationCategories, supplierOperationCategoryLabels, supplierOperationLabel, type SourceOperationType } from "../../../api/sourceOperations.contract";
import { standardModelVersion } from "../../../api/standardModels.contract";
import type { ResourceRow } from "../../../api/resources.types";
import { ApiError } from "../../../domain/common";
import { formatDateTime } from "../../../domain/date";
import ResourceDetailDrawer from "../resources/ResourceDetailDrawer.vue";
import F12SubjectPanel from "../subjects/F12SubjectPanel.vue";
import SupplierProfileForm from "./SupplierProfileForm.vue";
import SupplierAccessBatchPanel from './SupplierAccessBatchPanel.vue';
const batchSelected=ref<ResourceRow[]>([]);
import SupplierTestConfigPanel from "./SupplierTestConfigPanel.vue";

const route = useRoute();
const router = useRouter();
const supplierID = computed(() => String(route.params.supplierId || ""));
const rows = ref<ResourceRow[]>([]);
const selectedID = ref("");
const loading = ref(false);
const error = ref("");
const activeTab = ref("models");
const profile = ref<SupplierEntityDTO>();
const profileFormOpen = ref(false), profileSaving = ref(false), reloadingProfileConflict = ref(false), profileSaveError = ref(""), profileSaveConflict = ref(false), profileReloadKey = ref(0);
const profileLoading = ref(false), profileError = ref("");
const qualifications = ref<SupplierQualificationDTO[]>([]), qualificationTotal = ref(0), qualificationPage = ref(1), qualificationLoading = ref(false), qualificationError = ref(""), qualificationMetadataSupported = ref<boolean | null>(null);
const qualificationDialogOpen = ref(false), qualificationSaving = ref(false), qualificationSubmittingError = ref(""), qualificationDownloading = ref("");
const qualificationConflict = ref(false), qualificationConflictReady = ref(false), qualificationReloading = ref(false);
const qualificationName = ref(""), qualificationType = ref(""), qualificationValidFrom = ref(""), qualificationValidTo = ref(""), qualificationFileDraft = ref<File>(), qualificationReason = ref("补充供应商资质"), editingQualification = ref<SupplierQualificationDTO>();
const operationRows = ref<SourceOperation[]>([]), operationPage = ref(1), operationTotal = ref(0), operationLoading = ref(false), operationError = ref("");
const operationCategory = ref<SourceOperationType | "">("");
const operationDetail = ref<SourceOperation>(), operationDetailOpen = ref(false);
const profileAnchor = ref<HTMLElement>(), qualificationsAnchor = ref<HTMLElement>(), f12Anchor = ref<HTMLElement>();
const pendingQuotes = ref<SubmissionFlowView[]>([]), quotePage = ref(1), quoteTotal = ref(0), quoteLoading = ref(false), quoteError = ref("");
const progress=ref<SupplierProgressNode[]>([]),progressError=ref(''),flowDetail=ref('');
const nodeLabels:Record<string,string>={CONFIG:'测试与资源配置',CONTRACT:'合同与授信'};
let progressSequence=0;
const nodeStatusLabels:Record<string,string>={WAITING_PREDECESSOR:'等待前序',IN_PROGRESS:'办理中',COMPLETED:'已完成',CANCELED:'已取消',CANCELLED:'已取消'};
const submissionStatusLabels:Record<string,string>={DRAFT:'草稿',PENDING_APPROVAL:'待审批',REJECTED:'已驳回',PROCESSING:'办理中',COMPLETED:'已完成',WITHDRAWN:'已撤回'};
function resourceNodes(id:string){return progress.value.filter(n=>n.resource_id===id);}
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
    const result = await supplierSourceOperationsApi.list(id, operationPage.value, 20, current.signal, operationCategory.value || undefined);
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
    const result = await submissionFlowsApi.supplierHistory(id,quotePage.value,20,current.signal);
    if (!isCurrentSupplier(routeID, epoch) || current.signal.aborted) return;
    if (result.list.some(item => String(item.supplier_id) !== id)) throw new Error("报价结果包含其他供应商数据，已停止显示。");
    pendingQuotes.value = result.list; quoteTotal.value = result.total;
  } catch (value) {
    if (isCurrentSupplier(routeID, epoch) && !current.signal.aborted) {
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

function openQuote(quote: SubmissionFlowView) { flowDetail.value=quote.id; }
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
  progress.value=[];progressError.value='';flowDetail.value='';
  profileFormOpen.value = false; profileSaving.value = false; reloadingProfileConflict.value = false; profileSaveError.value = ""; profileSaveConflict.value = false;
  qualifications.value = []; qualificationTotal.value = 0; qualificationPage.value = 1; qualificationLoading.value = false; qualificationError.value = ""; qualificationMetadataSupported.value = null;
  qualificationDialogOpen.value = false; qualificationSaving.value = false; qualificationSubmittingError.value = ""; qualificationDownloading.value = "";
  qualificationConflict.value = false; qualificationConflictReady.value = false; qualificationReloading.value = false;
  qualificationName.value = ""; qualificationType.value = ""; qualificationValidFrom.value = ""; qualificationValidTo.value = ""; qualificationFileDraft.value = undefined; qualificationReason.value = "补充供应商资质"; editingQualification.value = undefined;
  operationRows.value = []; operationPage.value = 1; operationTotal.value = 0; operationLoading.value = false; operationError.value = "";
  operationCategory.value = ""; operationDetail.value = undefined; operationDetailOpen.value = false;
  pendingQuotes.value = []; quotePage.value = 1; quoteTotal.value = 0; quoteLoading.value = false; quoteError.value = "";
  void load();
  if (activeTab.value === "profile") { void loadProfile(); void loadQualifications(1); }
  else if (activeTab.value === "operations") void loadOperations();
  else if (activeTab.value === "quotes") void loadQuotes();
}
onMounted(() => { void load(); if (queryString(route.query.tab) === "profile") void showBusinessSection(queryString(route.query.section)); });
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
          <SupplierAccessBatchPanel :supplier-id="supplierID" :selected="batchSelected" @changed="load();loadProgress()"/>
          <el-table :data="rows" row-key="id" highlight-current-row :current-row-key="selectedID" style="width:100%" @selection-change="batchSelected=$event">
            <el-table-column type="selection" width="45"/>
            <el-table-column label="标准模型 / 官方 ID" min-width="190"><template #default="{ row }"><strong>{{ row.model_name }}</strong><span class="muted">{{ row.official_id }}</span></template></el-table-column>
            <el-table-column label="供应商接口模型 ID" min-width="180"><template #default="{ row }"><code>{{ row.supplier_model_id }}</code></template></el-table-column>
            <el-table-column label="部署节点" width="100"><template #default="{ row }">{{ regionLabel(row.region) }}</template></el-table-column>
            <el-table-column prop="coefficient" label="采购系数" width="110" />
            <el-table-column label="状态" min-width="150"><template #default="{ row }"><el-tag :type="row.state === 'ADMITTED' ? 'success' : 'info'" size="small">{{ stateLabel(row.state) }}</el-tag><span class="muted">{{rowHandling(row.id)}}</span></template></el-table-column>
            <el-table-column label="操作" width="110"><template #default="{ row }"><el-button size="small" @click="focus(row)">{{ row.id === selectedID ? "当前详情" : "查看详情" }}</el-button></template></el-table-column>
            <template #empty><el-empty description="该供应商暂无接入模型" :image-size="64" /></template>
          </el-table>
        </el-tab-pane>
        <el-tab-pane label="档案与办理" name="profile" lazy>
          <div v-loading="profileLoading" class="supplier-tab-content">
            <el-alert v-if="profileError" :title="profileError" type="error" :closable="false"><el-button text @click="loadProfile">重试</el-button></el-alert>
            <template v-if="profile">
              <div ref="profileAnchor" class="supplier-profile-heading"><h3>主体档案</h3><el-button v-if="profile.can_edit" type="primary" plain size="small" @click="profileFormOpen = true">维护档案</el-button></div>
              <dl class="supplier-profile"><dt>供应商名称</dt><dd>{{ profile.name }}</dd><dt>主体区域</dt><dd>{{ regionLabel(profile.region) }}</dd><dt>统一社会信用代码</dt><dd>{{ profile.registration_no || '未填写' }}</dd><dt>对接人</dt><dd>{{ profile.contact || '未填写' }}</dd><dt>联系电话</dt><dd>{{ profile.phone || '未填写' }}</dd><dt>税率</dt><dd>{{ profile.tax_rate === null ? '未填写' : `${(Number(profile.tax_rate) * 100).toFixed(2)}%` }}</dd><dt>结算币种</dt><dd>{{ profile.settlement_currency }}</dd><dt>归属采购人</dt><dd>人员 {{ profile.buyer_id }}</dd><dt>主体状态</dt><dd>{{ profile.status === 'ACTIVE' ? '正常' : '冻结' }} · 修订 {{ profile.revision }}</dd></dl>
              <SupplierTestConfigPanel :supplier-i-d="profile.id" :can-edit="profile.can_edit" />
              <div ref="qualificationsAnchor" class="supplier-qualification-heading"><div><h3>资质材料</h3><span class="muted">支持 PDF、PNG、JPEG，单文件最大 2 MiB；审核状态由审核能力提供。</span></div><el-button v-if="profile.can_edit" :disabled="qualificationLoading || qualificationMetadataSupported === null" type="primary" plain size="small" @click="openQualificationUpload">上传资质</el-button></div>
              <el-alert v-if="qualificationMetadataSupported === false" title="当前资质元数据结构缺失或不兼容；现阶段只显示已有资料并保留基础上传，类型、有效期和修订维护暂不可用。" type="info" :closable="false" />
              <el-alert v-if="qualificationError" :title="qualificationError" type="error" :closable="false"><el-button text @click="loadQualifications">重试</el-button></el-alert>
              <div v-else v-loading="qualificationLoading" class="qualification-list">
                <el-empty v-if="!qualifications.length && !qualificationLoading" description="暂无资质材料" :image-size="58" />
                <el-table v-else :data="qualifications" size="small" row-key="id" empty-text="暂无资质材料">
                  <el-table-column prop="name" label="资质名称" min-width="180" />
                  <el-table-column label="资质类型" min-width="140"><template #default="{ row }">{{ row.qualification_type || (qualificationMetadataSupported === true ? '未填写' : qualificationMetadataSupported === false ? '结构未就绪' : '状态未知') }}</template></el-table-column>
                  <el-table-column label="有效期" min-width="180"><template #default="{ row }">{{ row.valid_from || row.valid_to ? `${row.valid_from || '不限起始'} 至 ${row.valid_to || '长期'}` : '未记录' }}</template></el-table-column>
                  <el-table-column label="附件" min-width="160"><template #default="{ row }"><el-button v-if="profile.can_edit && row.can_download" link type="primary" :loading="qualificationDownloading === row.id" @click="downloadQualification(row)">下载 {{ row.file_name }}</el-button><span v-else>{{ row.has_attachment ? (profile.can_edit ? '历史附件暂不可下载' : '需归属采购人下载') : '未提供附件' }}</span></template></el-table-column>
                  <el-table-column label="上传时间" min-width="180"><template #default="{ row }">{{ formatDateTime(row.uploaded_at) }}</template></el-table-column>
                  <el-table-column label="审核状态" width="150"><template #default="{ row }">{{ row.review_status || (qualificationMetadataSupported === true ? '未记录' : qualificationMetadataSupported === false ? '审核字段待交付' : '状态未知') }}</template></el-table-column>
                  <el-table-column prop="revision" label="修订" width="80"><template #default="{ row }">{{ row.revision || '—' }}</template></el-table-column>
                  <el-table-column label="登记人" width="100"><template #default="{ row }">{{ row.created_by ? `人员 ${row.created_by}` : '未记录' }}</template></el-table-column>
                  <el-table-column label="操作" width="90"><template #default="{ row }"><el-button v-if="profile.can_edit && qualificationMetadataSupported" link type="primary" :disabled="!row.revision" @click="openQualificationEdit(row)">维护</el-button></template></el-table-column>
                </el-table>
                <el-pagination v-if="qualificationTotal > 20" v-model:current-page="qualificationPage" :page-size="20" :total="qualificationTotal" layout="total, prev, pager, next" @current-change="loadQualifications" />
              </div>
              <h3>接入模型与办理状态</h3>
              <el-alert v-if="progressError" type="error" :title="progressError" :closable="false"><el-button text @click="loadProgress">重试</el-button></el-alert>
              <el-table :data="rows" size="small" empty-text="暂无供应资源"><el-table-column prop="model_name" label="模型" min-width="160"/><el-table-column prop="supplier_model_id" label="接口模型 ID" min-width="180"/><el-table-column label="状态" width="120"><template #default="{ row }">{{ stateLabel(row.state) }}</template></el-table-column><el-table-column label="办理节点及人员" min-width="250"><template #default="{row}"><div v-for="node in resourceNodes(row.id)" :key="node.node_id"><el-button link type="primary" @click="router.push({path:'/internal/workbench',query:{node_id:node.node_id}})">{{nodeLabels[node.key]||node.key}}</el-button> · {{nodeStatusLabels[node.status]||node.status}} · {{node.assignee_name||'未分派'}}</div><span v-if="!resourceNodes(row.id).length">暂无已分派办理记录</span></template></el-table-column></el-table>
            </template>
            <div ref="f12Anchor"><F12SubjectPanel v-if="profile" subject-type="SUPPLIER" :subject-id="profile.id" :can-manage="profile.can_edit" @changed="loadProfile" /></div>
          </div>
        </el-tab-pane>
        <el-tab-pane label="操作记录" name="operations" lazy>
          <div v-loading="operationLoading" class="supplier-tab-content">
            <p class="muted">汇总主体、资质、正式配置、合同、授信、资源及计划的操作摘要。</p>
            <div class="operation-filters"><el-select v-model="operationCategory" clearable placeholder="全部业务类型" aria-label="操作记录业务分类" @change="changeOperationCategory"><el-option v-for="category in supplierOperationCategories" :key="category" :value="category" :label="supplierOperationCategoryLabels[category]" /></el-select><el-button :loading="operationLoading" @click="loadOperations">刷新</el-button></div>
            <el-alert v-if="operationError" :title="operationError" type="error" :closable="false"><el-button text @click="loadOperations">重试</el-button></el-alert>
            <el-table v-else :data="operationRows" empty-text="暂无操作记录"><el-table-column label="操作" min-width="170"><template #default="{ row }">{{ operationLabel(row) }}</template></el-table-column><el-table-column label="关联对象" min-width="210"><template #default="{ row }"><template v-if="row.resource_id"><strong>{{ row.supplier_model_id }}</strong><div class="muted">资源 {{ row.resource_id }} · {{ regionLabel(row.region) }}</div></template><template v-else><strong>{{ supplierOperationCategoryLabels[row.entity_type as SourceOperationType] }}</strong><div class="muted">对象 {{ row.entity_id }} · {{ row.supplier_name }}</div></template></template></el-table-column><el-table-column prop="operator_name" label="操作人" min-width="120"><template #default="{ row }">{{ row.operator_name || '未记录姓名' }}</template></el-table-column><el-table-column label="时间" min-width="170"><template #default="{ row }">{{ formatDateTime(row.created_at) }}</template></el-table-column><el-table-column prop="reason" label="说明" min-width="180"/><el-table-column label="详情" width="90"><template #default="{ row }"><el-button link type="primary" @click="openOperation(row)">查看</el-button></template></el-table-column></el-table>
            <el-pagination v-if="operationTotal" v-model:current-page="operationPage" :page-size="20" :total="operationTotal" layout="total, prev, pager, next" @current-change="loadOperations" />
          </div>
        </el-tab-pane>
        <el-tab-pane label="该供应商报价" name="quotes" lazy>
          <div v-loading="quoteLoading" class="supplier-tab-content">
            <p class="muted">当前主体的全部模型申报与改价记录，包含草稿、待审批、驳回、办理及已完成状态。</p>
            <el-alert v-if="quoteError" :title="quoteError" type="error" :closable="false"><el-button text @click="loadQuotes">重试</el-button></el-alert>
            <el-table v-else :data="pendingQuotes" empty-text="暂无报价申报历史"><el-table-column prop="id" label="申报单" width="110"/><el-table-column label="类型" width="120"><template #default="{row}">{{row.kind==='PRICE_CHANGE'?'价格变更':'模型申报'}}</template></el-table-column><el-table-column prop="revision" label="版本" width="80"/><el-table-column label="模型及采购系数" min-width="220"><template #default="{row}"><div v-for="item in row.rows" :key="item.id">{{item.model_name}} · {{item.coefficient}}</div></template></el-table-column><el-table-column label="提交时间" min-width="180"><template #default="{ row }">{{ formatDateTime(row.submitted_at) }}</template></el-table-column><el-table-column label="状态" width="110"><template #default="{row}">{{submissionStatusLabels[row.status]||row.status}}</template></el-table-column><el-table-column label="详情" width="100"><template #default="{ row }"><el-button link type="primary" @click="openQuote(row)">查看报价</el-button></template></el-table-column></el-table>
            <el-pagination v-if="quoteTotal" v-model:current-page="quotePage" :page-size="20" :total="quoteTotal" layout="total, prev, pager, next" @current-change="loadQuotes" />
          </div>
        </el-tab-pane>
      </el-tabs>
    </section>

  <SupplierProfileForm v-model="profileFormOpen" :profile="profile" :aliases-supported="profile?.aliases_supported" :saving="profileSaving" :reloading="reloadingProfileConflict" :reload-key="profileReloadKey" :conflict="profileSaveConflict" :error="profileSaveError" @save="saveProfile" @reload="reloadProfileAfterConflict" />
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
    <SubmissionFlowDialog v-if="flowDetail" :id="flowDetail" @close="flowDetail=''" @changed="loadQuotes();loadProgress()" />
    <ResourceDetailDrawer v-if="selected && activeTab === 'models'" :key="`${selected.id}:${selected.region}`" :id="selected.id" :region="selected.region" presentation="page" :expected-supplier-i-d="selected.supplier_id" :expected-model-i-d="selected.standard_model_id" initial-tab="profile" @close="clearSelected" @changed="load" />
  </section>
</template>

<style scoped>
.supplier-access-page{color:var(--app-text-primary)}.back-button{margin-bottom:10px}.page-heading{display:flex;justify-content:space-between;align-items:flex-start;gap:20px;margin-bottom:18px}.page-heading h1{margin:7px 0;font-size:26px}.page-heading p,.muted{color:var(--app-text-muted);font-size:12px}.eyebrow{color:var(--app-text-muted);font-size:11px;letter-spacing:.14em}.supplier-model-panel{padding:18px 20px}.model-context{display:flex;justify-content:space-between;align-items:center;gap:16px;padding:12px 14px;margin-bottom:12px;background:#f6f8fb;border:1px solid #e6e8ed;border-radius:8px}.model-context strong,.model-context .muted{display:block}.model-context strong{margin-top:4px}.muted{display:block;margin-top:4px}.supplier-tab-content{min-height:220px;padding:12px 0}.supplier-tab-content h3{margin:20px 0 10px;font-size:16px}.supplier-tab-content h4{margin:18px 0 8px}.supplier-profile{display:grid;grid-template-columns:160px minmax(0,1fr);gap:10px 20px;padding:12px 0}.supplier-profile dt{color:var(--app-text-muted)}.supplier-profile dd{margin:0;overflow-wrap:anywhere}.el-pagination{margin-top:18px;justify-content:flex-end}code{font-family:ui-monospace,SFMono-Regular,Consolas,monospace}@media(max-width:640px){.page-heading{align-items:flex-start}.supplier-model-panel{padding:12px}.supplier-profile{grid-template-columns:1fr;gap:6px}.supplier-profile dd{margin-bottom:10px}}
.supplier-profile-heading{display:flex;align-items:center;justify-content:space-between;gap:12px}.supplier-profile-heading h3{margin:20px 0 10px}.qualification-validity{display:grid;grid-template-columns:1fr 1fr;gap:12px}
.supplier-qualification-heading{display:flex;align-items:baseline;justify-content:space-between;gap:12px;margin-top:24px}.supplier-qualification-heading h3{margin:0 0 10px}.qualification-list{min-height:90px}
.operation-filters{display:flex;gap:10px;margin:12px 0}.operation-filters .el-select{max-width:220px}
</style>
