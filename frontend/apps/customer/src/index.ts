// @dining/app-customer —— 顾客点餐端(H5)。
//
// 目录边界即「未来小程序要替换的范围」:
//   src/routes.ts          路由(小程序换成 pages.json / 自己的路由表)
//   src/OrderPage.vue      点餐主页(菜单 + 购物车 + 订单跟踪 + 支付)
//   src/order/*            页面组件
//   src/composables/*      Web 专用的 UI 逻辑(如滚动联动)
// 而可跨端复用的接口、类型与业务逻辑在 @dining/{api,types,customer-core}。
export { customerRoutes } from './routes';
