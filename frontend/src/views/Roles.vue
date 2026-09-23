<template>
  <div class="page-card">
    <div class="toolbar">
      <span class="tip">
        角色决定员工能看哪些菜单、能点哪些按钮。内置角色可改权限但不可删除；
        <b>超级管理员始终拥有全部权限</b>，不可修改（防止把系统锁死）。
      </span>
      <t-button
        v-if="canEdit"
        theme="primary"
        @click="openAdd"
      >
        <template #icon>
          <add-icon />
        </template>
        新增角色
      </t-button>
    </div>

    <!-- 窄屏:表格 + 720px 弹窗在手机上都不适用,列表改卡片,权限勾选见下方分栏 -->
    <div
      v-if="isMobile"
      class="m-list"
    >
      <div
        v-if="!list.length"
        class="m-empty"
      >
        {{ loading ? '加载中…' : '暂无角色' }}
      </div>
      <div
        v-for="row in list"
        :key="row.roleId"
        class="mcard"
      >
        <div class="mcard-hd">
          <span class="mcard-no">{{ row.roleName }}</span>
          <t-tag
            v-if="row.isBuiltin === 1"
            size="small"
            variant="light"
            theme="primary"
          >
            内置
          </t-tag>
        </div>
        <div class="mcard-grid">
          <div class="mcard-cell">
            <div class="k">权限</div>
            <div class="v">{{ (row.permList || []).length }} / {{ permTotal }} 项</div>
          </div>
          <div class="mcard-cell">
            <div class="k">员工数</div>
            <div class="v">{{ row.userCount || 0 }} 人</div>
          </div>
          <div
            v-if="row.remark"
            class="mcard-cell"
          >
            <div class="k">说明</div>
            <div class="v">
              {{ row.remark }}
            </div>
          </div>
        </div>
        <div class="mcard-ft">
          <t-button
            v-if="canEdit"
            theme="primary"
            variant="text"
            size="small"
            @click="openEdit(row)"
          >
            编辑
          </t-button>
          <t-button
            v-if="canEdit"
            theme="danger"
            variant="text"
            size="small"
            :disabled="row.isBuiltin === 1"
            @click="onDelete(row)"
          >
            删除
          </t-button>
        </div>
      </div>
    </div>

    <t-table
      v-else
      :data="list"
      :columns="columns"
      row-key="roleId"
      :loading="loading"
    >
      <template #roleName="{ row }">
        <span class="role-name">{{ row.roleName }}</span>
        <t-tag
          v-if="row.isBuiltin === 1"
          size="small"
          variant="light"
          theme="primary"
        >
          内置
        </t-tag>
      </template>
      <template #permCount="{ row }">
        <span class="muted">{{ (row.permList || []).length }} / {{ permTotal }} 项</span>
      </template>
      <template #userCount="{ row }">
        <span class="muted">{{ row.userCount || 0 }} 人</span>
      </template>
      <template #op="{ row }">
        <t-space :size="4">
          <t-button
            v-if="canEdit"
            theme="primary"
            variant="text"
            size="small"
            @click="openEdit(row)"
          >
            编辑
          </t-button>
          <t-button
            v-if="canEdit"
            theme="danger"
            variant="text"
            size="small"
            :disabled="row.isBuiltin === 1"
            @click="onDelete(row)"
          >
            删除
          </t-button>
        </t-space>
      </template>
    </t-table>

    <t-dialog
      v-model:visible="dialogVisible"
      :header="form.roleId ? '编辑角色' : '新增角色'"
      width="720px"
      :confirm-btn="{ content: '保存', theme: 'primary' }"
      @confirm="save"
    >
      <t-form
        :data="form"
        label-width="90px"
      >
        <t-form-item
          label="角色名称"
          name="roleName"
        >
          <t-input
            v-model="form.roleName"
            placeholder="如 值班经理"
          />
        </t-form-item>
        <t-form-item
          label="角色标识"
          name="roleKey"
        >
          <t-input
            v-model="form.roleKey"
            :disabled="!!form.roleId"
            placeholder="英文标识，如 shift_lead；创建后不可修改"
          />
        </t-form-item>
        <t-form-item
          label="排序"
          name="sortOrder"
        >
          <t-input-number
            v-model="form.sortOrder"
            :min="0"
            style="width: 140px"
          />
        </t-form-item>
        <t-form-item
          label="备注"
          name="remark"
        >
          <t-input
            v-model="form.remark"
            placeholder="选填，说明该角色的使用场景"
          />
        </t-form-item>
      </t-form>

      <div class="perm-head">
        <span class="perm-title">权限配置</span>
        <span class="muted">
          共勾选 {{ form.permList.length }} 项
          <template v-if="!isAdminRole">
            （勾选「管理」类权限会自动带上同模块的「查看」；部分权限因数据依赖会连带授予
            其他模块的权限，勾选框旁有标注）
          </template>
        </span>
      </div>
      <div
        v-if="isAdminRole"
        class="admin-notice"
      >
        超级管理员固定拥有全部 {{ permTotal }} 项权限，不可修改。
      </div>

      <div
        v-if="loadingCatalog"
        class="muted"
        style="padding: 12px 0"
      >
        权限目录加载中…
      </div>
      <div
        v-else
        class="perm-body"
        :class="{ readonly: isAdminRole }"
      >
        <div
          v-for="g in groups"
          :key="g.key"
          class="perm-group"
        >
          <div class="perm-group-head">
            <t-checkbox
              :checked="groupAllChecked(g)"
              :indeterminate="groupSomeChecked(g)"
              :disabled="isAdminRole"
              @change="(v: boolean) => toggleGroup(g, v)"
            >
              <b>{{ g.name }}</b>
            </t-checkbox>
            <span class="perm-count">{{ groupCheckedCount(g) }}/{{ g.perms.length }}</span>
          </div>
          <div class="perm-items">
            <t-checkbox
              v-for="p in g.perms"
              :key="p.code"
              :checked="form.permList.includes(p.code)"
              :disabled="isAdminRole"
              @change="(v: boolean) => togglePerm(p.code, v)"
            >
              {{ p.name }}
              <span
                v-if="impliedLabel(p.code)"
                class="perm-imply"
                >（{{ impliedLabel(p.code) }}）</span
              >
            </t-checkbox>
          </div>
        </div>
      </div>
    </t-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { MessagePlugin, DialogPlugin } from 'tdesign-vue-next';
import { deleteRole, getPermCatalog, listRoles, saveRole, updateRole } from '../api';
import { hasPerm } from '../utils/perm';
import { useIsMobile } from '../utils/useMobile';
import type { Role } from '../types/entities';

const canEdit = computed(() => hasPerm('role:edit'));

interface PermItem {
  code: string;
  name: string;
}
interface PermGroup {
  key: string;
  name: string;
  perms: PermItem[];
}

// 列表行在操作函数里要把 roleId 传给删除接口(参数为 number | string),
// Role 字段全可选,这里收紧为必填。
type RoleRow = Role & { roleId: number };

const list = ref<RoleRow[]>([]);
const loading = ref(false);
const { isMobile } = useIsMobile();
const groups = ref<PermGroup[]>([]);
// 跨模块隐含依赖表,由 /perm/catalog 的 implies 字段下发: { 'credit:view': 'order:view' }
const impliesMap = ref<Record<string, string>>({});
const permTotal = ref<number>(0);
const loadingCatalog = ref(false);

const columns = [
  { colKey: 'roleName', title: '角色', width: 220 },
  { colKey: 'roleKey', title: '标识', width: 140 },
  { colKey: 'permCount', title: '权限数', width: 130 },
  { colKey: 'userCount', title: '员工数', width: 110 },
  { colKey: 'remark', title: '说明' },
  { colKey: 'op', title: '操作', width: 140 }
];

// ---- 权限勾选辅助 ----
// 「管理」类权限隐含同模块的「查看」——与后端 store.NormalizePerms 同一套规则。
// 前端同步这一规则,是为了避免「只勾了编辑,保存后却多了个查看」的困惑。
function moduleOf(code: string): string {
  const i = code.indexOf(':');
  return i > 0 ? code.slice(0, i) : '';
}

// ---- 权限隐含依赖 ----
// 后端 store.NormalizePerms 在保存与每次启动时会强制补全两条隐含规则:
//   ① 同模块:非 xxx:view 自动带上同模块的 xxx:view
//   ② 跨模块:见 impliesMap —— 例如勾「挂账查看」会连带拿到「订单查看」,
//      因为挂账管理页展示的其实是订单数据,走的就是 order/list 接口。
// 前端必须同步维护同一个闭包,否则会出现「用户明明取消了某项,保存后又被
// 后端补回来」的假取消 —— 这比干脆不提供取消更让人困惑。
const allPermCodes = computed(() => groups.value.flatMap(g => g.perms.map(p => p.code)));
const knownCodes = computed(() => new Set(allPermCodes.value));
const permNameMap = computed(() => {
  const m: Record<string, string> = {};
  for (const g of groups.value) {
    for (const p of g.perms) m[p.code] = p.name;
  }
  return m;
});

// 勾选 code 会连带授予的全部权限码(规则与后端 store.ImpliedBy 完全一致)
function impliedBy(code: string): string[] {
  const out: string[] = [];
  const seen = new Set<string>([code]);
  const walk = (c: string, depth: number): void => {
    if (depth > 32) return;
    const mod = moduleOf(c);
    if (mod && c !== `${mod}:view`) {
      const v = `${mod}:view`;
      if (knownCodes.value.has(v) && !seen.has(v)) {
        seen.add(v);
        out.push(v);
        walk(v, depth + 1);
      }
    }
    const x = impliesMap.value[c];
    if (x && knownCodes.value.has(x) && !seen.has(x)) {
      seen.add(x);
      out.push(x);
      walk(x, depth + 1);
    }
  };
  walk(code, 0);
  return out;
}

// 把权限集合收敛成「自洽闭包」:每一项所隐含的权限都必须在集合内。
// 顺序必须是**先裁剪后补全** —— 反过来的话,取消一个被别人依赖的权限时,
// 补全步骤会立刻把它加回来,取消操作看起来完全没生效。
function closePerms(set: Set<string>): Set<string> {
  for (let guard = 0; guard < 64; guard++) {
    let changed = false;
    for (const c of Array.from(set)) {
      if (impliedBy(c).some(x => !set.has(x))) {
        set.delete(c);
        changed = true;
      }
    }
    if (!changed) break;
  }
  for (let guard = 0; guard < 64; guard++) {
    let changed = false;
    for (const c of Array.from(set)) {
      for (const x of impliedBy(c)) {
        if (!set.has(x)) {
          set.add(x);
          changed = true;
        }
      }
    }
    if (!changed) break;
  }
  return set;
}

// 勾选框旁的提示文案,如「连带授予 订单查看」
function impliedLabel(code: string): string {
  const xs = impliedBy(code);
  if (!xs.length) return '';
  return '连带授予 ' + xs.map(c => permNameMap.value[c] || c).join('、');
}

function togglePerm(code: string, checked: boolean): void {
  const set = new Set(form.permList);
  if (checked) {
    set.add(code);
  } else {
    set.delete(code);
  }
  closePerms(set);
  // 按目录顺序输出,与后端落库字符串保持一致
  form.permList = allPermCodes.value.filter(c => set.has(c));
}

function groupCheckedCount(g: PermGroup): number {
  return g.perms.filter(p => form.permList.includes(p.code)).length;
}
function groupAllChecked(g: PermGroup): boolean {
  return g.perms.length > 0 && groupCheckedCount(g) === g.perms.length;
}
function groupSomeChecked(g: PermGroup): boolean {
  const n = groupCheckedCount(g);
  return n > 0 && n < g.perms.length;
}
function toggleGroup(g: PermGroup, checked: boolean): void {
  const set = new Set(form.permList);
  for (const p of g.perms) {
    if (checked) {
      set.add(p.code);
    } else {
      set.delete(p.code);
    }
  }
  // 整组取消时,依赖本组的跨模块权限也会被连带取消(如取消「订单」组会连带
  // 取消「挂账查看」),由 closePerms 统一处理,保证与后端补全结果一致。
  closePerms(set);
  form.permList = allPermCodes.value.filter(c => set.has(c));
}

// ---- 列表 ----
async function load(): Promise<void> {
  loading.value = true;
  try {
    const res = await listRoles();
    list.value = (res.items || []) as RoleRow[];
  } finally {
    loading.value = false;
  }
}

async function loadCatalog(): Promise<void> {
  loadingCatalog.value = true;
  try {
    const res = await getPermCatalog();
    // 后端返回 { key, name, perms:[{code,name}] }(见 store/permission.go),与本地结构同形;
    // 显式映射成必填形态,不再用断言压制 —— 字段缺失时落到空串而不是静默 undefined。
    groups.value = (res.groups ?? []).map(g => ({
      key: g.key || '',
      name: g.name || '',
      perms: (g.perms ?? []).map(p => ({ code: p.code || '', name: p.name || '' }))
    }));
    impliesMap.value = res.implies || {};
    permTotal.value = res.total || 0;
  } finally {
    loadingCatalog.value = false;
  }
}

// ---- 新增 / 编辑 ----
const dialogVisible = ref(false);
interface RoleForm {
  roleId: number | null;
  roleKey: string;
  roleName: string;
  sortOrder: number;
  remark: string;
  permList: string[];
}

const form = reactive<RoleForm>({ roleId: null, roleKey: '', roleName: '', sortOrder: 0, remark: '', permList: [] });

// 是否是「正在编辑超级管理员角色」——其权限不可改,界面整体置灰
const isAdminRole = computed(() => {
  if (!form.roleId) return false;
  const cur = list.value.find(r => r.roleId === form.roleId);
  return !!cur && cur.roleKey === 'admin';
});

function openAdd(): void {
  Object.assign(form, {
    roleId: null,
    roleKey: '',
    roleName: '',
    sortOrder: list.value.length + 1,
    remark: '',
    permList: []
  });
  dialogVisible.value = true;
}

function openEdit(row: RoleRow): void {
  Object.assign(form, {
    roleId: row.roleId ?? null,
    roleKey: row.roleKey ?? '',
    roleName: row.roleName ?? '',
    sortOrder: row.sortOrder || 0,
    remark: row.remark || '',
    permList: Array.from(row.permList || [])
  });
  dialogVisible.value = true;
}

async function save(): Promise<void> {
  if (!form.roleName) {
    MessagePlugin.warning('请填写角色名称');
    return;
  }
  if (!form.roleId && !form.roleKey) {
    MessagePlugin.warning('请填写角色标识');
    return;
  }
  if (!form.roleId && form.permList.length === 0) {
    MessagePlugin.warning('请至少勾选一项权限');
    return;
  }
  const payload = {
    roleId: form.roleId || undefined,
    roleKey: form.roleKey,
    roleName: form.roleName,
    sortOrder: form.sortOrder,
    remark: form.remark,
    permList: form.permList
  };
  try {
    if (form.roleId) {
      await updateRole(payload);
    } else {
      await saveRole(payload);
    }
    MessagePlugin.success('保存成功');
    dialogVisible.value = false;
    load();
  } catch {
    /* 失败已由拦截器统一 toast,弹窗保留已填内容供修改重试 */
  }
}

function onDelete(row: RoleRow): void {
  const dlg = DialogPlugin.confirm({
    header: '确认删除',
    theme: 'danger',
    body: `确认删除角色【${row.roleName}】？该角色下若还有员工将无法删除。`,
    onConfirm: async () => {
      try {
        await deleteRole(row.roleId);
        MessagePlugin.success('删除成功');
        dlg.hide();
        load();
      } catch {
        dlg.hide();
      }
    }
  });
}

onMounted(async () => {
  await Promise.all([loadCatalog(), load()]);
});
</script>

<style scoped>
.role-name {
  font-weight: 600;
  color: var(--ink);
  margin-right: 8px;
}

.muted {
  color: var(--ink-3);
}

.perm-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  margin: 6px 0 10px;
  padding-top: 12px;
  border-top: 1px solid var(--line);
}

.perm-title {
  font-size: 13px;
  font-weight: 700;
  color: var(--ink);
}

.admin-notice {
  background: var(--brand-ghost);
  border: 1px solid #ffd5c4;
  color: var(--brand-deep);
  border-radius: 8px;
  padding: 9px 12px;
  font-size: 12.5px;
  margin-bottom: 12px;
}

.perm-body {
  max-height: 46vh;
  overflow-y: auto;
  padding-right: 4px;
}

.perm-body.readonly {
  opacity: 0.65;
}

.perm-group {
  border: 1px solid var(--line);
  border-radius: 10px;
  padding: 10px 12px;
  margin-bottom: 10px;
}

.perm-group-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.perm-count {
  font-size: 12px;
  color: var(--ink-4);
}

.perm-items {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 20px;
  margin: 8px 0 2px 24px;
}

/* 隐含依赖提示:说明勾这一项会连带拿到别的权限,避免管理员以为自己只勾了一项 */
.perm-imply {
  font-size: 12px;
  color: var(--ink-4);
  margin-left: 2px;
}

/* 移动端:720px 的角色弹窗被压到 92vw,权限勾选区要收掉缩进、放开高度限制 */
@media (width <= 767px) {
  .perm-body {
    max-height: none;
  }

  .perm-items {
    gap: 8px 14px;
    margin-left: 4px;
  }
}
</style>
