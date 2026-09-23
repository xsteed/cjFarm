import type { Ref } from 'vue';
import { getOrderByNo, queryPay } from '../api';
import { ORDER_STATUS_CODE } from '../constants/orderStatus';
import type { OrderDetail } from '../types/entities';

export type ViewState = 'loading' | 'error' | 'menu' | 'order' | 'done' | 'canceled' | 'pay';

interface PollOptions {
  view: Ref<ViewState>;
  currentOrder: Ref<OrderDetail | null>;
  onlineQr: Ref<string>;
  clearUrgeTimer: () => void;
}

// 轮询节奏:顾客端 3s 一拍,够快感知后厨状态又不至于打爆后端。
const POLL_INTERVAL_MS = 3000;

export function useOrderPolling({ view, currentOrder, onlineQr, clearUrgeTimer }: PollOptions) {
  let pollTimer: ReturnType<typeof setInterval> | null = null;
  let pollCount = 0;
  // 上一轮响应还没回来(网络慢)时跳过本轮:两轮并发会乱序,
  // 后到的旧快照会把新数据覆盖,订单状态"回退一档"要等下一轮才自愈。
  let inFlight = false;
  // 页面被切到后台/锁屏时暂停轮询,回到前台立即补一轮再恢复节奏
  // (手机顾客切去微信再回来,不该漏掉这期间的状态变化)。
  let pausedByHidden = false;

  async function pollOnce(): Promise<void> {
    if (!currentOrder.value || inFlight) return;
    inFlight = true;
    // 请求发出前记住当前订单引用:await 期间若被外部替换(如 submit 加菜成功后写入
    // 含新菜的订单),本次响应就是旧快照,丢弃即可,避免"加菜成功后订单短暂少菜"。
    const base = currentOrder.value;
    try {
      // 在线支付每 5 轮(约 15s)主动向渠道查一次单:
      // 异步通知丢失或延迟时,由前端触发补单,避免顾客付款后页面一直停在待支付。
      pollCount++;
      const orderNo = currentOrder.value.orderNo as string;
      if (view.value === 'pay' && pollCount % 5 === 0) {
        const r = await queryPay(orderNo);
        if (r?.payStatus === 1) {
          const o = await getOrderByNo(orderNo);
          if (currentOrder.value === base) {
            currentOrder.value = o;
            onlineQr.value = '';
            view.value = 'order';
          }
          return;
        }
      }
      const o = await getOrderByNo(orderNo);
      if (currentOrder.value !== base) return;
      currentOrder.value = o;
      // 在线支付成功:支付视图自动回到订单状态页(显示已支付)
      if (view.value === 'pay' && o.payStatus === 1) {
        onlineQr.value = '';
        view.value = 'order';
      }
      if (o.orderStatus === ORDER_STATUS_CODE.CANCELED) {
        view.value = 'canceled';
        clearUrgeTimer();
        stopPolling();
      } else if (o.orderStatus === ORDER_STATUS_CODE.DONE) {
        view.value = 'done';
        clearUrgeTimer();
        stopPolling();
      }
    } catch {
      /* 轮询失败忽略,下轮重试 */
    } finally {
      inFlight = false;
    }
  }

  function onVisibilityChange(): void {
    if (document.hidden) {
      if (pollTimer !== null) {
        clearInterval(pollTimer);
        pollTimer = null;
        pausedByHidden = true;
      }
    } else if (pausedByHidden) {
      pausedByHidden = false;
      startPolling();
      void pollOnce();
    }
  }

  function startPolling(): void {
    stopPolling();
    pollTimer = setInterval(() => {
      void pollOnce();
    }, POLL_INTERVAL_MS);
    // 同一函数引用重复注册是幂等的;stopPolling 时统一移除。
    document.addEventListener('visibilitychange', onVisibilityChange);
  }

  function stopPolling(): void {
    if (pollTimer !== null) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
    pausedByHidden = false;
    document.removeEventListener('visibilitychange', onVisibilityChange);
  }

  return { startPolling, stopPolling };
}
