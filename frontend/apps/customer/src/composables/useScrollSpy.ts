import { ref } from 'vue';
import type { Ref } from 'vue';
import type { MenuCategory } from '@dining/types';

/** 顶部触发线:分组标题滚到列表顶部 20px 以内,即视为当前分类。 */
const TOP_LINE = 20;
/** 底部触发线:分组标题滚到列表可视区底部 20px 以内,即视为当前分类(仅末尾兜底用)。 */
const BOTTOM_LINE = 20;

export function useScrollSpy(menu: Ref<MenuCategory[]>) {
  const activeCat = ref<number | null>(null);
  const dishListRef = ref<HTMLElement | null>(null);
  const groupRefs: Record<number, HTMLElement> = {};

  function setListRef(el: unknown): void {
    dishListRef.value = el as HTMLElement | null;
  }

  function setGroupRef(categoryId: number | undefined, el: unknown): void {
    if (categoryId === undefined) return;
    // el 为 null 表示标题被卸载(搜索过滤掉该分类时就会发生):必须删掉这一项,
    // 否则失效的旧元素会一直留在表里,滚动联动拿它算出的偏移全是 0,高亮会错。
    // 注意:el 必须是「分组容器」而非分组标题——标题是 position:sticky,被位移后
    // rect.top 不再等于文档流位置,groupOffsetInList 会算出错误偏移
    // (点分类会滚过头,整组内容刚好滚出视口)。
    if (!el) {
      delete groupRefs[categoryId];
      return;
    }
    groupRefs[categoryId] = el as HTMLElement;
  }

  // 分组标题相对"滚动内容顶部"的偏移。用 rect 差值算而不是 offsetTop,
  // 这样布局怎么改(如分类栏从横排改竖排)都不会算错。
  function groupOffsetInList(el: HTMLElement, listEl: HTMLElement): number {
    return el.getBoundingClientRect().top - listEl.getBoundingClientRect().top + listEl.scrollTop;
  }

  /** 末尾兜底区的触发线(距列表可视区顶部的距离):下一类标题起点在首屏底边框之外时用底部线
   *  (标题滚进可视区即切换);整份菜单偏短、标题起点落在首屏里时退用视口中线,
   *  否则底部线会在 scrollTop=0 时抢跑。 */
  function fallbackLine(nextTop: number, viewH: number): number {
    return nextTop > viewH - BOTTOM_LINE ? viewH - BOTTOM_LINE : viewH / 2;
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
    const cats = menu.value;
    if (!cats.length) {
      activeCat.value = null;
      return;
    }
    // 已滚到底:末尾分类内容不足一屏时,它的标题永远无法顶到列表顶部,
    // 若不特判,点最后一个分类会一直高亮在上一类。
    // 但必须先确认「真的能滚」:整份菜单装得下一屏(宽屏/搜索结果少)时,
    // 上面这个条件对任意 scrollTop 都成立,会把高亮永远钉在最后一个分类。
    if (
      listEl.scrollHeight > listEl.clientHeight + 2 &&
      listEl.scrollTop + listEl.clientHeight >= listEl.scrollHeight - 2
    ) {
      activeCat.value = cats[cats.length - 1].categoryId ?? null;
      return;
    }

    const scrollTop = listEl.scrollTop;
    const viewH = listEl.clientHeight;
    // 各分类标题相对「列表内容顶部」的偏移;被搜索过滤掉的分类没有元素,整条跳过
    const offsets = cats.map(c => {
      const el = groupRefs[c.categoryId as number];
      return el ? groupOffsetInList(el, listEl) : null;
    });

    // 1) 顶部触发线:最后一个标题已滚过线的分类即当前分类;
    //    还没有任何标题过线时(刚进页面)留在第一个分类。
    let cur = 0;
    for (let i = 0; i < cats.length; i++) {
      const top = offsets[i];
      if (top !== null && top <= scrollTop + TOP_LINE) cur = i;
    }

    // 2) 末尾兜底:下一分类的标题再滚也顶不到顶部触发线(标题偏移 > 最大滚动距离 + 触发线,
    //    即它连同后面的内容不足一屏)时,旧逻辑只能等「滚到底」特判把高亮一键蹦到最后一类,
    //    中间的短分类(常见如只有一道菜、整块内容不满一屏的分类)整个被跳过,左栏看着就是
    //    「直接划过、不停留」。这里给整个末尾区间换一条不抢跑的触发线,标题滚过线就切高亮:
    //      - 下一类标题起点在首屏底边框之外 -> 底部触发线:标题滚进可视区即切换;
    //      - 标题起点落在首屏里(整份菜单偏短)-> 视口中线:底部线会在 scrollTop=0 时抢跑;
    //      - 标题起点在首屏上半区 -> 触发位置 <= 0 会抢跑,该类只能继续靠「滚到底」特判。
    //    触发位置随标题深度严格递增,来回滚不漏不跳;首个触发位置 > 0,不会刚进页面就误切。
    const nextTop = offsets[cur + 1];
    if (nextTop !== undefined && nextTop !== null && nextTop > listEl.scrollHeight - viewH + TOP_LINE) {
      const line = fallbackLine(nextTop, viewH);
      for (let i = cur + 1; i < cats.length; i++) {
        const top = offsets[i];
        if (top !== null && top > line && top - line <= scrollTop) cur = i;
      }
    }

    activeCat.value = cats[cur]?.categoryId ?? null;
  }

  return { activeCat, setListRef, setGroupRef, jumpTo, handleScroll };
}
