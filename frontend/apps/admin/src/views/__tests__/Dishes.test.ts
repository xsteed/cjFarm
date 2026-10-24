import { mount } from '@vue/test-utils';
import type { VueWrapper } from '@vue/test-utils';
import TDesign from 'tdesign-vue-next';
import Dishes from '../Dishes.vue';
import { listCategories, listDishes, sortDish } from '@dining/api';
import { setAuth } from '@dining/auth';
import { clearAllStorage, flushPromises, stubMatchMedia, stubResizeObserver } from '@shell/test/mocks.ts';
import type { Dish, MenuCategory } from '@dining/types';

// 与 auth 包共享同一份 @dining/api 模块,漏掉 onAuthCleared/getProfile/logoutRemember
// 会让 @dining/auth 加载失败;这里的接口全部换桩,避免真的走 axios。
vi.mock('@dining/api', () => ({
  onAuthCleared: vi.fn(),
  getProfile: vi.fn(),
  logoutRemember: vi.fn(),
  listDishes: vi.fn(),
  listCategories: vi.fn(),
  saveDish: vi.fn(),
  updateDish: vi.fn(),
  deleteDish: vi.fn(),
  saveCategory: vi.fn(),
  uploadFile: vi.fn(),
  sortDish: vi.fn()
}));

// 保存成功的 MessagePlugin 会往 body 挂 toast,测试里不需要
vi.mock('tdesign-vue-next', async importOriginal => {
  const actual = (await importOriginal()) as Record<string, unknown>;
  return {
    ...actual,
    MessagePlugin: { success: vi.fn(), warning: vi.fn(), error: vi.fn(), info: vi.fn(), close: vi.fn() }
  };
});

// 图标在 main.ts 里全局注册,测试环境没有
const iconStubs = {
  'search-icon': { template: '<i />' },
  'add-icon': { template: '<i />' }
};

const sortDishMock = vi.mocked(sortDish);

const categories: MenuCategory[] = [
  { categoryId: 1, categoryName: '凉菜', sortOrder: 1 },
  { categoryId: 2, categoryName: '热菜', sortOrder: 2 }
];

function dish(id: number, name: string, categoryId: number, categoryName: string, sortOrder: number): Dish {
  return {
    dishId: id,
    dishName: name,
    categoryId,
    categoryName,
    status: 1,
    soldOut: 0,
    signature: 0,
    sortOrder,
    specs: [{ specId: id * 10, specName: '份', price: 1000, stock: -1 }]
  };
}

// 后端已按「分类顺序 + 组内排序」返回,前端只需照序分组渲染
const dishes: Dish[] = [
  dish(1, '拍黄瓜', 1, '凉菜', 10),
  dish(2, '拌木耳', 1, '凉菜', 20),
  dish(3, '红烧肉', 2, '热菜', 30)
];

/** jsdom 没有 PointerEvent:用 MouseEvent 补齐指针事件关心的几个字段。 */
function firePointer(el: Element, type: string, x: number, y: number, pointerType = 'mouse'): void {
  const ev = new MouseEvent(type, { bubbles: true, cancelable: true, clientX: x, clientY: y });
  Object.defineProperty(ev, 'pointerId', { value: 1 });
  Object.defineProperty(ev, 'pointerType', { value: pointerType });
  el.dispatchEvent(ev);
}

// 必须挂到 document 上:拖拽的 move/up 监听在 window 上,
// 游离的 DOM 树里事件冒泡到不了 window,手势会「起了但不跟手」。
function mountDishes(): VueWrapper {
  return mount(Dishes, {
    attachTo: document.body,
    global: {
      plugins: [TDesign],
      stubs: { ...iconStubs },
      config: { errorHandler: () => undefined }
    }
  });
}

let restoreMatchMedia: () => void;
let restoreResizeObserver: () => void;
let mounted: VueWrapper | null = null;

beforeEach(() => {
  // 桌面断点:视图由 useViewMode 的本地缓存决定(默认卡片),两个用例各用各的视图
  restoreMatchMedia = stubMatchMedia();
  restoreResizeObserver = stubResizeObserver();
  clearAllStorage();
  setAuth({ token: 't', username: 'u', perms: ['dish:edit', 'category:view'] });

  vi.mocked(listDishes).mockResolvedValue({
    total: dishes.length,
    items: dishes.map(d => ({ ...d, specs: [...(d.specs || [])] }))
  });
  vi.mocked(listCategories).mockResolvedValue({ total: categories.length, items: categories });
  sortDishMock.mockReset();
  sortDishMock.mockResolvedValue({ items: [] });
});

afterEach(() => {
  mounted?.unmount();
  mounted = null;
  restoreMatchMedia();
  restoreResizeObserver();
});

describe('菜品管理页', () => {
  it('卡片视图按分类顺序分组,拖拽卡片会把新位置写回后端', async () => {
    const wrapper = mountDishes();
    mounted = wrapper;
    await flushPromises();

    const titles = wrapper.findAll('.grid-group-t').map(t => t.text());
    expect(titles[0]).toContain('凉菜');
    expect(titles[1]).toContain('热菜');

    const names = () => wrapper.findAll('.dish-card').map(c => c.find('.n').text());
    expect(names()).toEqual(['拍黄瓜', '拌木耳', '红烧肉']);

    // 把第一张卡拖到同组末尾:jsdom 没有布局,落点按「组末尾」计算
    const cards = wrapper.findAll('.dish-card');
    firePointer(cards[0].element, 'pointerdown', 10, 10);
    firePointer(cards[0].element, 'pointermove', 10, 80);
    firePointer(cards[0].element, 'pointerup', 10, 80);
    await flushPromises();

    expect(sortDishMock).toHaveBeenCalledTimes(1);
    // 锚点只取同分类邻居:前一个是「拌木耳」,后一个已跨到热菜,不带 nextDishId
    expect(sortDishMock.mock.calls[0][0]).toMatchObject({ dishId: 1, prevDishId: 2, nextDishId: undefined });
    // 本地顺序立即更新,不必等刷新
    expect(names()).toEqual(['拌木耳', '拍黄瓜', '红烧肉']);
  });

  it('列表视图每行带拖拽定位属性,分类名只在组首出现', async () => {
    localStorage.setItem('admin_view_mode:u:dishes', 'table');
    const wrapper = mountDishes();
    mounted = wrapper;
    await flushPromises();

    const rows = wrapper.findAll('.table-scroll tbody tr[data-row-index]');
    expect(rows.length).toBe(3);
    // 拖拽落位靠这个属性把 DOM 行映射回数据下标,丢了就整行拖不动
    expect(rows.map(r => r.attributes('data-row-index'))).toEqual(['0', '1', '2']);
    expect(rows[0].text()).toContain('凉菜');
    expect(rows[1].text()).not.toContain('凉菜');
    expect(rows[0].find('[data-drag-handle]').exists()).toBe(true);

    firePointer(rows[0].element, 'pointerdown', 10, 10);
    firePointer(rows[0].element, 'pointermove', 10, 300);
    firePointer(rows[0].element, 'pointerup', 10, 300);
    await flushPromises();

    expect(sortDishMock).toHaveBeenCalledTimes(1);
    expect(sortDishMock.mock.calls[0][0]).toMatchObject({ dishId: 1, prevDishId: 2, nextDishId: undefined });
  });
});
