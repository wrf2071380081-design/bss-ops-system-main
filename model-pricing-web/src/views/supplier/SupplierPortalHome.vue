<script setup lang="ts">
import { computed, ref } from "vue";
import { useRouter } from "vue-router";
import { useSessionStore } from "../../stores/session";

const router = useRouter();
const session = useSessionStore();
const activeTab = ref("start");

const supplierName = computed(() => session.user?.supplierName || "供应商");
const regionLabel = computed(() => session.user?.supplierRegion === "OVERSEAS" ? "海外主体" : "国内主体");

function newModel() {
  void router.push({ path: "/supplier/submissions/new", query: { kind: "MODEL_NEW" } });
}

function priceChange() {
  void router.push({ path: "/supplier/submissions/new", query: { kind: "PRICE_CHANGE" } });
}
</script>

<template>
  <section class="supplier-portal-head">
    <div class="eyebrow">TOKEN OPS / PORTAL</div>
    <h1>供应商报价系统</h1>
    <p>{{ supplierName }} · {{ regionLabel }}</p>
  </section>

  <el-tabs v-model="activeTab" class="supplier-tabs">
    <el-tab-pane label="发起申报" name="start">
      <section class="panel supplier-start">
        <div class="supplier-start__head">
          <h2>发起申报</h2>
        </div>
        <div class="supplier-start__body">
          <p>提交模型新增或价格变更，支持分别保存多份草稿。</p>
          <div class="supplier-start__actions">
            <el-button type="primary" @click="newModel">模型新增</el-button>
            <el-button @click="priceChange">价格变更</el-button>
          </div>
        </div>
      </section>
    </el-tab-pane>

    <el-tab-pane label="草稿" name="drafts">
      <section class="panel supplier-empty">
        <el-empty description="暂无草稿" :image-size="72" />
      </section>
    </el-tab-pane>

    <el-tab-pane label="申报记录" name="history">
      <section class="panel supplier-empty">
        <el-empty description="申报记录将在申报模块接入后展示" :image-size="72" />
      </section>
    </el-tab-pane>
  </el-tabs>

  <p class="supplier-footnote">Token Ops · 供应商申报门户</p>
</template>

<style scoped>
.supplier-portal-head { margin-bottom: 18px; }
.supplier-portal-head .eyebrow { color: var(--app-text-muted); font-size: 11px; letter-spacing: .16em; }
.supplier-portal-head h1 { margin: 8px 0 6px; font-size: 32px; letter-spacing: -.02em; }
.supplier-portal-head p { margin: 0; color: var(--app-text-muted); font-size: 13px; }
.supplier-tabs :deep(.el-tabs__header) { margin-bottom: 16px; }
.supplier-start { padding: 0; overflow: hidden; }
.supplier-start__head { padding: 18px 22px; border-bottom: 1px solid var(--app-border); }
.supplier-start__head h2 { margin: 0; font-size: 17px; }
.supplier-start__body { padding: 20px 22px 22px; }
.supplier-start__body p { margin: 0 0 16px; color: var(--app-text-muted); font-size: 13px; }
.supplier-start__actions { display: flex; gap: 10px; }
.supplier-empty { padding: 12px; }
.supplier-footnote { margin: 26px 0 0; color: var(--app-text-muted); font-size: 11px; }
</style>
