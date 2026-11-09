<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import {
  orgPermissionsApi,
  type OrganizationNodeDTO,
  type RoleSummaryDTO,
  type StaffSummaryDTO,
} from "../../../api/orgPermissions";
import { ApiError } from "../../../domain/common";

// 后端 RoleSummaryDTO 目前不返回 role.code（见 docs/api/12-org-permissions.md §5），
// 所以「超管 / 领导 / 员工」只能按角色名排到前面。后端补 code 后只改 ROLE_PRIORITY 这一处。
const ROLE_PRIORITY = ["平台管理员", "领导", "员工"];
// 与后端 internal/domain/orgperm/staff.go 的 loginIDRe / mobileRe 保持一致，前置拦截减少 400。
const LOGIN_ID_RE = /^[a-z][a-z0-9_]{3,31}$/;
const MOBILE_RE = /^1[3-9]\d{9}$/;

const rows = ref<StaffSummaryDTO[]>([]);
const total = ref(0);
const page = ref(1);
const size = ref(20);
const keyword = ref("");
const loading = ref(false);
const pageError = ref("");

const organizations = ref<OrganizationNodeDTO[]>([]);
const roles = ref<RoleSummaryDTO[]>([]);
const metaError = ref("");

const message = (value: unknown) =>
  value instanceof ApiError
    ? value.message
    : value instanceof Error
      ? value.message
      : "操作失败，请稍后重试。";

const statusLabel = (status: string) => (status === "ACTIVE" ? "在职" : status === "DISABLED" ? "已停用" : status);
const statusType = (status: string) => (status === "ACTIVE" ? "success" : "info");

// ---- 组织树扁平化成下拉选项（后端 org_unit.id 是 bigint，DTO 里转成了字符串）
interface OrgOption {
  id: number;
  label: string;
}
const orgOptions = computed<OrgOption[]>(() => {
  const out: OrgOption[] = [];
  const walk = (nodes: OrganizationNodeDTO[], depth: number) => {
    for (const node of nodes) {
      const numericId = Number(node.id);
      if (Number.isFinite(numericId)) out.push({ id: numericId, label: `${"　".repeat(depth)}${node.name}` });
      if (node.children?.length) walk(node.children, depth + 1);
    }
  };
  walk(organizations.value, 0);
  return out;
});

// 只放内部角色：SUPPLIER / CUSTOMER 在这里授予一定 400（owner 是 subject_operator，走各自入驻流程）。
const assignableRoles = computed(() =>
  roles.value
    .filter((role) => role.portal === "internal")
    .slice()
    .sort((a, b) => {
      const rank = (name: string) => {
        const index = ROLE_PRIORITY.indexOf(name);
        return index === -1 ? ROLE_PRIORITY.length : index;
      };
      return rank(a.name) - rank(b.name) || a.name.localeCompare(b.name, "zh-Hans-CN");
    }),
);

async function loadMeta() {
  metaError.value = "";
  try {
    const [orgRows, roleRows] = await Promise.all([orgPermissionsApi.getOrganizations(), orgPermissionsApi.getRoles()]);
    organizations.value = orgRows;
    roles.value = roleRows;
  } catch (value) {
    metaError.value = message(value);
  }
}

async function load(targetPage = page.value) {
  loading.value = true;
  pageError.value = "";
  try {
    const result = await orgPermissionsApi.listStaff({
      keyword: keyword.value.trim() || undefined,
      page: targetPage,
      size: size.value,
    });
    rows.value = result.list;
    total.value = result.total;
    page.value = result.page || targetPage;
  } catch (value) {
    rows.value = [];
    total.value = 0;
    pageError.value = message(value);
  } finally {
    loading.value = false;
  }
}

function search() {
  page.value = 1;
  void load(1);
}

function changePage(value: number) {
  void load(value);
}

function resetSearch() {
  keyword.value = "";
  search();
}

onMounted(() => {
  void loadMeta();
  void load(1);
});

// ============================================================
// 新建 / 编辑资料
// ============================================================
const formOpen = ref(false);
const formMode = ref<"create" | "edit">("create");
const formSubmitting = ref(false);
const formError = ref("");
const editingId = ref("");

const form = reactive({
  name: "",
  mobile: "",
  orgUnitId: null as number | null,
  loginId: "",
  password: "",
  roleIds: [] as string[],
  // 编辑资料时必须原样回传，否则有被清空的风险。
  status: "ACTIVE",
});

function resetForm() {
  form.name = "";
  form.mobile = "";
  form.orgUnitId = orgOptions.value[0]?.id ?? null;
  form.loginId = "";
  form.password = "";
  form.roleIds = [];
  form.status = "ACTIVE";
  formError.value = "";
}

function openCreate() {
  formMode.value = "create";
  editingId.value = "";
  resetForm();
  formOpen.value = true;
}

function openEdit(row: StaffSummaryDTO) {
  formMode.value = "edit";
  editingId.value = row.id;
  resetForm();
  form.name = row.name;
  form.mobile = row.mobile;
  form.orgUnitId = Number.isFinite(Number(row.orgUnitId)) ? Number(row.orgUnitId) : (orgOptions.value[0]?.id ?? null);
  form.loginId = row.account?.loginId || "";
  form.roleIds = row.roles.map((role) => role.id);
  form.status = row.status;
  formOpen.value = true;
}

function validate(): string {
  if (!form.name.trim()) return "请填写姓名。";
  if (!MOBILE_RE.test(form.mobile.trim())) return "手机号须为 11 位、1 开头且第二位为 3-9。";
  if (form.orgUnitId === null) return "请选择所属组织。";
  if (formMode.value === "create") {
    if (!LOGIN_ID_RE.test(form.loginId.trim()))
      return "登录名须 4-32 位，只含小写字母 / 数字 / 下划线，且以小写字母开头。";
    if (form.password.length < 8 || form.password.length > 64) return "初始口令长度须为 8-64 个字符。";
    if (!form.roleIds.length) return "请至少选择一个角色。";
  }
  return "";
}

async function submitForm() {
  const invalid = validate();
  if (invalid) {
    formError.value = invalid;
    return;
  }
  formSubmitting.value = true;
  formError.value = "";
  try {
    if (formMode.value === "create") {
      await orgPermissionsApi.createStaff({
        name: form.name.trim(),
        mobile: form.mobile.trim(),
        org_unit_id: form.orgUnitId as number,
        login_id: form.loginId.trim(),
        password: form.password,
        role_ids: form.roleIds.map(Number),
      });
      ElMessage.success("账号已创建，可让对方用该登录名与初始口令登录。");
    } else {
      await orgPermissionsApi.updateStaff(editingId.value, {
        name: form.name.trim(),
        mobile: form.mobile.trim(),
        org_unit_id: form.orgUnitId as number,
        status: form.status,
      });
      ElMessage.success("资料已更新。");
    }
    formOpen.value = false;
    await load();
  } catch (value) {
    formError.value = message(value);
  } finally {
    formSubmitting.value = false;
  }
}

// ============================================================
// 重置口令
// ============================================================
const passwordOpen = ref(false);
const passwordSubmitting = ref(false);
const passwordError = ref("");
const passwordTarget = ref<StaffSummaryDTO>();
const passwordForm = reactive({ password: "", confirm: "" });

function openPassword(row: StaffSummaryDTO) {
  passwordTarget.value = row;
  passwordForm.password = "";
  passwordForm.confirm = "";
  passwordError.value = "";
  passwordOpen.value = true;
}

async function submitPassword() {
  if (!passwordTarget.value) return;
  if (passwordForm.password.length < 8 || passwordForm.password.length > 64) {
    passwordError.value = "新口令长度须为 8-64 个字符。";
    return;
  }
  if (passwordForm.password !== passwordForm.confirm) {
    passwordError.value = "两次输入的口令不一致。";
    return;
  }
  passwordSubmitting.value = true;
  passwordError.value = "";
  try {
    await orgPermissionsApi.resetStaffPassword(passwordTarget.value.id, passwordForm.password);
    ElMessage.success("口令已重置，请通知本人尽快登录并修改。");
    passwordOpen.value = false;
    await load();
  } catch (value) {
    passwordError.value = message(value);
  } finally {
    passwordSubmitting.value = false;
  }
}

// ============================================================
// 角色管理
// ============================================================
const roleOpen = ref(false);
const roleLoading = ref(false);
const roleError = ref("");
const roleTargetId = ref("");
const roleTargetName = ref("");
const grantedRoles = ref<StaffSummaryDTO["roles"]>([]);
const grantIds = ref<string[]>([]);

async function openRoles(row: StaffSummaryDTO) {
  roleOpen.value = true;
  roleTargetId.value = row.id;
  roleTargetName.value = row.name;
  grantedRoles.value = row.roles;
  grantIds.value = [];
  roleError.value = "";
  await refreshRoles();
}

async function refreshRoles() {
  roleLoading.value = true;
  roleError.value = "";
  try {
    const detail = await orgPermissionsApi.getStaff(roleTargetId.value);
    grantedRoles.value = detail.roles;
    // 列表行同步，避免关闭抽屉后仍显示旧角色。
    const index = rows.value.findIndex((item) => item.id === detail.id);
    if (index >= 0) rows.value[index] = detail;
  } catch (value) {
    roleError.value = message(value);
  } finally {
    roleLoading.value = false;
  }
}

const grantCandidates = computed(() => {
  const held = new Set(grantedRoles.value.map((role) => role.id));
  return assignableRoles.value.filter((role) => !held.has(role.id));
});

async function submitGrant() {
  if (!grantIds.value.length) {
    roleError.value = "请选择要授予的角色。";
    return;
  }
  roleLoading.value = true;
  roleError.value = "";
  try {
    await orgPermissionsApi.grantStaffRoles(roleTargetId.value, grantIds.value.map(Number));
    grantIds.value = [];
    ElMessage.success("角色已授予。");
    await refreshRoles();
    await load();
  } catch (value) {
    roleError.value = message(value);
  } finally {
    roleLoading.value = false;
  }
}

async function revokeRole(roleId: string, roleName: string) {
  try {
    await ElMessageBox.confirm(
      `确认回收「${roleTargetName.value}」的「${roleName}」角色？回收后该员工将立即失去对应权限（需重新登录生效）。`,
      "回收角色",
      { type: "warning", confirmButtonText: "确认回收", cancelButtonText: "取消" },
    );
  } catch {
    return;
  }
  roleLoading.value = true;
  roleError.value = "";
  try {
    await orgPermissionsApi.revokeStaffRole(roleTargetId.value, roleId);
    ElMessage.success("角色已回收。");
    await refreshRoles();
    await load();
  } catch (value) {
    roleError.value = message(value);
  } finally {
    roleLoading.value = false;
  }
}
</script>

<template>
  <section class="page-heading">
    <div>
      <div class="eyebrow">ACCOUNT MANAGEMENT</div>
      <h1>账号管理</h1>
      <p>维护平台内部员工账号：新建、修改资料、重置口令与角色授权。</p>
    </div>
    <el-tag type="warning" effect="plain">仅平台管理员可见</el-tag>
  </section>

  <el-alert
    title="前端按钮仅用于交互提示；真实权限由后端两层校验（M11:E + PLATFORM_ADMIN 角色）。批量导入、停用与删除本期不提供。"
    type="warning"
    show-icon
    :closable="false"
  />

  <el-alert v-if="metaError" class="account-gap" :title="`组织与角色数据加载失败：${metaError}`" type="error" show-icon :closable="false">
    <el-button text @click="loadMeta">重新加载</el-button>
  </el-alert>

  <section class="panel account-toolbar">
    <div class="account-filters">
      <el-input
        v-model="keyword"
        class="account-keyword"
        placeholder="搜索姓名 / 手机号 / 登录名"
        clearable
        @keyup.enter="search"
        @clear="search"
      />
      <el-button type="primary" @click="search">搜索</el-button>
      <el-button @click="resetSearch">重置</el-button>
    </div>
    <el-button type="primary" @click="openCreate">+ 新建账号</el-button>
  </section>

  <el-alert v-if="pageError" class="account-gap" :title="pageError" type="error" show-icon :closable="false">
    <el-button text @click="load()">重新加载</el-button>
  </el-alert>

  <section class="panel">
    <div class="section-title">
      <h2>员工账号 <span class="muted">{{ total }} 人</span></h2>
      <el-button :loading="loading" @click="load()">刷新</el-button>
    </div>

    <el-table v-loading="loading" :data="rows" row-key="id" empty-text="暂无员工账号">
      <el-table-column label="姓名" min-width="120">
        <template #default="{ row }">
          <b>{{ row.name }}</b>
        </template>
      </el-table-column>
      <el-table-column label="登录名" min-width="130">
        <template #default="{ row }">
          <span class="mono">{{ row.account?.loginId || "—" }}</span>
        </template>
      </el-table-column>
      <el-table-column label="手机号" width="135" prop="mobile" />
      <el-table-column label="组织" min-width="140">
        <template #default="{ row }">{{ row.orgName || "—" }}</template>
      </el-table-column>
      <el-table-column label="角色" min-width="200">
        <template #default="{ row }">
          <el-tag v-for="role in row.roles" :key="role.id" class="account-role-tag" size="small" effect="plain">
            {{ role.name }}
          </el-tag>
          <span v-if="!row.roles.length" class="muted">未授权</span>
        </template>
      </el-table-column>
      <el-table-column label="状态" width="95">
        <template #default="{ row }">
          <el-tag :type="statusType(row.status)" size="small">{{ statusLabel(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="230" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" @click="openEdit(row)">编辑资料</el-button>
          <el-button link type="primary" @click="openPassword(row)">重置口令</el-button>
          <el-button link type="primary" @click="openRoles(row)">角色</el-button>
        </template>
      </el-table-column>
    </el-table>

    <div v-if="total > size" class="account-pagination">
      <el-pagination
        :current-page="page"
        :page-size="size"
        :total="total"
        layout="total, prev, pager, next"
        @current-change="changePage"
      />
    </div>
  </section>

  <!-- 新建 / 编辑资料 -->
  <el-drawer
    v-model="formOpen"
    :title="formMode === 'create' ? '新建账号' : '编辑资料'"
    size="min(560px, 96vw)"
  >
    <el-alert
      v-if="formMode === 'create'"
      class="account-gap"
      title="新建后会立即生成可登录账号。请把登录名与初始口令线下告知本人，并提醒其首次登录后修改。"
      type="info"
      show-icon
      :closable="false"
    />
    <el-form label-width="88px" label-position="top">
      <el-form-item label="姓名" required>
        <el-input v-model="form.name" maxlength="32" placeholder="真实姓名" />
      </el-form-item>
      <el-form-item label="手机号" required>
        <el-input v-model="form.mobile" maxlength="11" placeholder="11 位手机号，全平台唯一" />
      </el-form-item>
      <el-form-item label="所属组织" required>
        <el-select v-model="form.orgUnitId" placeholder="选择组织" style="width: 100%">
          <el-option v-for="org in orgOptions" :key="org.id" :label="org.label" :value="org.id" />
        </el-select>
      </el-form-item>
      <el-form-item label="登录名" :required="formMode === 'create'">
        <el-input
          v-model="form.loginId"
          :disabled="formMode === 'edit'"
          maxlength="32"
          placeholder="4-32 位小写字母 / 数字 / 下划线，小写字母开头"
        />
        <div v-if="formMode === 'edit'" class="muted">登录名创建后不可修改。</div>
      </el-form-item>
      <template v-if="formMode === 'create'">
        <el-form-item label="初始口令" required>
          <el-input v-model="form.password" type="password" show-password maxlength="64" placeholder="8-64 个字符" />
        </el-form-item>
        <el-form-item label="角色" required>
          <el-select v-model="form.roleIds" multiple placeholder="至少选择一个角色" style="width: 100%">
            <el-option v-for="role in assignableRoles" :key="role.id" :label="role.name" :value="role.id" />
          </el-select>
          <div class="muted">
            选「平台管理员」等于授予账号管理权与全部权限点；只做业务选「员工」或对应职能角色。
          </div>
        </el-form-item>
      </template>
      <el-alert v-if="formError" :title="formError" type="error" show-icon :closable="false" />
      <div class="account-form-actions">
        <el-button @click="formOpen = false">取消</el-button>
        <el-button type="primary" :loading="formSubmitting" @click="submitForm">
          {{ formMode === "create" ? "创建账号" : "保存" }}
        </el-button>
      </div>
    </el-form>
  </el-drawer>

  <!-- 重置口令 -->
  <el-dialog v-model="passwordOpen" title="重置口令" width="min(460px, 96vw)">
    <el-alert
      :title="`即将重置「${passwordTarget?.name || ''}」的登录口令。重置后旧口令立即失效。`"
      type="warning"
      show-icon
      :closable="false"
    />
    <el-form label-position="top" class="account-gap">
      <el-form-item label="新口令" required>
        <el-input v-model="passwordForm.password" type="password" show-password maxlength="64" placeholder="8-64 个字符" />
      </el-form-item>
      <el-form-item label="确认新口令" required>
        <el-input v-model="passwordForm.confirm" type="password" show-password maxlength="64" placeholder="再次输入" />
      </el-form-item>
      <el-alert v-if="passwordError" :title="passwordError" type="error" show-icon :closable="false" />
    </el-form>
    <template #footer>
      <el-button @click="passwordOpen = false">取消</el-button>
      <el-button type="primary" :loading="passwordSubmitting" @click="submitPassword">确认重置</el-button>
    </template>
  </el-dialog>

  <!-- 角色管理 -->
  <el-drawer v-model="roleOpen" :title="`角色管理 · ${roleTargetName}`" size="min(560px, 96vw)">
    <div v-loading="roleLoading">
      <el-alert v-if="roleError" :title="roleError" type="error" show-icon :closable="false" class="account-gap" />

      <h3>已持有角色</h3>
      <el-empty v-if="!grantedRoles.length" description="该员工暂无角色" :image-size="64" />
      <ul v-else class="account-role-list">
        <li v-for="role in grantedRoles" :key="role.id">
          <div>
            <b>{{ role.name }}</b>
            <span class="muted mono">{{ role.code }}</span>
          </div>
          <el-button link type="danger" @click="revokeRole(role.id, role.name)">回收</el-button>
        </li>
      </ul>

      <h3>授予新角色</h3>
      <el-select v-model="grantIds" multiple placeholder="选择要追加的角色" style="width: 100%">
        <el-option v-for="role in grantCandidates" :key="role.id" :label="role.name" :value="role.id" />
      </el-select>
      <div class="muted">
        外部门户角色（供应商 / 客户）不在此授予；角色互斥组合（销售 ↔ 定价运营、销售 ↔ 采购）会被后端拒绝。
      </div>
      <div class="account-form-actions">
        <el-button type="primary" :loading="roleLoading" @click="submitGrant">授予</el-button>
      </div>
    </div>
  </el-drawer>
</template>

<style scoped>
.account-gap { margin-top: 14px; }
.account-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 16px; flex-wrap: wrap; margin-top: 16px; }
.account-filters { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.account-keyword { width: 280px; }
.account-role-tag { margin: 0 6px 4px 0; }
.account-pagination { display: flex; justify-content: flex-end; margin-top: 16px; }
.account-form-actions { display: flex; justify-content: flex-end; gap: 10px; margin-top: 20px; }
.account-role-list { margin: 10px 0 24px; padding: 0; list-style: none; }
.account-role-list li { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 10px 14px; border-radius: 8px; background: var(--app-surface-muted); margin-bottom: 8px; }
.account-role-list span { margin-left: 10px; }
@media (max-width: 900px) { .account-keyword { width: 100%; } }
</style>
