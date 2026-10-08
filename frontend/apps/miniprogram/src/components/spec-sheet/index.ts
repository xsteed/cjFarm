// 规格 / 单项备注弹层。
//
// 对应 Web 端 apps/customer/src/order/RemarkSheet.vue + 菜单里的规格选择：
// H5 把规格 chip 放在菜品行内、备注放弹层；小程序把两者合到一个底部弹层里，
// 少一层交互（菜品行点「+」按默认规格直接加入，点菜名才进这个弹层改规格/备注）。
//
// 备注的组合规则与 H5 **完全一致**：`已选标签 + 自定义文本` 用「、」连接，
// 后端会把它原样打到后厨票上（见 packages/customer-core/src/useCart.ts 的 saveRemark）。
//
// 实现注意：WXML 的表达式只支持运算与属性访问，**不支持函数调用**（如
// `selected.indexOf(x) > -1`），所以选中态一律在 JS 里预计算成 `on` 标记再渲染。
import { money } from '@dining/shared/money';
import type { DishSpec, RemarkOption } from '@dining/types';

/** 渲染用的规格 chip：price 供逻辑使用，priceText 供展示。 */
interface SpecChip {
  specId: number;
  specName: string;
  price: number;
  priceText: string;
  on: boolean;
  /** 该规格已售罄（库存 0），灰显且不可点 */
  soldOut: boolean;
}

interface RemarkChip {
  name: string;
  on: boolean;
}

Component({
  properties: {
    visible: { type: Boolean, value: false },
    dishName: { type: String, value: '' },
    specs: { type: Array, value: [] as DishSpec[] },
    remarks: { type: Array, value: [] as RemarkOption[] },
    /** 打开时的默认规格与既有备注（用于「已在购物车中，再点开修改」） */
    initialSpecId: { type: Number, value: 0 },
    initialRemark: { type: String, value: '' },
    /** 该菜当前在购物车里的份数：>0 时提示「确定 = 再加一份」，免得把改备注误当成加购 */
    inCartQty: { type: Number, value: 0 }
  },

  data: {
    specChips: [] as SpecChip[],
    remarkChips: [] as RemarkChip[],
    custom: '',
    priceText: '0.00',
    multiSpec: false,
    /** 防重入锁：连点「确定」会触发多个 confirm 事件，父页面每收一次就加一份菜（不进模板，只为持有状态） */
    confirming: false
  },

  observers: {
    // 打开时把「既有备注」反解成「命中的选项 + 其余自定义文本」，与 H5 openRemark 同规则
    'visible, initialSpecId, initialRemark'(visible: boolean, initialSpecId: number, initialRemark: string) {
      if (!visible) {
        // 关闭时释放防重入锁，下次打开才能再点「确定」
        this.setData({ confirming: false });
        return;
      }

      const specs = this.data.specs as DishSpec[];
      const names = (this.data.remarks as RemarkOption[]).map(r => String(r.optionName || ''));
      const parts = String(initialRemark || '')
        .split('、')
        .filter(Boolean);
      const specId = Number(initialSpecId) || Number(specs[0]?.specId) || 0;

      this.setData(
        {
          multiSpec: specs.length > 1,
          specChips: specs.map(s => ({
            specId: Number(s.specId),
            specName: String(s.specName || ''),
            price: Number(s.price) || 0,
            priceText: money(s.price),
            on: Number(s.specId) === specId,
            soldOut: Number(s.stock) === 0
          })),
          remarkChips: names.map(name => ({ name, on: parts.includes(name) })),
          custom: parts.filter(p => !names.includes(p)).join('、')
        },
        () => this.syncPrice()
      );
    }
  },

  methods: {
    /** 把当前选中规格的展示价同步到页脚。 */
    syncPrice(): void {
      const cur = (this.data.specChips as SpecChip[]).find(c => c.on);
      this.setData({ priceText: cur ? cur.priceText : '0.00' });
    },

    noop(): void {
      /* 吞掉弹层内部的点击，避免冒泡到遮罩触发关闭 */
    },

    onClose(): void {
      this.triggerEvent('close');
    },

    onPickSpec(e: WechatMiniprogram.TouchEvent): void {
      const specId = Number(e.currentTarget.dataset.specid) || 0;
      const target = (this.data.specChips as SpecChip[]).find(c => c.specId === specId);
      if (!target || target.soldOut) return; // 售罄规格不可选
      const chips = (this.data.specChips as SpecChip[]).map(c => ({ ...c, on: c.specId === specId }));
      this.setData({ specChips: chips }, () => this.syncPrice());
    },

    onToggleRemark(e: WechatMiniprogram.TouchEvent): void {
      const name = String(e.currentTarget.dataset.name || '');
      const chips = (this.data.remarkChips as RemarkChip[]).map(c => (c.name === name ? { ...c, on: !c.on } : c));
      this.setData({ remarkChips: chips });
    },

    onCustomInput(e: WechatMiniprogram.Input): void {
      this.setData({ custom: e.detail.value });
    },

    onConfirm(): void {
      // 连点会走多次 confirm，父页面每收一次就 +1 份；锁到弹层关闭再释放
      if (this.data.confirming) return;
      const spec = (this.data.specChips as SpecChip[]).find(c => c.on);
      if (!spec || spec.soldOut) return; // 当前规格已售罄，不可确认（onPickSpec 已拦，这里兜底）
      this.setData({ confirming: true });
      const selected = (this.data.remarkChips as RemarkChip[]).filter(c => c.on).map(c => c.name);
      const custom = String(this.data.custom || '').trim();
      if (custom) selected.push(custom);
      this.triggerEvent('confirm', {
        specId: spec ? spec.specId : 0,
        specName: spec ? spec.specName : '',
        price: spec ? spec.price : 0,
        itemRemark: selected.join('、')
      });
    }
  }
});
