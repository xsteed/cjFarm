// 规格弹层「确定」的防重入用例。
//
// 弹层的确定 = 父页面加一份菜。不加锁时连点会触发多次 confirm 事件，
// 份数被抬到 2、3 份 —— 金额与后厨出餐量都会错。
import { describe, expect, it } from 'vitest';
import { fireObserver, mountComponent } from '../../../../test/harness';
import type { PageInstance } from '../../../../test/harness';

const OBSERVER = 'visible, initialSpecId, initialRemark';

async function mountSheet(): Promise<PageInstance> {
  return mountComponent('spec-sheet', () => import('../index'), {
    specs: [{ specId: 9, specName: '份', price: 68 }],
    remarks: []
  });
}

/** 真机上 observer 由属性变化自动触发，这里手动跑一次「打开弹层」。 */
function open(inst: PageInstance): void {
  fireObserver(inst, OBSERVER, true, 9, '');
}

function close(inst: PageInstance): void {
  fireObserver(inst, OBSERVER, false, 9, '');
}

describe('规格弹层 · 确定键', () => {
  it('连点「确定」只发一次 confirm', async () => {
    const inst = await mountSheet();
    open(inst);

    inst.onConfirm();
    inst.onConfirm();
    inst.onConfirm();

    expect(inst.events.filter(e => e.name === 'confirm')).toHaveLength(1);
  });

  it('关闭再打开后可以重新确定（锁在关闭时释放）', async () => {
    const inst = await mountSheet();
    open(inst);
    inst.onConfirm();
    expect(inst.events).toHaveLength(1);

    close(inst);
    open(inst);
    inst.onConfirm();

    expect(inst.events.filter(e => e.name === 'confirm')).toHaveLength(2);
  });

  it('confirm 事件带上选中的规格与价格（父页面据此加购）', async () => {
    const inst = await mountSheet();
    open(inst);

    inst.onConfirm();

    expect(inst.events[0].detail).toMatchObject({ specId: 9, specName: '份', price: 68, quantity: 1 });
  });

  it('数量步进器：加到 3 份 confirm 带上 3，减到边界 1 份不再往下减', async () => {
    const inst = await mountSheet();
    open(inst);

    inst.onQtyPlus();
    inst.onQtyPlus();
    expect(inst.data.qty).toBe(3);
    // 页脚价格 = 单价 × 数量
    expect(inst.data.priceText).toBe('204.00');

    inst.onQtyMinus();
    inst.onQtyMinus();
    inst.onQtyMinus();
    expect(inst.data.qty).toBe(1);

    inst.onConfirm();
    expect(inst.events[0].detail).toMatchObject({ quantity: 1 });
  });

  it('关闭再打开：数量回 1，不会把上一次的份数带进下一次加购', async () => {
    const inst = await mountSheet();
    open(inst);
    inst.onQtyPlus();
    inst.onQtyPlus();

    close(inst);
    open(inst);
    expect(inst.data.qty).toBe(1);

    inst.onConfirm();
    expect(inst.events[0].detail).toMatchObject({ quantity: 1 });
  });

  it('加到封顶 99 份后连点不再上涨', async () => {
    const inst = await mountSheet();
    open(inst);

    for (let i = 0; i < 120; i++) inst.onQtyPlus();

    expect(inst.data.qty).toBe(99);
  });
});
