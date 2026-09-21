<template>
  <div class="admin-shell">
    <!-- 移动端抽屉遮罩:点击空白处收起侧栏 -->
    <transition name="mask-fade">
      <div v-if="isMobile && drawerOpen" class="side-mask" @click="drawerOpen = false"></div>
    </transition>

    <!-- 侧栏:桌面端常驻;移动端收进左侧抽屉,由顶栏汉堡按钮唤出 -->
    <aside class="side" :class="{ 'side-open': drawerOpen }">
      <div class="logo">
        <div class="logo-dot">点</div>
        <div class="logo-text">
          <div class="logo-name">扫码点餐系统</div>
          <div class="logo-sub">Dining Admin</div>
        </div>
      </div>

      <nav class="menu">
        <router-link
          v-for="m in menus"
          :key="m.path"
          :to="m.path"
          class="mi"
          :class="{ on: isActive(m.path) }"
          @click="drawerOpen = false"
        >
          <span class="ic"><component :is="m.icon" /></span>
          <span class="txt">{{ m.title }}</span>
        </router-link>
      </nav>

      <!-- 移动端顶栏只留汉堡+标题+点餐页入口,账号操作下沉到抽屉底部;
           版本号固定在侧栏最底部,桌面端与移动端都显示(随 package.json 同步) -->
      <div class="side-foot">
        <div v-if="isMobile" class="sf-user">
          <user-circle-icon /><span>{{ displayName }}</span>
          <em v-if="roleName" class="sf-role">{{ roleName }}</em>
        </div>
        <button v-if="isMobile" class="sf-btn" @click="openChangePwd">修改密码</button>
        <button v-if="isMobile" class="sf-btn danger" @click="onLogout">退出登录</button>
        <div class="side-ver">v{{ appVersion }}</div>
      </div>
    </aside>

    <!-- 主体 -->
    <div class="main">
      <header class="top">
        <button v-if="isMobile" class="burger" aria-label="打开菜单" @click="drawerOpen = true">
          <span></span><span></span><span></span>
        </button>
        <div class="top-title">{{ currentTitle }}</div>
        <div class="top-acts">
          <button v-if="canOpenOrderPage" class="a ghost" @click="openOrderPage">
            <qrcode-icon />
            <span>顾客点餐页</span>
          </button>
          <template v-if="!isMobile">
            <span class="a user">
              <user-circle-icon />
              <span>{{ displayName }}</span>
              <em v-if="roleName" class="top-role">{{ roleName }}</em>
            </span>
            <button class="a" @click="openChangePwd">修改密码</button>
            <button class="a danger" @click="onLogout">退出登录</button>
          </template>
        </div>
      </header>

      <div class="content">
        <router-view />
      </div>

      <!-- 移动端底部 tabbar:只放最高频的入口,其余菜单收进「更多」打开侧栏抽屉。
           放在 .main 的 flex 流里(而不是 position:fixed),.content 会自动扣掉它的高度,
           不会出现「最后一行被 tabbar 盖住」的老问题。 -->
      <nav v-if="isMobile" class="tabbar">
        <router-link
          v-for="t in tabs"
          :key="t.path"
          :to="t.path"
          class="tb"
          :class="{ on: route.path === t.path }"
        >
          <span class="tb-ic"><component :is="t.icon" /></span>
          <span class="tb-txt">{{ t.title }}</span>
        </router-link>
        <button
          class="tb"
          :class="{ on: notInTabbar }"
          aria-label="全部菜单"
          @click="drawerOpen = true"
        >
          <span class="tb-ic"><ellipsis-icon /></span>
          <span class="tb-txt">更多</span>
        </button>
      </nav>
    </div>

    <!-- 修改密码弹窗 -->
    <t-dialog v-model:visible="pwdVisible" header="修改密码" width="400px" :footer="false">
      <div class="pwd-form">
        <div class="pwd-row">
          <label>原密码</label>
          <input v-model="pwdForm.oldPassword" type="password" placeholder="请输入当前密码" />
        </div>
        <div class="pwd-row">
          <label>新密码</label>
          <input v-model="pwdForm.newPassword" type="password" placeholder="至少 6 位" />
        </div>
        <div class="pwd-row">
          <label>确认新密码</label>
          <input v-model="pwdForm.confirm" type="password" placeholder="再次输入新密码" />
        </div>
        <div class="pwd-actions">
          <button class="pwd-btn cancel" @click="pwdVisible = false">取消</button>
          <button class="pwd-btn ok" :disabled="pwdSubmitting" @click="submitChangePwd">
            <span v-if="pwdSubmitting" class="spin light"></span>确认修改
          </button>
        </div>
      </div>
    </t-dialog>
  </div>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { MessagePlugin } from 'tdesign-vue-next'
import { changePassword, listTables } from '../api'
import { useIsMobile } from '../utils/useMobile'
import { accessibleMenus } from '../router'
import { clearAuth, getDisplayName, getRoleName, hasPerm } from '../utils/perm'
import { clearRemember } from '../utils/remember'
import pkg from '../../package.json'

const route = useRoute()
const router = useRouter()
const { isMobile } = useIsMobile()

// 移动端侧栏抽屉开合状态
const drawerOpen = ref(false)

// 路由变化(含浏览器后退、程序跳转)一律收起抽屉,避免新页面被遮罩挡住
watch(() => route.path, () => {
  drawerOpen.value = false
})

// 从移动端宽度切回桌面宽度时,抽屉状态没有意义,顺手复位
watch(isMobile, (v) => {
  if (!v) drawerOpen.value = false
})

// 菜单直接从路由表派生(见 router/index.js 的 meta.menuOrder / meta.perm):
// 好处是「加一个页面」只需加一条路由,菜单、权限过滤、默认落地页自动跟上,
// 不会出现菜单与权限表各自维护、逐渐漂移的问题。
// 无权限的菜单不渲染;全部无权限时路由守卫会把人送到兜底页。
const menus = computed(() => accessibleMenus())

// ---- 移动端底部 tabbar ----
// 只放四个最高频入口(看板/订单/桌台/菜品),其余菜单走「更多」打开抽屉 ——
// 13 个菜单全塞进去会挤成图标条,反而找不到东西。
// 顺序即「餐厅店员掏手机最常看的东西」的顺序。
const TAB_ITEMS = [
  { path: '/dining/dashboard', title: '看板', icon: 'DashboardIcon' },
  { path: '/dining/orders', title: '订单', icon: 'OrderAdjustmentColumnIcon' },
  { path: '/dining/tables', title: '桌台', icon: 'GridViewIcon' },
  { path: '/dining/dishes', title: '菜品', icon: 'RiceIcon' }
]
// 复用 accessibleMenus() 的权限过滤结果,保证无权限的入口不会出现在 tabbar 上
// (否则点进去会被路由守卫弹回,像个坏按钮)。
const tabs = computed(() => {
  const allowed = new Set(menus.value.map((m) => m.path))
  return TAB_ITEMS.filter((t) => allowed.has(t.path))
})
// 当前路由不在 tabbar 上时(系统配置、员工管理、角色权限…),高亮「更多」
const notInTabbar = computed(() => !tabs.value.some((t) => t.path === route.path))

const currentTitle = computed(() => route.meta.title || '')
const displayName = computed(() => getDisplayName())
const roleName = computed(() => getRoleName())
// 侧栏底部展示的版本号,直接取自 package.json,发版改版本号即可同步,不会漂移
const appVersion = pkg.version
// 「顾客点餐页」按钮内部要调桌台接口,没有 table:view 时点了必然 403,索性不显示。
const canOpenOrderPage = computed(() => hasPerm('table:view'))

function isActive(path) {
  return route.path === path
}

async function openOrderPage() {
  try {
    const res = await listTables({ pageNum: 1, pageSize: 1 })
    const rows = res.rows || []
    if (!rows.length) {
      MessagePlugin.warning('暂无桌台，请先到「桌台管理」创建桌台')
      return
    }
    window.open(`/order/${rows[0].tableId}`, '_blank')
  } catch {
    MessagePlugin.warning('获取桌台失败，请稍后重试')
  }
}

function onLogout() {
  clearAuth()
  clearRemember()
  router.replace('/login')
}

// ---- 修改密码 ----
const pwdVisible = ref(false)
const pwdSubmitting = ref(false)
const pwdForm = reactive({ oldPassword: '', newPassword: '', confirm: '' })

function openChangePwd() {
  drawerOpen.value = false
  pwdForm.oldPassword = ''
  pwdForm.newPassword = ''
  pwdForm.confirm = ''
  pwdVisible.value = true
}

async function submitChangePwd() {
  if (!pwdForm.oldPassword) {
    MessagePlugin.warning('请输入原密码')
    return
  }
  if (pwdForm.newPassword.length < 6) {
    MessagePlugin.warning('新密码至少 6 位')
    return
  }
  if (pwdForm.newPassword !== pwdForm.confirm) {
    MessagePlugin.warning('两次输入的新密码不一致')
    return
  }
  pwdSubmitting.value = true
  try {
    await changePassword({ oldPassword: pwdForm.oldPassword, newPassword: pwdForm.newPassword })
    // 后端改密时会把 token_version +1,当前令牌随即失效 —— 与其等下一个请求
    // 撞 401 被拦截器兜走,不如在这里主动清登录态并引导重新登录,提示更明确。
    clearAuth()
    clearRemember()
    pwdVisible.value = false
    MessagePlugin.success('密码修改成功，请用新密码重新登录')
    router.replace('/login')
  } finally {
    pwdSubmitting.value = false
  }
}
</script>

<style scoped>
.admin-shell {
  display: flex;
  height: 100vh;
  /* 移动端浏览器地址栏收起/展开会改变可视高度,100vh 会产生跳动 */
  height: 100dvh;
  background: var(--bg);
}

/* ---------- 侧栏 ---------- */
.side {
  width: 220px;
  flex-shrink: 0;
  background: #fff;
  border-right: 1px solid var(--line);
  padding: 18px 12px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
}
.logo {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 0 6px 18px;
  border-bottom: 1px solid var(--line);
  margin-bottom: 12px;
}
.logo-dot {
  width: 36px;
  height: 36px;
  border-radius: 10px;
  background: var(--grad-brand);
  color: #fff;
  font-size: 17px;
  font-weight: 800;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  box-shadow: 0 4px 12px rgba(240, 72, 31, 0.28);
}
.logo-name {
  font-size: 14px;
  font-weight: 700;
  color: var(--ink);
  line-height: 1.3;
}
.logo-sub {
  font-size: 11px;
  color: var(--ink-3);
  font-weight: 400;
}

.menu {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.mi {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 9px 11px;
  border-radius: 10px;
  font-size: 13px;
  color: var(--ink-2);
  text-decoration: none;
  transition: all 0.15s;
}
.mi:hover {
  background: var(--brand-ghost);
  color: var(--brand-deep);
}
.mi.on {
  background: var(--brand-soft);
  color: var(--brand-deep);
  font-weight: 600;
}
.ic {
  width: 22px;
  height: 22px;
  border-radius: 6px;
  background: #f0f0f0;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 14px;
  flex-shrink: 0;
  transition: background 0.15s;
}
.mi.on .ic {
  background: #fff;
  color: var(--brand-deep);
}
.txt {
  white-space: nowrap;
}

/* ---------- 主体 ---------- */
.main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
}
.top {
  height: 56px;
  flex-shrink: 0;
  background: #fff;
  border-bottom: 1px solid var(--line);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 0 22px;
}
.top-title {
  font-size: 15px;
  font-weight: 600;
  color: var(--ink);
  flex: 1;
  min-width: 0;
}
.top-acts {
  display: flex;
  align-items: center;
  gap: 8px;
}
.a {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 12px;
  color: var(--ink-2);
  padding: 7px 13px;
  border-radius: 9px;
  border: 1px solid var(--line);
  background: #fff;
  cursor: pointer;
  transition: all 0.15s;
}
.a:hover {
  border-color: #ffd5c4;
  color: var(--brand-deep);
  background: var(--brand-ghost);
}
.a.ghost {
  color: var(--brand-deep);
  border-color: #ffd5c4;
  background: var(--brand-ghost);
}
.a.user {
  border-color: transparent;
  background: transparent;
  cursor: default;
}
.a.danger:hover {
  color: var(--danger);
  border-color: #ffc9c9;
  background: var(--danger-soft);
}
/* 顶栏账号区里的角色徽标:和姓名同一行,用浅底标签区分,不抢视觉重心 */
.a.user .top-role {
  font-style: normal;
  font-size: 11px;
  line-height: 1;
  padding: 4px 7px;
  border-radius: 6px;
  background: var(--brand-soft);
  color: var(--brand-deep);
  font-weight: 600;
  white-space: nowrap;
}

.content {
  flex: 1;
  padding: 22px;
  overflow: auto;
}

/* ---------- 修改密码弹窗 ---------- */
.pwd-form {
  display: flex;
  flex-direction: column;
  gap: 14px;
  padding: 4px 0;
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
  transition: border-color 0.15s;
}
.pwd-row input:focus {
  border-color: var(--brand);
}
.pwd-actions {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  margin-top: 6px;
}
.pwd-btn {
  height: 36px;
  padding: 0 20px;
  border-radius: 8px;
  font-size: 13px;
  cursor: pointer;
  border: 1px solid var(--line);
  background: #fff;
  color: var(--ink-2);
}
.pwd-btn.cancel:hover {
  background: var(--bg);
}
.pwd-btn.ok {
  border: none;
  background: var(--grad-brand);
  color: #fff;
  font-weight: 600;
}
.pwd-btn.ok:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}
.pwd-btn .spin {
  display: inline-block;
  width: 12px;
  height: 12px;
  border: 2px solid rgba(255, 255, 255, 0.4);
  border-top-color: #fff;
  border-radius: 50%;
  animation: pwdspin 0.8s linear infinite;
  margin-right: 4px;
  vertical-align: -1px;
}
@keyframes pwdspin {
  to { transform: rotate(360deg); }
}

/* ---------- 抽屉底部账号区(仅移动端渲染) ---------- */
.side-foot {
  margin-top: auto;
  padding-top: 10px;
  border-top: 1px solid var(--line);
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.sf-user {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12.5px;
  color: var(--ink-3);
  padding: 0 4px 2px;
}
/* 抽屉底部的角色徽标(同 .top-role,尺寸略收以适应 236px 侧栏) */
.sf-user .sf-role {
  font-style: normal;
  font-size: 11px;
  line-height: 1;
  padding: 4px 6px;
  border-radius: 6px;
  background: var(--brand-soft);
  color: var(--brand-deep);
  font-weight: 600;
  white-space: nowrap;
}
.sf-btn {
  height: 38px;
  border-radius: 9px;
  border: 1px solid var(--line);
  background: #fff;
  color: var(--ink-2);
  font-size: 13px;
  cursor: pointer;
  transition: all 0.15s;
}
.sf-btn:hover {
  border-color: #ffd5c4;
  color: var(--brand-deep);
  background: var(--brand-ghost);
}
.sf-btn.danger {
  color: var(--danger);
  border-color: #ffc9c9;
  background: var(--danger-soft);
}

/* 侧栏底部版本号:桌面端/移动端都显示,固定在 sidebar 最底 */
.side-ver {
  margin-top: 8px;
  text-align: center;
  font-size: 13px;
  color: var(--ink-3);
  letter-spacing: 0.4px;
  line-height: 1;
}

/* ---------- 顶栏汉堡按钮 ---------- */
.burger {
  width: 34px;
  height: 34px;
  flex-shrink: 0;
  padding: 0;
  border: 1px solid var(--line);
  border-radius: 9px;
  background: #fff;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 3.5px;
  cursor: pointer;
}
.burger span {
  display: block;
  width: 15px;
  height: 1.5px;
  border-radius: 1px;
  background: var(--ink-2);
}

/* ---------- 抽屉遮罩 ---------- */
.side-mask {
  position: fixed;
  inset: 0;
  z-index: 900;
  background: rgba(0, 0, 0, 0.42);
}
.mask-fade-enter-active,
.mask-fade-leave-active {
  transition: opacity 0.2s;
}
.mask-fade-enter-from,
.mask-fade-leave-to {
  opacity: 0;
}

/* ============================================================
   移动端:侧栏由常驻列改为左侧抽屉
   ============================================================ */
@media (max-width: 767px) {
  .side {
    position: fixed;
    top: 0;
    left: 0;
    bottom: 0;
    z-index: 950;
    width: 236px;
    transform: translateX(-100%);
    transition: transform 0.24s ease;
    padding-top: calc(18px + env(safe-area-inset-top, 0px));
    padding-bottom: calc(18px + env(safe-area-inset-bottom, 0px));
    border-right: none;
    overscroll-behavior: contain;
  }
  .side.side-open {
    transform: translateX(0);
    box-shadow: 6px 0 28px rgba(0, 0, 0, 0.18);
  }

  .top {
    height: 52px;
    padding: 0 12px;
    gap: 8px;
  }
  .top-title {
    font-size: 14px;
  }
  .top-acts {
    gap: 6px;
    flex-shrink: 0;
  }
  .a {
    padding: 6px 10px;
  }

  .content {
    padding: 12px;
  }

  /* ---------- 底部 tabbar ---------- */
  /* 常驻在 .main 的 flex 流末尾,不 position:fixed —— 这样 .content 的可用高度
     自动减掉 tabbar,滚动到底也不会被盖住;也不需要给 .content 补 padding-bottom。 */
  .tabbar {
    flex-shrink: 0;
    display: flex;
    background: #fff;
    border-top: 1px solid var(--line);
    /* 全面屏底部横条区域留白,否则「菜品/更多」会贴在横条上 */
    padding-bottom: env(safe-area-inset-bottom, 0px);
  }
  .tb {
    flex: 1;
    min-width: 0;
    height: 54px;
    padding: 0;
    border: none;
    background: transparent;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 3px;
    font-size: 11px;
    color: var(--ink-3);
    text-decoration: none;
    cursor: pointer;
    transition: color 0.15s;
  }
  .tb:active {
    background: var(--brand-ghost);
  }
  .tb-ic {
    font-size: 20px;
    line-height: 1;
    display: flex;
    align-items: center;
    justify-content: center;
  }
  .tb-txt {
    line-height: 1;
    white-space: nowrap;
  }
  .tb.on {
    color: var(--brand);
    font-weight: 600;
  }
}
</style>
