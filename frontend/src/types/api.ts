import type { AxiosRequestConfig } from 'axios';

export type ApiResponse<T = unknown> = {
  code: number;
  msg: string;
  data: T;
};

// PageResult 是拦截器解包后分页接口返回的数据形态:
// 后端统一结构为 { code, msg, data: { total, items } },
// 解包后调用方拿到的即 data 本身,即 { total, items }。
export type PageResult<T = unknown> = {
  total: number;
  items: T[];
};

export type PageQuery = {
  pageNum?: number;
  pageSize?: number;
  [k: string]: unknown;
};

export interface SilentRequestConfig<D = unknown> extends AxiosRequestConfig<D> {
  silent?: boolean;
}

declare module 'axios' {
  // 泛型参数名与 any 默认值必须与 axios 原生声明逐字一致(模块增强按位置合并),
  // 无法用 unknown/其它名字替代 —— 关闭两条规则而不是改签名。
  // eslint-disable-next-line @typescript-eslint/no-unused-vars, @typescript-eslint/no-explicit-any
  export interface AxiosRequestConfig<D = any, P = any> {
    silent?: boolean;
  }
}
