// ============================================================================
// wx 全局的测试替身
//
// 单测跑在 Node 里，没有小程序运行时，所以这里手写一个最小但够用的 wx：
//   ・request 按「预置队列」逐个回包（可回成功也可回网络失败），并把每次请求记下来供断言
//   ・showToast / showModal / navigate* 全部记账
//   ・storage 用内存 Map
// 回包走微任务，保证与真实小程序一样是异步回调（拦截器、Promise 链路才测得准）。
// ============================================================================

export interface RecordedRequest {
  url: string;
  method?: string;
  data?: unknown;
  header?: Record<string, string>;
  timeout?: number;
}

export interface RecordedToast {
  title: string;
  icon: string;
}

export interface RecordedModal {
  title?: string;
  content?: string;
}

type Reply = { kind: 'success'; statusCode: number; data: unknown } | { kind: 'fail'; errMsg: string };

export class WxMock {
  requests: RecordedRequest[] = [];
  toasts: RecordedToast[] = [];
  modals: RecordedModal[] = [];
  navigations: { type: string; url?: string; delta?: number }[] = [];
  storage = new Map<string, unknown>();

  /** 点击 showModal 时的默认结果（可被单测改写） */
  modalConfirm = true;
  /**
   * showModal 处于可编辑模式时回填的输入内容。
   * 保持 undefined 即模拟「旧基础库不返回 content」，用来验证「不清空原备注」的兜底分支。
   */
  modalInput: string | undefined = undefined;
  /** 导航是否「失败」（用来测 redirectTo fail 分支） */
  navigationFails = false;

  /** wx.login 返回的 code（测小程序内支付用；置空即模拟登录失败） */
  loginCode = 'code-abc';
  /** wx.requestPayment 的结果：success / cancel / fail */
  paymentResult: 'success' | 'cancel' | 'fail' = 'success';
  /** 每次拉起收银台的参数（供断言「调起参数原样透传」） */
  payments: { timeStamp?: string; nonceStr?: string; package?: string }[] = [];

  /** wx.scanCode 扫到的内容 / 携带的页面 path（首页与菜单页的扫码入口用） */
  scanCodeResult = 'https://dining.test/order/8YG3Q335';
  scanCodePath = '';
  /** 扫码结果：'' 成功 / 'cancel' 用户取消 / 'error' 相机或权限失败 */
  scanCodeFail: '' | 'cancel' | 'error' = '';

  /** 最后一次 setNavigationBarTitle 的标题（首页会把标题换成店名） */
  navTitle = '';

  private queue: Reply[] = [];

  /** 装成全局 wx。 */
  install(): void {
    (globalThis as Record<string, unknown>).wx = this.api;
  }

  /** 清空所有记账与预置。 */
  reset(): void {
    this.requests = [];
    this.toasts = [];
    this.modals = [];
    this.navigations = [];
    this.storage.clear();
    this.queue = [];
    this.modalConfirm = true;
    this.modalInput = undefined;
    this.navigationFails = false;
    this.loginCode = 'code-abc';
    this.paymentResult = 'success';
    this.payments = [];
    this.scanCodeResult = 'https://dining.test/order/8YG3Q335';
    this.scanCodePath = '';
    this.scanCodeFail = '';
    this.navTitle = '';
  }

  /** 预置一次成功回包（按先入先出匹配请求）。 */
  reply(body: unknown, statusCode = 200): void {
    this.queue.push({ kind: 'success', statusCode, data: body });
  }

  /** 预置一次网络层失败（wx.request 的 fail 回调）。 */
  replyFail(errMsg = 'request:fail'): void {
    this.queue.push({ kind: 'fail', errMsg });
  }

  /** 最后一次请求。 */
  get lastRequest(): RecordedRequest | undefined {
    return this.requests[this.requests.length - 1];
  }

  /** 最后一次 toast 文案。 */
  get lastToast(): string {
    return this.toasts.length ? this.toasts[this.toasts.length - 1].title : '';
  }

  private next(): Reply {
    return this.queue.shift() ?? { kind: 'success', statusCode: 200, data: { code: 200, msg: 'ok', data: null } };
  }

  private api = {
    request: (opt: {
      url: string;
      method?: string;
      data?: unknown;
      header?: Record<string, string>;
      timeout?: number;
      success?: (res: { statusCode: number; data: unknown; header: Record<string, string> }) => void;
      fail?: (err: { errMsg: string }) => void;
    }): void => {
      this.requests.push({
        url: opt.url,
        method: opt.method,
        data: opt.data,
        header: opt.header,
        timeout: opt.timeout
      });
      const reply = this.next();
      void Promise.resolve().then(() => {
        if (reply.kind === 'fail') opt.fail?.({ errMsg: reply.errMsg });
        else opt.success?.({ statusCode: reply.statusCode, data: reply.data, header: {} });
      });
    },

    showToast: (opt: { title: string; icon?: string }): void => {
      this.toasts.push({ title: opt.title, icon: opt.icon ?? 'success' });
    },

    showModal: (opt: {
      title?: string;
      content?: string;
      editable?: boolean;
      placeholderText?: string;
      success?: (r: { confirm: boolean; content?: string }) => void;
    }): void => {
      this.modals.push({ title: opt.title, content: opt.content });
      const confirm = this.modalConfirm;
      const input = this.modalInput;
      // 可编辑模式才带 content；未预置时省略该字段，模拟旧基础库
      void Promise.resolve().then(() =>
        opt.success?.(input === undefined ? { confirm } : { confirm, content: input })
      );
    },

    redirectTo: (opt: { url: string; fail?: () => void }): void => {
      this.navigations.push({ type: 'redirectTo', url: opt.url });
      if (this.navigationFails) void Promise.resolve().then(() => opt.fail?.());
    },
    navigateTo: (opt: { url: string }): void => {
      this.navigations.push({ type: 'navigateTo', url: opt.url });
    },
    navigateBack: (opt?: { delta?: number; fail?: () => void }): void => {
      // delta 也记下来：切页时会用它判断「退几层才回到目标页」
      this.navigations.push({ type: 'navigateBack', delta: opt?.delta });
      if (this.navigationFails) void Promise.resolve().then(() => opt?.fail?.());
    },
    setClipboardData: (): void => {},

    // ---- 扫码 / 导航栏标题 ----
    scanCode: (opt: {
      success?: (r: { result: string; path?: string }) => void;
      fail?: (e: { errMsg: string }) => void;
    }): void => {
      void Promise.resolve().then(() => {
        if (this.scanCodeFail) {
          opt.fail?.({ errMsg: `scanCode:fail ${this.scanCodeFail}` });
          return;
        }
        opt.success?.({ result: this.scanCodeResult, path: this.scanCodePath });
      });
    },
    setNavigationBarTitle: (opt: { title: string }): void => {
      this.navTitle = opt.title;
    },

    // ---- 小程序内支付（JSAPI）----
    login: (opt: { success?: (r: { code: string }) => void; fail?: (e: unknown) => void }): void => {
      void Promise.resolve().then(() =>
        this.loginCode ? opt.success?.({ code: this.loginCode }) : opt.fail?.({ errMsg: 'login:fail' })
      );
    },
    requestPayment: (opt: {
      timeStamp?: string;
      nonceStr?: string;
      package?: string;
      signType?: string;
      paySign?: string;
      success?: () => void;
      fail?: (e: { errMsg: string }) => void;
    }): void => {
      this.payments.push({ timeStamp: opt.timeStamp, nonceStr: opt.nonceStr, package: opt.package });
      void Promise.resolve().then(() => {
        if (this.paymentResult === 'success') {
          opt.success?.();
          return;
        }
        opt.fail?.({
          errMsg: this.paymentResult === 'cancel' ? 'requestPayment:fail cancel' : 'requestPayment:fail'
        });
      });
    },

    getStorageSync: (key: string): unknown => this.storage.get(key) ?? '',
    setStorageSync: (key: string, value: unknown): void => {
      this.storage.set(key, value);
    },
    removeStorageSync: (key: string): void => {
      this.storage.delete(key);
    },

    // 其余 API 一律 no-op，避免测试因未实现而炸
    onAppHide: (): void => {},
    onAppShow: (): void => {}
  };
}

/** 全局单例：测试里统一用这个。 */
export const wxMock = new WxMock();

/** 把微任务队列跑空（等异步回包被处理完）。 */
export function flush(): Promise<void> {
  return new Promise(resolve => setTimeout(resolve, 0));
}
