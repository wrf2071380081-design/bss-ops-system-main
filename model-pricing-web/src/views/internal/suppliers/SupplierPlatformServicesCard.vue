<script setup lang="ts">
import { onMounted, ref, watch } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import {
  supplierPlatformServicesApi,
  type PlatformServiceDTO,
  type PlatformServicePlatform,
} from "../../../api/supplierPlatformServices";

/**
 * 原型「引入与生产平台服务商」卡片。
 *
 * 供应商是统一主体：国内/海外各是一个生产平台上的服务商账号，人工登记、人工确认。
 * 这里的记录**只是账号归属事实**，不代表模型已接入，也不改变任何资源状态。
 */
const props = defineProps<{ supplierId: string; canManage: boolean }>();

const rows = ref<PlatformServiceDTO[]>([]);
const loading = ref(false);
const saving = ref(false);
const error = ref("");
const dialogOpen = ref(false);
const form = ref<{ platform: PlatformServicePlatform; service_name: string; identifier: string }>({
  platform: "CN",
  service_name: "",
  identifier: "",
});

const platformLabel = (platform: string) => (platform === "OVERSEAS" ? "海外平台" : "国内平台");
const statusLabel = (status: string) => (status === "CONFIRMED" ? "已关联" : "待核对");

async function load() {
  if (!props.supplierId) return;
  loading.value = true;
  error.value = "";
  try {
    rows.value = await supplierPlatformServicesApi.list(props.supplierId);
  } catch (value) {
    rows.value = [];
    error.value = value instanceof Error ? value.message : "平台服务商记录加载失败。";
  } finally {
    loading.value = false;
  }
}

function openDialog() {
  if (!props.canManage) {
    ElMessage.warning("当前账号不是该供应商的归属采购人，也没有供应商维护权限。");
    return;
  }
  form.value = { platform: "CN", service_name: "", identifier: "" };
  dialogOpen.value = true;
}

async function submit() {
  if (saving.value) return;
  if (!form.value.service_name.trim() || !form.value.identifier.trim()) {
    ElMessage.warning("请填写服务商名称与固定服务商标识。");
    return;
  }
  saving.value = true;
  try {
    await supplierPlatformServicesApi.create(
      props.supplierId,
      { platform: form.value.platform, service_name: form.value.service_name.trim(), identifier: form.value.identifier.trim() },
      crypto.randomUUID(),
    );
    ElMessage.success("已登记，状态为待核对");
    dialogOpen.value = false;
    await load();
  } catch (value) {
    ElMessage.error(value instanceof Error ? value.message : "登记失败，请重试。");
  } finally {
    saving.value = false;
  }
}

async function confirmRow(row: PlatformServiceDTO) {
  try {
    await supplierPlatformServicesApi.confirm(props.supplierId, row.id, crypto.randomUUID());
    ElMessage.success("已确认关联");
    await load();
  } catch (value) {
    ElMessage.error(value instanceof Error ? value.message : "确认失败，请重试。");
  }
}

async function removeRow(row: PlatformServiceDTO) {
  try {
    await ElMessageBox.confirm(
      `移除「${platformLabel(row.platform)} · ${row.service_name}」这条关联记录？历史操作记录会保留。`,
      "移除平台服务商",
      { confirmButtonText: "移除", cancelButtonText: "取消", type: "warning" },
    );
  } catch {
    return;
  }
  try {
    await supplierPlatformServicesApi.remove(props.supplierId, row.id);
    ElMessage.success("已移除");
    await load();
  } catch (value) {
    ElMessage.error(value instanceof Error ? value.message : "移除失败，请重试。");
  }
}

onMounted(load);
watch(() => props.supplierId, load);
</script>

<template>
  <div class="platform-service-card">
    <div class="card-heading">
      <div>
        <h3>引入与生产平台服务商</h3>
        <p class="sub">同一供应商主体可同时关联国内、海外平台的服务商账号；资源部署位置按每次申报确定。</p>
      </div>
      <el-button size="small" @click="openDialog">登记服务商</el-button>
    </div>
    <el-alert v-if="error" :title="error" type="error" :closable="false" />
    <el-table v-loading="loading" :data="rows" size="small" empty-text="还没有登记平台服务商">
      <el-table-column label="所属平台" width="120">
        <template #default="{ row }">{{ platformLabel(row.platform) }}</template>
      </el-table-column>
      <el-table-column prop="service_name" label="服务商名称" min-width="180" />
      <el-table-column prop="identifier" label="固定服务商标识" min-width="180" />
      <el-table-column label="关联状态" width="110">
        <template #default="{ row }">
          <el-tag size="small" :type="row.status === 'CONFIRMED' ? 'success' : 'warning'">{{ statusLabel(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="150">
        <template #default="{ row }">
          <el-button v-if="row.status === 'PENDING'" link type="primary" @click="confirmRow(row)">确认关联</el-button>
          <el-button link type="danger" @click="removeRow(row)">移除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="dialogOpen" title="登记生产平台服务商" width="min(520px, 94vw)" append-to-body>
      <p class="sub dialog-sub">只记录生产平台已填写并固定的名称与标识；不调用生产 API，也不代表模型接入。</p>
      <el-form label-position="top">
        <el-form-item label="所属平台">
          <el-select v-model="form.platform" style="width: 100%">
            <el-option label="国内平台" value="CN" />
            <el-option label="海外平台" value="OVERSEAS" />
          </el-select>
        </el-form-item>
        <el-form-item label="服务商名称">
          <el-input v-model="form.service_name" maxlength="128" placeholder="生产平台上显示的公司名" />
        </el-form-item>
        <el-form-item label="固定服务商标识">
          <el-input v-model="form.identifier" maxlength="128" placeholder="生产平台固定的服务商 ID" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button :disabled="saving" @click="dialogOpen = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="submit">登记</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.platform-service-card { margin-top: 18px; }
.card-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; margin-bottom: 10px; }
.card-heading h3 { margin: 0 0 4px; font-size: 15px; }
.sub { margin: 0; color: var(--app-text-muted); font-size: 12px; line-height: 1.6; }
.dialog-sub { margin-bottom: 12px; }
</style>
