import { createRouter, createWebHistory } from 'vue-router'
import { clearAuth, hasPerm, hasToken, permsLoaded, refreshProfile } from '../utils/perm'

// ⚠️ 菜单与路由使用**同一份定义**:侧栏菜单由 AdminLayout 从路由表里筛出
//    带 meta.menu 的记录(按 meta.menuOrder 排序),不再另维护一个菜单数组。
//    这样「新增一个页面」只需在这里加一条路由,菜单、路由守卫、默认落地页自动跟上,
//    不会出现「菜单加了但权限忘了配」或反之的漂移。
//
// meta 字段说明:
//   title     页面标题(同时用于浏览器标题与侧栏文字)
//   perm      访问所需权限码;留空表示登录即可访问(仅 NoPermission 这类兜底页)
//   icon      侧栏图标名(在 main.js 里全局注册的 tdesign 图标组件名)
//   menuOrder 侧栏排序;不填表示不进侧栏
const NO_PERMISSION_PATH = '/dining/no-permission'

const routes = [
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
        path: 'dashboard', name: 'Dashboard',
        component: () => import('../views/Dashboard.vue'),
        // 仪表盘展示订单与菜品概览,故要求订单查看权限;
        // 无此权限的角色登录后会落到「第一个有权限的菜单」(见 firstAccessiblePath)。
        meta: { title: '仪表盘', perm: 'order:view', icon: 'DashboardIcon', menuOrder: 1 }
      },
      {
        path: 'orders', name: 'Orders',
        component: () => import('../views/Orders.vue'),
        meta: { title: '订单管理', perm: 'order:view', icon: 'OrderAdjustmentColumnIcon', menuOrder: 2 }
      },
      {
        path: 'tables', name: 'Tables',
        component: () => import('../views/Tables.vue'),
        // 桌台用「四格桌位布局」而不是 Table2Icon —— 后者是电子表格图形，
        // 既读不出「餐桌」语义，笔画也比相邻菜单重一大截，视觉上像坏了一张图。
        meta: { title: '桌台管理', perm: 'table:view', icon: 'GridViewIcon', menuOrder: 3 }
      },
      {
        path: 'dishes', name: 'Dishes',
        component: () => import('../views/Dishes.vue'),
        meta: { title: '菜品管理', perm: 'dish:view', icon: 'RiceIcon', menuOrder: 4 }
      },
      {
        path: 'categories', name: 'Categories',
        component: () => import('../views/Categories.vue'),
        meta: { title: '分类管理', perm: 'category:view', icon: 'ViewListIcon', menuOrder: 5 }
      },
      {
        path: 'remarks', name: 'Remarks',
        component: () => import('../views/Remarks.vue'),
        meta: { title: '备注管理', perm: 'remark:view', icon: 'Edit2Icon', menuOrder: 6 }
      },
      {
        path: 'credit', name: 'Credit',
        component: () => import('../views/Credit.vue'),
        meta: { title: '挂账管理', perm: 'credit:view', icon: 'MoneyIcon', menuOrder: 7 }
      },
      {
        path: 'report', name: 'Report',
        component: () => import('../views/Report.vue'),
        meta: { title: '数据报表', perm: 'report:view', icon: 'ChartBarIcon', menuOrder: 8 }
      },
      {
        path: 'printers', name: 'Printers',
        component: () => import('../views/Printers.vue'),
        meta: { title: '打印机管理', perm: 'printer:view', icon: 'PrintIcon', menuOrder: 9 }
      },
      {
        path: 'printlogs', name: 'PrintLogs',
        component: () => import('../views/PrintLogs.vue'),
        // 补打动作本身还要求 printer:edit(后端接口已按此登记)。
        meta: { title: '打印记录', perm: 'printer:view', icon: 'FileIcon', menuOrder: 10 }
      },
      {
        path: 'config', name: 'Config',
        component: () => import('../views/Config.vue'),
        meta: { title: '系统配置', perm: 'config:view', icon: 'SettingIcon', menuOrder: 11 }
      },
      {
        path: 'users', name: 'Users',
        component: () => import('../views/Users.vue'),
        meta: { title: '员工管理', perm: 'user:view', icon: 'UserIcon', menuOrder: 12 }
      },
      {
        path: 'roles', name: 'Roles',
        component: () => import('../views/Roles.vue'),
        meta: { title: '角色权限', perm: 'role:view', icon: 'LockOnIcon', menuOrder: 13 }
      },
      {
        path: 'operlogs', name: 'OperLogs',
        component: () => import('../views/OperLogs.vue'),
        // 操作日志只给「能查」的人看;清理按钮在页面内再按 log:manage 二次判断。
        meta: { title: '操作日志', perm: 'log:view', icon: 'HistoryIcon', menuOrder: 14 }
      },
      {
        // 兜底页:账号一个可用菜单都没有时落在这里,避免「空白布局」式的白屏。
        path: 'no-permission', name: 'NoPermission',
        component: () => import('../views/NoPermission.vue'),
        meta: { title: '无可用功能' }
      }
    ]
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

/** 侧栏菜单项(由路由表派生,按 menuOrder 排序,已按权限过滤)。 */
export function accessibleMenus() {
  return router
    .getRoutes()
    .filter((r) => r.meta && r.meta.menuOrder)
    .sort((a, b) => a.meta.menuOrder - b.meta.menuOrder)
    .filter((r) => !r.meta.perm || hasPerm(r.meta.perm))
    .map((r) => ({ path: r.path, title: r.meta.title, icon: r.meta.icon }))
}

/** 第一个「有权限访问」的菜单路径;一个都没有时返回兜底页路径。 */
export function firstAccessiblePath() {
  const menu = accessibleMenus()
  return menu.length ? menu[0].path : NO_PERMISSION_PATH
}

// 全局守卫:
//   1) 管理端需登录 → 未登录跳登录页(带上 redirect 便于登录后回到原页面);
//   2) 权限未加载(localStorage 缺失)"或本次会话首次进入" → 拉一次自己的权限,
//      这样商户改过某人的角色后,该员工刷新页面即可收敛,不会一直看到过期菜单;
//   3) 目标页面超出权限 → 落到「第一个有权限的菜单」,一个都没有才显示无权限页。
//
// 这样即便员工手敲了没有权限的地址,看到的也是自己能用的页面,而不是功能入口一片空白。
let profileRefreshed = false

router.beforeEach(async (to) => {
  const requiresAuth = to.matched.some((r) => r.meta.requiresAuth)

  if (requiresAuth && !hasToken()) {
    return { path: '/login', query: { redirect: to.fullPath } }
  }
  if (to.path === '/login') {
    return hasToken() ? '/dining/dashboard' : true
  }
  if (!requiresAuth) {
    return true
  }

  // 本次会话首次进入管理端时刷新一次权限;之后沿用缓存,避免每次跳转都打接口。
  if (!permsLoaded() || !profileRefreshed) {
    profileRefreshed = true
    try {
      await refreshProfile()
    } catch {
      // 401 已由 axios 拦截器统一处理(清登录态并跳登录页);
      // 其它错误(网络抖动等)只要本地还有缓存就继续用,不把人踢出去。
      if (!permsLoaded()) {
        clearAuth()
        return { path: '/login', query: { redirect: to.fullPath } }
      }
    }
  }

  const perm = to.meta.perm
  if (perm && !hasPerm(perm)) {
    const fallback = firstAccessiblePath()
    return { path: fallback === to.path ? NO_PERMISSION_PATH : fallback }
  }
  return true
})

router.afterEach((to) => {
  if (to.meta.title) {
    document.title = `${to.meta.title} - 扫码点餐管理系统`
  }
})

export default router
