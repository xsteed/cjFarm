// ============================================================================
// 接口层组装点
//
// `@dining/api` 里已经建好 axios 实例与拦截器（响应解包 {code,msg,data}、
// 统一中文错误文案、401 清登录态）。小程序端只做三件事把它「本地化」：
//   1) 换传输层为 wx.request；
//   2) 把 baseURL 指向已备案的 HTTPS 域名；
//   3) 注入提示与 401 处理——这两件事在 Web 端由 main.ts / http.ts 默认实现承担，
//      在小程序端由这里接管（这也是 packages 里留出 setUiNotifier 与
//      setUnauthorizedHandler 两个口子的原因）。
//
// 页面统一 `import { getMenu, ... } from '../api/client'`，不要直接 import
// '@dining/api'，否则拦截器可能在被装配之前就被使用。
// ============================================================================
import { http } from '@dining/api';
import { setUiNotifier } from '@dining/shared/ui';
import { API_BASE_URL, REQUEST_TIMEOUT_MS } from '../config';
import { currentCustomerToken, installMemberUnauthorizedHandler } from '../store/member';
import { toast } from '../utils/toast';
import { wxAdapter } from './wx-adapter';

// ---- 1) 传输层 ----
http.defaults.adapter = wxAdapter;
http.defaults.baseURL = API_BASE_URL;
http.defaults.timeout = REQUEST_TIMEOUT_MS;
// 请求体保持对象，交给 wx.request 按 content-type 序列化（理由见 wx-adapter.ts 顶部注释）。
http.defaults.transformRequest = [(data: unknown) => data];
// 关掉 axios 默认的 transformRequest 后，Content-Type 需要自己声明。
http.defaults.headers.post['Content-Type'] = 'application/json';

// ---- 2) 提示 ----
// 逻辑包不依赖任何 UI 库，只调用 @dining/shared 的 ui（success/warning/error）。
setUiNotifier({
  success: (message: string) => toast.success(message),
  warning: (message: string) => toast.warning(message),
  error: (message: string) => toast.error(message)
});

// ---- 3) 401 ----
// 小程序里会 401 的只有「会员专属接口」（顾客令牌 2h 过期是常态）：
// 交给会员模块静默重登（wx.login 无感知），成功后调用方重放请求，不打断顾客。
// 匿名点餐接口正常永远不会 401；真出现（域名/网关层问题）也不弹窗打断点餐，
// 下一次会员数据请求会触发完整的重新登录并给出明确文案。
installMemberUnauthorizedHandler();

// ---- 4) 顾客会员令牌 ----
// 会员令牌存在 App 实例的 member 上（跨页共享）。之所以在拦截器里**每次现读**而不是
// 闭包缓存：client.ts 会被打进每个页面的包里各一份，模块级变量不共享（见 types.ts 说明），
// 而 App 实例是全局唯一的。
http.interceptors.request.use(config => {
  const token = currentCustomerToken();
  if (token) config.headers.Authorization = `Bearer ${token}`;
  return config;
});

// 顾客端用到的接口全部从这里转发；管理端接口也一并带出（原生小程序无 tree-shaking 负担顾虑，
// 但页面不会调用它们，仅保留导出以便后续扩展）。
export * from '@dining/api';
