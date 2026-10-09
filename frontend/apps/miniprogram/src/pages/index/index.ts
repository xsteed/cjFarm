// 落地页 + 点餐主页。
//
// 进入方式：扫小程序码（scene=桌台码）/ 扫普通二维码跳转 / 从「最近使用」重新进入
// （后者没有参数，用本地缓存的桌台码兜底）。
//
// 流程：解析桌台码 → 拉公开配置 / 桌台 / 菜单 / 备注 → 本桌已有进行中订单则直接进订单页，
// 否则渲染菜单。交互：左侧分类、右侧菜品，「+」快速加入（多规格时进规格弹层），
// 点菜名进规格/备注弹层；份数增减与提交下单都在购物车页。
import { getMenu, getPublicConfig, getRemarks, getTable } from '../../api/client';
import { devTableCode, mediaUrl } from '../../config';
import { cartAdd, cartChangeQty, cartCount, cartDishQty, cartSettle, cartState } from '../../store/cart';
import { memberEnabled } from '../../store/member';
import {
  applyConfig,
  applyOrder,
  applyTable,
  clearCurrentOrder,
  readPersisted,
  sessionState
} from '../../store/session';
import { toast } from '../../utils/toast';
import { resolveTableCode } from '../../utils/scene';
import { money } from '@dining/shared/money';
import { ORDER_STATUS } from '@dining/shared/dicts';
import { isActiveStatus } from '@dining/shared/orderStatus';
// 搜索与「几个规格算多」都与 H5 共用同一份实现：改一处两端同步
import { filterMenu } from '@dining/customer-core/search';
import { INLINE_SPEC_MAX } from '@dining/customer-core/spec';
import type { Dish, DishSpec, MenuCategory, OrderDetail, RemarkOption, TableInfo } from '@dining/types';

/** 右栏顶部的锚点 id：换搜索词后要把列表拉回顶部（见 scrollListTop）。 */
const LIST_TOP_ANCHOR = 'list-top';

/** 规格 chip：菜品行内直接点选的那几个规格。 */
interface SpecChip {
  /** 带上 dishId：模板里嵌套 wx:for 后拿不到外层行的字段 */
  dishId: number;
  specId: number;
  specName: string;
  on: boolean;
  /** 该规格已售罄（库存 0），灰显且不可点 */
  soldOut: boolean;
}

/** 菜品行的展示模型（WXML 不做函数调用，要显示的值都在这里算好）。 */
interface DishRow {
  dishId: number;
  dishName: string;
  description: string;
  image: string;
  priceText: string;
  /** 规格多到行内放不下（> INLINE_SPEC_MAX）：价格只能给最低价 +「起」，＋ 也要进弹层 */
  manySpecs: boolean;
  /** 有没有规格可选。没有的话这道菜根本下不了单（历史数据可能留下这种菜） */
  orderable: boolean;
  /** 已售罄：上架中但今日沽清，顾客端仍展示（知道有什么），但置灰不可点 */
  soldOut: boolean;
  /** 当前选中的规格 id：步进器要用 `菜品:规格` 定位购物车行 */
  specId: number;
  /** 单规格的规格名（只有单规格才显示，多规格由 chip 体现） */
  specName: string;
  /** 行内展示的规格 chip（空数组 = 不展示，走弹层） */
  specChips: SpecChip[];
  /** 本单已点份数（当前订单里累加） */
  orderedQty: number;
  /** 当前选中规格在购物车里的份数（与 H5 dishQty 同口径：只算所选规格） */
  cartQty: number;
}

/**
 * 右栏连续列表中的一行：分类标题 / 空分类提示 / 菜品。
 *
 * 用「扁平的一行」而不是「分类 → 菜品」两层嵌套：小程序模板里 wx:for 的别名
 * 会按未定义字段被 scripts/audit-wxml.mjs 拦下（除非把别名加进它的白名单）。
 * 扁平化还有个好处——分类之间是自然衔接的，上一个分类看完直接接下一个分类，
 * 内容少的分类不会在右侧留出一大片空白，让顾客误以为「这个分类只有这么点」。
 */
type MenuRow =
  | { kind: 'sec'; key: string; anchor: string; categoryId: number; categoryName: string }
  | { kind: 'empty'; key: string }
  | ({ kind: 'dish'; key: string } & DishRow);

/** 分类标题的锚点：跳转用 categoryId 查，测量偏移用下标与标题顺序对齐。 */
interface SectionRef {
  categoryId: number;
  anchor: string;
}

/** 分类标题相对「列表内容顶部」的偏移，滚动时据此判断当前分类。 */
interface SectionOffset {
  categoryId: number;
  top: number;
}

interface CatRow {
  categoryId: number;
  categoryName: string;
  /** 左栏自身的 scroll-into-view 锚点：分类多时把当前项带回可视区 */
  navId: string;
}

interface ServingRow {
  key: string;
  /** 订单号：点击可跳去订单页看该单的出餐进度 */
  orderNo: string;
  label: string;
}

interface SheetDish {
  dishName: string;
  specs: DishSpec[];
  initialSpecId: number;
  initialRemark: string;
}

/**
 * 加入购物车所需的最小规格信息。
 * 字段都可选，这样 DishSpec（来自接口）与弹层 confirm 事件（来自组件）都能直接传入，
 * 不必在两处之间做类型断言。
 */
interface SpecPick {
  specId?: number;
  specName?: string;
  price?: number;
}

Page({
  data: {
    loading: true,
    error: '',
    // 「我的」tab 是否显示：跟随后端开关；配置到达前先隐藏，避免闪现
    showProfile: false,
    shopName: '扫码点餐',
    tableLabel: '',
    cats: [] as CatRow[],
    activeCatId: 0,
    /** 右栏：所有分类首尾相连成一条列表，滚动时自动切换左侧高亮 */
    rows: [] as MenuRow[],
    /** scroll-view 的 scroll-into-view 目标：点左侧分类时跳过去 */
    scrollIntoId: '',
    /** 左栏自己的 scroll-into-view 目标：分类多时让高亮项始终可见 */
    navIntoId: '',
    /** 搜索词（空串 = 未搜索，右栏回到正常的分类浏览） */
    keyword: '',
    /** 搜索结果态：隐藏左栏（分类对搜出来的结果没有意义）、列表回到顶部 */
    searching: false,
    /** 搜了但没有命中：右栏显示「没找到」而不是空白 */
    searchEmpty: false,
    /** 骨架屏的占位行下标（WXML 表达式不支持数组字面量，只能从 data 给） */
    skCats: [0, 1, 2, 3, 4, 5],
    skRows: [0, 1, 2, 3],
    servingRows: [] as ServingRow[],
    /** 加菜提示条文案（本桌有进行中且未支付的订单时非空，见 refreshRows） */
    appendNotice: '',
    /** 备选备注（传给 spec-sheet 渲染 chip） */
    remarks: [] as RemarkOption[],
    cartCount: 0,
    cartTotalText: '0.00',
    sheetVisible: false,
    sheetDishId: 0,
    /** 该菜在购物车里的现有份数：弹层据此提示「确定后是再加一份」 */
    sheetInCart: 0,
    sheetDish: { dishName: '', specs: [], initialSpecId: 0, initialRemark: '' } as SheetDish
  },

  /** 菜单与备注原始数据（不进 data，避免整棵树参与 setData 序列化）。 */
  menu: [] as MenuCategory[],

  /** 分类标题锚点，顺序与列表里的标题一致（测量偏移时按下标对齐）。 */
  sections: [] as SectionRef[],

  /** 分类标题偏移缓存：滚动时只做纯计算，不再发起节点查询（减少卡顿）。 */
  offsets: [] as SectionOffset[],

  /** 右栏可视高度与当前位置，用于判断「是否已滚到底」。 */
  listHeight: 0,
  scrollTop: 0,

  /** 分类标题偏移是否需要补量（滚在中间时量不准，等滚回顶部再补，见 measureSections）。 */
  needMeasure: false,

  /**
   * 每道菜「当前选中的规格」（与 H5 useCart 的 selectedSpec 同语义）。
   * 不进 data：改了它只影响某一行的展示，重建那一行即可（见 syncRow）。
   */
  specPick: {} as Record<number, number>,

  /** 程序触发的滚动（点分类）期间不让 scroll 事件回写高亮，否则高亮会来回跳。 */
  spyLock: false,
  spyTimer: null as ReturnType<typeof setTimeout> | null,

  /**
   * 本次进入是否为「订单页点加菜」。
   * 是的话，即使本桌有进行中订单也留在菜单页继续点，不再跳回订单页（见 pages/order 的 onAppend）。
   */
  appendMode: false,

  /**
   * 本次实际用于加载的桌台码。
   * 首屏失败时会话里还没有桌台（applyTable 成功后才写入），重试必须靠它，
   * 否则「重新加载」只会再次落到「请扫码进入」。
   */
  loadedCode: '',

  /**
   * 已加载失败的菜品图 key。
   * 记住它，重建列表时直接给空串 —— 否则每次 onShow 都会把坏 URL 再赋一次，
   * 顾客会看到占位图闪成破图再闪回占位。
   * 惰性创建：写在 options 里的 Set 会被所有页面实例共享（options 只被浅拷贝）。
   */
  failedImgs: null as Set<string> | null,

  onLoad(options: Record<string, string | undefined>) {
    // 优先级：扫码参数 > 上次进入的桌台 > 开发期兜底（正式版为空，见 config.ts）
    const code = resolveTableCode(options) || readPersisted().tableCode || devTableCode();
    if (!code) {
      this.setData({ loading: false, error: '请扫描餐桌上的二维码进入点餐' });
      return;
    }
    this.loadedCode = code;
    this.appendMode = String(options.append || '') === '1';
    void this.loadAll(code);
  },

  onShow() {
    // 从购物车页返回时份数/合计可能已变；已有订单也可能被商家改了状态
    this.refreshRows();
  },

  onUnload() {
    // 分类跳转触发的滚动锁定时器：页面已销毁时回调再 setData 会报「页面已卸载」告警
    if (this.spyTimer) {
      clearTimeout(this.spyTimer);
      this.spyTimer = null;
    }
  },

  async loadAll(code: string) {
    // 重新加载意味着网络可能已恢复：清掉本次实例里累积的坏图记录，让菜品图再试一次
    // （实例内「只增不减」是为了避免同一个坏 URL 被反复请求、占位图反复闪回）
    this.failedImgs = null;
    try {
      const [config, table, menu, remarks] = await Promise.all([
        getPublicConfig(),
        getTable(code),
        getMenu(),
        getRemarks()
      ]);
      const info = table as TableInfo;

      applyConfig(config);
      // 公开配置刚到达：此刻才知道会员功能是否开启（开关藏在这份配置里）
      this.setData({ showProfile: memberEnabled() });
      applyTable(info);
      this.menu = menu;
      this.setData({ remarks });

      const current = info.currentOrder as OrderDetail | null | undefined;
      if (current) {
        applyOrder(current);
        // 「加菜」返回：留在菜单页继续点。订单已写进会话，购物车页会据此判定为加菜。
        if (this.appendMode) {
          this.renderMenu(info);
          return;
        }
        // 本桌有未结账订单（后端 currentOrder 只含 pay_status=0 的进行中订单）：
        // 交给订单页（加菜 / 催菜 / 支付都在那边）。
        wx.redirectTo({
          url: '/pages/order/index',
          // 跳转失败（页面被删/路径写错）时不能让本页卡在 loading，退化为正常渲染菜单
          fail: () => this.renderMenu(info)
        });
        return;
      }

      // 没有未结账订单：若是「已付款但仍在出餐」（servingOrders 非空），
      // 保留持久化的 orderNo，顾客点顶部提示条还能回去看出餐进度；
      // 本桌彻底没有进行中的单，才把恢复线索清干净。
      if ((info.servingOrders || []).length) clearCurrentOrder();
      else applyOrder(null);

      this.renderMenu(info);
    } catch (e) {
      this.setData({ loading: false, error: this.friendlyError(e) });
    }
  },

  /** 渲染点餐界面（首次进入 / 无未结账订单 / 跳订单页失败时都会走这里）。 */
  renderMenu(info: TableInfo) {
    const cats: CatRow[] = this.menu.map((c, i) => ({
      categoryId: Number(c.categoryId) || 0,
      categoryName: String(c.categoryName || ''),
      // 锚点用下标：HTML id 不能以数字开头，也不是每个分类都有 id
      navId: `nav-${i}`
    }));

    this.setData({
      loading: false,
      error: '',
      shopName: sessionState().shopName,
      tableLabel: this.tableLabelOf(info),
      cats,
      activeCatId: cats.length ? cats[0].categoryId : 0,
      servingRows: this.servingRowsOf(info)
    });
    this.refreshRows();
  },

  /** 把后端错误转成顾客能看懂、且知道下一步做什么的文案（与 H5 friendlyLoadError 同口径）。 */
  friendlyError(e: unknown): string {
    const err = e as { friendlyMessage?: string; message?: string } | null | undefined;
    const msg = err?.friendlyMessage || err?.message || '';
    if (/桌台|桌号|不存在/.test(msg)) return '桌号不存在，请重新扫描餐桌上的二维码';
    if (/网络|Network|域名|not in domain/i.test(msg)) return '网络连接失败，请检查网络后重试';
    if (/超时/.test(msg)) return '网络响应较慢，请稍后重试';
    return msg || '加载失败，请检查网络后重试';
  },

  tableLabelOf(table: TableInfo): string {
    return String(table.tableName || table.tableNo || '').trim() || '本桌';
  },

  /** 本桌「已付款但出餐未完成」的订单：显示进度，并可点进订单页看明细。 */
  servingRowsOf(table: TableInfo): ServingRow[] {
    return (table.servingOrders || []).map((o, i) => {
      const short = String(o.shortNo || o.orderNo || '');
      const text = ORDER_STATUS[Number(o.orderStatus)]?.label || '处理中';
      return { key: short || `s${i}`, orderNo: String(o.orderNo || ''), label: `#${short} · ${text}` };
    });
  },

  /**
   * 重建右栏列表（首次渲染、下单后、从购物车返回后、搜索词变化都走这里）。
   * 一次把所有分类拼成连续列表，左侧分类只负责定位（见 jumpToCat / onScroll），
   * 不再决定右侧显示什么——所以分类里菜品少时，下一类会紧接着出现，不会留白。
   */
  refreshRows() {
    const ordered = this.orderedQtyMap();
    const rows: MenuRow[] = [];
    const sections: SectionRef[] = [];

    this.visibleMenu().forEach((c, i) => {
      const categoryId = Number(c.categoryId) || 0;
      // 锚点用下标而不是 categoryId：HTML id 不能以数字开头，也不保证每个分类都有 id
      const anchor = `sec-${i}`;
      sections.push({ categoryId, anchor });
      rows.push({
        kind: 'sec',
        key: `sec-${i}`,
        anchor,
        categoryId,
        categoryName: String(c.categoryName || '')
      });

      const dishes = (c.dishes || []) as Dish[];
      if (!dishes.length) rows.push({ kind: 'empty', key: `empty-${i}` });

      for (const d of dishes) rows.push(this.dishRowOf(d, ordered));
    });

    // 加菜提示：从订单页点「加菜」或下单成功后再回来，都要让顾客知道这一轮会并入原单。
    // 放在 refreshRows 而不是 renderMenu，是因为「返回菜单页」走的是 onShow（不重跑 onLoad）。
    const current = sessionState().currentOrder;
    const appendNotice =
      current && isActiveStatus(Number(current.orderStatus)) && Number(current.payStatus) === 0
        ? '本桌已有进行中的订单，本次点菜将作为「加菜」并入该单'
        : '';

    this.sections = sections;
    this.setData(
      {
        rows,
        appendNotice,
        searchEmpty: this.data.searching && !rows.length,
        cartCount: cartCount(),
        // 人数取自会话：购物车页改过人数后，这里的合计要跟着一致（茶位费按人数算）
        cartTotalText: money(cartSettle(sessionState().config, sessionState().personCount).total)
      },
      // 行高定下来之后才能量标题位置（图片是固定尺寸，不依赖加载完成）
      () => this.measureSections()
    );
  },

  /** 当前要渲染的菜单：搜索时是过滤后的结果，否则是完整菜单（filterMenu 内部对空词短路）。 */
  visibleMenu(): MenuCategory[] {
    return filterMenu(this.menu, this.data.keyword);
  },

  /**
   * 把一道菜转成渲染行：价格、选中规格、购物车份数都在这里算好（WXML 只做展示）。
   *
   * 规格的处理按数量分三档，与 H5 的口径对齐：
   *   ・1 个规格：行内可直接加减，价格后带 `/规格名`
   *   ・2~3 个：行内列出 chip 直接点选，价格按所选规格给
   *   ・更多：行内放不下，只能进弹层，价格给最低价 +「起」
   */
  dishRowOf(dish: Dish, ordered: Record<number, number>): MenuRow {
    const dishId = Number(dish.dishId) || 0;
    const specs = (dish.specs || []) as DishSpec[];
    const specId = this.pickedSpecIdOf(dishId, specs);
    const picked = specs.find(s => Number(s.specId) === specId);
    const only = specs.length === 1;
    const inline = specs.length > 1 && specs.length <= INLINE_SPEC_MAX;
    const min = specs.length ? Math.min(...specs.map(s => Number(s.price) || 0)) : 0;
    // 与 H5 dishQty 同口径：只算「当前选中规格」的份数，显示与加减的对象永远一致
    const line = cartState().find(l => l.dishId === dishId && l.specId === specId);
    // 人工售罄（dish.sold_out）或「所有规格库存都卖完」→ 菜品级售罄。
    const allSpecsSoldOut = specs.length > 0 && specs.every(s => Number(s.stock) === 0);
    const soldOut = Number(dish.soldOut) === 1 || allSpecsSoldOut;

    return {
      kind: 'dish',
      key: `dish-${dishId}`,
      dishId,
      dishName: String(dish.dishName || ''),
      description: String(dish.description || ''),
      image: this.failedImgs?.has(`dish-${dishId}`) ? '' : mediaUrl(dish.dishImage),
      priceText: money(picked && (only || inline) ? Number(picked.price) || 0 : min),
      manySpecs: specs.length > INLINE_SPEC_MAX,
      orderable: specs.length > 0,
      soldOut,
      specId,
      specName: only ? String(picked?.specName || '') : '',
      specChips: inline
        ? specs.map(s => ({
            // chip 里带上 dishId：模板中嵌套 wx:for 之后拿不到外层行的字段
            dishId,
            specId: Number(s.specId) || 0,
            specName: String(s.specName || ''),
            on: Number(s.specId) === specId,
            soldOut: Number(s.stock) === 0
          }))
        : [],
      orderedQty: ordered[dishId] || 0,
      cartQty: line ? line.quantity : 0
    };
  },

  /**
   * 某道菜当前选中的规格 id。
   * 只返回「还没售罄」的规格：顾客之前选过 / 购物车里已有的那条若已售罄就跳过，
   * 退回第一个有库存的规格。全部售罄时返回 0（菜品按售罄处理，见 dishRowOf）。
   */
  pickedSpecIdOf(dishId: number, specs: DishSpec[]): number {
    const available = specs.filter(s => Number(s.stock) !== 0);
    const candidates: number[] = [];
    candidates.push(Number(this.specPick[dishId]) || 0);
    const lines = cartState().filter(l => l.dishId === dishId);
    if (lines.length === 1) candidates.push(Number(lines[0].specId) || 0);
    for (const id of candidates) {
      if (id && available.some(s => Number(s.specId) === id)) return id;
    }
    return Number(available[0]?.specId) || 0;
  },

  /**
   * 量出每个分类标题相对「列表内容顶部」的偏移，缓存下来供滚动时比对。
   * 用 rect 差值算而不是 offsetTop：列表怎么改（内边距、左右栏宽度）都不会算错。
   *
   * 只在列表贴顶时量：分类标题是吸顶的（.sec 用了 sticky），滚在中间时
   * 「正被吸住的那个标题」的 rect.top 就等于列表顶部，量出来的偏移会等于当前
   * scrollTop（远大于它的真实位置），之后往回滚时左侧高亮会明显滞后。
   * 其余情况只标记待补量，等用户滚回顶部再量——刷新前后行高不变，旧偏移仍然有效。
   */
  measureSections() {
    if (this.scrollTop > 1) {
      this.needMeasure = true;
      return;
    }
    this.needMeasure = false;

    const q = this.createSelectorQuery();
    q.select('.dishes').boundingClientRect();
    q.selectAll('.sec').boundingClientRect();
    q.exec(res => {
      const list = res[0] as { top: number; height: number } | null;
      const secs = res[1] as { top: number }[] | null;
      // 数量对不上说明拿到的是上一次渲染的结果，丢掉等下一轮量
      if (!list || !secs || secs.length !== this.sections.length) return;
      this.listHeight = Number(list.height) || 0;
      this.offsets = secs.map((r, i) => ({
        categoryId: this.sections[i].categoryId,
        top: Number(r.top) - Number(list.top) + this.scrollTop
      }));
    });
  },

  /** 滚动右栏：把左侧高亮同步到当前顶部的分类。 */
  onScroll(e: WechatMiniprogram.CustomEvent<{ scrollTop: number; scrollHeight: number }>): void {
    const { scrollTop, scrollHeight } = e.detail;
    this.scrollTop = Number(scrollTop) || 0;
    // 滚回顶部时补量之前的偏移（滚动中量不准，见 measureSections）
    if (this.needMeasure && this.scrollTop <= 1) this.measureSections();
    // 点分类触发的动画滚动会让高亮来回跳，锁定期间由 jumpToCat 说了算
    if (this.spyLock || !this.offsets.length) return;

    const last = this.offsets[this.offsets.length - 1].categoryId;
    let cur = this.offsets[0].categoryId;
    // 已滚到底：末尾分类内容不足一屏时，它的标题永远顶不到列表顶部，
    // 不特判的话点最后一个分类会一直高亮在上一类。
    if (this.listHeight > 0 && this.scrollTop + this.listHeight >= Number(scrollHeight) - 2) {
      cur = last;
    } else {
      for (const o of this.offsets) {
        // 标题已滚到列表顶部 20px 以内，即视为当前分类
        if (o.top <= this.scrollTop + 20) cur = o.categoryId;
      }
    }
    if (cur !== this.data.activeCatId) this.setData({ activeCatId: cur, navIntoId: this.navIdOf(cur) });
  },

  /** 左栏里某个分类的 scroll-into-view 锚点（分类多到滚出可视区时，用它把高亮项带回来）。 */
  navIdOf(categoryId: number): string {
    const hit = (this.data.cats as CatRow[]).find(c => c.categoryId === categoryId);
    return hit ? hit.navId : '';
  },

  /**
   * 程序触发的右栏滚动：锁住滚动联动（否则高亮会跟着动画来回跳），
   * 动画结束后复位锚点、交还给滚动联动。
   *
   * 复位是必需的：scroll-into-view 的值没变就不会再滚，
   * 手动滑走后再跳同一个目标（同一分类 / 顶部）会失效。
   */
  programScroll(anchor: string, patch: Record<string, number | string>): void {
    this.spyLock = true;
    if (this.spyTimer) clearTimeout(this.spyTimer);
    this.spyTimer = setTimeout(() => {
      this.spyLock = false;
      if (this.data.scrollIntoId) this.setData({ scrollIntoId: '' });
    }, 500);
    this.setData({ ...patch, scrollIntoId: anchor });
  },

  /** 点左侧分类：高亮并滚动到对应分组标题。 */
  jumpToCat(categoryId: number): void {
    const section = this.sections.find(s => s.categoryId === categoryId);
    if (!section) return;
    this.programScroll(section.anchor, {
      activeCatId: categoryId,
      navIntoId: this.navIdOf(categoryId)
    });
  },

  /** 右栏回到顶部（搜索词变化时用），左栏高亮一起归位到第一个分类。 */
  scrollListTop(): void {
    const first = (this.data.cats as CatRow[])[0];
    this.programScroll(LIST_TOP_ANCHOR, {
      activeCatId: first ? first.categoryId : 0,
      navIntoId: first ? first.navId : ''
    });
  },

  /** 当前订单里各菜品的已点份数（菜单上的「已点」标记）。 */
  orderedQtyMap(): Record<number, number> {
    const map: Record<number, number> = {};
    for (const it of sessionState().currentOrder?.items || []) {
      const id = Number(it.dishId) || 0;
      map[id] = (map[id] || 0) + (Number(it.quantity) || 0);
    }
    return map;
  },

  /** 点左侧分类：只滚动定位，不再重建右栏（右栏是所有分类的连续列表）。 */
  onSwitchCat(e: WechatMiniprogram.TouchEvent): void {
    const categoryId = Number(e.currentTarget.dataset.id) || 0;
    // 已经停在这个分类上（高亮来自滚动联动）就没必要再滚一次
    if (categoryId === this.data.activeCatId) return;
    this.jumpToCat(categoryId);
  },

  findDish(dishId: number): Dish | undefined {
    for (const c of this.menu) {
      const hit = (c.dishes || []).find(d => Number(d.dishId) === dishId);
      if (hit) return hit;
    }
    return undefined;
  },

  /**
   * 菜品图加载失败（域名没进白名单 / 图片被删）：把该行的图换成占位块。
   * 不处理的话顾客看到破图或一块空白，容易以为整个菜单坏了。
   */
  onImgError(e: WechatMiniprogram.CustomEvent): void {
    const key = String(e.currentTarget.dataset.key || '');
    if (!key) return;
    if (!this.failedImgs) this.failedImgs = new Set<string>();
    this.failedImgs.add(key);
    this.setData({
      rows: this.data.rows.map(r => (r.kind === 'dish' && r.key === key ? { ...r, image: '' } : r))
    });
  },

  /**
   * 行内步进器的「−」：减的是「当前选中规格」。
   * 购物车的行 key 与 store/cart.ts 一致：`菜品:规格`。
   */
  onMinus(e: WechatMiniprogram.TouchEvent): void {
    const dishId = Number(e.currentTarget.dataset.id) || 0;
    const specId = Number(e.currentTarget.dataset.spec) || 0;
    cartChangeQty(`${dishId}:${specId}`, -1);
    this.syncRow(dishId);
  },

  /**
   * 行内规格 chip：只切换「这道菜现在用哪个规格」，不动购物车里已有的条目
   * （与 H5 useCart.selectSpec 同语义：改的是菜单上的选择，不是已下单的东西）。
   */
  onPickSpec(e: WechatMiniprogram.TouchEvent): void {
    const dishId = Number(e.currentTarget.dataset.id) || 0;
    const specId = Number(e.currentTarget.dataset.spec) || 0;
    if (!dishId || !specId) return;
    const dish = this.findDish(dishId);
    const spec = (dish?.specs || []).find(s => Number(s.specId) === specId);
    if (!spec || Number(spec.stock) === 0) {
      toast.info('该规格已售罄');
      return;
    }
    this.specPick[dishId] = specId;
    this.syncRow(dishId);
  },

  /**
   * 「+」：
   *   ・1~3 个规格 → 直接加入「当前选中规格」（行内已经能看到选的是哪一个）
   *   ・更多规格 → 进弹层让顾客选，不默默按默认规格下单
   */
  onAdd(e: WechatMiniprogram.TouchEvent): void {
    const dishId = Number(e.currentTarget.dataset.id) || 0;
    const dish = this.findDish(dishId);
    if (!dish) return;
    if (Number(dish.soldOut) === 1) {
      toast.info('该菜品已售罄，看看别的吧');
      return;
    }
    const specs = (dish.specs || []) as DishSpec[];
    if (specs.length > INLINE_SPEC_MAX) {
      this.openSheet(dishId);
      return;
    }
    if (!specs.length) {
      toast.info('该菜品暂不可点，请联系店员');
      return;
    }
    const pickedId = this.pickedSpecIdOf(dishId, specs);
    const picked = specs.find(s => Number(s.specId) === pickedId);
    if (!picked) {
      // 所有规格都已售罄（人工售罄上面的分支已拦，这里是「库存卖完」）
      toast.info('该菜品已售罄，看看别的吧');
      return;
    }
    // 不传备注 = 不改动该行已填的备注（快速「+」不该抹掉顾客填好的「不要辣」）
    this.addToCart(dishId, picked);
  },

  /** 点菜名：打开规格 / 备注弹层。 */
  onOpenSheet(e: WechatMiniprogram.TouchEvent): void {
    this.openSheet(Number(e.currentTarget.dataset.id) || 0);
  },

  openSheet(dishId: number): void {
    const dish = this.findDish(dishId);
    if (!dish) return;
    const specs = (dish.specs || []) as DishSpec[];
    const allSoldOut = specs.length > 0 && specs.every(s => Number(s.stock) === 0);
    if (Number(dish.soldOut) === 1 || allSoldOut) {
      toast.info('该菜品已售罄，看看别的吧');
      return;
    }
    if (!specs.length) {
      toast.info('该菜品暂不可点，请联系店员');
      return;
    }
    // 打开时定位到「这道菜当前选中的规格」（行内 chip 或购物车里的那一条），
    // 连同该规格已填的备注一起带进弹层，点开即可改
    const specId = this.pickedSpecIdOf(dishId, specs);
    const line = cartState().find(l => l.dishId === dishId && l.specId === specId);
    this.setData({
      sheetVisible: true,
      sheetDishId: dishId,
      sheetInCart: cartDishQty(dishId),
      sheetDish: {
        dishName: String(dish.dishName || ''),
        specs,
        initialSpecId: specId,
        initialRemark: line ? line.itemRemark : ''
      }
    });
  },

  /** itemRemark 省略 = 不修改该行已有备注（快捷「+」路径）；传空串才是显式清空。 */
  addToCart(dishId: number, spec: SpecPick, itemRemark?: string): void {
    const dish = this.findDish(dishId);
    if (!dish) return;
    cartAdd({
      dishId,
      dishName: String(dish.dishName || ''),
      specId: Number(spec.specId) || 0,
      specName: String(spec.specName || ''),
      price: Number(spec.price) || 0,
      image: mediaUrl(dish.dishImage),
      itemRemark
    });
    this.syncRow(dishId);
  },

  /**
   * 只重建「这一行」并刷新结算栏：加/减份数、切规格都只影响一行。
   *
   * 不走 refreshRows：整表重建要把几十条菜品重新过一遍桥，还会白量一次分类标题位置
   * （行高不变，偏移不会失效）。切规格还会同时改价格与 chip 选中态，所以整行重建最省心。
   */
  syncRow(dishId: number): void {
    const patch: Record<string, number | string | MenuRow> = {
      cartCount: cartCount(),
      // 人数取自会话：购物车页改过人数后，这里的合计要跟着一致（茶位费按人数算）
      cartTotalText: money(cartSettle(sessionState().config, sessionState().personCount).total)
    };
    const idx = this.data.rows.findIndex(r => r.kind === 'dish' && r.dishId === dishId);
    const dish = idx >= 0 ? this.findDish(dishId) : undefined;
    if (idx >= 0 && dish) patch[`rows[${idx}]`] = this.dishRowOf(dish, this.orderedQtyMap());
    this.setData(patch);
  },

  onSheetClose(): void {
    this.setData({ sheetVisible: false });
  },

  onSheetConfirm(e: WechatMiniprogram.CustomEvent): void {
    const detail = e.detail as SpecPick & { itemRemark?: string };
    const dishId = this.data.sheetDishId;
    if (dishId) {
      // 弹层里选的规格就是这道菜之后的「当前规格」：行内的 chip、价格、步进器都要跟着走
      this.specPick[dishId] = Number(detail.specId) || 0;
      this.addToCart(dishId, detail, String(detail.itemRemark || ''));
      toast.success('已加入购物车');
    }
    this.setData({ sheetVisible: false });
  },

  // ---------- 搜索 ----------
  onSearchInput(e: WechatMiniprogram.Input): void {
    this.applyKeyword(String(e.detail.value || ''));
  },

  onSearchClear(): void {
    this.applyKeyword('');
  },

  /**
   * 统一的搜索入口：切换搜索态（藏左栏）→ 重建列表 → 把列表拉回顶部。
   * 结果的「第一条」必须从最上面开始看，否则顾客会以为搜出来就这几道。
   */
  applyKeyword(raw: string): void {
    const keyword = String(raw || '');
    const searching = !!keyword.trim();
    if (keyword === this.data.keyword && searching === this.data.searching) return;
    // setData 会同步更新 this.data，紧接着的 refreshRows 就能读到新关键词
    this.setData({ keyword, searching });
    this.refreshRows();
    this.scrollListTop();
  },

  onGoCart(): void {
    if (!this.data.cartCount) return;
    wx.navigateTo({ url: '/pages/cart/index' });
  },

  /** 点顶部「出餐中」提示条：跳订单页看该单进度（订单页优先用 query 里的 orderNo）。 */
  onTapServing(e: WechatMiniprogram.TouchEvent): void {
    const orderNo = String(e.currentTarget.dataset.no || '');
    if (!orderNo) return;
    wx.navigateTo({ url: `/pages/order/index?orderNo=${encodeURIComponent(orderNo)}` });
  },

  onRetry(): void {
    // 首屏失败时会话里还没有桌台（applyTable 成功后才写入），所以优先用本次解析出的码
    const code = this.loadedCode || sessionState().tableCode || readPersisted().tableCode;
    if (!code) {
      this.setData({ error: '请扫描餐桌上的二维码进入点餐' });
      return;
    }
    this.setData({ loading: true, error: '' });
    void this.loadAll(code);
  }
});
