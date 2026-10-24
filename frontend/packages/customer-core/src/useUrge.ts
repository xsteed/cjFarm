import { ref } from 'vue';
import type { Ref } from 'vue';
import { ui } from '@dining/shared';
import { urgeOrder } from '@dining/api';
import type { OrderDetail } from '@dining/types';

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
      // 年轻化 + 安抚语气：确认"催到了"，同时把等待包装成期待感
      ui.success('催单成功～大厨火力全开，美味正在冲刺赶来的路上');
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
    const total = Math.max(0, Math.floor(sec) || 0);
    urgeCooldown.value = total;
    if (total <= 0) return;
    // 按「截止时刻」计算剩余秒数,而不是每拍 -1:setInterval 本身不精确,
    // 且后台标签页被节流后回调会成批补发,逐拍自减在数百秒的冷却里会累计漂移
    // (页面显示的倒计时与后端真实冷却对不上,顾客早点了反而被拒)。
    const deadline = Date.now() + total * 1000;
    urgeTimer = setInterval(() => {
      const left = Math.max(0, Math.ceil((deadline - Date.now()) / 1000));
      urgeCooldown.value = left;
      if (left <= 0) clearUrgeTimer();
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
