<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ElMessage } from "element-plus";
import { login, loginSupplierMobile, registerSupplier } from "../api/auth";
import { ApiError } from "../domain/common";
import { useSessionStore, type PortalType } from "../stores/session";

const route = useRoute();
const router = useRouter();
const session = useSessionStore();

const portal = computed(() => route.meta.portal as PortalType);
const isInternal = computed(() => portal.value === "internal");
const homes: Record<PortalType, string> = {
  internal: "/internal/workbench",
  supplier: "/supplier/home",
  customer: "/internal/login",
};
const roles: Array<{ portal: PortalType; label: string; hint: string; mark: string }> = [
  { portal: "internal", label: "内部用户", hint: "员工账号", mark: "内" },
  { portal: "supplier", label: "供应商", hint: "手机号登录", mark: "供" },
];

const account = ref("");
const password = ref("");
const busy = ref(false);
const error = ref("");

// 供应商登录/注册
const supplierTab = ref<"login" | "register">("login");
const mobile = ref("");
const code = ref("1234");
const companyName = ref("");
const region = ref<"CN" | "OVERSEAS">("CN");
const creditCode = ref("");
const contact = ref("");
const licenseName = ref("");
const agreement = ref(false);

const demoSuppliers = [
  { name: "云桥算力", phone: "13800000001", region: "国内" },
  { name: "远帆智能", phone: "13800000002", region: "海外" },
  { name: "青禾云", phone: "13800000003", region: "国内" },
  { name: "星河算力", phone: "13800000004", region: "海外" },
];

watch(portal, () => {
  password.value = "";
  error.value = "";
  supplierTab.value = "login";
});

function sendCode() {
  ElMessage.info("演示阶段验证码固定为 1234");
}

function fillSupplier(item: { name: string; phone: string }) {
  mobile.value = item.phone;
  code.value = "1234";
  error.value = "";
}

function onLicense(event: Event) {
  const input = event.target as HTMLInputElement;
  licenseName.value = input.files?.[0]?.name || "";
}

function errorText(cause: unknown) {
  if (cause instanceof ApiError) {
    if (cause.httpStatus === 401) return isInternal.value ? "账号或密码不正确。" : "手机号或验证码不正确。";
    if (cause.httpStatus === 409) return "该手机号或企业已注册。";
    return cause.message;
  }
  return "登录响应不符合约定，请联系管理员。";
}

async function finish(result: Parameters<typeof session.startSession>[0], displayName: string) {
  const target = portal.value;
  session.startSession(result, target, displayName);
  const redirect = route.query.redirect;
  const destination = typeof redirect === "string" && redirect.startsWith(`/${target}/`)
    ? router.resolve(redirect) : null;
  await router.replace(destination?.meta.requiresAuth && destination.meta.portal === target
    ? destination.fullPath : homes[target]);
}

async function submitInternal() {
  if (busy.value) return;
  error.value = "";
  if (!account.value.trim() || !password.value) { error.value = "请输入账号和密码。"; return; }
  busy.value = true;
  try {
    const loginID = account.value.trim();
    const result = await login("internal", loginID, password.value);
    await finish(result, loginID);
  } catch (cause) {
    error.value = errorText(cause);
  } finally {
    password.value = "";
    busy.value = false;
  }
}

async function submitSupplierLogin() {
  if (busy.value) return;
  error.value = "";
  if (!mobile.value.trim() || !code.value.trim()) { error.value = "请输入手机号和验证码。"; return; }
  busy.value = true;
  try {
    const result = await loginSupplierMobile(mobile.value.trim(), code.value.trim());
    await finish(result, mobile.value.trim());
  } catch (cause) {
    error.value = errorText(cause);
  } finally {
    busy.value = false;
  }
}

async function submitSupplierRegister() {
  if (busy.value) return;
  error.value = "";
  if (!mobile.value.trim() || !code.value.trim() || !companyName.value.trim()) {
    error.value = "请填写手机号、验证码和企业名称。"; return;
  }
  if (!agreement.value) { error.value = "请先勾选确认规则。"; return; }
  busy.value = true;
  try {
    const result = await registerSupplier({
      mobile: mobile.value.trim(),
      code: code.value.trim(),
      company_name: companyName.value.trim(),
      region: region.value,
      credit_code: creditCode.value.trim(),
      contact: contact.value.trim(),
      license_name: licenseName.value,
      agreement: agreement.value,
    });
    await finish(result, companyName.value.trim());
  } catch (cause) {
    error.value = errorText(cause);
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <main class="login-page">
    <section class="login-card">
      <div class="login-brand">
        <span class="login-brand__mark">M</span>
        <span>模型与定价中心<small>MODEL OPERATIONS</small></span>
      </div>

      <header class="login-head">
        <h1>{{ isInternal ? "用户登录" : "供应商登录" }}</h1>
        <p>{{ isInternal ? "使用员工账号进入内部工作台。" : "供应商登录 / 注册；演示阶段验证码固定为 1234。" }}</p>
      </header>

      <nav class="login-roles" aria-label="选择登录身份">
        <router-link
          v-for="role in roles"
          :key="role.portal"
          class="login-role"
          :class="{ 'is-active': portal === role.portal }"
          :aria-current="portal === role.portal ? 'page' : undefined"
          :to="`/${role.portal}/login`"
        >
          <span class="login-role__mark">{{ role.mark }}</span>
          <span class="login-role__text"><strong>{{ role.label }}</strong><small>{{ role.hint }}</small></span>
        </router-link>
      </nav>

      <form v-if="isInternal" class="login-form" @submit.prevent="submitInternal">
        <label for="login-account">账号</label>
        <el-input id="login-account" v-model="account" autocomplete="username" placeholder="请输入账号" :disabled="busy" />
        <label for="login-password">密码</label>
        <el-input id="login-password" v-model="password" type="password" autocomplete="current-password" placeholder="请输入密码" :disabled="busy" />
        <p v-if="error" role="alert" class="login-error">{{ error }}</p>
        <el-button type="primary" native-type="submit" :loading="busy">登录</el-button>
      </form>

      <div v-else class="supplier-panel">
        <el-tabs v-model="supplierTab">
          <el-tab-pane label="登录" name="login">
            <form class="login-form" @submit.prevent="submitSupplierLogin">
              <label for="supplier-mobile">手机号</label>
              <el-input id="supplier-mobile" v-model="mobile" placeholder="请输入手机号" :disabled="busy" />
              <label for="supplier-code">验证码</label>
              <div class="code-row">
                <el-input id="supplier-code" v-model="code" placeholder="演示验证码 1234" :disabled="busy" />
                <el-button :disabled="busy" @click="sendCode">获取验证码</el-button>
              </div>
              <p class="login-hint">演示阶段不发送短信，验证码固定为 <b>1234</b>。</p>
              <p v-if="error" role="alert" class="login-error">{{ error }}</p>
              <el-button type="primary" native-type="submit" :loading="busy">登录</el-button>
            </form>

            <div class="demo-accounts">
              <span>演示账号（点击填入）：</span>
              <button v-for="item in demoSuppliers" :key="item.phone" type="button" @click="fillSupplier(item)">
                {{ item.name }} · {{ item.region }} · {{ item.phone }}
              </button>
            </div>
          </el-tab-pane>

          <el-tab-pane label="注册" name="register">
            <form class="login-form" @submit.prevent="submitSupplierRegister">
              <div class="form-grid">
                <div>
                  <label for="reg-mobile">手机号</label>
                  <el-input id="reg-mobile" v-model="mobile" placeholder="请输入手机号" :disabled="busy" />
                </div>
                <div>
                  <label for="reg-code">验证码</label>
                  <div class="code-row">
                    <el-input id="reg-code" v-model="code" placeholder="1234" :disabled="busy" />
                    <el-button :disabled="busy" @click="sendCode">获取验证码</el-button>
                  </div>
                </div>
                <div>
                  <label for="reg-company">企业名称</label>
                  <el-input id="reg-company" v-model="companyName" placeholder="请输入企业名称" :disabled="busy" />
                </div>
                <div>
                  <label for="reg-region">主体属性 / 部署节点</label>
                  <el-select id="reg-region" v-model="region" style="width: 100%" :disabled="busy">
                    <el-option value="CN" label="国内" />
                    <el-option value="OVERSEAS" label="海外" />
                  </el-select>
                </div>
                <div>
                  <label for="reg-credit">统一社会信用代码</label>
                  <el-input id="reg-credit" v-model="creditCode" placeholder="请输入统一社会信用代码" :disabled="busy" />
                </div>
                <div>
                  <label for="reg-contact">对接负责人</label>
                  <el-input id="reg-contact" v-model="contact" placeholder="请输入对接负责人" :disabled="busy" />
                </div>
              </div>

              <label>营业执照</label>
              <input class="login-file" type="file" accept="image/*,.pdf" :disabled="busy" @change="onLicense" />
              <p class="login-hint">演示阶段仅记录文件名，不上传文件。</p>

              <el-checkbox v-model="agreement" :disabled="busy">
                我已知悉并确认主体、部署节点、签约主体及上架平台的对应规则
              </el-checkbox>

              <p v-if="error" role="alert" class="login-error">{{ error }}</p>
              <el-button type="primary" native-type="submit" :loading="busy">注册并进入</el-button>
            </form>
          </el-tab-pane>
        </el-tabs>
      </div>
    </section>
  </main>
</template>

<style scoped>
.login-page { min-height: 100vh; display: grid; place-items: center; padding: 32px; background: radial-gradient(circle at 50% 0%, var(--app-hover-bg), transparent 58%), var(--app-bg); }
.login-card { width: min(100%, 620px); padding: 38px 40px 34px; border: 1px solid var(--app-border); border-radius: 22px; background: var(--app-surface); box-shadow: 0 26px 80px rgba(22, 35, 70, .10); }
.login-brand { display: flex; align-items: center; gap: 11px; color: var(--app-text-secondary); font-size: 13px; font-weight: 700; }
.login-brand__mark { display: grid; width: 36px; height: 36px; place-items: center; border-radius: 10px; background: var(--app-accent); color: var(--app-accent-contrast); font-size: 20px; }
.login-brand small { display: block; margin-top: 2px; color: var(--app-text-muted); font-size: 9px; font-weight: 500; letter-spacing: .12em; }
.login-head { margin: 24px 0 22px; }
.login-head h1 { margin: 0 0 8px; font-size: 30px; letter-spacing: -.02em; }
.login-head p { margin: 0; color: var(--app-text-muted); font-size: 13px; line-height: 1.6; }
.login-roles { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; margin-bottom: 24px; }
.login-role { display: flex; align-items: center; gap: 9px; min-height: 72px; padding: 12px; border: 1px solid var(--app-border); border-radius: 13px; background: var(--app-surface-muted); color: var(--app-text-primary); text-decoration: none; transition: border-color .18s, background-color .18s, transform .18s; }
.login-role:hover { border-color: var(--app-accent); transform: translateY(-2px); }
.login-role:focus-visible { outline: 2px solid var(--app-accent); outline-offset: 2px; }
.login-role.is-active { border-color: var(--app-accent); background: var(--app-hover-bg); box-shadow: inset 0 0 0 1px var(--app-accent); }
.login-role__mark { display: grid; flex: 0 0 32px; height: 32px; place-items: center; border-radius: 9px; background: var(--app-surface); color: var(--app-accent); font-size: 13px; font-weight: 700; }
.login-role__text { display: grid; gap: 3px; min-width: 0; }
.login-role__text strong { font-size: 13px; }
.login-role__text small { color: var(--app-text-muted); font-size: 11px; }
.login-form { display: grid; gap: 9px; }
.login-form label { margin-top: 7px; font-size: 13px; font-weight: 650; }
.login-form > .el-button { width: 100%; margin-top: 14px; }
.login-hint { margin: 2px 0 0; color: var(--app-text-muted); font-size: 11px; }
.login-hint b { color: var(--app-accent); }
.login-error { margin: 4px 0 0; color: var(--el-color-danger); font-size: 12px; }
.code-row { display: flex; gap: 8px; }
.code-row .el-input { flex: 1; }
.supplier-panel { margin-top: 2px; }
.form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 11px 14px; }
.form-grid > div { display: grid; gap: 7px; }
.login-file { width: 100%; padding: 8px 10px; border: 1px solid var(--app-border); border-radius: 8px; background: var(--app-surface-muted); color: var(--app-text-secondary); font-size: 12px; }
.demo-accounts { display: flex; flex-wrap: wrap; gap: 7px; margin-top: 16px; padding-top: 13px; border-top: 1px dashed var(--app-border); color: var(--app-text-muted); font-size: 11px; }
.demo-accounts button { padding: 4px 9px; border: 1px solid var(--app-border); border-radius: 999px; background: var(--app-surface); color: var(--app-text-secondary); cursor: pointer; font-size: 11px; }
.demo-accounts button:hover { border-color: var(--app-accent); color: var(--app-accent); }
@media (max-width: 560px) { .login-page { padding: 16px; }.login-card { padding: 26px 20px; }.login-roles { grid-template-columns: 1fr; }.form-grid { grid-template-columns: 1fr; } }
</style>
