// 轮询器（订单状态）。
//
// 与 H5 的 `useOrderPolling` 同样的节奏（3s 一拍 + 防并发重叠），但去掉了 Vue 依赖：
// 页面在 onShow 里 start、onHide 里 stop —— 小程序的页面生命周期天然覆盖了
// 「切后台」与「跳去别的页面」两种情况，所以不需要再挂 visibilitychange /
// onAppHide 监听（这是与 Web 端最直观的一处平台差异）。
export interface Poller {
  start(): void;
  stop(): void;
  /** 立即补一拍（如从支付页返回订单页时，不必等下一个 3s）。 */
  trigger(): void;
}

export function createPoller(tick: () => void | Promise<void>, intervalMs: number): Poller {
  // 用 ReturnType 而不是写死 number：小程序运行时返回 number，而单测跑在 Node 下
  // （@types/node 在场）返回 Timeout，写死任一种都会在另一边报类型错。
  let timer: ReturnType<typeof setInterval> | null = null;
  // 上一拍还没回来时跳过本拍：两拍并发会乱序，旧快照会覆盖新数据。
  let busy = false;

  async function run(): Promise<void> {
    if (busy) return;
    busy = true;
    try {
      await tick();
    } catch {
      /* 轮询失败忽略，下一拍重试（与 H5 一致的静默策略） */
    } finally {
      busy = false;
    }
  }

  return {
    start(): void {
      if (timer !== null) return;
      timer = setInterval(() => void run(), intervalMs);
    },
    stop(): void {
      if (timer !== null) {
        clearInterval(timer);
        timer = null;
      }
    },
    trigger(): void {
      // 故意不检查 timer：手动刷新（订单页的「刷新」）就是在轮询已停止的状态下
      // 才需要补拍（错误态/已结束状态）。页面卸载后不再 setData 由调用方的
      // unloaded 标志负责，不能靠这里静默丢弃 —— 那会让刷新按钮直接失效。
      void run();
    }
  };
}
