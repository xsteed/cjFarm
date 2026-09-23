import { ref } from 'vue';
import type { Ref } from 'vue';
import type { MenuCategory } from '../types/entities';

export function useScrollSpy(menu: Ref<MenuCategory[]>) {
  const activeCat = ref<number | null>(null);
  const dishListRef = ref<HTMLElement | null>(null);
  const groupRefs: Record<number, HTMLElement> = {};

  function setListRef(el: unknown): void {
    dishListRef.value = el as HTMLElement | null;
  }

  function setGroupRef(categoryId: number | undefined, el: unknown): void {
    if (el && categoryId !== undefined) groupRefs[categoryId] = el as HTMLElement;
  }

  // 分组标题相对"滚动内容顶部"的偏移。用 rect 差值算而不是 offsetTop,
  // 这样布局怎么改(如分类栏从横排改竖排)都不会算错。
  function groupOffsetInList(el: HTMLElement, listEl: HTMLElement): number {
    return el.getBoundingClientRect().top - listEl.getBoundingClientRect().top + listEl.scrollTop;
  }

  function jumpTo(categoryId: number | undefined): void {
    activeCat.value = categoryId ?? null;
    if (categoryId === undefined) return;
    const el = groupRefs[categoryId];
    const listEl = dishListRef.value;
    if (el && listEl) {
      listEl.scrollTo({ top: groupOffsetInList(el, listEl), behavior: 'smooth' });
    }
  }

  function handleScroll(): void {
    const listEl = dishListRef.value;
    if (!listEl) return;
    // 已滚到底:末尾分类内容不足一屏时,它的标题永远无法顶到列表顶部,
    // 若不特判,点最后一个分类会一直高亮在上一类。
    if (listEl.scrollTop + listEl.clientHeight >= listEl.scrollHeight - 2) {
      activeCat.value = menu.value[menu.value.length - 1]?.categoryId ?? null;
      return;
    }
    let cur = menu.value[0]?.categoryId ?? null;
    for (const c of menu.value) {
      const id = c.categoryId as number;
      const el = groupRefs[id];
      if (!el) continue;
      // 分组标题已滚到列表顶部 20px 以内,即视为当前分类
      if (groupOffsetInList(el, listEl) <= listEl.scrollTop + 20) cur = id;
    }
    activeCat.value = cur;
  }

  return { activeCat, setListRef, setGroupRef, jumpTo, handleScroll };
}
