<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from "vue";
import { ElMessage } from "element-plus";
import { quoteCandidatesApi, type AffectedCustomerResult } from "../../../api/quoteCandidates";
import { formatDateTime } from "../../../domain/date";

// 受影响客户：这条供应资源已经卖给了谁（改价/上下架前的影响面）。
// 数据域过滤在服务端按客户归属收口，这里只展示返回结果。
const props = defineProps<{ resourceId: string }>();
const data = ref<AffectedCustomerResult>(), loading = ref(false), error = ref("");
let controller: AbortController | undefined;

async function load() {
  controller?.abort();
  const request = controller = new AbortController();
  loading.value = true; error.value = "";
  try {
    data.value = await quoteCandidatesApi.affectedCustomers(Number(props.resourceId));
  } catch (value) {
    if (request.signal.aborted) return;
    data.value = undefined;
    error.value = value instanceof Error ? value.message : "受影响客户加载失败。";
  } finally {
    if (!request.signal.aborted) loading.value = false;
  }
}

function quoteStateText(status: string) {
  return ({ EFFECTIVE: "生效", DRAFT: "草稿", PENDING: "待审批", APPROVED: "已批准", EXPIRED: "已过期", REJECTED: "已驳回", FORMAL: "正式" } as Record<string, string>)[status] || status;
}
function quoteTypeText(type: string) {
  return ({ APPLY: "套用价目表", CLONE: "克隆历史", TEMP: "临时报价", RESOURCE: "按资源报价", SPECIAL: "特价", CONTRACT: "合同" } as Record<string, string>)[type] || type;
}

watch(() => props.resourceId, () => { void load(); }, { immediate: true });
onBeforeUnmount(() => { controller?.abort(); });
</script>

<template>
  <section class="affected-panel">
    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false">
      <el-button text @click="load">重新加载</el-button>
    </el-alert>
    <el-table v-else v-loading="loading" :data="data?.list ?? []" row-key="customer_id" size="small">
      <el-table-column label="客户" min-width="180">
        <template #default="{ row }">
          <div>{{ row.customer_name }}</div>
          <div class="muted">{{ row.level_code }} · 归属销售 ID {{ row.owner_sales_operator_id }}</div>
        </template>
      </el-table-column>
      <el-table-column label="关联报价单" min-width="240">
        <template #default="{ row }">
          <div v-for="quote in row.quotes" :key="quote.quote_id" class="quote-line">
            <span>#{{ quote.quote_id }} · V{{ quote.version_no }}</span>
            <el-tag size="small" :type="quote.status === 'EFFECTIVE' ? 'success' : 'info'">{{ quoteStateText(quote.status) }}</el-tag>
            <span class="muted">{{ quoteTypeText(quote.quote_type) }}</span>
            <span class="mono">{{ quote.unit_price }} {{ quote.currency }}</span>
            <span class="muted">{{ formatDateTime(quote.valid_until, "无有效期", false) }}</span>
          </div>
          <span v-if="!row.quotes.length" class="muted">—</span>
        </template>
      </el-table-column>
      <template #empty><el-empty description="这条资源还没有生效的报价单" :image-size="48" /></template>
    </el-table>
    <p class="muted">口径：只统计已给客户、不是草稿的报价单；销售按数据域只看自己负责的客户。</p>
  </section>
</template>

<style scoped>
.affected-panel { display: grid; gap: 12px; }
.quote-line { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; font-size: 13px; padding: 2px 0; }
.muted { color: var(--app-text-muted); font-size: 12px; }
</style>
