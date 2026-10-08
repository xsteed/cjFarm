// @dining/shared —— 零依赖的通用层:登录态存储键、安全存储、金额/展示格式化、
// H5 地址校验、订单状态常量与展示字典、UI 通知适配器。
//
// 这里只允许放「不依赖 Vue 组件 / 不依赖 TDesign / 不依赖 DOM 结构」的代码，
// 这样 Web 端与未来的小程序端都能直接引用。
export * from './authKeys';
export * from './safeStorage';
export * from './money';
export * from './orderDisplay';
export * from './h5Base';
export * from './orderStatus';
export * from './dicts';
export * from './ui';
