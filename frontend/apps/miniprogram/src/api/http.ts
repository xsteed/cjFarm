// ============================================================================
// 小程序接口传输层（wx.request 封装，无 axios）
//
// 把后端 /api/* 的响应结构（`{ code, msg, data }`）解包成业务数据，并在失败时
// 转成中文可读的 ApiError。逻辑层（client.ts / 各页面）只调 httpGet / httpPost，
// 不关心传输细节。与 H5 的 @dining/api 是「同一套语义的另一实现」。
// ============================================================================
import { safeStorage } from '../shared/storage';
import { LS_CUSTOMER_TOKEN } from '../shared/keys';
import { ui } from '../shared/ui';

const HTTP_FALLBACK_MSG: Record<number, string> = {
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

/** 顾客端接口路径判定。baseURL 已经是完整 origin，这里只看相对路径。 */
function isCustomerPath(url: string): boolean {
  return url.startsWith('/customer/');
}

let baseURL = '/api';
let timeoutMs = 15000;

/** 配置传输层：origin + /api 作为 baseURL，以及超时。由各端入口调用一次。 */
export function configureHttp(opts: { baseURL?: string; timeoutMs?: number }): void {
  if (opts.baseURL) baseURL = opts.baseURL;
  if (opts.timeoutMs) timeoutMs = opts.timeoutMs;
}

// ---------------------------------------------------------------------------
// 401 处置策略（可替换）
//
// 默认实现只记录一次未授权；小程序在 app.ts 里注册成「清令牌 + 静默重登」。
// 各端入口调一次 setUnauthorizedHandler 即可，忘了注册也只是回到这个空默认行为，
// 而不是静默不处理。
// ---------------------------------------------------------------------------
let onUnauthorized: () => void = () => {};

/** 替换「令牌失效」后的处置策略（小程序入口调用）。 */
export function setUnauthorizedHandler(handler: () => void): void {
  onUnauthorized = handler;
}

/** 接口错误：携带中文文案、HTTP 状态、原始响应体（催菜冷却读 err.response.data.data）。 */
export class ApiError extends Error {
  /** 中文可读文案（业务 msg 优先，否则状态码映射）。 */
  friendlyMessage: string;
  /** HTTP 状态码（业务失败恒为 200，网络失败为 0）。 */
  status: number;
  /** 原始响应体（含 data 子对象），便于调用方读取结构化字段。 */
  response?: { data?: unknown };

  constructor(message: string, status = 0, responseData?: unknown) {
    super(message);
    this.name = 'ApiError';
    this.friendlyMessage = message;
    this.status = status;
    if (responseData !== undefined) this.response = { data: responseData };
  }
}

/** 把失败转成中文文案：业务 msg > 超时 > 状态码映射 > 状态码兜底 > 网络失败。 */
function friendlyMessageFrom(body: { msg?: string } | undefined, status: number, errMsg?: string): string {
  if (body && body.msg) return body.msg;
  if (errMsg) {
    if (errMsg.includes('timeout')) return '请求超时，请检查网络后重试';
    if (errMsg.includes('fail')) return '网络连接失败，请检查网络后重试';
  }
  if (status && HTTP_FALLBACK_MSG[status]) return HTTP_FALLBACK_MSG[status];
  if (status) return `请求失败（${status}），请稍后重试`;
  return '网络连接失败，请检查网络后重试';
}

interface RequestConfig {
  silent?: boolean;
  /** GET 查询参数（httpGet 内部组装进 config）。 */
  params?: Record<string, unknown>;
}

function buildQuery(params?: Record<string, unknown>): string {
  if (!params) return '';
  const parts: string[] = [];
  for (const [k, v] of Object.entries(params)) {
    if (v === '' || v === undefined || v === null) continue; // 空值丢弃
    parts.push(`${encodeURIComponent(k)}=${encodeURIComponent(String(v))}`);
  }
  return parts.length ? `?${parts.join('&')}` : '';
}

function request<T>(method: 'GET' | 'POST', path: string, body: unknown, config?: RequestConfig): Promise<T> {
  const params = config?.params;
  const silent = !!config?.silent;
  const url = baseURL + path + (method === 'GET' ? buildQuery(params as Record<string, unknown>) : '');

  const header: Record<string, string> = {};
  if (isCustomerPath(path)) {
    const token = safeStorage.get(LS_CUSTOMER_TOKEN);
    if (token) header.Authorization = `Bearer ${token}`;
  }
  if (method === 'POST') {
    // 请求体保持对象，交给 wx.request 按 content-type 序列化（官方明确支持的路径，
    // 传字符串在各基础库版本上存在被二次 stringify 的风险）。
    header['Content-Type'] = 'application/json';
  }

  return new Promise<T>((resolve, reject) => {
    wx.request({
      url,
      method,
      data: method === 'POST' ? (body as WechatMiniprogram.IAnyObject) : undefined,
      header,
      timeout: timeoutMs,
      success(res) {
        const status = res.statusCode;
        const bodyData = res.data as { code?: number; msg?: string; data?: unknown } | undefined;
        if (status === 200) {
          if (bodyData && bodyData.code === 200) {
            // 带数据的对象结果 → 交出 data；仅消息结果（无 data）→ 原样返回
            if (bodyData.data !== undefined) resolve(bodyData.data as T);
            else resolve(bodyData as unknown as T);
            return;
          }
          // 业务失败：业务 msg 优先，保留响应体给调用方读结构化字段
          const msg = (bodyData && bodyData.msg) || '请求失败';
          if (!silent) ui.error(msg);
          reject(new ApiError(msg, 200, bodyData));
          return;
        }
        // HTTP 错误
        if (status === 401) {
          // 令牌失效：清掉本地令牌并触发注册的处置策略（会员模块在此静默重登）。
          // 文案固定走状态映射，不取 body.msg —— 401 响应体里可能带的是无关正文。
          safeStorage.remove(LS_CUSTOMER_TOKEN);
          onUnauthorized();
          reject(new ApiError(HTTP_FALLBACK_MSG[401], 401, bodyData));
          return;
        }
        const msg = friendlyMessageFrom(bodyData, status);
        if (!silent) ui.error(msg);
        reject(new ApiError(msg, status, bodyData));
      },
      fail(err) {
        const msg = friendlyMessageFrom(undefined, 0, err?.errMsg);
        if (!silent) ui.error(msg);
        reject(new ApiError(msg, 0));
      }
    });
  });
}

/** GET：第二个参数是查询参数对象（空值会被丢弃并做编码），第三个可传 { silent: true }
 *  跳过全局错误提示 —— 轮询路径必须静默，否则网络故障时每 3~5s 弹一次 toast。 */
export function httpGet<T>(path: string, params?: Record<string, unknown>, config?: { silent?: boolean }): Promise<T> {
  return request<T>('GET', path, undefined, { params, silent: config?.silent });
}

/** POST：body 为请求体，config 可传 { silent: true } 跳过全局错误提示。 */
export function httpPost<T>(path: string, body?: unknown, config?: RequestConfig): Promise<T> {
  return request<T>('POST', path, body, config);
}

/** 上传文件(multipart):wx.uploadFile 封装,响应解包与错误语义与 request 一致。 */
export function httpUploadFile<T>(path: string, filePath: string, name = 'file', config?: { silent?: boolean }): Promise<T> {
  const url = baseURL + path;
  const header: Record<string, string> = {};
  if (isCustomerPath(path)) {
    const token = safeStorage.get(LS_CUSTOMER_TOKEN);
    if (token) header.Authorization = `Bearer ${token}`;
  }
  return new Promise<T>((resolve, reject) => {
    wx.uploadFile({
      url,
      filePath,
      name,
      header,
      timeout: timeoutMs,
      success(res) {
        const status = res.statusCode;
        let bodyData: { code?: number; msg?: string; data?: unknown } | undefined;
        try {
          bodyData = JSON.parse(res.data) as { code?: number; msg?: string; data?: unknown };
        } catch {
          /* 非 JSON 响应体:走状态码映射 */
        }
        if (status === 200 && bodyData && bodyData.code === 200 && bodyData.data !== undefined) {
          resolve(bodyData.data as T);
          return;
        }
        const msg = friendlyMessageFrom(bodyData, status);
        if (!config?.silent) ui.error(msg);
        reject(new ApiError(msg, status, bodyData));
      },
      fail(err) {
        const msg = friendlyMessageFrom(undefined, 0, err?.errMsg);
        if (!config?.silent) ui.error(msg);
        reject(new ApiError(msg, 0));
      }
    });
  });
}
