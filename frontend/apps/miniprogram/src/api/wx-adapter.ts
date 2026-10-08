// ============================================================================
// axios → wx.request 适配器
//
// 目的：让 Web 端的接口层 `@dining/api`（基于 axios 实例 + 拦截器）在小程序里
// **一行都不用改**就能跑。小程序没有 XMLHttpRequest/fetch，axios 自带的 xhr
// 适配器不可用，所以这里替换掉 `http.defaults.adapter`。
//
// 与 axios 默认适配器的契约差异（两点，都在下面显式处理了）：
//   1) 请求体序列化：把 transformRequest 置为透传（见 client.ts），
//      让 config.data 保持对象交给 wx.request 自己按 content-type 序列化。
//      原因：wx.request 对「已是字符串 + content-type: application/json」的行为
//      在各基础库版本上不够明确，存在被二次 JSON.stringify 的风险；传对象是官方
//      明确支持的路径。顾客端接口全部是 JSON 对象入参，不受影响。
//   2) 状态码判定：axios 的 settle 逻辑不对外导出，这里按同样规则实现
//      （读 config.validateStatus，默认 2xx 放行，否则抛 AxiosError）。
// ============================================================================
import { AxiosError } from 'axios';
import type { AxiosAdapter, AxiosResponse, InternalAxiosRequestConfig } from 'axios';

type HeaderBag = Record<string, unknown> & { toJSON?: () => Record<string, unknown> };

/** 拼出绝对 URL：axios 的 baseURL + url + params。 */
function buildUrl(config: InternalAxiosRequestConfig): string {
  const base = String(config.baseURL || '');
  let url = String(config.url || '');
  if (!/^https?:\/\//i.test(url)) {
    url = `${base.replace(/\/+$/, '')}${url.startsWith('/') ? url : `/${url}`}`;
  }
  const params = config.params as Record<string, unknown> | undefined;
  if (params) {
    const qs = Object.keys(params)
      .filter(k => params[k] !== undefined && params[k] !== null && params[k] !== '')
      .map(k => `${encodeURIComponent(k)}=${encodeURIComponent(String(params[k]))}`)
      .join('&');
    if (qs) url += (url.includes('?') ? '&' : '?') + qs;
  }
  return url;
}

/** 把 axios 的 AxiosHeaders 归一成 wx.request 能吃的普通对象。 */
function buildHeader(config: InternalAxiosRequestConfig): Record<string, string> {
  const raw = config.headers as unknown as HeaderBag | undefined;
  const src = raw && typeof raw.toJSON === 'function' ? raw.toJSON() : (raw ?? {});
  const out: Record<string, string> = {};
  for (const key of Object.keys(src)) {
    const v = (src as Record<string, unknown>)[key];
    if (v === undefined || v === null) continue;
    out[key] = String(v);
  }
  // 兜底：对象请求体必须声明 json，否则 wx.request 会按表单编码发出。
  const hasCt = Object.keys(out).some(k => k.toLowerCase() === 'content-type');
  const data = config.data as unknown;
  if (!hasCt && data !== undefined && data !== null && typeof data === 'object') {
    out['content-type'] = 'application/json';
  }
  return out;
}

/** 复刻 axios 的 settle：按 validateStatus 决定 resolve 还是 reject。 */
function settle(resolve: (v: AxiosResponse) => void, reject: (e: unknown) => void, response: AxiosResponse): void {
  const validate = response.config.validateStatus;
  if (!response.status || !validate || validate(response.status)) {
    resolve(response);
    return;
  }
  const code = Math.floor(response.status / 100) === 4 ? AxiosError.ERR_BAD_REQUEST : AxiosError.ERR_BAD_RESPONSE;
  reject(
    new AxiosError(
      `Request failed with status code ${response.status}`,
      code,
      response.config,
      response.request,
      response
    )
  );
}

export const wxAdapter: AxiosAdapter = (config: InternalAxiosRequestConfig) =>
  new Promise<AxiosResponse>((resolve, reject) => {
    wx.request({
      url: buildUrl(config),
      method: String(config.method || 'get').toUpperCase() as WechatMiniprogram.RequestOption['method'],
      header: buildHeader(config),
      data: config.data as WechatMiniprogram.RequestOption['data'],
      timeout: config.timeout || undefined,
      success: res => {
        settle(resolve, reject, {
          data: res.data,
          status: res.statusCode,
          statusText: String(res.statusCode),
          headers: (res.header ?? {}) as AxiosResponse['headers'],
          config,
          request: res
        } as AxiosResponse);
      },
      fail: err => {
        // 网络层失败（无网、域名未白名单、超时）：统一成 axios 语义的错误，
        // 这样 @dining/api 的 friendlyMessage() 能照常给出中文文案。
        reject(new AxiosError(err?.errMsg || '网络连接失败', AxiosError.ERR_NETWORK, config));
      }
    });
  });
