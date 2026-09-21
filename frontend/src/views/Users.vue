<template>
  <div class="page-card">
    <div class="toolbar">
      <div class="toolbar-left">
        <t-input
          v-model="query.keyword"
          placeholder="搜索用户名 / 姓名 / 手机号"
          clearable
          style="width: 240px"
          @enter="load"
        />
        <t-select
          v-model="query.roleId"
          :options="roleOptions"
          placeholder="全部角色"
          clearable
          style="width: 150px"
        />
        <t-select
          v-model="query.status"
          :options="statusOptions"
          placeholder="全部状态"
          clearable
          style="width: 130px"
        />
        <t-button theme="default" variant="outline" @click="load">
          <template #icon><search-icon /></template>
          查询
        </t-button>
      </div>
      <t-button v-if="canEdit" theme="primary" @click="openAdd">
        <template #icon><add-icon /></template>
        新增员工
      </t-button>
    </div>

    <!-- 窄屏:7 列表格合计 1030px 只能横向拖;改为卡片流 -->
    <div v-if="isMobile" class="m-list">
      <div v-if="!list.length" class="m-empty">{{ loading ? '加载中…' : '暂无员工' }}</div>
      <div v-for="row in list" :key="row.userId" class="mcard">
        <div class="mcard-hd">
          <div style="min-width: 0">
            <span class="mcard-no">{{ row.realName || row.username }}</span>
            <t-tag v-if="row.roleName" theme="primary" variant="light-outline" class="rel-role">{{ row.roleName }}</t-tag>
            <t-tag v-else theme="danger" variant="light-outline" class="rel-role">角色失效</t-tag>
            <div class="mcard-sub">{{ row.username }}<span v-if="row.phone"> · {{ row.phone }}</span></div>
          </div>
          <t-tag :theme="row.status === 1 ? 'success' : 'default'" variant="light">
            {{ row.status === 1 ? '启用' : '停用' }}
          </t-tag>
        </div>

        <div class="mcard-grid">
          <div class="mcard-cell">
            <div class="k">最后登录</div>
            <div class="v">{{ row.lastLoginTime || '从未登录' }}</div>
          </div>
        </div>

        <div class="mcard-ft">
          <t-button v-if="canEdit" theme="primary" variant="text" size="small" @click="openEdit(row)">编辑</t-button>
          <t-button v-if="canEdit" theme="primary" variant="text" size="small" @click="openResetPwd(row)">重置密码</t-button>
          <t-button
            v-if="canEdit && !isSelf(row)"
            :theme="row.status === 1 ? 'warning' : 'success'"
            variant="text"
            size="small"
            @click="onToggleStatus(row)"
          >
            {{ row.status === 1 ? '停用' : '启用' }}
          </t-button>
          <t-button v-if="canEdit && !isSelf(row)" theme="danger" variant="text" size="small" @click="onDelete(row)">删除</t-button>
          <span v-if="isSelf(row)" class="mcard-note">本人</span>
        </div>
      </div>

      <div v-if="total > query.pageSize" class="m-pager">
        <t-pagination
          :current="query.pageNum"
          :page-size="query.pageSize"
          :total="total"
          @change="onPageChange"
        />
      </div>
    </div>

    <t-table
      v-else
      :data="list"
      :columns="columns"
      row-key="userId"
      :loading="loading"
      :pagination="pagination"
      @page-change="onPageChange"
    >
      <template #role="{ row }">
        <t-tag v-if="row.roleName" theme="primary" variant="light-outline">{{ row.roleName }}</t-tag>
        <t-tag v-else theme="danger" variant="light-outline">角色失效</t-tag>
      </template>

      <template #status="{ row }">
        <t-tag :theme="row.status === 1 ? 'success' : 'default'" variant="light">
          {{ row.status === 1 ? '启用' : '停用' }}
        </t-tag>
      </template>

      <template #lastLoginTime="{ row }">
        <span class="muted">{{ row.lastLoginTime || '从未登录' }}</span>
      </template>

      <template #op="{ row }">
        <t-space :size="4">
          <t-button v-if="canEdit" theme="primary" variant="text" size="small" @click="openEdit(row)">
            编辑
          </t-button>
          <t-button v-if="canEdit" theme="primary" variant="text" size="small" @click="openResetPwd(row)">
            重置密码
          </t-button>
          <t-button
            v-if="canEdit && !isSelf(row)"
            :theme="row.status === 1 ? 'warning' : 'success'"
            variant="text"
            size="small"
            @click="onToggleStatus(row)"
          >
            {{ row.status === 1 ? '停用' : '启用' }}
          </t-button>
          <t-button v-if="canEdit && !isSelf(row)" theme="danger" variant="text" size="small" @click="onDelete(row)">
            删除
          </t-button>
          <t-tag v-if="isSelf(row)" size="small" variant="light" theme="primary">本人</t-tag>
        </t-space>
      </template>
    </t-table>

    <!-- 新增 / 编辑 -->
    <t-dialog
      v-model:visible="dialogVisible"
      :header="form.userId ? '编辑员工' : '新增员工'"
      width="520px"
      :confirm-btn="{ content: '保存', theme: 'primary' }"
      @confirm="save"
    >
      <t-form :data="form" label-width="90px">
        <t-form-item label="用户名" name="username">
          <t-input v-model="form.username" :disabled="!!form.userId" placeholder="登录名，2~32 位字母/数字/_.@-" />
          <template v-if="form.userId" #help>
            <span class="muted">用户名创建后不可修改</span>
          </template>
        </t-form-item>
        <t-form-item v-if="!form.userId" label="初始密码" name="password">
          <t-input v-model="form.password" type="password" placeholder="至少 6 位，交给员工后请提醒尽快修改" />
        </t-form-item>
        <t-form-item label="姓名" name="realName">
          <t-input v-model="form.realName" placeholder="订单操作留痕会显示这个名字" />
        </t-form-item>
        <t-form-item label="角色" name="roleId">
          <t-select v-model="form.roleId" :options="roleOptions" placeholder="请选择角色" />
          <!-- 角色下拉为空时必须说清原因。后端 user:view 已隐含 role:view(见
               store.permImplies),正常走不到 rolesLoadFailed;这里留作兜底,
               避免「下拉空着却没有任何提示」这种最难排查的表现。 -->
          <div v-if="rolesLoadFailed" class="field-tip warn">
            角色列表加载失败（可能缺少「角色查看」权限），暂无法设置员工角色。
          </div>
          <div v-else-if="!roleOptions.length" class="field-tip">
            暂无启用中的角色，请先在「角色权限」页创建并启用角色。
          </div>
        </t-form-item>
        <t-form-item label="手机号" name="phone">
          <t-input v-model="form.phone" placeholder="选填" />
        </t-form-item>
        <t-form-item label="备注" name="remark">
          <t-input v-model="form.remark" placeholder="选填，如所属班次" />
        </t-form-item>
      </t-form>
      <div v-if="form.userId && isSelfId(form.userId)" class="hint">
        提示：不能修改自己的角色，也不能停用或删除自己的账号（防止把自己锁在系统外）。
      </div>
    </t-dialog>

    <!-- 重置密码 -->
    <t-dialog
      v-model:visible="pwdVisible"
      :header="`重置密码 - ${pwdTarget.realName || pwdTarget.username}`"
      width="420px"
      :confirm-btn="{ content: '确认重置', theme: 'primary' }"
      @confirm="submitResetPwd"
    >
      <div class="pwd-form">
        <div class="pwd-row">
          <label>新密码</label>
          <input v-model="pwdForm.password" type="text" placeholder="至少 6 位" />
        </div>
        <div class="pwd-row">
          <label>确认密码</label>
          <input v-model="pwdForm.confirm" type="text" placeholder="再次输入" />
        </div>
      </div>
      <div class="hint">重置后该员工当前登录会立即失效，需要用新密码重新登录。</div>
    </t-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { MessagePlugin, DialogPlugin } from 'tdesign-vue-next'
import {
  deleteUser,
  listRoles,
  listUsers,
  resetUserPassword,
  saveUser,
  toggleUserStatus,
  updateUser
} from '../api'
import { getUsername, hasPerm } from '../utils/perm'
import { useIsMobile } from '../utils/useMobile'

// user:edit 才显示增删改类按钮;user:view 进来只看列表。
// 注意:后端同样会校验,这里的显隐只是避免「点了才被拒」的体验问题。
const canEdit = computed(() => hasPerm('user:edit'))

const list = ref([])
const loading = ref(false)
const roles = ref([])
// 角色列表是否拉取失败(通常是权限不足)——用于给表单一个明确提示而不是静默空下拉
const rolesLoadFailed = ref(false)
const total = ref(0)
const { isMobile } = useIsMobile()

const query = reactive({ keyword: '', roleId: undefined, status: undefined, pageNum: 1, pageSize: 10 })

const roleOptions = computed(() =>
  roles.value.filter((r) => r.status === 1).map((r) => ({ label: r.roleName, value: r.roleId }))
)
const statusOptions = [
  { label: '启用', value: 1 },
  { label: '停用', value: 0 }
]

const pagination = computed(() => ({
  current: query.pageNum,
  pageSize: query.pageSize,
  total: total.value,
  showJumper: true,
  pageSizeOptions: [10, 20, 50]
}))

const columns = [
  { colKey: 'username', title: '用户名', width: 150 },
  { colKey: 'realName', title: '姓名', width: 120 },
  { colKey: 'role', title: '角色', width: 130 },
  { colKey: 'phone', title: '手机号', width: 140 },
  { colKey: 'status', title: '状态', width: 90 },
  { colKey: 'lastLoginTime', title: '最后登录', width: 180 },
  { colKey: 'op', title: '操作', width: 260 }
]

function isSelf(row) {
  return row.username === getUsername()
}
function isSelfId(id) {
  const me = list.value.find((u) => u.username === getUsername())
  return !!me && me.userId === id
}

async function load() {
  loading.value = true
  try {
    const res = await listUsers({
      keyword: query.keyword || undefined,
      roleId: query.roleId || undefined,
      // status 传 0 是合法筛选条件,不能用 || 兜底成 undefined
      status: query.status === undefined || query.status === null ? undefined : query.status,
      pageNum: query.pageNum,
      pageSize: query.pageSize
    })
    list.value = res.rows || []
    total.value = res.total || 0
  } finally {
    loading.value = false
  }
}

async function loadRoles() {
  rolesLoadFailed.value = false
  try {
    const res = await listRoles()
    roles.value = res.rows || []
  } catch {
    // 角色列表拉取失败时不影响员工列表展示,但要在表单里说清楚(见模板的 rolesLoadFailed)
    rolesLoadFailed.value = true
  }
}

function onPageChange(pageInfo) {
  query.pageNum = pageInfo.current
  query.pageSize = pageInfo.pageSize
  load()
}

// ---- 新增 / 编辑 ----
const dialogVisible = ref(false)
const form = reactive({ userId: null, username: '', password: '', realName: '', roleId: undefined, phone: '', remark: '' })

function openAdd() {
  Object.assign(form, {
    userId: null, username: '', password: '', realName: '',
    roleId: undefined, phone: '', remark: ''
  })
  dialogVisible.value = true
}

function openEdit(row) {
  Object.assign(form, {
    userId: row.userId, username: row.username, password: '',
    realName: row.realName || '', roleId: row.roleId || undefined,
    phone: row.phone || '', remark: row.remark || ''
  })
  dialogVisible.value = true
}

async function save() {
  if (!form.userId) {
    if (!form.username) {
      MessagePlugin.warning('请填写用户名')
      return
    }
    if (!form.password || form.password.length < 6) {
      MessagePlugin.warning('初始密码至少 6 位')
      return
    }
  }
  if (!form.roleId) {
    MessagePlugin.warning('请选择角色')
    return
  }
  if (form.userId) {
    await updateUser({
      userId: form.userId,
      realName: form.realName,
      roleId: form.roleId,
      phone: form.phone,
      remark: form.remark
    })
  } else {
    await saveUser({
      username: form.username,
      password: form.password,
      realName: form.realName,
      roleId: form.roleId,
      phone: form.phone,
      remark: form.remark
    })
  }
  MessagePlugin.success('保存成功')
  dialogVisible.value = false
  load()
}

// ---- 重置密码 ----
const pwdVisible = ref(false)
const pwdTarget = ref({})
const pwdForm = reactive({ password: '', confirm: '' })

function openResetPwd(row) {
  pwdTarget.value = row
  pwdForm.password = ''
  pwdForm.confirm = ''
  pwdVisible.value = true
}

async function submitResetPwd() {
  if (!pwdForm.password || pwdForm.password.length < 6) {
    MessagePlugin.warning('新密码至少 6 位')
    return
  }
  if (pwdForm.password !== pwdForm.confirm) {
    MessagePlugin.warning('两次输入的密码不一致')
    return
  }
  await resetUserPassword({ userId: pwdTarget.value.userId, password: pwdForm.password })
  MessagePlugin.success('密码已重置')
  pwdVisible.value = false
}

// ---- 启停用 / 删除 ----
function onToggleStatus(row) {
  const enabling = row.status !== 1
  DialogPlugin.confirm({
    header: enabling ? '确认启用' : '确认停用',
    body: enabling
      ? `确认启用账号【${row.realName || row.username}】？`
      : `确认停用账号【${row.realName || row.username}】？停用后该员工会立即无法登录。`,
    theme: enabling ? 'default' : 'warning',
    onConfirm: async () => {
      await toggleUserStatus({ userId: row.userId, status: enabling ? 1 : 0 })
      MessagePlugin.success(enabling ? '已启用' : '已停用')
      load()
    }
  })
}

function onDelete(row) {
  DialogPlugin.confirm({
    header: '确认删除',
    theme: 'danger',
    body: `确认删除账号【${row.realName || row.username}】？删除后该账号无法登录，历史订单里记录的操作人姓名会保留。`,
    onConfirm: async () => {
      await deleteUser(row.userId)
      MessagePlugin.success('删除成功')
      load()
    }
  })
}

onMounted(async () => {
  await loadRoles()
  load()
})
</script>

<style scoped>
.muted {
  color: var(--ink-3);
}
.hint {
  margin-top: 10px;
  font-size: 12px;
  color: var(--ink-3);
  line-height: 1.7;
}
/* 表单项下方的说明 / 告警文案 */
.field-tip {
  margin-top: 6px;
  font-size: 12px;
  color: var(--ink-3);
  line-height: 1.6;
}
.field-tip.warn {
  color: var(--warning);
}
.pwd-form {
  display: flex;
  flex-direction: column;
  gap: 14px;
  padding: 4px 0 2px;
}
.pwd-row {
  display: flex;
  align-items: center;
  gap: 12px;
}
.pwd-row label {
  width: 72px;
  font-size: 13px;
  color: var(--ink-2);
  flex-shrink: 0;
}
.pwd-row input {
  flex: 1;
  height: 38px;
  box-sizing: border-box;
  border: 1.5px solid var(--line);
  border-radius: 8px;
  padding: 0 12px;
  font-size: 13px;
  color: var(--ink);
  outline: none;
}
.pwd-row input:focus {
  border-color: var(--brand);
}

/* 移动端卡片里的角色标签与「本人」标记 */
.rel-role {
  margin-left: 6px;
  vertical-align: 1px;
}
.mcard-note {
  align-self: center;
  font-size: 12px;
  color: var(--brand-deep);
}
</style>
