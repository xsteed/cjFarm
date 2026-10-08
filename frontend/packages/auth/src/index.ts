// @dining/auth —— 管理端登录态与权限(前端侧)。
//
// 抽取成独立包的原因:路由守卫(shell)、侧栏菜单(apps/admin)、按钮级判断
// (apps/admin 各页)都要读同一份权限缓存,而它又依赖 @dining/api 的 profile 接口。
// 放在包里可以避免「shell 反向 import 某个 app」这种循环依赖。
//
// 注意:前端权限只用于「菜单/按钮是否显示」的体验优化,真正的安全边界在后端
// (AdminAuth 401 + RequirePerm 403)。
export * from './perm';
export * from './remember';
