// 轻提示的单测。
// 重点是那条容易踩的规则：只有**带图标**的 toast 才受微信的 7 字限制；
// 纯文字提示必须完整保留，否则「为什么下单失败」这类关键信息会被吃掉。
import { beforeEach, describe, expect, it } from 'vitest';
import { toast } from '../../src/utils/toast';
import { wxMock } from '../../test/mock-wx';

describe('toast', () => {
  beforeEach(() => wxMock.reset());

  it('success 带 success 图标，超过 7 字截断', () => {
    toast.success('下单成功');
    expect(wxMock.toasts[0]).toEqual({ title: '下单成功', icon: 'success' });

    // 截断后连省略号一起不超过 7 个字符,否则微信会再截一次、把省略号吃掉
    toast.success('加菜成功，已通知后厨');
    expect(wxMock.lastToast).toBe('加菜成功，已…');
  });

  it('error / warning / info 用纯文字图标，且完整保留长文案', () => {
    const long = '该桌台已有未结账的订单，请扫描桌上的新二维码进入订单页继续点菜（或联系服务员）';
    toast.error(long);
    expect(wxMock.toasts[0]).toEqual({ title: long, icon: 'none' });

    toast.warning('订单已支付，不可加菜');
    expect(wxMock.lastToast).toBe('订单已支付，不可加菜');

    toast.info('已催过啦，请等待 96 秒');
    expect(wxMock.lastToast).toBe('已催过啦，请等待 96 秒');
  });

  it('空白文案不弹提示', () => {
    toast.success('');
    toast.error('   ');
    expect(wxMock.toasts).toHaveLength(0);
  });
});
