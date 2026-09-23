<template>
  <div class="admin-shell">
    <!-- 移动端抽屉遮罩:点击空白处收起侧栏 -->
    <transition name="mask-fade">
      <div
        v-if="isMobile && drawerOpen"
        class="side-mask"
        @click="drawerOpen = false"
      ></div>
    </transition>

    <!-- 侧栏:桌面端常驻(顶栏按钮可折叠成窄图标栏);移动端收进左侧抽屉,由顶栏汉堡按钮唤出 -->
    <aside
      class="side"
      :class="{ 'side-open': drawerOpen, collapsed }"
    >
      <div class="logo">
        <div class="logo-dot">点</div>
        <div class="logo-text">
          <div class="logo-name">扫码点餐系统</div>
          <div class="logo-sub">Dining Admin</div>
        </div>
      </div>

      <nav class="menu">
        <template
          v-for="n in tree"
          :key="n.key"
        >
          <!-- 单条目:直接渲染成普通菜单项(组内只剩一个可见条目时也退化成这种) -->
          <router-link
            v-if="n.path"
            :to="n.path"
            class="mi"
            :class="{ on: isActive(n.path) }"
            :title="n.title"
            @click="drawerOpen = false"
          >
            <span class="ic"><component :is="n.icon" /></span>
            <span class="txt">{{ n.title }}</span>
          </router-link>
          <!-- 多条目分组:组头点击开合,当前路由所在组自动展开 -->
          <div
            v-else
            class="mg"
          >
            <button
              class="mg-head"
              :class="{ on: groupActive(n) }"
              :aria-expanded="isGroupOpen(n.key)"
              :title="n.title"
              @click="toggleGroup(n.key)"
            >
              <span class="ic"><component :is="n.icon" /></span>
              <span class="txt">{{ n.title }}</span>
              <span
                class="mg-arrow"
                :class="{ open: isGroupOpen(n.key) }"
              >
                <chevron-down-icon />
              </span>
            </button>
            <div
              v-show="isGroupOpen(n.key)"
              class="mg-body"
            >
              <router-link
                v-for="c in n.children ?? []"
                :key="c.path"
                :to="c.path"
                class="mi sub"
                :class="{ on: isActive(c.path) }"
                @click="drawerOpen = false"
              >
                <span class="txt">{{ c.title }}</span>
              </router-link>
            </div>
          </div>
        </template>
      </nav>

      <!-- 移动端顶栏只留汉堡+标题+点餐页入口,账号操作下沉到抽屉底部;
           版本号固定在侧栏最底部,桌面端与移动端都显示(随 package.json 同步) -->
      <div class="side-foot">
        <div
          v-if="isMobile"
          class="sf-user"
        >
          <user-circle-icon /><span>{{ displayName }}</span>
          <em
            v-if="roleName"
            class="sf-role"
            >{{ roleName }}</em
          >
        </div>
        <button
          v-if="isMobile"
          class="sf-btn danger"
          @click="onLogout"
        >
          退出登录
        </button>
        <div class="side-ver">v{{ appVersion }}</div>
      </div>
    </aside>

    <!-- 主体 -->
    <div class="main">
      <header class="top">
        <!-- 桌面端:侧栏折叠/展开开关(移动端侧栏是抽屉,无折叠概念,故不渲染) -->
        <button
          v-if="!isMobile"
          class="collapse-btn"
          :aria-label="collapsed ? '展开菜单' : '收起菜单'"
          :title="collapsed ? '展开菜单' : '收起菜单'"
          @click="collapsed = !collapsed"
        >
          <menu-unfold-icon v-if="collapsed" />
          <menu-fold-icon v-else />
        </button>
        <button
          v-if="isMobile"
          class="burger"
          aria-label="打开菜单"
          @click="drawerOpen = true"
        >
          <span></span><span></span><span></span>
        </button>
        <div class="top-title">
          {{ currentTitle }}
        </div>
        <div class="top-acts">
          <button
            v-if="canOpenOrderPage"
            class="a ghost"
            @click="openOrderPage"
          >
            <qrcode-icon />
            <span>顾客点餐页</span>
          </button>
          <template v-if="!isMobile">
            <span class="a user">
              <user-circle-icon />
              <span>{{ displayName }}</span>
              <em
                v-if="roleName"
                class="top-role"
                >{{ roleName }}</em
              >
            </span>
            <button
              class="a danger"
              @click="onLogout"
            >
              退出登录
            </button>
          </template>
        </div>
      </header>

      <div class="content">
        <router-view />
      </div>

      <!-- 移动端底部 tabbar:只放最高频的入口,其余菜单收进「更多」打开侧栏抽屉。
           放在 .main 的 flex 流里(而不是 position:fixed),.content 会自动扣掉它的高度,
           不会出现「最后一行被 tabbar 盖住」的老问题。 -->
      <nav
        v-if="isMobile"
        class="tabbar"
      >
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
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { MessagePlugin } from 'tdesign-vue-next';
import { listTables } from '../api';
import { useIsMobile } from '../utils/useMobile';
import { accessibleMenus, menuTree } from '../router';
import type { MenuNode } from '../router';
import { clearAuth, getDisplayName, getRoleName, hasPerm } from '../utils/perm';
import { revokeRemember } from '../utils/remember';
import pkg from '../../package.json';

const route = useRoute();
const router = useRouter();
// isMobile 主断点已扩到 1024px,覆盖手机(iOS 375~430 / 安卓 360~412)与
// 平板竖屏(iPad mini 768 / iPad 810 / iPad Air 834 / iPad Pro 1024):
// 平板竖屏用固定侧栏 220px 会把内容区压到不足 600px,统一走抽屉 + 底部 tabbar。
const { isMobile } = useIsMobile();

// 移动端侧栏抽屉开合状态
const drawerOpen = ref(false);

// 桌面端侧栏折叠状态:收起后只剩图标列(64px),组菜单 hover 弹出子菜单浮层。
// 仅桌面端有意义,移动端不渲染折叠按钮、该状态也不参与布局。
const collapsed = ref(false);

// 路由变化(含浏览器后退、程序跳转)一律收起抽屉,避免新页面被遮罩挡住
watch(
  () => route.path,
  () => {
    drawerOpen.value = false;
  }
);

// 从移动端宽度切回桌面宽度时,抽屉状态没有意义,顺手复位
watch(isMobile, v => {
  if (!v) drawerOpen.value = false;
});

// 菜单直接从路由表派生(见 router/index.ts 的 meta.menuOrder / meta.perm / meta.group):
// 好处是「加一个页面」只需加一条路由,菜单、权限过滤、默认落地页自动跟上,
// 不会出现菜单与权限表各自维护、逐渐漂移的问题。
// 无权限的菜单不渲染;全部无权限时路由守卫会把人送到兜底页。
const menus = computed(() => accessibleMenus());

// 折叠分组菜单树(见 router/index.ts 的 menuTree):单条目直接是一行链接,
// 多条目渲染成「组头 + 可展开子菜单」,侧栏 15 个入口压成 7 行。
const tree = computed(() => menuTree());
// 展开中的分组:默认全部展开(树发生变化时把新出现的分组一并展开),用户可手动收起;
// 被手动收起后若导航进该组子页面,仍自动展开,避免「页面在这里、菜单收着」。
const openGroups = ref<Set<string>>(new Set());
watch(
  tree,
  nodes => {
    for (const n of nodes) {
      if (n.children) openGroups.value.add(n.key);
    }
  },
  { immediate: true }
);
watch(
  () => route.path,
  p => {
    const node = tree.value.find(n => n.children?.some(c => c.path === p));
    if (node) openGroups.value.add(node.key);
  }
);
function isGroupOpen(key: string): boolean {
  return openGroups.value.has(key);
}
function toggleGroup(key: string): void {
  const next = new Set(openGroups.value);
  if (next.has(key)) {
    next.delete(key);
  } else {
    next.add(key);
  }
  openGroups.value = next;
}
function groupActive(n: MenuNode): boolean {
  return !!n.children?.some(c => c.path === route.path);
}

// ---- 移动端底部 tabbar ----
// 只放四个最高频入口(看板/订单/桌台/菜品),其余菜单走「更多」打开抽屉 ——
// 15 个菜单全塞进去会挤成图标条,反而找不到东西。
// 顺序即「餐厅店员掏手机最常看的东西」的顺序。
const TAB_ITEMS = [
  { path: '/dining/dashboard', title: '看板', icon: 'DashboardIcon' },
  { path: '/dining/orders', title: '订单', icon: 'OrderAdjustmentColumnIcon' },
  { path: '/dining/tables', title: '桌台', icon: 'GridViewIcon' },
  { path: '/dining/dishes', title: '菜品', icon: 'RiceIcon' }
];
// 复用 accessibleMenus() 的权限过滤结果,保证无权限的入口不会出现在 tabbar 上
// (否则点进去会被路由守卫弹回,像个坏按钮)。
const tabs = computed(() => {
  const allowed = new Set(menus.value.map(m => m.path));
  return TAB_ITEMS.filter(t => allowed.has(t.path));
});
// 当前路由不在 tabbar 上时(系统配置、员工管理、角色权限…),高亮「更多」
const notInTabbar = computed(() => !tabs.value.some(t => t.path === route.path));

const currentTitle = computed(() => route.meta.title || '');
const displayName = computed(() => getDisplayName());
const roleName = computed(() => getRoleName());
// 侧栏底部展示的版本号,直接取自 package.json,发版改版本号即可同步,不会漂移
const appVersion = pkg.version;
// 「顾客点餐页」按钮内部要调桌台接口,没有 table:view 时点了必然 403,索性不显示。
const canOpenOrderPage = computed(() => hasPerm('table:view'));

function isActive(path: string | undefined): boolean {
  return !!path && route.path === path;
}

async function openOrderPage() {
  try {
    const res = await listTables({ pageNum: 1, pageSize: 1 });
    const rows = res.items || [];
    if (!rows.length) {
      MessagePlugin.warning('暂无桌台，请先到「桌台管理」创建桌台');
      return;
    }
    // 优先使用桌台稳定码(不可枚举),与二维码弹窗的 URL 规则保持一致;缺码的存量桌台回退数字 ID。
    window.open(`/order/${rows[0].tableCode || rows[0].tableId}`, '_blank');
  } catch {
    MessagePlugin.warning('获取桌台失败，请稍后重试');
  }
}

function onLogout() {
  clearAuth();
  // 吊销本设备的「记住我」令牌(清本地 + 服务端删除):只清本地的话,
  // 服务端令牌仍有效至 7/30 天,共用电脑上被抄走的令牌无法靠退出来止损。
  revokeRemember();
  router.replace('/login');
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
  transition: width 0.2s ease;
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
  box-shadow: 0 4px 12px rgb(240 72 31 / 28%);
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

/* ---------- 折叠分组菜单 ---------- */
.mg-head {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 9px 11px;
  border: none;
  border-radius: 10px;
  background: transparent;
  font-size: 13px;
  color: var(--ink-2);
  text-align: left;
  cursor: pointer;
  transition: all 0.15s;
}

.mg-head:hover {
  background: var(--brand-ghost);
  color: var(--brand-deep);
}

/* 组内有激活的子菜单时,组头跟着点亮,避免「页面在这里、菜单看不出在哪」 */
.mg-head.on {
  color: var(--brand-deep);
  font-weight: 600;
}

/* 组头右侧的展开箭头:展开时旋转 180° */
.mg-arrow {
  margin-left: auto;
  display: flex;
  align-items: center;
  font-size: 14px;
  color: var(--ink-3);
  transition: transform 0.18s;
}

.mg-arrow.open {
  transform: rotate(180deg);
}

.mg-body {
  display: flex;
  flex-direction: column;
  gap: 2px;
  margin: 2px 0 4px;
}

/* 子菜单项:文字与父级组头文字左对齐(11 内边距 + 22 图标 + 10 间距 = 43px),
   用小圆点代替图标,读得出「从属于上面分组」的层级。 */
.mi.sub {
  padding: 7px 11px 7px 30px;
  gap: 8px;
}

.mi.sub::before {
  content: '';
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: #d0d0d0;
  flex-shrink: 0;
}

.mi.sub.on::before {
  background: var(--brand-deep);
}

/* ---------- 侧栏折叠(仅桌面端使用) ---------- */
/* 顶栏的折叠/展开开关按钮 */
.collapse-btn {
  width: 32px;
  height: 32px;
  flex-shrink: 0;
  border: 1px solid var(--line);
  border-radius: 9px;
  background: #fff;
  color: var(--ink-2);
  font-size: 18px;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: all 0.15s;
}

.collapse-btn:hover {
  color: var(--brand-deep);
  border-color: #ffd5c4;
  background: var(--brand-ghost);
}

/* 折叠后侧栏收成 64px 图标列;overflow 放开,让 hover 浮层能溢出到侧栏外面 */
.side.collapsed {
  width: 64px;
  padding: 18px 8px;
  overflow: visible;
}

.side.collapsed .logo {
  justify-content: center;
  padding: 0 0 18px;
}

.side.collapsed .logo-text {
  display: none;
}

.side.collapsed .mi,
.side.collapsed .mg-head {
  justify-content: center;
  padding: 9px 0;
}

.side.collapsed .txt,
.side.collapsed .mg-arrow {
  display: none;
}

/* 折叠态下分组子菜单用 hover 浮层展开(纯 CSS):默认隐藏,悬停组头时弹出 */
.side.collapsed .mg {
  position: relative;
}

.side.collapsed .mg-body {
  display: none !important;
  position: absolute;
  left: calc(100% + 8px);
  top: 0;
  width: 168px;
  padding: 6px;
  margin: 0;
  background: #fff;
  border: 1px solid var(--line);
  border-radius: 10px;
  box-shadow: 0 8px 24px rgb(0 0 0 / 12%);
  z-index: 970;
}

.side.collapsed .mg:hover .mg-body {
  display: flex !important;
}

/* 浮层里的子项恢复正常形态:有文字、缩进收窄 */
.side.collapsed .mi.sub {
  padding: 7px 10px;
}

/* 折叠后版本号与底部区域一并隐藏,只留图标列 */
.side.collapsed .side-foot {
  display: none;
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
  background: rgb(0 0 0 / 42%);
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
   紧凑布局(手机 + 平板竖屏):侧栏由常驻列改为左侧抽屉。
   断点取 1024px 而非 767px,是为了覆盖 iPad 竖屏(iPad mini 768 /
   iPad 810 / iPad Air 834 / iPad Pro 1024)——固定侧栏 220px 会把这些
   宽度的内容区压到不足 600px,体验还不如抽屉。
   ============================================================ */
@media (width <= 1024px) {
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
    box-shadow: 6px 0 28px rgb(0 0 0 / 18%);
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

  /* 平板竖屏(768~1024px)内容区更宽,留白恢复到接近桌面,避免卡片顶满整屏 */
  @media (width >= 768px) {
    .content {
      padding: 20px;
    }
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
    padding-bottom: env(safe-area-inset-bottom, 0);
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
