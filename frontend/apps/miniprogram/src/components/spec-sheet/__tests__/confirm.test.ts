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

    expect(inst.events[0].detail).toMatchObject({ specId: 9, specName: '份', price: 68 });
  });
});
