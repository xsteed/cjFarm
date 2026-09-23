import { computed, reactive, ref } from 'vue';
import type { Ref } from 'vue';
import type { Dish, DishSpec, RemarkOption } from '../types/entities';

export interface CartItem {
  key: string;
  dishId: number;
  dishName: string;
  specId: number;
  specName: string;
  price: number;
  quantity: number;
  remark: string;
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

  function round2(v: number): number {
    return Math.round(v * 100) / 100;
  }

  function currentSpec(d: Dish): DishSpec {
    const specs = d.specs || [];
    return (
      specs.find(s => s.specId === selectedSpec[d.dishId as number]) ||
      specs[0] || { specId: 0, specName: '', price: 0 }
    );
  }

  function dishQty(dishId: number | undefined): number {
    // 按「当前选中规格」的条目计数,与步进器 +/- 的作用对象(changeQty 按 key 定位)对齐:
    // 同菜多规格各自计数,切换规格后卡片显示的是所选规格的份数,显示与操作永远一致。
    const key = `${dishId}-${selectedSpec[dishId as number] || 0}`;
    const f = cart.value.find(i => i.key === key);
    return f ? f.quantity : 0;
  }

  function selectSpec(d: Dish, s: DishSpec): void {
    // 只切换「菜单上当前选中的规格」,不改写购物车里已加的条目。
    // 旧实现按 dishId 找到第一条并改写其 key/价格:同菜多规格时会篡改既有条目
    // (已加小份×2 后切到大份再操作,小份的份数会被并进大份),还会产生重复 key。
    // 现在各规格条目独立,切换后由 changeQty 按所选规格的 key 增减。
    selectedSpec[d.dishId as number] = s.specId as number;
  }

  function changeQty(d: Dish, delta: number): void {
    const spec = currentSpec(d);
    const key = `${d.dishId}-${spec.specId || 0}`;
    const f = cart.value.find(i => i.key === key);
    if (f) {
      f.quantity += delta;
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
        remark: ''
      });
    }
  }

  function changeCartQty(item: CartItem, delta: number): void {
    item.quantity += delta;
    if (item.quantity <= 0) cart.value = cart.value.filter(i => i.key !== item.key);
  }

  function clearCart(): void {
    cart.value = [];
  }

  function openRemark(item: CartItem): void {
    remarkTarget.item = item;
    const parts = (item.remark || '').split('、').filter(Boolean);
    remarkTarget.selected = parts.filter(p => remarks.value.some(r => r.optionName === p));
    remarkTarget.custom = parts.filter(p => !remarks.value.some(r => r.optionName === p)).join('、');
    remarkVisible.value = true;
  }

  function toggleRemark(name: string): void {
    const i = remarkTarget.selected.indexOf(name);
    if (i > -1) remarkTarget.selected.splice(i, 1);
    else remarkTarget.selected.push(name);
  }

  function saveRemark(): void {
    const parts = [...remarkTarget.selected];
    if (remarkTarget.custom.trim()) parts.push(remarkTarget.custom.trim());
    if (remarkTarget.item) remarkTarget.item.remark = parts.join('、');
    remarkVisible.value = false;
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
    saveRemark
  };
}
