<script setup lang="ts">
import { formatDateTime } from "../../domain/date";
import { onMounted, ref } from "vue";
import { supplierAccountApi } from "../../api/supplierAccount";
import type { SupplierPortalProfileDTO } from "../../api/supplierAccount.types";
import { ApiError } from "../../domain/common";
import { QUALIFICATION_STATUS, SUPPLIER_STATUS } from "../../domain/status";

const profile = ref<SupplierPortalProfileDTO>();
const loading = ref(false);
const error = ref("");

const errorMessage = (value: unknown) => value instanceof ApiError ? value.message : "操作失败，请稍后重试。";
const formatTime = (value?: string | null) => formatDateTime(value, "—", false);

async function load() {
  loading.value = true; error.value = "";
  try { profile.value = await supplierAccountApi.getProfile(); }
  catch (value) { error.value = errorMessage(value); }
  finally { loading.value = false; }
}

onMounted(load);
</script>

<template>
  <section class="page-heading"><div><div class="eyebrow">SUPPLIER PROFILE</div><h1>主体档案</h1><p>查看供应商主体、商务与联系信息（只读）。</p></div></section>
  <el-alert v-if="error" :title="error" type="error" show-icon :closable="false"><el-button text @click="load">重新加载</el-button></el-alert>
  <section v-loading="loading" class="panel profile-panel">
    <template v-if="profile">
      <div class="profile-head"><div><div class="eyebrow">主体 #{{ profile.id }}</div><h2>{{ profile.legalName }}</h2></div><div class="profile-tags"><el-tag :type="SUPPLIER_STATUS[profile.status].type">{{ SUPPLIER_STATUS[profile.status].label }}</el-tag><el-tag :type="QUALIFICATION_STATUS[profile.qualificationStatus].type">资质{{ QUALIFICATION_STATUS[profile.qualificationStatus].label }}</el-tag></div></div>
      <el-alert title="本页当前为只读视图。联系资料维护（PUT /api/supplier/profile）待后端可写字段落库后开放；法定主体、登记信息与结算条件不在本页修改。" type="info" show-icon :closable="false" />
      <h3>主体与结算</h3>
      <el-descriptions :column="2" border><el-descriptions-item label="统一登记信息">{{ profile.registrationNoMasked }}</el-descriptions-item><el-descriptions-item label="结算币种">{{ profile.settlementCurrency }}</el-descriptions-item><el-descriptions-item label="结算条件">{{ profile.paymentTerms }}</el-descriptions-item></el-descriptions>
      <h3>联系</h3>
      <el-descriptions :column="2" border><el-descriptions-item label="联系人">{{ profile.contactName }}</el-descriptions-item><el-descriptions-item label="手机">{{ profile.contactPhoneMasked }}</el-descriptions-item><el-descriptions-item label="最后更新" :span="2">{{ formatTime(profile.updatedAt) }}</el-descriptions-item></el-descriptions>
    </template>
  </section>
</template>

<style scoped>
.profile-panel { min-height: 360px; padding: 22px; }.profile-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 20px; margin-bottom: 18px; }.profile-head h2 { margin: 8px 0 4px; }.profile-tags { display: flex; gap: 8px; }.profile-panel h3 { margin-top: 24px; }.profile-form { margin-top: 10px; }.profile-form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 0 14px; }@media (max-width: 760px) { .profile-head { flex-direction: column; }.profile-form-grid { grid-template-columns: 1fr; } }
</style>
