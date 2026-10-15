import { describe, it, expect, beforeEach, vi } from 'vitest';
import { ref } from 'vue';
import { useOrderActions } from '../useOrderActions';
import { setAuth } from '@dining/auth';
import { clearAllStorage } from '@shell/test/mocks.ts';
import type { OrderDetail } from '@dining/types';

// @dining/auth 在模块加载时会依赖 @dining/api(refreshProfile),漏了会让整个套件加载失败。
vi.mock('@dining/api', () => ({
  onAuthCleared: vi.fn(),
  getProfile: vi.fn(),
  logoutRemember: vi.fn()
}));

const ALL_PERMS = ['order:operate', 'order:settle', 'order:cancel', 'order:edit'];

function order(overrides: Partial<OrderDetail> = {}): OrderDetail {
  return {
    orderId: 1,
    orderNo: 'NO001',
    tableNo: '1',
    tableName: '大厅',
    orderStatus: 1,
    payStatus: 0,
    dishAmount: 28,
    seatFee: 0,
    discountAmount: 0,
    totalAmount: 28,
    items: [],
    ...overrides
  };
}

function actions(detail: Partial<OrderDetail>) {
  return useOrderActions(ref(order(detail)));
}

beforeEach(() => {
  clearAllStorage();
  setAuth({ token: 't', username: 'zhangsan', perms: ALL_PERMS });
});

describe('useOrderActions 按钮可用性', () => {
  // 「完成」是归档收尾,不该要求服务员先把「制作中 / 已上齐」逐个点一遍：
  // 门店实际很少逐级操作,先付模式下付款后单子停在 1/2 时更是完全没有出口。
  it('已支付订单在任意进行中状态都可完成', () => {
    for (const orderStatus of [1, 2, 3]) {
      expect(actions({ orderStatus, payStatus: 1 }).canFinish.value).toBe(true);
    }
  });

  it('未支付订单不可完成（需先收款/结账）', () => {
    for (const orderStatus of [1, 2, 3]) {
      expect(actions({ orderStatus, payStatus: 0 }).canFinish.value).toBe(false);
    }
  });

  it('终态订单不可完成', () => {
    expect(actions({ orderStatus: 4, payStatus: 1 }).canFinish.value).toBe(false);
    expect(actions({ orderStatus: 5, payStatus: 1 }).canFinish.value).toBe(false);
  });

  it('收款/结账仍按「进行中 + 未支付」放行', () => {
    expect(actions({ orderStatus: 1, payStatus: 0 }).canPay.value).toBe(true);
    expect(actions({ orderStatus: 1, payStatus: 1 }).canPay.value).toBe(false);
  });

  it('改单/取消仍只在「进行中 + 未支付」时可用', () => {
    expect(actions({ orderStatus: 2, payStatus: 0 }).canEditOrder.value).toBe(true);
    expect(actions({ orderStatus: 2, payStatus: 0 }).canCancelOrder.value).toBe(true);
    expect(actions({ orderStatus: 2, payStatus: 1 }).canEditOrder.value).toBe(false);
    expect(actions({ orderStatus: 2, payStatus: 1 }).canCancelOrder.value).toBe(false);
  });

  it('无 order:operate 权限时不显示完成', () => {
    setAuth({ token: 't', username: 'zhangsan', perms: ['order:settle'] });
    expect(actions({ orderStatus: 1, payStatus: 1 }).canFinish.value).toBe(false);
  });
});
