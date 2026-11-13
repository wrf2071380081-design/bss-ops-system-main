<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from "vue";
import { ElMessage } from "element-plus";
import { f12SubjectRecordsApi } from "../../../api/f12SupplierRecords";
import type { F12FlowWriteContext } from "../../../api/f12SupplierRecords";
import type { F12ContractRecord, F12CreditRecord, F12CurrentCredit, F12SubjectType } from "../../../api/f12SupplierRecords.contract";
import { normalizeF12CreditInput } from "../../../api/f12SupplierRecords.contract";
import type { F12ReadinessSnapshot } from "../../../api/f12Readiness.contract";
import { ApiError } from "../../../domain/common";
import { f12ErrorMessage } from "../../../domain/f12Errors";
import { canManageF12Subject } from "../../../domain/f12Permissions";

const props = defineProps<{ subjectType: F12SubjectType; subjectId: string; canManage: boolean; flowContext?: F12FlowWriteContext; flowCanManage?: boolean; layout?: "full" | "merged"; addBlockedReason?: string; currency?: string }>();
const emit = defineEmits<{ (event: "refresh-flow-context"): void; (event: "changed"): void; (event: "readiness-change", ready: boolean): void }>();
// These flags only control the UI. Every write and attachment read is authorized again by the server.
const canManage = computed(() => canManageF12Subject(props.canManage, Boolean(props.flowContext), props.flowCanManage));
/**
 * 附件下载：本版内部员工均可下载查看，不再跟随「维护权限」。
 * 服务端已同步放开读权限（supplypg.AuthorizeF12Read）；如需收紧，两端一起改回。
 */
const canDownloadAttachment = true;
const historyPageSize = 20;
const loading = ref(false), saving = ref(false), downloading = ref("");
const error = ref("");
const contractError = ref(""), creditError = ref(""), archiveError = ref("");
const contractConflict = ref(false), contractConflictReady = ref(false);
const creditConflict = ref(false), creditConflictReady = ref(false);
const archiveConflict = ref(false), archiveConflictReady = ref(false);
const contractDataRefreshed = ref(false), archiveDataRefreshed = ref(false);
const contracts = ref<F12ContractRecord[]>([]), contractPage = ref(1), contractTotal = ref(0);
const credits = ref<F12CreditRecord[]>([]), creditPage = ref(1), creditTotal = ref(0);
const currentCredits = ref<F12CurrentCredit[]>([]);
const readiness = ref<F12ReadinessSnapshot | null>(null), readinessError = ref("");
const contractDialog = ref(false), creditDialog = ref(false), archiveDialog = ref(false);
const editingContract = ref<F12ContractRecord | null>(null), archiveTarget = ref<F12ContractRecord | null>(null);
const refreshedContract = ref<F12ContractRecord | null>(null);
const contractFile = ref<File | null>(null), creditFile = ref<File | null>(null);
const uploadedContractFile = ref<{ id: string; name: string } | null>(null), uploadedCreditFile = ref<{ id: string; name: string } | null>(null);
const contractUploadLifecycle = ref(""), contractWriteLifecycle = ref(""), creditUploadLifecycle = ref(""), creditWriteLifecycle = ref(""), archiveWriteLifecycle = ref("");
const contractForm = reactive({ amount: "", documentName: "", documentNo: "", reason: "" });
const creditForm = reactive({ amount: "", currency: "CNY", documentName: "", documentNo: "", reason: "" });
// 原型：币种不在表单里选，跟着主体走（国内人民币、海外美元），只在标签上体现。
const currency = computed(() => (props.currency || "CNY").toUpperCase());
const currencyText = computed(() => (currency.value === "USD" ? "美元 USD" : "人民币 CNY"));
// 原型里「备注」是可选的；留空时用默认说明，保证审计里不会出现空原因。
const contractReason = () => contractForm.reason.trim() || (editingContract.value ? "修订合同资料" : "上传合同资料");
const creditReason = () => creditForm.reason.trim() || "上传授信资料";
const archiveReason = ref("");
let request: AbortController | undefined;
let sequence = 0;
let subjectEpoch = 0;

type SubjectSnapshot = { subjectType: F12SubjectType; subjectID: string; epoch: number; flowContext?: F12FlowWriteContext };

function captureSubject(): SubjectSnapshot {
  return {
    subjectType: props.subjectType,
    subjectID: props.subjectId,
    epoch: subjectEpoch,
    flowContext: props.flowContext ? { ...props.flowContext } : undefined,
  };
}

function isCurrentSubject(snapshot: SubjectSnapshot) {
  const current = props.flowContext;
  return snapshot.epoch === subjectEpoch && snapshot.subjectType === props.subjectType && snapshot.subjectID === props.subjectId &&
    snapshot.flowContext?.flowID === current?.flowID && snapshot.flowContext?.moduleKey === current?.moduleKey &&
    snapshot.flowContext?.expectedRevision === current?.expectedRevision;
}

function clearSubjectDrafts() {
  contractDialog.value = false; creditDialog.value = false; archiveDialog.value = false;
  contractFile.value = null; creditFile.value = null; uploadedContractFile.value = null; uploadedCreditFile.value = null;
  editingContract.value = null; archiveTarget.value = null;
  refreshedContract.value = null;
  contractError.value = ""; creditError.value = ""; archiveError.value = "";
  clearContractConflict(); clearCreditConflict(); clearArchiveConflict();
  saving.value = false; downloading.value = "";
  readiness.value = null; readinessError.value = "";
}

function clearSubjectProjection() {
  contracts.value = []; contractTotal.value = 0; contractPage.value = 1;
  credits.value = []; creditTotal.value = 0; creditPage.value = 1; currentCredits.value = [];
  error.value = "";
}

function newLifecycle(prefix: string) {
  const token = globalThis.crypto?.randomUUID?.() || `${Date.now()}-${Math.random().toString(16).slice(2)}`;
  return `${prefix}:${props.subjectType}:${props.subjectId}:${token}`;
}

function isRevisionConflict(value: unknown) {
  return value instanceof ApiError && value.httpStatus === 409;
}

function clearContractConflict() { contractConflict.value = false; contractConflictReady.value = false; refreshedContract.value = null; contractDataRefreshed.value = false; }
function clearCreditConflict() { creditConflict.value = false; creditConflictReady.value = false; }
function clearArchiveConflict() { archiveConflict.value = false; archiveConflictReady.value = false; archiveDataRefreshed.value = false; }

watch(() => [props.flowContext?.flowID, props.flowContext?.moduleKey, props.flowContext?.expectedRevision] as const, ([flowID, moduleKey, revision], previous) => {
  if (flowID === previous[0] && moduleKey === previous[1] && revision === previous[2]) return;
  readiness.value = null; readinessError.value = "";
  if (flowID !== previous[0] || moduleKey !== previous[1]) clearSubjectDrafts();
  if (!flowID || !moduleKey || !revision) { void load(); return; }
  if (contractConflict.value) {
    contractConflictReady.value = contractDataRefreshed.value;
    contractWriteLifecycle.value = newLifecycle(editingContract.value ? "contract-revision" : "contract-create");
    contractError.value = `办理上下文已刷新到 R${revision}。草稿仍保留，请核对后再次保存。`;
  }
  if (creditConflict.value) {
    creditConflictReady.value = true;
    creditWriteLifecycle.value = newLifecycle("credit-version");
    creditError.value = `办理上下文已刷新到 R${revision}。草稿仍保留，请核对后再次保存。`;
  }
  if (archiveConflict.value) {
    archiveConflictReady.value = archiveDataRefreshed.value;
    archiveWriteLifecycle.value = newLifecycle("contract-archive");
    archiveError.value = `办理上下文已刷新到 R${revision}。请核对合同状态后再次归档。`;
  }
  void load();
});

async function load() {
  emit('readiness-change',false);
  request?.abort();
  const current = request = new AbortController();
  const currentSequence = ++sequence;
  loading.value = true;
  error.value = "";
  try {
    const [contractResult, creditResult] = await Promise.all([
      f12SubjectRecordsApi.contracts(props.subjectType, props.subjectId, current.signal, contractPage.value, historyPageSize),
      f12SubjectRecordsApi.credits(props.subjectType, props.subjectId, current.signal, creditPage.value, historyPageSize),
    ]);
    if (currentSequence !== sequence || current.signal.aborted) return;
    contracts.value = contractResult.list;
    contractTotal.value = contractResult.total;
    credits.value = creditResult.list;
    creditTotal.value = creditResult.total;
    currentCredits.value = creditResult.current;
    readiness.value = null;
    readinessError.value = "";
    const flowContext = props.flowContext;
    if (flowContext) {
      try {
        const result = await f12SubjectRecordsApi.readiness(props.subjectType, props.subjectId, flowContext, current.signal);
        if (currentSequence !== sequence || current.signal.aborted) return;
        if (result.source_flow_revision !== flowContext.expectedRevision) {
          readinessError.value = `办理流程已更新到 R${result.source_flow_revision}，当前页面为 R${flowContext.expectedRevision}；请从待办重新进入。`;
          emit("refresh-flow-context");
        } else {
          readiness.value = result;
          emit('readiness-change',result.ready);
        }
      } catch (value) {
        if (currentSequence === sequence && !current.signal.aborted) readinessError.value = value instanceof Error ? value.message : "办理资料齐备状态暂时无法加载。";
      }
    }
  } catch (value) {
    if (currentSequence === sequence && !current.signal.aborted) error.value = value instanceof Error ? value.message : "合同与授信暂时无法加载。";
  } finally {
    if (currentSequence === sequence && !current.signal.aborted) loading.value = false;
  }
}

function openCreateContract() {
  if (!canManage.value || saving.value || loading.value || error.value) return;
  contractError.value = ""; clearContractConflict();
  editingContract.value = null;
  contractForm.amount = ""; contractForm.documentName = ""; contractForm.documentNo = ""; contractForm.reason = "";
  contractFile.value = null; uploadedContractFile.value = null;
  contractUploadLifecycle.value = newLifecycle("contract-attachment"); contractWriteLifecycle.value = newLifecycle("contract-create"); contractDialog.value = true;
}
function openReviseContract(row: F12ContractRecord) {
  if (!canManage.value || saving.value || loading.value || error.value || !row.contract_key || row.is_latest !== true || row.status === "ARCHIVED") return;
  contractError.value = ""; clearContractConflict();
  editingContract.value = row;
  contractForm.amount = ""; contractForm.documentName = row.document_name || ""; contractForm.documentNo = row.document_no || ""; contractForm.reason = "";
  contractFile.value = null; uploadedContractFile.value = null;
  contractUploadLifecycle.value = newLifecycle("contract-attachment"); contractWriteLifecycle.value = newLifecycle("contract-revision"); contractDialog.value = true;
}
function onContractFileChange(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0] || null;
  contractFile.value = file; uploadedContractFile.value = null;
  contractUploadLifecycle.value = newLifecycle("contract-attachment");
  contractWriteLifecycle.value = newLifecycle(editingContract.value ? "contract-revision" : "contract-create");
  if (file && (file.size < 1 || file.size > 2 * 1024 * 1024)) {
    contractFile.value = null;
    (event.target as HTMLInputElement).value = "";
    contractError.value = "附件必须在 1 字节到 2 MiB 之间。";
  } else if (file && !/\.(pdf|png|jpe?g)$/i.test(file.name)) {
    contractFile.value = null; (event.target as HTMLInputElement).value = "";
    contractError.value = "仅支持 PDF、PNG 或 JPEG 附件。";
  } else {
    contractError.value = "";
  }
}
function onCreditFileChange(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0] || null;
  creditFile.value = file; uploadedCreditFile.value = null;
  creditUploadLifecycle.value = newLifecycle("credit-attachment");
  creditWriteLifecycle.value = newLifecycle("credit-version");
  if (file && (file.size < 1 || file.size > 2 * 1024 * 1024)) {
    creditFile.value = null;
    (event.target as HTMLInputElement).value = "";
    creditError.value = "附件必须在 1 字节到 2 MiB 之间。";
  } else if (file && !/\.(pdf|png|jpe?g)$/i.test(file.name)) {
    creditFile.value = null; (event.target as HTMLInputElement).value = "";
    creditError.value = "仅支持 PDF、PNG 或 JPEG 附件。";
  } else {
    creditError.value = "";
  }
}

function openCreditDialog() {
  if (!canManage.value || saving.value || loading.value || error.value) return;
  creditError.value = ""; clearCreditConflict();
  creditForm.amount = ""; creditForm.currency = currency.value; creditForm.documentName = ""; creditForm.documentNo = ""; creditForm.reason = "";
  creditFile.value = null; uploadedCreditFile.value = null;
  creditUploadLifecycle.value = newLifecycle("credit-attachment"); creditWriteLifecycle.value = newLifecycle("credit-version"); creditDialog.value = true;
}

async function getContractFileID(snapshot: SubjectSnapshot): Promise<string | null> {
  if (!isCurrentSubject(snapshot)) return null;
  if (uploadedContractFile.value) return uploadedContractFile.value.id;
  if (contractFile.value) {
    const uploaded = await f12SubjectRecordsApi.uploadAttachment(snapshot.subjectType, snapshot.subjectID, "CONTRACT", contractFile.value, contractReason(), contractUploadLifecycle.value, snapshot.flowContext);
    if (!isCurrentSubject(snapshot)) return null;
    uploadedContractFile.value = uploaded;
    return uploaded.id;
  }
  if (editingContract.value?.file_id) return editingContract.value.file_id;
  throw new Error("首次登记合同必须上传 PDF、PNG 或 JPEG 附件。");
}

async function saveContract() {
  if (!canManage.value || saving.value || loading.value || error.value) return;
  if (contractConflict.value && !contractConflictReady.value) return;
  if (!contractForm.documentName.trim() || !contractForm.documentNo.trim()) {
    contractError.value = "请填写资料名称和资料编号。"; return;
  }
  const snapshot = captureSubject();
  const lifecycle = contractWriteLifecycle.value;
  saving.value = true; contractError.value = "";
  try {
    const fileID = await getContractFileID(snapshot);
    if (!fileID || !isCurrentSubject(snapshot)) return;
    const input = { subjectType: snapshot.subjectType, subjectID: snapshot.subjectID, documentName: contractForm.documentName, documentNo: contractForm.documentNo, fileID, reason: contractReason() };
    if (editingContract.value) {
      await f12SubjectRecordsApi.reviseContract(editingContract.value.contract_key!, editingContract.value.version, input, lifecycle, snapshot.flowContext);
    } else {
      await f12SubjectRecordsApi.createContract(input, lifecycle, snapshot.flowContext);
    }
    if (!isCurrentSubject(snapshot)) return;
    contractDialog.value = false; contractPage.value = 1; emit("changed"); await load();
    if (!isCurrentSubject(snapshot)) return;
    ElMessage.success("合同资料已保存为新版本。");
  } catch (value) {
    if (!isCurrentSubject(snapshot)) return;
    contractError.value = f12ErrorMessage(value, "合同资料保存失败。");
    if (isRevisionConflict(value)) { contractConflict.value = true; contractConflictReady.value = false; contractDataRefreshed.value = false; refreshedContract.value = null; }
    else clearContractConflict();
  } finally { if (isCurrentSubject(snapshot)) saving.value = false; }
}

async function refreshContractConflict() {
  if (loading.value || saving.value) return;
  const snapshot = captureSubject();
  const contractKey = editingContract.value?.contract_key;
  contractConflictReady.value = false;
  contractDataRefreshed.value = false;
  let latest: F12ContractRecord | null = null;
  try { if (contractKey) latest = await loadLatestContract(contractKey); else await load(); }
  catch (value) { if (isCurrentSubject(snapshot)) contractError.value = f12ErrorMessage(value, "刷新最新合同失败，草稿仍保留。"); return; }
  if (!isCurrentSubject(snapshot)) return;
  if (error.value) { contractError.value = `刷新失败：${error.value}；当前草稿仍保留。`; return; }
  if (contractKey) {
    if (!latest || latest.status === "ARCHIVED") {
      contractConflictReady.value = false;
      contractError.value = "该合同已变化或归档。草稿已保留，请取消后核对最新历史记录。";
      return;
    }
    editingContract.value = latest;
    refreshedContract.value = latest;
    contractError.value = `已刷新到合同版本 V${latest.version}；草稿及已上传附件均保留。核对后可按新版本重试。`;
  } else {
    contractError.value = "合同列表已刷新；草稿及已上传附件均保留，请核对是否已有相同资料。";
  }
  contractWriteLifecycle.value = newLifecycle(editingContract.value ? "contract-revision" : "contract-create");
  contractDataRefreshed.value = true;
  if (props.flowContext) {
    emit("refresh-flow-context");
    contractError.value += " 已请求办理入口刷新流程版本；收到新版本前不能重试。";
  } else {
    contractConflictReady.value = true;
  }
}

async function loadLatestContract(contractKey: string) {
  request?.abort();
  const current = request = new AbortController(), currentSequence = ++sequence;
  const snapshot = captureSubject();
  loading.value = true;
  try {
    return await f12SubjectRecordsApi.latestContract(snapshot.subjectType, snapshot.subjectID, contractKey, current.signal);
  } finally { if (currentSequence === sequence && !current.signal.aborted) loading.value = false; }
}

function openArchive(row: F12ContractRecord) {
  if (!canManage.value || saving.value || loading.value || error.value || !row.contract_key || row.is_latest !== true || row.status === "ARCHIVED") return;
  archiveError.value = ""; clearArchiveConflict(); archiveTarget.value = row; archiveReason.value = ""; archiveWriteLifecycle.value = newLifecycle("contract-archive"); archiveDialog.value = true;
}
async function archiveContract() {
  const row = archiveTarget.value;
  if (!canManage.value || loading.value || error.value) return;
  if (archiveConflict.value && !archiveConflictReady.value) return;
  if (!row || !archiveReason.value.trim() || saving.value) { archiveError.value = "请填写归档原因。"; return; }
  const snapshot = captureSubject();
  const lifecycle = archiveWriteLifecycle.value;
  saving.value = true; archiveError.value = "";
  try {
    await f12SubjectRecordsApi.archiveContract(row, archiveReason.value, lifecycle, snapshot.flowContext);
    if (!isCurrentSubject(snapshot)) return;
    archiveDialog.value = false; emit("changed"); await load();
    if (!isCurrentSubject(snapshot)) return;
    ElMessage.success("合同已归档，历史版本已保留。");
  } catch (value) {
    if (!isCurrentSubject(snapshot)) return;
    archiveError.value = f12ErrorMessage(value, "合同归档失败。");
    if (isRevisionConflict(value)) { archiveConflict.value = true; archiveConflictReady.value = false; archiveDataRefreshed.value = false; }
    else clearArchiveConflict();
  }
  finally { if (isCurrentSubject(snapshot)) saving.value = false; }
}

async function refreshArchiveConflict() {
  if (loading.value || saving.value || !archiveTarget.value?.contract_key) return;
  const snapshot = captureSubject();
  const key = archiveTarget.value.contract_key;
  archiveConflictReady.value = false;
  archiveDataRefreshed.value = false;
  let latest: F12ContractRecord | null;
  try { latest = await loadLatestContract(key); }
  catch (value) { if (isCurrentSubject(snapshot)) archiveError.value = f12ErrorMessage(value, "刷新最新合同失败，归档原因仍保留。"); return; }
  if (!isCurrentSubject(snapshot)) return;
  if (!latest || latest.status === "ARCHIVED") {
    archiveConflictReady.value = false;
    archiveError.value = "该合同已变化或归档，请关闭弹窗后核对最新记录。";
    return;
  }
  archiveTarget.value = latest;
  archiveDataRefreshed.value = true;
  archiveWriteLifecycle.value = newLifecycle("contract-archive");
  if (props.flowContext) {
    archiveConflictReady.value = false;
    emit("refresh-flow-context");
    archiveError.value = `已刷新到合同版本 V${latest.version}；归档原因保留。已请求办理入口刷新流程版本，收到新版本前不能重试。`;
  } else {
    archiveConflictReady.value = true;
    archiveError.value = `已刷新到合同版本 V${latest.version}；归档原因保留。核对后可按新版本重试。`;
  }
}

function currentCredit(currency: string) { return currentCredits.value.find(row => row.currency === currency); }
function creditStatusLabel(status: F12CurrentCredit["status"]) {
  if (status === "AVAILABLE") return "有效";
  if (status === "AMBIGUOUS_VERSION") return "版本不明确，需核对";
  return "历史金额无效，需核对";
}
function creditAmount(record?: F12CreditRecord) { return record ? `${record.amount} ${record.currency}` : "不可确认"; }

// ---- 合并视图（原型「档案与办理 → 合同与授信」是一张表）----
// 默认 full 布局给客户详情用；供应商档案页传 layout="merged"。
const isMerged = computed(() => props.layout === "merged");
type MergedRow = {
  key: string; kind: "CONTRACT" | "CREDIT"; name: string; documentName: string; documentNo: string;
  quota: string; createdAt: string; operator: string; note: string; fileID?: string;
  version: number; latest: boolean; versions: number;
  contract?: F12ContractRecord; credit?: F12CreditRecord;
};
const latestCreditVersion = (currency: string) => Math.max(0, ...credits.value.filter(row => row.currency === currency).map(row => row.version));
const mergedRows = computed<MergedRow[]>(() => {
  const rows: MergedRow[] = [];
  for (const row of contracts.value) {
    rows.push({
      key: `contract-${row.id}`, kind: "CONTRACT", name: `合同 · ${row.document_name || "未记录名称"}`,
      documentName: row.document_name || "contract.pdf", documentNo: row.document_no || "—", quota: "—",
      createdAt: row.created_at, operator: row.created_by || "—", note: row.reason || "未记录",
      fileID: row.file_id, version: row.version, latest: row.is_latest === true,
      versions: contracts.value.filter(item => (item.document_no || item.id) === (row.document_no || row.id)).length,
      contract: row,
    });
  }
  for (const row of credits.value) {
    rows.push({
      key: `credit-${row.id}`, kind: "CREDIT", name: `授信 · ${row.currency}`,
      documentName: row.document_name || "credit.pdf", documentNo: row.document_no || "—",
      quota: creditAmount(row), createdAt: row.created_at, operator: row.created_by || "—",
      note: row.reason || "未记录", fileID: row.file_id, version: row.version,
      latest: row.version === latestCreditVersion(row.currency),
      versions: credits.value.filter(item => item.currency === row.currency).length,
      credit: row,
    });
  }
  return rows.sort((a, b) => (Date.parse(b.createdAt) || 0) - (Date.parse(a.createdAt) || 0));
});
const historyTarget = ref<MergedRow | null>(null);
const historyRows = computed(() => {
  const target = historyTarget.value;
  if (!target) return [] as MergedRow[];
  return mergedRows.value.filter(row => row.kind === target.kind &&
    (target.kind === "CONTRACT" ? (row.contract?.document_no || row.contract?.id) === (target.contract?.document_no || target.contract?.id) : row.credit?.currency === target.credit?.currency));
});
function openHistory(row: MergedRow) { historyTarget.value = row; }
function createFromMenu(command: string) {
  // 原型：首次接入的两个模块没办完之前，「新增」只提示不给开单。
  if (props.addBlockedReason) { ElMessage.warning(props.addBlockedReason); return; }
  if (command === "contract") openCreateContract();
  else openCreditDialog();
}
function readinessReasonLabel(reason: string) {
  if (reason === "CONTRACT_REQUIRED") return "缺少有效合同资料";
  if (reason === "CREDIT_REQUIRED") return "缺少有效授信资料";
  if (reason.startsWith("CREDIT_REQUIRED:")) return `缺少 ${reason.split(":")[1]} 授信资料`;
  return "存在未满足的办理资料条件";
}
function formatDate(value: string | null) { return value ? new Date(value).toLocaleString("zh-CN", { hour12: false }) : "未登记"; }
function changeContractPage(page: number) { contractPage.value = page; void load(); }
function changeCreditPage(page: number) { creditPage.value = page; void load(); }

function safeDownloadFilename(value: string, fallback = "attachment") {
  const safe = value.replace(/[\\/\u0000-\u001f\u007f]/g, "_").trim().slice(0, 180);
  return safe || fallback;
}

function downloadFilename(contentDisposition: string | undefined, fallback: string) {
  fallback = safeDownloadFilename(fallback);
  if (!contentDisposition) return fallback;
  const extended = contentDisposition.match(/(?:^|;)\s*filename\*\s*=\s*([^;]+)/i)?.[1]?.trim().replace(/^"|"$/g, "");
  const quoted = contentDisposition.match(/(?:^|;)\s*filename\s*=\s*"((?:\\.|[^"])*)"/i)?.[1]?.replace(/\\([\\"])/g, "$1");
  const plain = contentDisposition.match(/(?:^|;)\s*filename\s*=\s*([^;]+)/i)?.[1]?.trim().replace(/^"|"$/g, "");
  let name = extended || quoted || plain || fallback;
  const encodedName = name.match(/^[^']*''(.*)$/)?.[1];
  if (encodedName) { try { name = decodeURIComponent(encodedName); } catch { name = fallback; } }
  return safeDownloadFilename(name, fallback);
}

async function saveCreditVersion() {
  if (!canManage.value || saving.value || loading.value || error.value) return;
  if (creditConflict.value && !creditConflictReady.value) return;
  if (!creditForm.documentName.trim() || !creditForm.documentNo.trim() || !creditForm.amount.trim()) {
    creditError.value = "请填写资料名称、资料编号和授信额度。"; return;
  }
  let credit: ReturnType<typeof normalizeF12CreditInput>;
  try { credit = normalizeF12CreditInput(creditForm.amount, creditForm.currency); }
  catch (value) { creditError.value = value instanceof Error ? value.message : "请检查额度和币种。"; return; }
  const existing = currentCredit(credit.currency);
  if (existing?.status === "AMBIGUOUS_VERSION") { creditError.value = "当前维度存在重复版本，需先核对历史记录后再维护。"; return; }
  const snapshot = captureSubject();
  const uploadLifecycle = creditUploadLifecycle.value;
  const writeLifecycle = creditWriteLifecycle.value;
  saving.value = true; creditError.value = "";
  try {
    if (!creditFile.value && !uploadedCreditFile.value) throw new Error("每条授信版本都必须上传对应依据附件。");
    if (!uploadedCreditFile.value) {
      const uploaded = await f12SubjectRecordsApi.uploadAttachment(snapshot.subjectType, snapshot.subjectID, "CREDIT", creditFile.value!, creditReason(), uploadLifecycle, snapshot.flowContext);
      if (!isCurrentSubject(snapshot)) return;
      uploadedCreditFile.value = uploaded;
    }
    if (!isCurrentSubject(snapshot)) return;
    const direction = snapshot.subjectType === "CUSTOMER" ? "US_TO_CUSTOMER" : "SUPPLIER_TO_US";
    await f12SubjectRecordsApi.addCreditVersion({ subjectType: snapshot.subjectType, subjectID: snapshot.subjectID, direction, ...credit,
      documentName: creditForm.documentName, documentNo: creditForm.documentNo, fileID: uploadedCreditFile.value.id, expectedVersion: existing?.latest_version ?? existing?.record?.version ?? 0, reason: creditReason() }, writeLifecycle, snapshot.flowContext);
    if (!isCurrentSubject(snapshot)) return;
    creditDialog.value = false; creditPage.value = 1; emit("changed"); await load();
    if (!isCurrentSubject(snapshot)) return;
    ElMessage.success("授信已保存为新版本。");
  } catch (value) {
    if (!isCurrentSubject(snapshot)) return;
    creditError.value = f12ErrorMessage(value, "授信保存失败。");
    if (isRevisionConflict(value)) { creditConflict.value = true; creditConflictReady.value = false; }
    else clearCreditConflict();
  }
  finally { if (isCurrentSubject(snapshot)) saving.value = false; }
}

async function refreshCreditConflict() {
  if (loading.value || saving.value) return;
  const snapshot = captureSubject();
  await load();
  if (!isCurrentSubject(snapshot)) return;
  if (error.value) { creditError.value = `刷新失败：${error.value}；授信草稿及附件均保留。`; return; }
  const currency = creditForm.currency.trim().toUpperCase();
  const latest = currentCredit(currency);
  if (latest?.status === "AMBIGUOUS_VERSION") {
    creditConflictReady.value = false;
    creditError.value = "刷新后仍发现重复授信版本，不能自动重试；请先核对历史记录。";
    return;
  }
  const version = latest?.latest_version ?? latest?.record?.version ?? 0;
  creditError.value = `已刷新 ${currency} 当前版本（V${version}）；授信草稿及已上传附件均保留。核对后可重试。`;
  creditWriteLifecycle.value = newLifecycle("credit-version");
  if (props.flowContext) {
    emit("refresh-flow-context");
    creditError.value += " 已请求办理入口刷新流程版本；收到新版本前不能重试。";
  } else {
    creditConflictReady.value = true;
  }
}

async function downloadFile(fileID: string | undefined, purpose: "CONTRACT" | "CREDIT", filename: string) {
  if (!canDownloadAttachment || !fileID || downloading.value) return;
  const snapshot = captureSubject();
  downloading.value = fileID; error.value = "";
  try {
    const file = await f12SubjectRecordsApi.attachmentFile(snapshot.subjectType, snapshot.subjectID, fileID, purpose);
    if (!isCurrentSubject(snapshot)) return;
    const url = URL.createObjectURL(file.blob), link = document.createElement("a");
    link.href = url; link.download = downloadFilename(file.contentDisposition, filename); link.rel = "noopener"; link.click();
    window.setTimeout(() => URL.revokeObjectURL(url), 1000);
  } catch (value) { if (isCurrentSubject(snapshot)) error.value = f12ErrorMessage(value, "附件下载失败。", "download"); }
  finally { if (isCurrentSubject(snapshot)) downloading.value = ""; }
}

watch(() => [props.subjectType, props.subjectId] as const, () => {
  ++subjectEpoch;
  clearSubjectDrafts();
  clearSubjectProjection();
  void load();
}, { immediate: true });
watch(canManage, allowed => {
  if (allowed) return;
  ++subjectEpoch;
  clearSubjectDrafts();
});
onBeforeUnmount(() => { ++sequence; ++subjectEpoch; request?.abort(); });
</script>

<template>
  <section class="f12-subject-panel" :data-subject-type="subjectType" :data-subject-id="subjectId">
    <div class="panel-heading">
      <div><h3>合同与授信</h3><p v-if="!isMerged">主体 {{ subjectId }} · {{ subjectType === "SUPPLIER" ? "供应商" : "客户" }}</p></div>
      <div class="heading-actions">
      <!-- 办理上下文（工作台合同授信待办进来）按原型只给两个上传动作：先把合同、授信两份资料传齐。 -->
      <template v-if="flowContext">
        <el-button v-if="canManage" size="small" :disabled="loading || saving || !!error" @click="openCreateContract">上传合同资料</el-button>
        <el-button v-if="canManage" size="small" type="primary" :disabled="loading || saving || !!error" @click="openCreditDialog">上传授信资料</el-button>
      </template>
      <template v-else-if="isMerged">
        <!-- 首次接入没办完时「新增」只提示（原型同样只弹提示，不给开单），所以不展开菜单。 -->
        <el-button v-if="canManage && addBlockedReason" size="small" @click="createFromMenu('contract')">新增</el-button>
        <el-dropdown v-else-if="canManage" trigger="click" :disabled="loading || saving || !!error" @command="createFromMenu">
          <el-button size="small" :disabled="loading || saving || !!error">新增</el-button>
          <template #dropdown><el-dropdown-menu><el-dropdown-item command="contract">合同</el-dropdown-item><el-dropdown-item command="credit">授信</el-dropdown-item></el-dropdown-menu></template>
        </el-dropdown>
      </template>
      <template v-else>
        <el-button v-if="canManage" size="small" :disabled="loading || saving || !!error" @click="openCreateContract">上传合同资料</el-button><el-button v-if="canManage" size="small" type="primary" :disabled="loading || saving || !!error" @click="openCreditDialog">上传授信资料</el-button>
      </template>
      <el-button v-if="!isMerged && !flowContext" size="small" :loading="loading" :disabled="saving" @click="load">刷新</el-button>
    </div>
    </div>
    <el-alert v-if="error" type="error" :title="error" :closable="false" show-icon />
    <el-alert v-if="!canManage" type="info" title="当前为只读查看；合同/授信的新增与修改仅向主体责任人或当前办理人开放，附件查看与下载不受限制。" :closable="false" />
    <el-alert v-if="flowContext && readinessError" type="warning" :title="readinessError" :closable="false" show-icon />
    <el-alert v-else-if="flowContext && readiness" :type="readiness.ready ? 'success' : 'warning'" :title="readiness.ready ? `办理资料已齐备 · 流程 R${readiness.source_flow_revision}` : `办理资料待补齐 · 流程 R${readiness.source_flow_revision}`" :closable="false" show-icon>
      <ul v-if="readiness.missing_reasons.length"><li v-for="reason in readiness.missing_reasons" :key="reason">{{ readinessReasonLabel(reason) }}</li></ul>
      <span v-else>合同版本 {{ readiness.contracts.map(row => `V${row.version}`).join("、") || "—" }}；授信版本 {{ readiness.credits.map(row => `${row.currency} V${row.version}`).join("、") || "—" }}</span>
    </el-alert>
    <div v-loading="loading" class="panel-body">
      <!-- 供应商档案页：合同与授信合成一张表（原型「类型/名称｜资料编号｜授信额度｜时间｜操作人｜备注｜附件｜操作」）。 -->
      <template v-if="isMerged">
        <el-table :data="mergedRows" row-key="key" size="small" empty-text="暂无合同或授信记录">
          <el-table-column label="类型／名称" min-width="180"><template #default="{ row }">{{ row.name }}<span v-if="row.version > 1" class="merged-version">V{{ row.version }}</span></template></el-table-column>
          <el-table-column label="资料编号" min-width="170"><template #default="{ row }">{{ row.documentNo }}</template></el-table-column>
          <el-table-column label="授信额度" min-width="120"><template #default="{ row }">{{ row.quota }}</template></el-table-column>
          <el-table-column label="时间" min-width="150"><template #default="{ row }">{{ formatDate(row.createdAt) }}</template></el-table-column>
          <el-table-column label="操作人" min-width="100"><template #default="{ row }">{{ row.operator }}</template></el-table-column>
          <el-table-column label="备注" min-width="140" show-overflow-tooltip><template #default="{ row }">{{ row.note }}</template></el-table-column>
          <el-table-column label="附件" min-width="100">
            <template #default="{ row }">
              <el-button v-if="canDownloadAttachment && row.fileID" link type="primary" :loading="downloading === row.fileID" @click="downloadFile(row.fileID, row.kind, row.documentName)">下载</el-button>
              <span v-else>未关联</span>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="170">
            <template #default="{ row }">
              <el-button v-if="row.kind === 'CONTRACT' && row.latest && row.contract && row.contract.status !== 'ARCHIVED'" link type="primary" :disabled="loading || saving" @click="openReviseContract(row.contract)">编辑</el-button>
              <el-button v-if="row.kind === 'CREDIT' && row.latest" link type="primary" :disabled="loading || saving" @click="openCreditDialog">调整</el-button>
              <el-button link type="primary" @click="openHistory(row)">版本历史</el-button>
            </template>
          </el-table-column>
        </el-table>
        <p class="merged-count">共 {{ mergedRows.length }} 条<span v-if="contractTotal + creditTotal > mergedRows.length">（合同与授信各取最近 {{ historyPageSize }} 条，可在客户/供应商维护中查看更早版本）</span></p>
      </template>
      <template v-else>
      <h4>合同资料</h4>
      <el-table :data="contracts" row-key="id" size="small" empty-text="暂无合同记录">
        <el-table-column prop="document_name" label="合同名称" min-width="150"><template #default="{ row }">{{ row.document_name || "旧结构未记录名称" }}</template></el-table-column>
        <el-table-column prop="document_no" label="合同编号" min-width="140"><template #default="{ row }">{{ row.document_no || "—" }}</template></el-table-column>
        <el-table-column label="版本" width="110"><template #default="{ row }">V{{ row.version }}<span v-if="row.is_latest === true" class="latest-version">当前版本</span><span v-else-if="row.is_latest === false" class="historical-version">历史版本</span></template></el-table-column>
        <el-table-column label="状态" width="100"><template #default="{ row }">{{ row.status === "ARCHIVED" ? "已归档" : "办理中" }}</template></el-table-column>
        <el-table-column label="附件" min-width="140"><template #default="{ row }"><el-button v-if="canDownloadAttachment && row.file_id" link type="primary" :loading="downloading === row.file_id" @click="downloadFile(row.file_id, 'CONTRACT', row.document_name || 'contract.pdf')">下载 {{ row.has_attachment ? "附件" : "" }}</el-button><span v-else>{{ row.has_attachment ? "旧附件暂不可下载" : "未关联" }}</span></template></el-table-column>
        <el-table-column prop="created_by" :label="subjectType === 'SUPPLIER' ? '操作人' : '登记人ID'" width="100"><template #default="{ row }">{{ row.created_by || "—" }}</template></el-table-column>
        <el-table-column v-if="subjectType !== 'SUPPLIER'" label="实际办理人ID" width="120"><template #default="{ row }">{{ row.actual_operator_id || "—" }}</template></el-table-column>
        <el-table-column v-if="subjectType !== 'SUPPLIER'" label="办理来源" min-width="180"><template #default="{ row }">{{ row.source_flow_id ? `${row.source_module_key} · 流程 ${row.source_flow_id} / R${row.source_flow_revision}` : "普通维护" }}</template></el-table-column>
        <el-table-column label="说明" min-width="180" show-overflow-tooltip><template #default="{ row }">{{ row.reason || "未记录" }}</template></el-table-column>
        <el-table-column label="保存时间" min-width="170"><template #default="{ row }">{{ formatDate(row.created_at) }}</template></el-table-column>
         <el-table-column v-if="canManage" label="操作" width="145"><template #default="{ row }"><el-button v-if="row.is_latest === true && row.status !== 'ARCHIVED'" link type="primary" :disabled="loading || saving || !!error" @click="openReviseContract(row)">修订</el-button><el-button v-if="row.is_latest === true && row.status !== 'ARCHIVED'" link type="danger" :disabled="loading || saving || !!error" @click="openArchive(row)">归档</el-button></template></el-table-column>
      </el-table>
      <el-pagination v-if="contractTotal > historyPageSize" class="history-pagination contract-history-pagination" :current-page="contractPage" :page-size="historyPageSize" :total="contractTotal" layout="total, prev, pager, next" :disabled="loading" @current-change="changeContractPage" />

      <h4>当前授信</h4>
      <el-table :data="currentCredits" row-key="currency" size="small" empty-text="未登记授信">
        <el-table-column prop="currency" label="币种" width="100" />
        <el-table-column label="方向" min-width="140"><template #default="{ row }">{{ row.direction === "SUPPLIER_TO_US" ? "供应商 → 我方" : "我方 → 客户" }}</template></el-table-column>
        <el-table-column label="额度" min-width="160"><template #default="{ row }">{{ creditAmount(row.record) }}</template></el-table-column>
        <el-table-column label="状态" min-width="180"><template #default="{ row }">{{ creditStatusLabel(row.status) }}</template></el-table-column>
        <el-table-column label="版本" width="90"><template #default="{ row }">{{ row.latest_version || row.record?.version || "—" }}</template></el-table-column>
      </el-table>

      <h4>授信历史</h4>
      <el-table :data="credits" row-key="id" size="small" empty-text="暂无授信记录">
        <el-table-column label="额度" min-width="150"><template #default="{ row }">{{ creditAmount(row) }}</template></el-table-column>
        <el-table-column label="方向" min-width="140"><template #default="{ row }">{{ row.direction === "SUPPLIER_TO_US" ? "供应商 → 我方" : "我方 → 客户" }}</template></el-table-column>
        <el-table-column prop="document_name" label="依据名称" min-width="150"><template #default="{ row }">{{ row.document_name || "旧结构未记录名称" }}</template></el-table-column>
        <el-table-column prop="document_no" label="依据编号" min-width="130"><template #default="{ row }">{{ row.document_no || "—" }}</template></el-table-column>
        <el-table-column prop="version" label="版本" width="80" />
        <el-table-column label="附件" min-width="120"><template #default="{ row }"><el-button v-if="canDownloadAttachment && row.file_id" link type="primary" :loading="downloading === row.file_id" @click="downloadFile(row.file_id, 'CREDIT', row.document_name || 'credit.pdf')">下载</el-button><span v-else>未登记 / 旧附件</span></template></el-table-column>
        <el-table-column prop="created_by" :label="subjectType === 'SUPPLIER' ? '操作人' : '登记人ID'" width="100"><template #default="{ row }">{{ row.created_by || "—" }}</template></el-table-column>
        <el-table-column v-if="subjectType !== 'SUPPLIER'" label="实际办理人ID" width="120"><template #default="{ row }">{{ row.actual_operator_id || "—" }}</template></el-table-column>
        <el-table-column v-if="subjectType !== 'SUPPLIER'" label="办理来源" min-width="180"><template #default="{ row }">{{ row.source_flow_id ? `${row.source_module_key} · 流程 ${row.source_flow_id} / R${row.source_flow_revision}` : "普通维护" }}</template></el-table-column>
        <el-table-column label="说明" min-width="180" show-overflow-tooltip><template #default="{ row }">{{ row.reason || "未记录" }}</template></el-table-column>
        <el-table-column label="保存时间" min-width="170"><template #default="{ row }">{{ formatDate(row.created_at) }}</template></el-table-column>
      </el-table>
      <el-pagination v-if="creditTotal > historyPageSize" class="history-pagination credit-history-pagination" :current-page="creditPage" :page-size="historyPageSize" :total="creditTotal" layout="total, prev, pager, next" :disabled="loading" @current-change="changeCreditPage" />
      <p class="read-only-note">合同与授信分别登记；当前额度取对应币种的最新有效版本，不累加历史。旧记录未关联可下载附件时显示不可下载。保存冲突时保留表单，请刷新核对后再提交。</p>
      </template>
    </div>

    <el-dialog :model-value="!!historyTarget" append-to-body title="版本历史" width="min(680px, 94vw)" @close="historyTarget = null">
      <p class="muted">{{ historyTarget?.name }} · 已加载 {{ historyRows.length }} 个版本</p>
      <el-table :data="historyRows" size="small" empty-text="暂无历史版本">
        <el-table-column label="版本" width="80"><template #default="{ row }">V{{ row.version }}</template></el-table-column>
        <el-table-column label="资料编号" min-width="170"><template #default="{ row }">{{ row.documentNo }}</template></el-table-column>
        <el-table-column label="授信额度" min-width="110"><template #default="{ row }">{{ row.quota }}</template></el-table-column>
        <el-table-column label="备注" min-width="150" show-overflow-tooltip><template #default="{ row }">{{ row.note }}</template></el-table-column>
        <el-table-column label="保存时间" min-width="150"><template #default="{ row }">{{ formatDate(row.createdAt) }}</template></el-table-column>
        <el-table-column label="操作" width="90"><template #default="{ row }"><el-tag v-if="row.latest" size="small" type="success">当前</el-tag><span v-else class="muted">历史</span></template></el-table-column>
      </el-table>
      <template #footer><el-button @click="historyTarget = null">关闭</el-button></template>
    </el-dialog>

    <el-dialog v-model="contractDialog" append-to-body :title="editingContract ? '修订合同资料' : '上传合同资料'" width="min(860px, 94vw)" destroy-on-close :close-on-click-modal="false" :close-on-press-escape="!saving && !loading" :show-close="!saving && !loading">
      <el-alert v-if="contractError" :type="contractConflict ? 'warning' : 'error'" :title="contractError" :closable="false">
        <template v-if="contractConflict"><el-button text :disabled="loading || saving" @click="refreshContractConflict">刷新合同版本{{ flowContext ? '并请求刷新办理上下文' : '' }}</el-button></template>
      </el-alert>
      <el-form label-position="top" :disabled="saving || loading" @submit.prevent="saveContract">
        <el-descriptions v-if="refreshedContract" title="服务器最新资料（草稿保留，请核对）" :column="1" border size="small">
          <el-descriptions-item label="版本">V{{ refreshedContract.version }}</el-descriptions-item>
          <el-descriptions-item label="合同名称">{{ refreshedContract.document_name || '未记录' }}</el-descriptions-item>
          <el-descriptions-item label="合同编号">{{ refreshedContract.document_no || '未记录' }}</el-descriptions-item>
          <el-descriptions-item label="附件">{{ refreshedContract.has_attachment ? '已关联；未选新文件时沿用最新版本附件' : '未关联' }}</el-descriptions-item>
        </el-descriptions>
        <!-- 原型「上传合同资料」：两列四格 + 备注占整行。 -->
        <div class="f12-form-grid">
          <el-form-item label="资料名称"><el-input v-model="contractForm.documentName" maxlength="128" :disabled="saving" aria-label="资料名称" /></el-form-item>
          <el-form-item label="资料编号"><el-input v-model="contractForm.documentNo" maxlength="128" :disabled="saving" aria-label="资料编号" /></el-form-item>
          <el-form-item :label="`授信额度（${currencyText}），合同可留空`"><el-input v-model="contractForm.amount" inputmode="decimal" :disabled="saving" aria-label="授信额度" /></el-form-item>
          <el-form-item label="附件上传">
            <input class="f12-file" type="file" accept=".pdf,.png,.jpg,.jpeg" :disabled="saving || loading" @change="onContractFileChange">
            <small>2 MB 以内；{{ uploadedContractFile?.name || contractFile?.name || (editingContract?.file_id ? '不选新文件则沿用本版本附件' : '首次登记必须上传附件') }}</small>
          </el-form-item>
          <el-form-item label="备注" class="full"><el-input v-model="contractForm.reason" type="textarea" :rows="2" maxlength="500" :disabled="saving" aria-label="备注" /></el-form-item>
        </div>
      </el-form>
      <template #footer><el-button :disabled="saving || loading" @click="contractDialog = false">取消</el-button><el-button type="primary" :loading="saving" :disabled="saving || loading || !!error || contractConflict && !contractConflictReady" @click="saveContract">{{ contractConflictReady ? '按最新版本重试' : '保存资料' }}</el-button></template>
    </el-dialog>

    <el-dialog v-model="creditDialog" append-to-body title="上传授信资料" width="min(860px, 94vw)" destroy-on-close :close-on-click-modal="false" :close-on-press-escape="!saving && !loading" :show-close="!saving && !loading">
      <el-alert v-if="creditError" :type="creditConflict ? 'warning' : 'error'" :title="creditError" :closable="false">
        <template v-if="creditConflict"><el-button text :disabled="loading || saving" @click="refreshCreditConflict">刷新当前额度{{ flowContext ? '并请求刷新办理上下文' : '' }}</el-button></template>
      </el-alert>
      <el-form label-position="top" :disabled="saving || loading" @submit.prevent="saveCreditVersion">
        <div class="f12-form-grid">
          <el-form-item label="资料名称"><el-input v-model="creditForm.documentName" maxlength="128" :disabled="saving" aria-label="资料名称" /></el-form-item>
          <el-form-item label="资料编号"><el-input v-model="creditForm.documentNo" maxlength="128" :disabled="saving" aria-label="资料编号" /></el-form-item>
          <el-form-item :label="`授信额度（${currencyText}）`"><el-input v-model="creditForm.amount" inputmode="decimal" placeholder="正数，最多两位小数" :disabled="saving" aria-label="授信额度" /></el-form-item>
          <el-form-item label="附件上传">
            <input class="f12-file" type="file" accept=".pdf,.png,.jpg,.jpeg" :disabled="saving || loading" @change="onCreditFileChange">
            <small>2 MB 以内；{{ uploadedCreditFile?.name || creditFile?.name || '每个授信版本都需提供对应依据附件' }}</small>
          </el-form-item>
          <el-form-item label="备注" class="full"><el-input v-model="creditForm.reason" type="textarea" :rows="2" maxlength="500" :disabled="saving" aria-label="备注" /></el-form-item>
        </div>
      </el-form>
      <template #footer><el-button :disabled="saving || loading" @click="creditDialog = false">取消</el-button><el-button type="primary" :loading="saving" :disabled="saving || loading || !!error || creditConflict && !creditConflictReady" @click="saveCreditVersion">{{ creditConflictReady ? '按最新版本重试' : '保存资料' }}</el-button></template>
    </el-dialog>

    <el-dialog v-model="archiveDialog" append-to-body title="归档合同" width="min(440px, 94vw)" :close-on-click-modal="false" :close-on-press-escape="!saving && !loading" :show-close="!saving && !loading">
      <el-alert v-if="archiveError" :type="archiveConflict ? 'warning' : 'error'" :title="archiveError" :closable="false">
        <template v-if="archiveConflict"><el-button text :disabled="loading || saving" @click="refreshArchiveConflict">刷新合同版本</el-button></template>
      </el-alert>
      <p>归档会新增一个历史版本，不删除原合同记录和附件。</p>
      <el-input v-model="archiveReason" type="textarea" :rows="3" maxlength="500" placeholder="请填写归档原因" :disabled="saving || loading" />
      <template #footer><el-button :disabled="saving || loading" @click="archiveDialog = false">取消</el-button><el-button type="danger" :loading="saving" :disabled="saving || loading || !!error || archiveConflict && !archiveConflictReady" @click="archiveContract">{{ archiveConflictReady ? '按最新版本重试归档' : '确认归档' }}</el-button></template>
    </el-dialog>
  </section>
</template>

<style scoped>
.f12-subject-panel{padding:4px 0}.panel-heading{display:flex;justify-content:space-between;align-items:center;gap:12px;margin-bottom:14px}.panel-heading h3{margin:0 0 4px}.panel-heading p,.read-only-note{font-size:12px;color:var(--app-text-muted);margin:4px 0 12px}.heading-actions{display:flex;gap:8px}.panel-body h4{margin:18px 0 8px;font-size:14px}.panel-body h4:first-child{margin-top:0}.history-pagination{justify-content:flex-end;margin-top:10px}small{display:block;color:var(--app-text-muted);margin-top:4px}
/* 原型「上传合同/授信资料」：两列网格，备注占整行，页脚只有取消与保存。 */
.f12-form-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:4px 16px}
.f12-form-grid .full{grid-column:1/-1}
.f12-form-grid :deep(.el-form-item){margin-bottom:14px}
.f12-file{width:100%;padding:5px 0}
@media(max-width:640px){.f12-form-grid{grid-template-columns:1fr}}
.latest-version,.historical-version{display:block;font-size:12px;color:var(--app-text-muted)}.latest-version{color:var(--el-color-primary)}
@media(max-width:640px){.panel-heading{align-items:flex-start;flex-direction:column}.heading-actions{flex-wrap:wrap}}
</style>
