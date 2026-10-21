<script setup lang="ts">
import { ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { logout } from "../api/auth";
import { useSessionStore } from "../stores/session";

const route = useRoute();
const router = useRouter();
const session = useSessionStore();
const loggingOut = ref(false);

const menu = [
  { path: "/supplier/home", label: "申报", mark: "▤" },
  { path: "/supplier/models", label: "模型管理", mark: "◇" },
  { path: "/supplier/profile", label: "账户设置", mark: "○" },
];

function isActive(path: string) {
  if (path === "/supplier/home") return route.path === path;
  return route.path === path || route.path.startsWith(`${path}/`);
}

async function signOut() {
  if (loggingOut.value) return;
  loggingOut.value = true;
  try {
    if (session.token) await logout("supplier");
  } catch {
    // 会话已过期也允许清理本地状态。
  } finally {
    session.clear();
    loggingOut.value = false;
    await router.replace("/supplier/login");
  }
}
</script>

<template>
  <div class="supplier-shell">
    <aside class="supplier-sidebar">
      <router-link class="supplier-brand" to="/supplier/home">
        <span class="supplier-brand__mark">T</span>
        <span><b>Token Ops</b><small>BUSINESS CONSOLE</small></span>
      </router-link>

      <div class="supplier-group">业务运营</div>
      <nav class="supplier-nav" aria-label="供应商导航">
        <router-link
          v-for="item in menu"
          :key="item.path"
          :to="item.path"
          class="supplier-nav__item"
          :class="{ 'is-active': isActive(item.path) }"
        >
          <span class="supplier-nav__mark">{{ item.mark }}</span>
          <span>{{ item.label }}</span>
        </router-link>
      </nav>

      <div class="supplier-sidebar__footer">
        <router-link class="supplier-link" to="/internal/login">返回内部登录</router-link>
        <button type="button" class="supplier-logout" :disabled="loggingOut" @click="signOut">退出登录</button>
      </div>
    </aside>

    <div class="supplier-main">
      <header class="supplier-topbar">
        <button type="button" class="supplier-back" @click="router.back()">← 返回上一页</button>
        <span class="supplier-topbar__title">供应商报价系统</span>
      </header>
      <main class="supplier-content">
        <router-view />
      </main>
    </div>
  </div>
</template>

<style scoped>
.supplier-shell { display: flex; min-height: 100vh; background: var(--app-bg); }
.supplier-sidebar { display: flex; flex-direction: column; width: 232px; padding: 22px 16px; border-right: 1px solid var(--app-border); background: var(--app-surface); }
.supplier-brand { display: flex; align-items: center; gap: 10px; padding: 0 8px; color: var(--app-text-primary); text-decoration: none; }
.supplier-brand__mark { display: grid; width: 38px; height: 38px; place-items: center; border-radius: 10px; background: var(--app-hover-bg); color: var(--app-accent); font-size: 21px; font-weight: 800; }
.supplier-brand b { display: block; font-size: 19px; letter-spacing: -.02em; }
.supplier-brand small { display: block; margin-top: 2px; color: var(--app-text-muted); font-size: 9px; letter-spacing: .14em; }
.supplier-group { margin: 30px 8px 10px; color: var(--app-text-muted); font-size: 11px; }
.supplier-nav { display: grid; gap: 4px; }
.supplier-nav__item { display: flex; align-items: center; gap: 11px; min-height: 42px; padding: 0 12px; border-radius: 10px; color: var(--app-text-secondary); font-size: 13px; text-decoration: none; }
.supplier-nav__item:hover { background: var(--app-surface-muted); color: var(--app-text-primary); }
.supplier-nav__item.is-active { background: var(--app-hover-bg); color: var(--app-accent); font-weight: 650; }
.supplier-nav__mark { display: grid; width: 18px; place-items: center; font-size: 14px; }
.supplier-sidebar__footer { display: grid; gap: 8px; margin-top: auto; padding: 14px 8px 4px; border-top: 1px solid var(--app-border); }
.supplier-link { color: var(--app-text-muted); font-size: 12px; text-decoration: none; }
.supplier-link:hover { color: var(--app-accent); }
.supplier-logout { padding: 0; border: 0; background: transparent; color: var(--app-text-muted); cursor: pointer; font-size: 12px; text-align: left; }
.supplier-logout:hover { color: var(--el-color-danger); }
.supplier-main { flex: 1; min-width: 0; }
.supplier-topbar { display: flex; align-items: center; gap: 16px; height: 60px; padding: 0 28px; border-bottom: 1px solid var(--app-border); background: var(--app-surface); }
.supplier-back { padding: 6px 13px; border: 1px solid var(--app-border); border-radius: 8px; background: var(--app-surface); color: var(--app-text-secondary); cursor: pointer; font-size: 12px; }
.supplier-back:hover { border-color: var(--app-accent); color: var(--app-accent); }
.supplier-topbar__title { color: var(--app-text-muted); font-size: 13px; }
.supplier-content { max-width: 1120px; padding: 28px 32px 40px; }
@media (max-width: 760px) { .supplier-sidebar { width: 190px; }.supplier-content { padding: 22px 18px; } }
</style>
