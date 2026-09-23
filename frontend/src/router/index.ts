import { createRouter, createWebHistory } from 'vue-router';
import type { RouteRecordRaw } from 'vue-router';
import { clearAuth, hasPerm, hasToken, permsLoaded, refreshProfile, tokenExpired } from '../utils/perm';

// 路由 meta 的统一类型声明（模块增强，全局生效）。菜单、守卫与 AdminLayout 共用。
declare module 'vue-router' {
  interface RouteMeta {
    title?: string;
    perm?: string;
    icon?: string;
    menuOrder?: number;
    /** 菜单分组 key(见 MENU_GROUPS):同组的菜单收进一个可折叠子菜单;不填即一级菜单。 */
    group?: string;
    requiresAuth?: boolean;
  }
}

// ⚠️ 菜单与路由使用**同一份定义**:侧栏菜单由 AdminLayout 从路由表里筛出
//    带 meta.menuOrder 的记录(按 meta.group 聚成折叠组、组内按 menuOrder 排序),
//    不再另维护一个菜单数组。这样「新增一个页面」只需在这里加一条路由,
//    菜单、路由守卫、默认落地页自动跟上,不会出现「菜单加了但权限忘了配」的漂移。
//
// meta 字段说明:
//   title     页面标题(同时用于浏览器标题与侧栏文字)
//   perm      访问所需权限码;留空表示登录即可访问(仅 NoPermission 这类兜底页)
//   icon      侧栏图标名(在 icons.ts 里定义、main.ts 全局注册的 tdesign 图标组件名)
//   group     菜单分组 key(见 MENU_GROUPS):同组菜单折叠成一个子菜单;不填即一级菜单
//   menuOrder 排序号;不填表示不进侧栏(分组行序取组内最小值)
const NO_PERMISSION_PATH = '/dining/no-permission';

/**
 * 侧栏菜单分组:把同类的低频入口收进可折叠子菜单,一级菜单从 15 行压到 7 行。
 * 数组顺序即分组在侧栏的先后;组内条目按各自 menuOrder 排序。
 * 分组只影响「展示」:权限过滤仍在每条路由的 meta.perm 上,路由守卫不变;
 * 某组只剩一个可见条目时,侧栏会把它退化成普通菜单项,不多套一层折叠。
 */
const MENU_GROUPS: ReadonlyArray<{ key: string; title: string; icon: string }> = [
  { key: 'order', title: '订单中心', icon: 'OrderAdjustmentColumnIcon' },
  { key: 'dish', title: '菜品中心', icon: 'RiceIcon' },
  { key: 'print', title: '打印中心', icon: 'PrintIcon' },
  { key: 'system', title: '系统管理', icon: 'SettingIcon' }
];

const routes: RouteRecordRaw[] = [
  {
    path: '/',
    redirect: '/dining/dashboard'
  },
  {
    path: '/login',
    name: 'Login',
    component: () => import('../views/Login.vue'),
    meta: { title: '登录' }
  },
  {
    path: '/order/:tableId',
    name: 'OrderPage',
    component: () => import('../views/OrderPage.vue'),
    meta: { title: '扫码点餐' }
  },
  {
    path: '/printTicket',
    name: 'PrintTicket',
    component: () => import('../views/PrintTicket.vue'),
    meta: { title: '打印票据' }
  },
  {
    path: '/dining',
    component: () => import('../layout/AdminLayout.vue'),
    redirect: '/dining/dashboard',
    meta: { requiresAuth: true },
    children: [
      {
        path: 'dashboard',
        name: 'Dashboard',
        component: () => import('../views/Dashboard.vue'),
        // 仪表盘展示订单与菜品概览,故要求订单查看权限;
        // 无此权限的角色登录后会落到「第一个有权限的菜单」(见 firstAccessiblePath)。
        meta: { title: '仪表盘', perm: 'order:view', icon: 'DashboardIcon', menuOrder: 1 }
      },
      {
        path: 'orders',
        name: 'Orders',
        component: () => import('../views/Orders.vue'),
        meta: { title: '订单管理', perm: 'order:view', icon: 'OrderAdjustmentColumnIcon', menuOrder: 2, group: 'order' }
      },
      {
        path: 'tables',
        name: 'Tables',
        component: () => import('../views/Tables.vue'),
        // 桌台用「四格桌位布局」而不是 Table2Icon —— 后者是电子表格图形，
        // 既读不出「餐桌」语义，笔画也比相邻菜单重一大截，视觉上像坏了一张图。
        meta: { title: '桌台管理', perm: 'table:view', icon: 'GridViewIcon', menuOrder: 3 }
      },
      {
        path: 'dishes',
        name: 'Dishes',
        component: () => import('../views/Dishes.vue'),
        meta: { title: '菜品管理', perm: 'dish:view', icon: 'RiceIcon', menuOrder: 4, group: 'dish' }
      },
      {
        path: 'categories',
        name: 'Categories',
        component: () => import('../views/Categories.vue'),
        meta: { title: '分类管理', perm: 'category:view', icon: 'ViewListIcon', menuOrder: 5, group: 'dish' }
      },
      {
        path: 'remarks',
        name: 'Remarks',
        component: () => import('../views/Remarks.vue'),
        meta: { title: '备注管理', perm: 'remark:view', icon: 'Edit2Icon', menuOrder: 6, group: 'dish' }
      },
      {
        path: 'credit',
        name: 'Credit',
        component: () => import('../views/Credit.vue'),
        meta: { title: '挂账管理', perm: 'credit:view', icon: 'MoneyIcon', menuOrder: 7, group: 'order' }
      },
      {
        path: 'report',
        name: 'Report',
        component: () => import('../views/Report.vue'),
        meta: { title: '数据报表', perm: 'report:view', icon: 'ChartBarIcon', menuOrder: 8 }
      },
      {
        path: 'printers',
        name: 'Printers',
        component: () => import('../views/Printers.vue'),
        meta: { title: '打印机管理', perm: 'printer:view', icon: 'PrintIcon', menuOrder: 9, group: 'print' }
      },
      {
        path: 'printlogs',
        name: 'PrintLogs',
        component: () => import('../views/PrintLogs.vue'),
        // 补打动作本身还要求 printer:edit(后端接口已按此登记)。
        meta: { title: '打印记录', perm: 'printer:view', icon: 'FileIcon', menuOrder: 10, group: 'print' }
      },
      {
        path: 'config',
        name: 'Config',
        component: () => import('../views/Config.vue'),
        meta: { title: '系统配置', perm: 'config:view', icon: 'SettingIcon', menuOrder: 11, group: 'system' }
      },
      {
        path: 'users',
        name: 'Users',
        component: () => import('../views/Users.vue'),
        meta: { title: '员工管理', perm: 'user:view', icon: 'UserIcon', menuOrder: 12, group: 'system' }
      },
      {
        path: 'roles',
        name: 'Roles',
        component: () => import('../views/Roles.vue'),
        meta: { title: '角色权限', perm: 'role:view', icon: 'LockOnIcon', menuOrder: 13, group: 'system' }
      },
      {
        path: 'operlogs',
        name: 'OperLogs',
        component: () => import('../views/OperLogs.vue'),
        // 操作日志只给「能查」的人看;清理按钮在页面内再按 log:manage 二次判断。
        meta: { title: '操作日志', perm: 'log:view', icon: 'HistoryIcon', menuOrder: 14, group: 'system' }
      },
      {
        // 登录设备:管理自己的「记住我」免登录会话(查看/吊销),不设权限点,登录即可。
        path: 'devices',
        name: 'Devices',
        component: () => import('../views/Devices.vue'),
        meta: { title: '登录设备', icon: 'UserCircleIcon', menuOrder: 15, group: 'system' }
      },
      {
        // 修改密码:个人账号设置,不设权限点,登录即可访问。
        path: 'change-password',
        name: 'ChangePassword',
        component: () => import('../views/ChangePassword.vue'),
        meta: { title: '修改密码', icon: 'LockOnIcon', menuOrder: 16, group: 'system' }
      },
      {
        // 兜底页:账号一个可用菜单都没有时落在这里,避免「空白布局」式的白屏。
        path: 'no-permission',
        name: 'NoPermission',
        component: () => import('../views/NoPermission.vue'),
        meta: { title: '无可用功能' }
      }
    ]
  }
];

const router = createRouter({
  history: createWebHistory(),
  routes
});

/** 侧栏叶子菜单(由路由表派生的一条记录)。 */
export interface MenuLeaf {
  path: string;
  title: string;
  icon: string;
  menuOrder: number;
}

/** 侧栏节点:单条目(path)直接渲染为普通菜单项;多条目(children)渲染为可折叠分组。 */
export interface MenuNode {
  /** 稳定标识:分组 key 或(未分组条目的)路由路径。 */
  key: string;
  title: string;
  icon: string;
  path?: string;
  children?: MenuLeaf[];
}

interface MenuEntry {
  leaf: MenuLeaf;
  group?: string;
}

/** 全部「有权限访问」的菜单叶子(已按权限过滤,保留分组标记)。 */
function accessibleEntries(): MenuEntry[] {
  return router
    .getRoutes()
    .filter(r => r.meta?.menuOrder && (!r.meta.perm || hasPerm(r.meta.perm)))
    .map(r => ({
      leaf: {
        path: r.path,
        title: r.meta.title ?? '',
        icon: r.meta.icon ?? '',
        menuOrder: r.meta.menuOrder ?? 0
      },
      group: r.meta.group
    }));
}

/**
 * 把叶子按「分组」聚合成侧栏的一行单元:有分组的收进一组,无分组的各自一行;
 * 行序 = 组内最小 menuOrder(仪表盘 1 → 订单中心 2 → …),组内按 menuOrder 排。
 * 分组只影响展示:组里只剩一个可见条目时整组退化成普通菜单项,不多套一层。
 */
function menuUnits(): Array<{ key: string; entries: MenuEntry[] }> {
  const all = accessibleEntries();
  const byGroup = new Map<string, MenuEntry[]>();
  const singles: MenuEntry[] = [];
  for (const e of all) {
    if (e.group) {
      const arr = byGroup.get(e.group) ?? [];
      arr.push(e);
      byGroup.set(e.group, arr);
    } else {
      singles.push(e);
    }
  }
  const known = MENU_GROUPS.filter(g => byGroup.has(g.key)).map(g => ({ key: g.key, entries: byGroup.get(g.key)! }));
  // 路由 meta 里写了 group 但没在 MENU_GROUPS 登记的分组:不静默丢菜单,兜底成一单元。
  const unknown = [...byGroup.keys()].filter(k => !MENU_GROUPS.some(g => g.key === k));
  const units = [
    ...known,
    ...unknown.map(k => ({ key: k, entries: byGroup.get(k)! })),
    ...singles.map(e => ({ key: e.leaf.path, entries: [e] }))
  ];
  const minOrder = (u: { entries: MenuEntry[] }) => Math.min(...u.entries.map(e => e.leaf.menuOrder));
  units.sort((a, b) => minOrder(a) - minOrder(b));
  for (const u of units) {
    u.entries.sort((a, b) => a.leaf.menuOrder - b.leaf.menuOrder);
  }
  return units;
}

/** 侧栏菜单树(分组 + 单条目),供 AdminLayout 渲染折叠菜单。 */
export function menuTree(): MenuNode[] {
  return menuUnits().map(u => {
    const first = u.entries[0].leaf;
    if (u.entries.length === 1) {
      return { key: u.key, title: first.title, icon: first.icon, path: first.path };
    }
    const g = MENU_GROUPS.find(x => x.key === u.key);
    return {
      key: u.key,
      title: g?.title ?? first.title,
      icon: g?.icon ?? first.icon,
      children: u.entries.map(e => e.leaf)
    };
  });
}

/** 侧栏菜单的扁平叶子列表(含分组内条目),用于底部 tabbar 过滤与默认落地页。 */
export function accessibleMenus(): MenuLeaf[] {
  return menuUnits().flatMap(u => u.entries.map(e => e.leaf));
}

/** 第一个「有权限访问」的菜单路径;一个都没有时返回兜底页路径。 */
export function firstAccessiblePath(): string {
  const menu = accessibleMenus();
  return menu.length ? menu[0].path : NO_PERMISSION_PATH;
}

// 全局守卫:
//   1) 管理端需登录 → 未登录跳登录页(带上 redirect 便于登录后回到原页面);
//   2) 权限未加载(localStorage 缺失)"或本次会话首次进入" → 拉一次自己的权限,
//      这样商户改过某人的角色后,该员工刷新页面即可收敛,不会一直看到过期菜单;
//   3) 目标页面超出权限 → 落到「第一个有权限的菜单」,一个都没有才显示无权限页。
//
// 这样即便员工手敲了没有权限的地址,看到的也是自己能用的页面,而不是功能入口一片空白。
let profileRefreshed = false;

router.beforeEach(async to => {
  const requiresAuth = to.matched.some(r => r.meta.requiresAuth);

  // 令牌缺失、或本地即可判定已过期(解析令牌 payload 的 exp,见 perm.tokenExpired):
  // 直接在 SPA 内带 redirect 去登录页。否则过期令牌会先进页面、发请求撞 401、
  // 被拦截器整页弹回登录页,白绕一圈;这里一次跳转就位,「记住我」静默换发后
  // 还能凭 redirect 回到原页面。服务端侧失效(改密/停用/后端重启)仍走 401 兜底。
  if (requiresAuth && (!hasToken() || tokenExpired())) {
    return { path: '/login', query: { redirect: to.fullPath } };
  }
  if (to.path === '/login') {
    // 本地令牌已过期时别送去 dashboard —— 那里第一次请求就会 401,
    // 被拦截器整页弹回登录页,白绕一圈;留在登录页让「记住我」静默换发
    // (若令牌仍有效)或手动登录接手。
    return hasToken() && !tokenExpired() ? '/dining/dashboard' : true;
  }
  if (!requiresAuth) {
    return true;
  }

  // 本次会话首次进入管理端时刷新一次权限;之后沿用缓存,避免每次跳转都打接口。
  if (!permsLoaded() || !profileRefreshed) {
    profileRefreshed = true;
    try {
      await refreshProfile();
    } catch {
      // 401 已由 axios 拦截器统一处理(清登录态并跳登录页);
      // 其它错误(网络抖动等)只要本地还有缓存就继续用,不把人踢出去。
      if (!permsLoaded()) {
        clearAuth();
        return { path: '/login', query: { redirect: to.fullPath } };
      }
    }
  }

  const perm = to.meta.perm;
  if (perm && !hasPerm(perm)) {
    const fallback = firstAccessiblePath();
    return { path: fallback === to.path ? NO_PERMISSION_PATH : fallback };
  }
  return true;
});

router.afterEach(to => {
  if (to.meta.title) {
    document.title = `${to.meta.title} - 扫码点餐管理系统`;
  }
});

export default router;
