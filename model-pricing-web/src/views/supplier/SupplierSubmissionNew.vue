<script setup lang="ts">
import { computed } from "vue";
import { useRoute, useRouter } from "vue-router";

const route = useRoute();
const router = useRouter();
const kind = computed(() => route.query.kind === "PRICE_CHANGE" ? "价格变更" : "模型新增");
</script>

<template>
  <section class="supplier-portal-head">
    <div class="eyebrow">SUPPLIER / SUBMISSION</div>
    <h1>发起申报 · {{ kind }}</h1>
    <p>提交{{ kind }}申报，保存后可继续编辑或提交审批。</p>
  </section>

  <section class="panel submission-empty">
    <el-empty :description="`${kind}申报表单建设中，后续接入标准模型选择、测试与办理流程`" :image-size="80" />
    <el-button @click="router.push('/supplier/home')">返回申报首页</el-button>
  </section>
</template>

<style scoped>
.supplier-portal-head { margin-bottom: 18px; }
.supplier-portal-head .eyebrow { color: var(--app-text-muted); font-size: 11px; letter-spacing: .16em; }
.supplier-portal-head h1 { margin: 8px 0 6px; font-size: 32px; letter-spacing: -.02em; }
.supplier-portal-head p { margin: 0; color: var(--app-text-muted); font-size: 13px; }
.submission-empty { display: grid; justify-items: center; gap: 10px; padding: 26px; }
</style>
