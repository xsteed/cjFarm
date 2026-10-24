import { computed, reactive, ref } from 'vue';
import type { Ref } from 'vue';
import { ui } from '@dining/shared';
import { round2 } from '@dining/shared/money';
import { isOrderable, isSoldOut } from './spec';
import type { Dish, DishSpec, RemarkOption } from '@dining/types';

export interface CartItem {
  key: string;
  dishId: number;
  dishName: string;
  specId: number;
  specName: string;
  price: number;
  quantity: number;
  remark: string;
  /** 加购时该规格的剩余份数快照：-1 = 不限量，>=0 为可购份数。
   *  购物车抽屉里的 + 拿不到菜单对象，靠它夹紧（后端下单事务按 stock >= qty 校验）。 */
  stock: number;
}

/** 规格剩余份数归一：-1 / 非数字 / 负数视为不限量，>=0 为可购份数（与后端 po.Spec 一致）。 */
function rawStock(spec: { stock?: number | null }): number {
  const n = Number(spec.stock);
  return Number.isFinite(n) && n >= 0 ? n : -1;
}

/** 可加购上限；不限量时返回 Infinity，便于统一比较。 */
function stockCap(spec: { stock?: number | null }): number {
  const s = rawStock(spec);
  return s < 0 ? Number.POSITIVE_INFINITY : s;
}

export interface RemarkTarget {
  item: CartItem | null;
  selected: string[];
  custom: string;
}

export function useCart(remarks: Ref<RemarkOption[]>) {
  const cart = ref<CartItem[]>([]);
  // 每道菜当前选中的规格(默认第一个)
  const selectedSpec = reactive<Record<number, number>>({});
  const remarkVisible = ref(false);
  const remarkTarget = reactive<RemarkTarget>({ item: null, selected: [], custom: '' });

  const totalCount = computed(() => cart.value.reduce((s, i) => s + i.quantity, 0));
  const dishTotal = computed(() => round2(cart.value.reduce((s, i) => s + i.price * i.quantity, 0)));

  function currentSpec(d: Dish): DishSpec {
    const specs = d.specs || [];
    const selected = specs.find(s => s.specId === selectedSpec[d.dishId as number]);
    // 选中的规格若已售罄（库存 0），退回第一个还有库存的规格，保证价格/步进器指向可点的东西。
    if (selected && Number(selected.stock) !== 0) return selected;
    return specs.find(s => Number(s.stock) !== 0) || selected || specs[0] || { specId: 0, specName: '', price: 0 };
  }

  function dishQty(dishId: number | undefined, dish?: Dish): number {
    // 按「当前生效规格」的条目计数,与步进器 +/- 的作用对象(changeQty 里的 currentSpec)对齐:
    // 同菜多规格各自计数;所选规格沽清后 currentSpec 会回退到有货的规格,这里必须跟着回退,
    // 否则卡片显示的份数与 +/- 实际改的条目是两条不同的购物车行,「显示与操作永远一致」不成立。
    const specId = (dish ? currentSpec(dish).specId : selectedSpec[dishId as number]) || 0;
    const key = `${dishId}-${specId}`;
    const f = cart.value.find(i => i.key === key);
    return f ? f.quantity : 0;
  }

  function selectSpec(d: Dish, s: DishSpec): void {
    // 售罄规格（库存 0）不可选。
    if (Number(s.stock) === 0) return;
    // 只切换「菜单上当前选中的规格」,不改写购物车里已加的条目。
    // 旧实现按 dishId 找到第一条并改写其 key/价格:同菜多规格时会篡改既有条目
    // (已加小份×2 后切到大份再操作,小份的份数会被并进大份),还会产生重复 key。
    // 现在各规格条目独立,切换后由 changeQty 按所选规格的 key 增减。
    selectedSpec[d.dishId as number] = s.specId as number;
  }

  function changeQty(d: Dish, delta: number): void {
    // 只有「加」才校验:减号操作的是购物车里已有的条目,不该因为今天沽清就减不掉。
    if (delta > 0) {
      if (!isOrderable(d)) {
        // 小程序的 ui 适配器只分 success/warning/error,这里的 info 档落到 warning
        ui.warning('该菜品暂不可点，请联系店员');
        return;
      }
      // 售罄菜不下单：后端下单时也会拦（ResolveOrderItems），这里提前挡在 UI 层，
      // 避免顾客点到最后一步才收到「已售罄」。
      if (isSoldOut(d)) {
        ui.warning('该菜品已售罄，看看别的吧');
        return;
      }
    }
    const spec = currentSpec(d);
    const cap = stockCap(spec);
    // 只有「加」才受沽清影响:减号操作的是购物车里已有的条目,不该因为该规格今天沽清
    // 就减不掉（否则卡片步进器会「点不动」,顾客只能去购物车抽屉里删）。
    if (delta > 0 && cap === 0) return;
    const key = `${d.dishId}-${spec.specId || 0}`;
    const f = cart.value.find(i => i.key === key);
    if (f) {
      // 按剩余份数夹紧:stock=2 时不能加到 5 份(后端下单事务按 stock >= qty 校验,
      // 超出只会让顾客在提交时整单失败)。
      if (delta > 0 && f.quantity + delta > cap) {
        ui.warning(`该规格仅剩 ${cap} 份`);
        f.stock = rawStock(spec);
        f.quantity = cap;
        return;
      }
      f.quantity += delta;
      f.stock = rawStock(spec);
      if (f.quantity <= 0) cart.value = cart.value.filter(i => i.key !== key);
    } else if (delta > 0) {
      cart.value.push({
        key,
        dishId: d.dishId as number,
        dishName: d.dishName || '',
        specId: spec.specId as number,
        specName: spec.specName || '',
        price: Number(spec.price) || 0,
        quantity: 1,
        remark: '',
        stock: rawStock(spec)
      });
    }
  }

  function changeCartQty(item: CartItem, delta: number): void {
    // 抽屉里的 + 也要按加购时记下的库存快照夹紧（这里拿不到菜单对象，用快照）。
    if (delta > 0 && item.stock >= 0 && item.quantity + delta > item.stock) {
      ui.warning(item.stock > 0 ? `该规格仅剩 ${item.stock} 份` : '该规格已售罄');
      return;
    }
    item.quantity += delta;
    if (item.quantity <= 0) cart.value = cart.value.filter(i => i.key !== item.key);
  }

  function clearCart(): void {
    cart.value = [];
  }

  /** 把已存的备注反解成「命中的选项 + 其余自定义文本」(与小程序 spec-sheet 同规则)。 */
  function parseRemark(remark: string): { selected: string[]; custom: string } {
    const names = remarks.value.map(r => String(r.optionName || ''));
    const parts = String(remark || '')
      .split('、')
      .filter(Boolean);
    return {
      selected: parts.filter(p => names.includes(p)),
      custom: parts.filter(p => !names.includes(p)).join('、')
    };
  }

  function openRemark(item: CartItem): void {
    remarkTarget.item = item;
    const parsed = parseRemark(item.remark);
    remarkTarget.selected = parsed.selected;
    remarkTarget.custom = parsed.custom;
    remarkVisible.value = true;
  }

  function toggleRemark(name: string): void {
    const i = remarkTarget.selected.indexOf(name);
    if (i > -1) remarkTarget.selected.splice(i, 1);
    else remarkTarget.selected.push(name);
  }

  /** 关闭但不写入:与 saveRemark 严格区分,顾客「看看选项又不想改」时不该动到购物车。 */
  function closeRemark(): void {
    resetRemarkTarget();
  }

  /** 清空当前编辑的选项与自定义文本(真正生效要再点「确定」)。 */
  function clearRemark(): void {
    remarkTarget.selected = [];
    remarkTarget.custom = '';
  }

  function saveRemark(): void {
    const parts = [...remarkTarget.selected];
    if (remarkTarget.custom.trim()) parts.push(remarkTarget.custom.trim());
    if (remarkTarget.item) remarkTarget.item.remark = parts.join('、');
    resetRemarkTarget();
  }

  /** 收尾:关弹层 + 丢掉对购物车行的引用(该行可能已被减到 0 移除)。 */
  function resetRemarkTarget(): void {
    remarkVisible.value = false;
    remarkTarget.item = null;
    remarkTarget.selected = [];
    remarkTarget.custom = '';
  }

  // ---------- 规格 / 备注弹层(对齐小程序 components/spec-sheet) ----------
  const sheetVisible = ref(false);
  const sheetDish = ref<Dish | null>(null);
  const sheetSpecId = ref(0);
  const sheetRemark = reactive({ selected: [] as string[], custom: '' });
  /** 该菜在购物车里的份数(同菜多规格合计,与小程序 cartDishQty 同口径) */
  const sheetInCartQty = ref(0);
  /** 弹层里的购买数量:招牌菜 / 多规格菜都走弹层,下单位数在这里定,默认 1 */
  const sheetQty = ref(1);
  /** 防重入:连点「确定」会来多次 confirm,每收一次就多加一份 */
  let sheetConfirming = false;

  function openSheet(d: Dish): void {
    if (isSoldOut(d)) {
      ui.warning('该菜品已售罄，看看别的吧');
      return;
    }
    if (!isOrderable(d)) {
      ui.warning('该菜品暂不可点，请联系店员');
      return;
    }
    // 打开时定位到「这道菜当前选中的规格」,连同该规格已填的备注一起带进来,点开即可改
    const spec = currentSpec(d);
    const line = cart.value.find(i => i.key === `${d.dishId}-${spec.specId || 0}`);
    sheetDish.value = d;
    sheetSpecId.value = Number(spec.specId) || 0;
    const parsed = parseRemark(line ? line.remark : '');
    sheetRemark.selected = parsed.selected;
    sheetRemark.custom = parsed.custom;
    sheetInCartQty.value = cart.value.filter(i => i.dishId === d.dishId).reduce((s, i) => s + i.quantity, 0);
    sheetQty.value = 1;
    sheetVisible.value = true;
  }

  function closeSheet(): void {
    sheetVisible.value = false;
    sheetDish.value = null;
    sheetSpecId.value = 0;
    sheetRemark.selected = [];
    sheetRemark.custom = '';
    sheetInCartQty.value = 0;
    sheetQty.value = 1;
    sheetConfirming = false;
  }

  function pickSheetSpec(specId: number): void {
    const s = (sheetDish.value?.specs || []).find(x => Number(x.specId) === specId);
    if (!s || Number(s.stock) === 0) return; // 售罄规格点不动
    sheetSpecId.value = Number(s.specId) || 0;
    // 切规格时把「新规格那一行」已有的备注带进来:否则会把旧规格的备注写进新规格，
    // 顺带覆盖掉新规格已经填好的内容。
    const line = cart.value.find(i => i.key === `${sheetDish.value?.dishId}-${Number(s.specId) || 0}`);
    const parsed = parseRemark(line ? line.remark : '');
    sheetRemark.selected = parsed.selected;
    sheetRemark.custom = parsed.custom;
  }

  function toggleSheetRemark(name: string): void {
    const i = sheetRemark.selected.indexOf(name);
    if (i > -1) sheetRemark.selected.splice(i, 1);
    else sheetRemark.selected.push(name);
  }

  /** 加购;同一 dish+spec 合并成一行(与小程序 cartAdd 同规则:合并时覆盖备注)。qty 默认 1(弹层可选多份)。 */
  function addToCart(d: Dish, spec: DishSpec, remark?: string, qty = 1): void {
    const key = `${d.dishId}-${spec.specId || 0}`;
    const cap = stockCap(spec);
    // qty 收敛:弹层可能传 0/负数/小数(与小程序 cartAdd 的 Math.max(1, floor) 同规则)
    const want = Math.max(1, Math.floor(Number(qty) || 1));
    const exist = cart.value.find(i => i.key === key);
    const base = exist ? exist.quantity : 0;
    let add = want;
    if (Number.isFinite(cap) && base + add > cap) {
      // 超出剩余份数:夹到上限并明确告知,别让顾客到提交才被后端拒
      add = Math.max(0, cap - base);
      if (add <= 0) {
        ui.warning(cap > 0 ? `该规格仅剩 ${cap} 份` : '该商品已售罄');
        return;
      }
      ui.warning(`该规格仅剩 ${cap} 份，已按上限加入`);
    }
    if (exist) {
      exist.quantity += add;
      exist.stock = rawStock(spec);
      if (remark !== undefined) exist.remark = remark;
      return;
    }
    cart.value.push({
      key,
      dishId: d.dishId as number,
      dishName: d.dishName || '',
      specId: spec.specId as number,
      specName: spec.specName || '',
      price: Number(spec.price) || 0,
      quantity: add,
      remark: remark ?? '',
      stock: rawStock(spec)
    });
  }

  function confirmSheet(): void {
    const d = sheetDish.value;
    if (!d || sheetConfirming) return;
    const spec = (d.specs || []).find(s => Number(s.specId) === sheetSpecId.value);
    if (!spec || Number(spec.stock) === 0) return; // 售罄规格兜底再拦一次
    sheetConfirming = true;
    const parts = [...sheetRemark.selected];
    const custom = sheetRemark.custom.trim();
    if (custom) parts.push(custom);
    // 弹层里选的规格就是这道菜之后的「当前规格」:行内 chip / 价格 / 步进器都要跟着走
    selectedSpec[d.dishId as number] = sheetSpecId.value;
    addToCart(d, spec, parts.join('、'), sheetQty.value);
    ui.success('已加入购物车');
    closeSheet();
  }

  return {
    cart,
    selectedSpec,
    remarkVisible,
    remarkTarget,
    totalCount,
    dishTotal,
    round2,
    currentSpec,
    dishQty,
    selectSpec,
    changeQty,
    changeCartQty,
    clearCart,
    openRemark,
    toggleRemark,
    closeRemark,
    clearRemark,
    saveRemark,
    sheetVisible,
    sheetDish,
    sheetSpecId,
    sheetRemark,
    sheetInCartQty,
    sheetQty,
    openSheet,
    closeSheet,
    pickSheetSpec,
    toggleSheetRemark,
    confirmSheet,
    addToCart
  };
}
