import axios from 'axios';
import type { AxiosError, AxiosResponse } from 'axios';
import { MessagePlugin } from 'tdesign-vue-next';
import { LS_TOKEN, AUTH_KEYS } from '../utils/authKeys';
import { safeStorage } from '../utils/safeStorage';

// 后端路由体系已重构为 /api/* 分组(见 backend/main.go setupAPI):
// /api/auth/* 登录、/api/customer/* 顾客端、/api/admin/* 管理端、/api/common/upload 上传。
// vite dev proxy 与生产 Nginx 均已按 /api 前缀对齐。
export const baseURL = '/api';

export const http = axios.create({
  baseURL,
  timeout: 15000
});

// 请求拦截:自动携带登录令牌
http.interceptors.request.use(config => {
  const token = safeStorage.get(LS_TOKEN);
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

// HTTP 状态码兜底文案。后端业务错误已带中文 msg,这里的映射只在
// 拿不到 msg(网关/代理/超时等)时生效,避免把英文技术文案抛给用户。
export const HTTP_FALLBACK_MSG: Record<number, string> = {
  400: '请求有误，请刷新后重试',
  401: '登录状态已过期，请重新登录',
  403: '没有操作权限',
  404: '请求的内容不存在',
  408: '请求超时，请检查网络后重试',
  500: '服务繁忙，请稍后重试',
  502: '服务正在重启，请稍后重试',
  503: '服务暂时不可用，请稍后重试',
  504: '服务响应超时，请稍后重试'
};

type ApiErrorBody = {
  msg?: string;
};

type ApiSuccessBody = {
  code?: number;
  msg?: string;
  data?: unknown;
};

type FriendlyAxiosError = AxiosError<ApiErrorBody> & {
  friendlyMessage?: string;
};

// friendlyMessage 把 axios 抛出的英文技术错误转换为中文可读文案。
// 优先级:业务 msg > 超时 > 状态码映射 > 状态码兜底 > 网络失败。
export function friendlyMessage(err: AxiosError<ApiErrorBody>): string {
  const bizMsg = err.response?.data?.msg;
  if (bizMsg) return bizMsg;
  if (err.code === 'ECONNABORTED') return '请求超时，请检查网络后重试';
  const status = err.response?.status;
  if (status && HTTP_FALLBACK_MSG[status]) return HTTP_FALLBACK_MSG[status];
  if (status) return `请求失败（${status}），请稍后重试`;
  return '网络连接失败，请检查网络后重试';
}

// 响应拦截:统一处理业务码与错误。
// config 里传 { silent: true } 可跳过全局错误 toast,由调用方自行处置 ——
// 用于「页面打开时的后台尝试」类请求(如登录页的静默换发),失败不该打扰用户。
// 拦截器把 AxiosResponse 解包成业务数据(与各接口函数的 R 泛型呼应):
// 运行时返回的是解包后的 body,类型上仍是 AxiosResponse —— 用断言桥接,
// axios 拦截器签名要求 fulfilled 返回 AxiosResponse | Promise<AxiosResponse>。
const unwrapResponse = ((res: AxiosResponse) => {
  const data = res.data as ApiSuccessBody | undefined;
  if (data && data.code === 200) {
    // 带数据的对象结果(含分页:{ total, items } 整体收在 data 下)
    if (data.data !== undefined) return data.data;
    // 仅消息结果(如保存成功)
    return data;
  }
  const msg = data?.msg || '请求失败';
  if (!res.config?.silent) MessagePlugin.error(msg);
  return Promise.reject(new Error(msg));
}) as unknown as Parameters<typeof http.interceptors.response.use>[0];

http.interceptors.response.use(unwrapResponse, err => {
  const error = err as FriendlyAxiosError;
  if (error.response?.status === 401) {
    // 令牌失效:清掉全部登录态键(含权限/角色缓存)。
    // 只删令牌会留下过期的 admin_perms,下次进管理端菜单会先按旧权限渲染一下。
    for (const k of AUTH_KEYS) {
      safeStorage.remove(k);
    }
    if (!location.pathname.startsWith('/login')) {
      // 带 redirect 回跳,与路由守卫踢人(见 router/index.js)保持一致:
      // 若「记住我」令牌仍有效,登录页会静默换发新登录态并回到被打断的
      // 页面,而不是莫名落到默认落地页。safeRedirect 只接受站内路径。
      const redirect = encodeURIComponent(location.pathname + location.search);
      location.href = `/login?redirect=${redirect}`;
    }
    return Promise.reject(error);
  }
  // silent 请求:不弹全局错误(登录页静默换发失败时,用户还没做任何操作,
  // 弹「网络连接失败」只会造成困惑),错误原样抛给调用方处理。
  if (error.config?.silent) return Promise.reject(error);
  const msg = friendlyMessage(error);
  MessagePlugin.error(msg);
  // 覆写 message:调用方 catch(e) 后 e.message 默认是英文技术文案
  // (如 "Request failed with status code 400"),此处统一换成中文。
  error.message = msg;
  error.friendlyMessage = msg;
  return Promise.reject(error);
});
