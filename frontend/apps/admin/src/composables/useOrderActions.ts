import { computed } from 'vue';
import type { ComputedRef, Ref } from 'vue';
import { hasPerm } from '@dining/auth';
import { ORDER_STATUS_CODE, isActiveStatus } from '@dining/shared';
import type { OrderDetail } from '@dining/types';
import { isRefundable } from '../utils/refundable';

export interface OrderActions {
  canOperate: ComputedRef<boolean>;
  canSettle: ComputedRef<boolean>;
  canCancelPerm: ComputedRef<boolean>;
  canEditPerm: ComputedRef<boolean>;
  canCreditSettle: ComputedRef<boolean>;
  canRefundOperate: ComputedRef<boolean>;
  canRefundView: ComputedRef<boolean>;
  canReprint: ComputedRef<boolean>;
  active: ComputedRef<boolean>;
  unpaid: ComputedRef<boolean>;
  canEditOrder: ComputedRef<boolean>;
  canMake: ComputedRef<boolean>;
  canServe: ComputedRef<boolean>;
  canPay: ComputedRef<boolean>;
  canCancelOrder: ComputedRef<boolean>;
  canFinish: ComputedRef<boolean>;
  canCreditSettleOrder: ComputedRef<boolean>;
  canCancelSettleOrder: ComputedRef<boolean>;
  canRefundOrder: ComputedRef<boolean>;
  hasExtraActions: ComputedRef<boolean>;
}

/**
 * 订单详情/操作的权限与按钮可见性矩阵。
 * 前端隐藏/禁用只是体验优化，后端 RequirePerm 仍会 403。
 */
export function useOrderActions(detail: Ref<OrderDetail | null>): OrderActions {
  const canOperate = computed(() => hasPerm('order:operate')); // 制作 / 上菜 / 完成 / 已催办
  const canSettle = computed(() => hasPerm('order:settle')); // 收款 / 结账 / 撤销结算
  const canCancelPerm = computed(() => hasPerm('order:cancel')); // 取消订单
  const canEditPerm = computed(() => hasPerm('order:edit')); // 手动改单
  const canCreditSettle = computed(() => hasPerm('credit:settle')); // 核销挂账
  const canRefundOperate = computed(() => hasPerm('refund:operate'));
  const canRefundView = computed(() => hasPerm('refund:view'));
  const canReprint = computed(() => hasPerm('printer:edit')); // 补打会真的出纸

  // 进行中状态：1 已下单 / 2 制作中 / 3 已上齐(用餐中)
  const active = computed(() => isActiveStatus(detail.value?.orderStatus));
  const unpaid = computed(() => !!detail.value && detail.value.payStatus === 0);

  // 按钮可用性（按钮恒显示，不适用时置灰，避免布局跳动）
  const canEditOrder = computed(() => canEditPerm.value && active.value && unpaid.value);
  const canMake = computed(() => canOperate.value && detail.value?.orderStatus === ORDER_STATUS_CODE.PLACED);
  const canServe = computed(() => canOperate.value && detail.value?.orderStatus === ORDER_STATUS_CODE.MAKING);
  const canPay = computed(() => canSettle.value && active.value && unpaid.value);
  // 取消仅对「未支付」订单开放:已收款订单必须先退款,后端 CancelOrder 同样拒绝
  // (少了 unpaid 这道条件,按钮可点但点了必然报错)。
  const canCancelOrder = computed(() => canCancelPerm.value && active.value && unpaid.value);
  // 完成(归档收尾)对「任意进行中状态 + 已支付」开放,不再要求先点到「已上齐」。
  // 门店实际很少逐级点中间状态;先付模式下付款后单子停在「已下单」时更是没有出口。
  // 未支付订单点了仍会被后端拒绝(先收款/结账),所以这里只管状态与收款两道。
  const canFinish = computed(() => canOperate.value && active.value && detail.value?.payStatus === 1);
  const canCreditSettleOrder = computed(
    () => canCreditSettle.value && detail.value?.settleType === 'credit' && detail.value?.creditStatus === 1
  );
  const canCancelSettleOrder = computed(
    () =>
      canSettle.value &&
      detail.value?.payStatus === 1 &&
      detail.value?.settleType !== 'normal' &&
      !(detail.value?.settleType === 'credit' && detail.value?.creditStatus === 2)
  );
  // 仅在线支付(微信/支付宝)且已支付、未全额退款、且无处理中退款的订单可发起在线退款。
  // 状态判定收敛在 utils/refundable 的 isRefundable,与订单列表行(Orders.vue)同一份实现。
  const canRefundOrder = computed(() => canRefundOperate.value && isRefundable(detail.value));
  const hasExtraActions = computed(
    () => canFinish.value || canCreditSettleOrder.value || canCancelSettleOrder.value || canRefundOrder.value
  );

  return {
    canOperate,
    canSettle,
    canCancelPerm,
    canEditPerm,
    canCreditSettle,
    canRefundOperate,
    canRefundView,
    canReprint,
    active,
    unpaid,
    canEditOrder,
    canMake,
    canServe,
    canPay,
    canCancelOrder,
    canFinish,
    canCreditSettleOrder,
    canCancelSettleOrder,
    canRefundOrder,
    hasExtraActions
  };
}
