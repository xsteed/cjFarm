export * from './http';
export * from './auth';
export * from './user';
export * from './customer';
export * from './member';
export * from './wallet';
export * from './order';
export * from './printer';
export * from './report';
export * from './visit';
export * from './common';
// 页面历史上习惯 `import { ORDER_STATUS } from '../api'`(字典与接口同源)，这里保留该出口。
// 只转出 dicts，不做整包 `export * from '@dining/shared'` —— 后者会把存储键、金额工具等
// 一起塞进接口包，既造成命名面污染，也容易与接口同名导出冲突。
export * from '@dining/shared/dicts';
