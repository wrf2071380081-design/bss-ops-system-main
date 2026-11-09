<script setup lang="ts">
// 原型「供应商详情 → 接入模型」顶部的三个动作：接入 / 上架 / 下架。
//  - 接入：对勾选的待接入模型弹「生产配置登记」，填一组公共 Base URL + API Key，逐个完成技术登记。
//  - 上架 / 下架：走同一张上下架登记单（父组件的 ResourceStateRegisterDialog）。
// 原型的「分步配置 / 确认开通 / 取消接入」三个动作已按需求删除。
import { computed, ref, watch } from "vue";
import { canCompleteAccess, completeResourceAccess } from "../../../api/resourceAccess";
import type { ResourceRow } from "../../../api/resources.types";

const props = defineProps<{ supplierId: string; supplierName?: string; selected: ResourceRow[] }>();
const emit = defineEmits<{ changed: []; register: [kind: "up" | "down"] }>();

const open = ref(false), busy = ref(false), error = ref(""), dialogError = ref("");
const baseURL = ref(""), apiKey = ref(""), confirmed = ref(false);
const checked = ref<Record<string, boolean>>({});
const results = ref<{ row: ResourceRow; ok: boolean; message: string }[]>([]);
let epoch = 0;

const accessRows = computed(() => props.selected.filter(canCompleteAccess));
const blockedRows = computed(() => props.selected.filter(row => !canCompleteAccess(row)));
const chosenRows = computed(() => accessRows.value.filter(row => checked.value[row.id] !== false));

watch(() => props.supplierId, () => { epoch += 1; open.value = false; busy.value = false; results.value = []; });

function startAccess() {
  epoch += 1;
  error.value = "";
  results.value = [];
  baseURL.value = "";
  apiKey.value = "";
  confirmed.value = false;
  if (!props.selected.length) { error.value = "请先勾选要接入的模型。"; open.value = false; return; }
  if (blockedRows.value.length) {
    const names = blockedRows.value.map(row => row.model_name).join("、");
    error.value = `所选资源包含非待接入模型：${names}。已接入或已下架的资源请用上架 / 下架，不要重复接入。`;
    open.value = false;
    return;
  }
  checked.value = Object.fromEntries(accessRows.value.map(row => [row.id, true]));
  dialogError.value = "";
  open.value = true;
}

async function submit() {
  if (busy.value) return;
  const rows = chosenRows.value;
  dialogError.value = "";
  if (!rows.length) { dialogError.value = "请至少勾选一个要登记接入的模型。"; return; }
  if (!baseURL.value.trim()) { dialogError.value = "请填写公共生产 Base URL。"; return; }
  if (!confirmed.value) { dialogError.value = "请确认所选模型已在生产平台配置成功。"; return; }
  busy.value = true;
  const current = epoch;
  const collected: typeof results.value = [];
  for (const row of rows) {
    try {
      await completeResourceAccess(row, { baseURL: baseURL.value, apiKey: apiKey.value });
      collected.push({ row, ok: true, message: "已接入" });
    } catch (err) {
      collected.push({ row, ok: false, message: err instanceof Error ? err.message : "接入失败" });
    }
    if (current !== epoch) return;
    results.value = [...collected];
  }
  busy.value = false;
  if (current !== epoch) return;
  const failed = collected.filter(item => !item.ok).length;
  if (!failed) {
    open.value = false;
    results.value = [];
  }
  emit("changed");
}
</script>

<template>
  <div class="batch-access-actions">
    <el-button :disabled="!selected.length || busy" @click="startAccess">接入</el-button>
    <el-button :disabled="!selected.length || busy" @click="emit('register', 'up')">上架</el-button>
    <el-button :disabled="!selected.length || busy" @click="emit('register', 'down')">下架</el-button>
    <span v-if="selected.length">已选 {{ selected.length }} 个模型</span>
    <el-alert v-if="error" class="batch-error" type="error" :closable="false" :title="error" />
  </div>

  <el-dialog v-model="open" :title="`生产配置登记 · ${supplierName || '当前供应商'}`" width="min(760px, 94vw)" append-to-body :close-on-click-modal="false" :close-on-press-escape="!busy" :show-close="!busy">
    <p class="dialog-note">仅在本批可办理范围内选择共用一组生产凭据的模型。</p>
    <el-table :data="chosenRows" size="small" empty-text="没有可登记接入的模型">
      <el-table-column label="本次登记" width="90">
        <template #default="{ row }"><el-checkbox v-model="checked[row.id]" :disabled="busy" :aria-label="`登记 ${row.model_name}`" /></template>
      </el-table-column>
      <el-table-column label="模型" min-width="150"><template #default="{ row }">{{ row.model_name }}</template></el-table-column>
      <el-table-column label="实际接口模型 ID" min-width="200">
        <template #default="{ row }"><el-input :model-value="row.supplier_model_id" readonly :aria-label="`${row.model_name} 接口模型 ID`" /></template>
      </el-table-column>
      <el-table-column v-if="results.length" label="结果" min-width="180">
        <template #default="{ row }">
          <span :class="results.find(item => item.row.id === row.id)?.ok ? 'ok' : 'fail'">{{ results.find(item => item.row.id === row.id)?.message }}</span>
        </template>
      </el-table-column>
    </el-table>
    <el-form label-position="top" class="config-fields" :disabled="busy || !!results.length">
      <el-form-item label="公共生产 Base URL *"><el-input v-model="baseURL" placeholder="https://api.example.com/v1" maxlength="512" aria-label="公共生产 Base URL" /></el-form-item>
      <el-form-item label="公共生产 API Key（该地址已登记时可留空）"><el-input v-model="apiKey" type="password" show-password autocomplete="new-password" placeholder="生产环境凭据，保存后不回显" maxlength="512" aria-label="公共生产 API Key" /></el-form-item>
    </el-form>
    <el-checkbox v-model="confirmed" :disabled="busy || !!results.length">确认所选模型已在生产平台配置成功</el-checkbox>
    <el-alert v-if="dialogError" class="dialog-error" type="error" :closable="false" :title="dialogError" />
    <template #footer>
      <el-button :disabled="busy" @click="open = false">{{ results.length ? '关闭' : '取消' }}</el-button>
      <el-button type="primary" :loading="busy" :disabled="busy || !!results.length" @click="submit">完成所选模型登记</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.batch-access-actions{display:flex;gap:8px;align-items:center;flex-wrap:wrap;margin:10px 0 4px}
.batch-access-actions span{font-size:13px;color:var(--app-text-muted);margin-left:4px}
.batch-error{flex-basis:100%;margin-top:8px}
.dialog-note{margin:0 0 12px;font-size:13px;color:var(--app-text-muted)}
.config-fields{margin-top:16px}
.dialog-error{margin-top:12px}
.ok{color:var(--el-color-success)}.fail{color:var(--el-color-danger)}
</style>
