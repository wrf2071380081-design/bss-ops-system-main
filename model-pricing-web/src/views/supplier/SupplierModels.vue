<template>
  <section class="supplier-portal-head">
    <div class="eyebrow">SUPPLIER / MODELS</div>
    <h1>模型管理</h1>
    <p>查看本企业已申报的模型、采购系数与接入状态。</p>
  </section>

  <section class="panel supplier-empty">
    <el-empty description="模型管理将在申报 / 资源模块接入后展示" :image-size="72" />
  </section>
</template>

<style scoped>
.supplier-portal-head { margin-bottom: 18px; }
.supplier-portal-head .eyebrow { color: var(--app-text-muted); font-size: 11px; letter-spacing: .16em; }
.supplier-portal-head h1 { margin: 8px 0 6px; font-size: 32px; letter-spacing: -.02em; }
.supplier-portal-head p { margin: 0; color: var(--app-text-muted); font-size: 13px; }
.supplier-empty { padding: 12px; }
</style>
