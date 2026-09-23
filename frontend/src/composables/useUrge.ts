import { ref } from 'vue';
import type { Ref } from 'vue';
import { MessagePlugin } from 'tdesign-vue-next';
import { urgeOrder } from '../api';
import type { OrderDetail } from '../types/entities';

export function useUrge(currentOrder: Ref<OrderDetail | null>, tableId: Ref<number>) {
  const urging = ref(false);
  const urgeCooldown = ref(0); // 剩余冷却秒数
  let urgeTimer: ReturnType<typeof setInterval> | null = null;

  async function urge(): Promise<void> {
    if (!currentOrder.value || urging.value || urgeCooldown.value > 0) return;
    urging.value = true;
    try {
      // silent:冷却拒绝不弹全局错误 toast,倒计时由本函数自行展示。
      const res = await urgeOrder(currentOrder.value.orderNo as string, tableId.value, { silent: true });
      MessagePlugin.success('已通知后厨加急，请稍候');
      startUrgeCooldown(Number(res?.cooldown) || 180);
    } catch (e) {
      // 冷却拒绝时后端在响应体附带结构化剩余秒数 {data: {cooldown}},
      // 直接读取展示倒计时(不再从 msg 文案正则解析,文案调整不影响前端)。
      const err = e as { response?: { data?: { data?: { cooldown?: number } } } } | null | undefined;
      const cd = Number(err?.response?.data?.data?.cooldown);
      if (Number.isFinite(cd) && cd > 0) startUrgeCooldown(cd);
    } finally {
      urging.value = false;
    }
  }

  function startUrgeCooldown(sec: number): void {
    clearUrgeTimer();
    urgeCooldown.value = Math.max(0, Math.floor(sec) || 0);
    if (urgeCooldown.value <= 0) return;
    urgeTimer = setInterval(() => {
      urgeCooldown.value -= 1;
      if (urgeCooldown.value <= 0) clearUrgeTimer();
    }, 1000);
  }

  function clearUrgeTimer(): void {
    if (urgeTimer !== null) {
      clearInterval(urgeTimer);
      urgeTimer = null;
    }
  }

  return { urging, urgeCooldown, urge, startUrgeCooldown, clearUrgeTimer };
}
