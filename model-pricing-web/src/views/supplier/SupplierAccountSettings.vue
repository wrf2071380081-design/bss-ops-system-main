<script setup lang="ts">
import { computed } from "vue";
import { useSessionStore } from "../../stores/session";

const session = useSessionStore();
const supplierName = computed(() => session.user?.supplierName || "—");
const regionLabel = computed(() => session.user?.supplierRegion === "OVERSEAS" ? "海外主体" : "国内主体");
const loginPhone = computed(() => session.user?.name || "—");
</script>

<template>
  <section class="supplier-portal-head">
    <div class="eyebrow">SUPPLIER / ACCOUNT</div>
    <h1>账户设置</h1>
    <p>查看本企业的主体信息与登录账号。</p>
  </section>

  <section class="panel account-panel">
    <el-descriptions :column="1" border>
      <el-descriptions-item label="企业名称">{{ supplierName }}</el-descriptions-item>
      <el-descriptions-item label="主体属性">{{ regionLabel }}</el-descriptions-item>
      <el-descriptions-item label="登录手机号">{{ loginPhone }}</el-descriptions-item>
      <el-descriptions-item label="账号状态">正常</el-descriptions-item>
    </el-descriptions>
    <p class="account-note">企业资料编辑、资质与联系人维护将由供应商档案模块接入后开放。</p>
  </section>
</template>

<style scoped>
.supplier-portal-head { margin-bottom: 18px; }
.supplier-portal-head .eyebrow { color: var(--app-text-muted); font-size: 11px; letter-spacing: .16em; }
.supplier-portal-head h1 { margin: 8px 0 6px; font-size: 32px; letter-spacing: -.02em; }
.supplier-portal-head p { margin: 0; color: var(--app-text-muted); font-size: 13px; }
.account-panel { padding: 24px; max-width: 720px; }
.account-note { margin: 18px 0 0; color: var(--app-text-muted); font-size: 12px; }
</style>
