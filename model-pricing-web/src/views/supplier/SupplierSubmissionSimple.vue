<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ElMessage } from "element-plus";
import { supplyChainApi, type SupplierModelOption } from "../../api/supplyChain";

const route = useRoute();
const router = useRouter();
const kind = computed(() => route.query.kind === "PRICE_CHANGE" ? "价格变更" : "模型新增");
const models = ref<SupplierModelOption[]>([]);
const modelID = ref("");
const supplierModel = ref("");
const coefficient = ref("0.6000");
const loading = ref(false);
const submitting = ref(false);
const error = ref("");

async function loadModels() {
  loading.value = true;
  error.value = "";
  try {
    models.value = await supplyChainApi.standardModels();
  } catch (value) {
    error.value = value instanceof Error ? value.message : "标准模型目录加载失败。";
  } finally {
    loading.value = false;
  }
}

function selectModel(id: string) {
  const item = models.value.find(model => model.id === id);
  if (item) supplierModel.value = item.official_id;
}

async function submit() {
  if (submitting.value) return;
  error.value = "";
  if (!modelID.value) { error.value = "请选择标准模型。"; return; }
  if (!supplierModel.value.trim()) { error.value = "请填写接口模型 ID。"; return; }
  const amount = Number(coefficient.value);
  if (!Number.isFinite(amount) || amount <= 0 || amount > 1) { error.value = "采购系数须大于 0 且不超过 1。"; return; }
  submitting.value = true;
  try {
    const id = await supplyChainApi.createSubmission({
      model_id: Number(modelID.value),
      supplier_model_name: supplierModel.value.trim(),
      coefficient: coefficient.value.trim(),
    });
    ElMessage.success(`申报 #${id} 已提交，等待领导审批`);
    await router.push("/supplier/home");
  } catch (value) {
    error.value = value instanceof Error ? value.message : "申报提交失败，请重试。";
  } finally {
    submitting.value = false;
  }
}

onMounted(loadModels);
</script>

<template>
  <section class="supplier-portal-head">
    <div class="eyebrow">SUPPLIER / SUBMISSION</div>
    <h1>发起申报 · {{ kind }}</h1>
    <p>提交后进入领导审批；审批通过后由办理人完成 mock 测试与登记接入。</p>
  </section>

  <section class="panel submission-panel" v-loading="loading">
    <el-alert v-if="kind === '价格变更'" title="价格变更暂未接入本页，先走模型新增链路。" type="warning" :closable="false" />
    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" />

    <el-form label-position="top" @submit.prevent="submit">
      <el-form-item label="对应标准模型 *">
        <el-select v-model="modelID" filterable clearable placeholder="选择标准模型" style="width: 100%" @change="selectModel">
          <el-option v-for="item in models" :key="item.id" :label="`${item.company} · ${item.name}（${item.official_id}）`" :value="item.id" />
        </el-select>
      </el-form-item>
      <el-form-item label="接口模型 ID *">
        <el-input v-model="supplierModel" placeholder="供应商侧接口模型 ID" />
      </el-form-item>
      <el-form-item label="采购系数 *">
        <el-input v-model="coefficient" inputmode="decimal" placeholder="例如 0.6000" />
      </el-form-item>
      <el-alert title="测试地址 / Key 在办理阶段由办理人 mock，不在本页填写。" type="info" :closable="false" />
      <div class="form-actions">
        <el-button @click="router.push('/supplier/home')">返回</el-button>
        <el-button type="primary" :loading="submitting" @click="submit">提交申报</el-button>
      </div>
    </el-form>
  </section>
</template>

<style scoped>
.supplier-portal-head { margin-bottom: 18px; }
.supplier-portal-head .eyebrow { color: var(--app-text-muted); font-size: 11px; letter-spacing: .16em; }
.supplier-portal-head h1 { margin: 8px 0 6px; font-size: 32px; letter-spacing: -.02em; }
.supplier-portal-head p { margin: 0; color: var(--app-text-muted); font-size: 13px; }
.submission-panel { display: grid; gap: 16px; padding: 24px; max-width: 720px; }
.form-actions { display: flex; justify-content: flex-end; gap: 10px; }
</style>
