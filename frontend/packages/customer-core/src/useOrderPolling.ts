import type { Ref } from 'vue';
import { getOrderByNo, queryPay } from '@dining/api';
import { ORDER_STATUS_CODE } from '@dining/shared';
import type { OrderDetail } from '@dining/types';
import { subscribeVisibility } from './visibility';

export type ViewState = 'loading' | 'error' | 'menu' | 'order' | 'settled' | 'done' | 'canceled' | 'pay';

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
  // 可见性订阅的取消函数(startPolling 建立、stopPolling 释放)
  let unsubscribeVisibility: (() => void) | null = null;
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
            view.value = 'settled';
          }
          return;
        }
      }
      const o = await getOrderByNo(orderNo);
      if (currentOrder.value !== base) return;
      currentOrder.value = o;
      if (o.orderStatus === ORDER_STATUS_CODE.CANCELED) {
        view.value = 'canceled';
        clearUrgeTimer();
        stopPolling();
        return;
      }
      if (o.orderStatus === ORDER_STATUS_CODE.DONE) {
        view.value = 'done';
        clearUrgeTimer();
        stopPolling();
        return;
      }
      // 进行中且已付款 → 顾客侧结束(已结账),仍在出餐则继续展示进度;
      // 仅在订单/支付视图时切换,避免打断顾客在菜单里的选菜。
      if (o.payStatus === 1 && (view.value === 'order' || view.value === 'pay')) {
        onlineQr.value = '';
        view.value = 'settled';
      } else if (o.payStatus === 0 && view.value === 'settled') {
        // 撤销结算:订单回到未结账,顾客侧恢复到可加菜/付款的订单视图。
        view.value = 'order';
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
    // 可见性订阅走适配器(Web 端 = visibilitychange;小程序端可换成 onAppShow/onAppHide)。
    unsubscribeVisibility = subscribeVisibility(onVisibilityChange);
  }

  function stopPolling(): void {
    if (pollTimer !== null) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
    pausedByHidden = false;
    unsubscribeVisibility?.();
    unsubscribeVisibility = null;
  }

  return { startPolling, stopPolling };
}
