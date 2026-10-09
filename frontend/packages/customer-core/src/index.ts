// @dining/customer-core —— 顾客点餐端的「业务逻辑层」(不含任何 UI 组件)。
//
// 这一层刻意不 import TDesign、不直接引用 DOM，因此:
//   - H5 顾客端(apps/customer)直接复用;
//   - 未来的微信小程序(apps/miniprogram)只需换掉 UI 与平台适配器
//     (setUiNotifier / setVisibilityAdapter)，业务逻辑不再重写。
//
// 纯 DOM 相关的交互(如菜单滚动联动 useScrollSpy)属于「Web 版 UI 逻辑」，
// 留在 apps/customer 内，不进本包。
export * from './search';
export * from './spec';
export * from './useCart';
export * from './useOrderPolling';
export * from './useUrge';
export * from './visibility';
