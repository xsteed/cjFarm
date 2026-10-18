// 「左栏分类 ↔ 右栏滚动」联动的单测。
//
// 这块逻辑全建立在节点测量上(分组标题相对列表内容顶部的偏移),单测里量不到真实布局,
// 所以用假元素把偏移摆好,只验证判定本身:
//   ・标题滚到列表顶部 20px 内 → 高亮切过去,往回滚切回来
//   ・已滚到底 → 高亮最后一项(末尾内容不足一屏时,标题永远顶不到列表顶部)
//   ・回归:末尾只有一道菜的分类,滚过时高亮必须逐一停留,
//     不能从上一类直接蹦到最后(旧逻辑只有「滚到底」特判,短分类整个被跳过)
//   ・整份菜单偏短(标题起点落在首屏里):刚进页面不抢跑,滚过去也能选中
import { ref } from 'vue';
import { describe, expect, it } from 'vitest';
import { useScrollSpy } from '../useScrollSpy';
import type { MenuCategory } from '@dining/types';

/** 一道菜的行高约 106px(84px 图 + 11px*2 内边距),分组标题约 29px,
 *  只有一道菜的分类 ≈ 135px;列表底部还有 96px 内边距(见 MenuView 的 .op-list)。 */
const DISH_H = 106;
const TITLE_H = 29;
const PAD_BOTTOM = 96;
const VIEW_H = 700;

interface CatSpec {
  id: number;
  /** 分类标题相对列表内容顶部的偏移(px) */
  top: number;
  /** 分类整块高度(px) */
  height: number;
}

function cat(id: number, top: number, dishCount: number): CatSpec {
  return { id, top, height: TITLE_H + dishCount * DISH_H };
}

/** 按布局搭好假列表 + 假标题元素:标题的 rect.top 随 scrollTop 联动(与真实滚动一致),
 *  groupOffsetInList 由此算回固定偏移。 */
function mountSpy(cats: CatSpec[]) {
  const list = {
    scrollTop: 0,
    clientHeight: VIEW_H,
    get scrollHeight(): number {
      const last = cats[cats.length - 1];
      return last.top + last.height + PAD_BOTTOM;
    },
    getBoundingClientRect(): { top: number } {
      return { top: 0 };
    },
    scrollTo({ top }: { top: number }): void {
      this.scrollTop = top;
    }
  };
  const menu = ref<MenuCategory[]>(cats.map(c => ({ categoryId: c.id, categoryName: `分类${c.id}` })));
  const spy = useScrollSpy(menu);
  spy.setListRef(list);
  for (const c of cats) {
    spy.setGroupRef(c.id, {
      getBoundingClientRect(): { top: number } {
        return { top: c.top - list.scrollTop };
      }
    });
  }
  return {
    spy,
    /** 滚到指定位置并触发一次 scroll(等价于真实滚动里的一帧) */
    scrollTo(s: number): void {
      list.scrollTop = s;
      spy.handleScroll();
    }
  };
}

describe('useScrollSpy · 顶部触发线(常规长分类)', () => {
  // 三类各 10 道菜,每类都超过一屏
  const LONG = [cat(1, 0, 10), cat(2, 1089, 10), cat(3, 2178, 10)];

  it('标题滚过顶部 20px 线才切高亮,往回滚切回去', () => {
    const { spy, scrollTo } = mountSpy(LONG);
    scrollTo(0);
    expect(spy.activeCat.value).toBe(1);
    scrollTo(500);
    expect(spy.activeCat.value).toBe(1);
    scrollTo(1109); // 第 2 类标题(1089)刚过线
    expect(spy.activeCat.value).toBe(2);
    scrollTo(1000);
    expect(spy.activeCat.value).toBe(1);
    scrollTo(2198); // 第 3 类标题(2178)刚过线
    expect(spy.activeCat.value).toBe(3);
  });

  it('已滚到底:高亮最后一个分类', () => {
    const { spy, scrollTo } = mountSpy(LONG);
    scrollTo(3363 - VIEW_H); // 2663,最大滚动距离
    expect(spy.activeCat.value).toBe(3);
  });

  it('跳转到某分类:高亮并滚到它的标题偏移', () => {
    const { spy } = mountSpy(LONG);
    spy.jumpTo(2);
    expect(spy.activeCat.value).toBe(2);
    // 平滑滚动结束停在标题偏移处,此时高亮应仍在第 2 类
    spy.handleScroll();
    expect(spy.activeCat.value).toBe(2);
  });
});

describe('useScrollSpy · 末尾短分类不再被跳过', () => {
  // 第 1 类 20 道菜(一屏以上),末尾三类各只有一道菜:
  // 它们连同后面的内容不足一屏,标题再滚也顶不到顶部触发线。
  const TAIL = [cat(1, 0, 20), cat(2, 2149, 1), cat(3, 2284, 1), cat(4, 2419, 1)];

  it('向下滚:每个短分类都被选中停留,不会从上一类直接蹦到最后', () => {
    const { spy, scrollTo } = mountSpy(TAIL);
    const seen: (number | null)[] = [];
    for (const s of [0, 500, 1000, 1469, 1604, 1739, 1947]) {
      scrollTo(s);
      seen.push(spy.activeCat.value);
    }
    // 旧逻辑在这里只会是 1,1,1,1,1,1,4 —— 第 2/3/4 类整个被跳过
    expect(seen).toEqual([1, 1, 1, 2, 3, 4, 4]);
  });

  it('向上滚:同样逐类回落,不漏不跳', () => {
    const { spy, scrollTo } = mountSpy(TAIL);
    scrollTo(1947);
    expect(spy.activeCat.value).toBe(4);
    scrollTo(1700);
    expect(spy.activeCat.value).toBe(3);
    scrollTo(1500);
    expect(spy.activeCat.value).toBe(2);
    scrollTo(1400);
    expect(spy.activeCat.value).toBe(1);
  });

  it('滚到底仍由特判收尾:高亮最后一个分类', () => {
    const { spy, scrollTo } = mountSpy(TAIL);
    scrollTo(1948); // 2650 - 700 - 2,触底
    expect(spy.activeCat.value).toBe(4);
  });
});

describe('useScrollSpy · 整份菜单偏短', () => {
  // 共约 1.8 屏:第 1 类 6 道菜,末尾两类短(1 道 / 3 道)。
  // 第 2 类标题(665)落在首屏里,底部触发线会抢跑,改用视口中线。
  const SHORT = [cat(1, 0, 6), cat(2, 665, 1), cat(3, 800, 3)];

  it('刚进页面不抢跑:停在第一个分类', () => {
    const { spy, scrollTo } = mountSpy(SHORT);
    scrollTo(0);
    expect(spy.activeCat.value).toBe(1);
    scrollTo(300);
    expect(spy.activeCat.value).toBe(1);
  });

  it('标题滚到视口中线:短分类也能被选中并停留', () => {
    const { spy, scrollTo } = mountSpy(SHORT);
    scrollTo(315); // 第 2 类标题(665)滚到屏幕中间
    expect(spy.activeCat.value).toBe(2);
    scrollTo(449);
    expect(spy.activeCat.value).toBe(2);
    scrollTo(450); // 第 3 类标题(800)滚到屏幕中间
    expect(spy.activeCat.value).toBe(3);
    scrollTo(400); // 往回滚逐类回落
    expect(spy.activeCat.value).toBe(2);
  });
});

describe('useScrollSpy · 标题起点在首屏上半区', () => {
  // 整份菜单几乎一屏(742px):第 2 类标题(241)在首屏上半区,
  // 任何触发线都会在 scrollTop=0 时抢跑,只能继续靠「滚到底」特判。
  const TINY = [cat(1, 0, 2), cat(2, 241, 1), cat(3, 376, 1), cat(4, 511, 1)];

  it('不进页面就误切:停在第一个分类,滚到底落在最后一类', () => {
    const { spy, scrollTo } = mountSpy(TINY);
    scrollTo(0);
    expect(spy.activeCat.value).toBe(1);
    scrollTo(40); // 742 - 700 - 2,触底
    expect(spy.activeCat.value).toBe(4);
  });
});
