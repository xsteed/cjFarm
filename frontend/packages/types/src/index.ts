// @dining/types —— 后端接口契约的唯一来源(实体 + 分页/响应包装类型)。
// 纯类型，无运行时产物；Web 端与小程序端共用同一份，避免两端各写一套字段名。
export * from './entities';
export * from './api';
