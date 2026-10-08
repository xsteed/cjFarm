import type { RouteRecordRaw } from 'vue-router';

// ⚠️ 菜单与路由使用**同一份定义**:侧栏菜单由 AdminLayout 从路由表里筛出
//    带 meta.menuOrder 的记录(按 meta.group 聚成折叠组、组内按 menuOrder 排序),
//    不再另维护一个菜单数组。这样「新增一个页面」只需在这里加一条路由,
//    菜单、路由守卫、默认落地页自动跟上,不会出现「菜单加了但权限忘了配」的漂移。
//
// meta 字段说明:
//   title     页面标题(同时用于浏览器标题与侧栏文字)
//   perm      访问所需权限码;留空表示登录即可访问(仅 NoPermission 这类兜底页)
//   icon      侧栏图标名(在 shell 的 src/icons.ts 里定义、main.ts 全局注册的 tdesign 图标组件名)
//   group     菜单分组 key(见 menu.ts 的 MENU_GROUPS):同组菜单折叠成一个子菜单;不填即一级菜单
//   menuOrder 排序号;不填表示不进侧栏(分组行序取组内最小值)

/** 账号一个可用菜单都没有时的兜底页路径(见 menu.ts 的 firstAccessiblePath)。 */
export const NO_PERMISSION_PATH = '/dining/no-permission';

export const adminRoutes: RouteRecordRaw[] = [
  {
    path: '/',
    redirect: '/dining/dashboard'
  },
  {
    path: '/login',
    name: 'Login',
    component: () => import('./views/Login.vue'),
    meta: { title: '登录' }
  },
  {
    path: '/dining',
    component: () => import('./layout/AdminLayout.vue'),
    redirect: '/dining/dashboard',
    meta: { requiresAuth: true },
    children: [
      {
        path: 'dashboard',
        name: 'Dashboard',
        component: () => import('./views/Dashboard.vue'),
        // 仪表盘展示订单与菜品概览,故要求订单查看权限;
        // 无此权限的角色登录后会落到「第一个有权限的菜单」(见 menu.ts 的 firstAccessiblePath)。
        meta: { title: '仪表盘', perm: 'order:view', icon: 'DashboardIcon', menuOrder: 1 }
      },
      {
        path: 'orders',
        name: 'Orders',
        component: () => import('./views/Orders.vue'),
        meta: { title: '订单管理', perm: 'order:view', icon: 'OrderAdjustmentColumnIcon', menuOrder: 2, group: 'order' }
      },
      {
        path: 'tables',
        name: 'Tables',
        component: () => import('./views/Tables.vue'),
        // 桌台用「四格桌位布局」而不是 Table2Icon —— 后者是电子表格图形，
        // 既读不出「餐桌」语义，笔画也比相邻菜单重一大截，视觉上像坏了一张图。
        meta: { title: '桌台管理', perm: 'table:view', icon: 'GridViewIcon', menuOrder: 3 }
      },
      {
        path: 'dishes',
        name: 'Dishes',
        component: () => import('./views/Dishes.vue'),
        meta: { title: '菜品管理', perm: 'dish:view', icon: 'RiceIcon', menuOrder: 4, group: 'dish' }
      },
      {
        path: 'categories',
        name: 'Categories',
        component: () => import('./views/Categories.vue'),
        meta: { title: '分类管理', perm: 'category:view', icon: 'ViewListIcon', menuOrder: 5, group: 'dish' }
      },
      {
        path: 'remarks',
        name: 'Remarks',
        component: () => import('./views/Remarks.vue'),
        meta: { title: '备注管理', perm: 'remark:view', icon: 'Edit2Icon', menuOrder: 6, group: 'dish' }
      },
      {
        path: 'credit',
        name: 'Credit',
        component: () => import('./views/Credit.vue'),
        meta: { title: '挂账管理', perm: 'credit:view', icon: 'MoneyIcon', menuOrder: 7, group: 'order' }
      },
      {
        path: 'report',
        name: 'Report',
        component: () => import('./views/Report.vue'),
        meta: { title: '数据报表', perm: 'report:view', icon: 'ChartBarIcon', menuOrder: 8 }
      },
      {
        path: 'printers',
        name: 'Printers',
        component: () => import('./views/Printers.vue'),
        meta: { title: '打印机管理', perm: 'printer:view', icon: 'PrintIcon', menuOrder: 9, group: 'print' }
      },
      {
        path: 'printlogs',
        name: 'PrintLogs',
        component: () => import('./views/PrintLogs.vue'),
        // 补打动作本身还要求 printer:edit(后端接口已按此登记)。
        meta: { title: '打印记录', perm: 'printer:view', icon: 'FileIcon', menuOrder: 10, group: 'print' }
      },
      {
        path: 'config',
        name: 'Config',
        component: () => import('./views/Config.vue'),
        meta: { title: '系统配置', perm: 'config:view', icon: 'SettingIcon', menuOrder: 11, group: 'system' }
      },
      {
        path: 'users',
        name: 'Users',
        component: () => import('./views/Users.vue'),
        meta: { title: '员工管理', perm: 'user:view', icon: 'UserIcon', menuOrder: 12, group: 'system' }
      },
      {
        path: 'roles',
        name: 'Roles',
        component: () => import('./views/Roles.vue'),
        meta: { title: '角色权限', perm: 'role:view', icon: 'LockOnIcon', menuOrder: 13, group: 'system' }
      },
      {
        path: 'operlogs',
        name: 'OperLogs',
        component: () => import('./views/OperLogs.vue'),
        // 操作日志只给「能查」的人看;清理按钮在页面内再按 log:manage 二次判断。
        meta: { title: '操作日志', perm: 'log:view', icon: 'HistoryIcon', menuOrder: 14, group: 'system' }
      },
      {
        // 登录设备:管理自己的「记住我」免登录会话(查看/吊销),不设权限点,登录即可。
        path: 'devices',
        name: 'Devices',
        component: () => import('./views/Devices.vue'),
        meta: { title: '登录设备', icon: 'UserCircleIcon', menuOrder: 15, group: 'system' }
      },
      {
        // 修改密码:个人账号设置,不设权限点,登录即可访问。
        path: 'change-password',
        name: 'ChangePassword',
        component: () => import('./views/ChangePassword.vue'),
        meta: { title: '修改密码', icon: 'LockOnIcon', menuOrder: 16, group: 'system' }
      },
      {
        // 兜底页:账号一个可用菜单都没有时落在这里,避免「空白布局」式的白屏。
        path: 'no-permission',
        name: 'NoPermission',
        component: () => import('./views/NoPermission.vue'),
        meta: { title: '无可用功能' }
      }
    ]
  }
];
