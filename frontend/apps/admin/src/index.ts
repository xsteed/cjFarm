// @dining/app-admin —— 管理端(登录 + 后台)。
//
// 对外只暴露 shell 组合路由时需要的两样东西:
//   - 路由表(adminRoutes / NO_PERMISSION_PATH)
//   - 菜单与落地页派生(menu.ts,入参为路由表)
// 页面组件由路由懒加载,不从这里导出。
export { adminRoutes, NO_PERMISSION_PATH } from './routes';
export { menuTree, accessibleMenus, firstAccessiblePath } from './menu';
export type { MenuLeaf, MenuNode, RouteLike } from './menu';
