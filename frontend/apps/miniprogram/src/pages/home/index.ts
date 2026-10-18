// 默认主页（小程序首页）。
//
// 为什么需要它：以前 app.json 的首页就是点餐页，没扫码（从「最近使用」「搜索」「分享」
// 进来）只能看到一句「请扫描餐桌上的二维码进入点餐」—— 扫码失败或第一次进来的顾客
// 没有任何可做的事。现在首页负责三件事：
//   1) 扫码入口：在微信里直接唤起 wx.scanCode，扫桌面点餐码进菜单（不依赖顾客会找「扫一扫」）；
//   2) 续用入口：上次的桌台 / 进行中的订单，一回小程序就能接着点、接着看；
//   3) 橱窗：店名标语 + 招牌菜，先让人知道这家店卖什么。
//
// 扫码进桌仍然是**直达菜单**（后端出的小程序码 PagePath 固定为 pages/index/index），
// 首页只在「没带桌台码」时出现，不会给扫码顾客多垫一次点击。
//
// 接口全部是现成的公开接口（/customer/config、/customer/menu、/customer/table），
// 后端零改动；任何一项失败都只影响对应区块，扫码主路径永远可用。
import { getMenu, getPublicConfig, getTable } from '../../api/client';
import { devTableCode, mediaUrl } from '../../config';
import { ORDER_STATUS } from '@dining/shared/dicts';
import { money } from '@dining/shared/money';
import { isActiveStatus } from '@dining/shared/orderStatus';
import { memberEnabled } from '../../store/member';
import { applyConfig, readPersisted, sessionState } from '../../store/session';
import { scanTableCode } from '../../utils/scan';
import { toast } from '../../utils/toast';
import type { Dish, DishSpec, MenuCategory, TableInfo } from '@dining/types';

/** 招牌菜卡片（WXML 不做函数调用，展示串都在这里算好，见 docs/miniprogram.md 5.3）。 */
interface SigDish {
  dishId: number;
  dishName: string;
  image: string;
  priceText: string;
  /** 多个规格：价格是「起售价」，模板补一个「起」字 */
  manySpecs: boolean;
  soldOut: boolean;
}

/** 首页最多展示几道招牌菜：再多就把首页变成第二个菜单了 */
const SIG_MAX = 8;

Page({
  data: {
    shopName: '扫码点餐',
    slogan: '',
    /** 「我的」tab 是否显示：跟随后端会员开关 */
    showProfile: true,
    /** 上次进入的桌台码（会话优先，其次本地缓存）：有值才显示「继续点餐」 */
    tableCode: '',
    /** 桌台名（如「大厅03桌」）；接口没回来 / 桌台已删时退化为通用文案 */
    tableLabel: '',
    /** 会话里的当前订单：进行中的单 */
    hasOrder: false,
    orderShort: '',
    orderStatusText: '',
    orderAmountText: '0.00',
    /** 本店招牌 */
    sigDishes: [] as SigDish[]
  },

  /** 已经拉过桌台详情的桌台码：避免 onShow 每次回到首页都重复请求（桌台名基本不变）。 */
  loadedTableCode: '',

  /** 已加载失败的菜品图：重建列表时直接给空串，不反复闪破图（与菜单页同口径）。 */
  failedImgs: null as Set<number> | null,

  onLoad() {
    // 两块远程数据互不依赖，各自失败只影响自己那块
    void this.loadBrand();
    void this.loadSignature();
  },

  onShow() {
    // 从菜单 / 订单页返回时，会话里的桌台与订单可能已经变了（比如刚下单、刚结账）
    this.syncSession();
  },

  /** 把会话（+本地缓存）里的桌台与订单映射到首页卡片。 */
  syncSession() {
    const s = sessionState();
    const persisted = readPersisted();
    // 开发版兜底：普通编译不带桌台码，DEV_TABLE_CODE 让「继续点餐」仍能一键进本机那张桌；
    // 体验版/正式版 devTableCode() 恒为空串，线上不会凭空多出一个入口。
    const code = String(s.tableCode || persisted.tableCode || devTableCode() || '');
    const order = s.currentOrder;
    const patch: Record<string, unknown> = { showProfile: memberEnabled() };

    // 只认进行中的单（1/2/3）：订单页会把已完成的单也写进会话，
    // 那种单挂在首页「进行中」卡片上会让人以为还没做完。
    if (order?.orderNo && isActiveStatus(Number(order.orderStatus))) {
      patch.hasOrder = true;
      patch.orderShort = String(order.shortNo || order.orderNo || '');
      patch.orderStatusText = ORDER_STATUS[Number(order.orderStatus)]?.label || '处理中';
      patch.orderAmountText = money(Number(order.totalAmount) || 0);
    } else {
      patch.hasOrder = false;
      patch.orderShort = '';
      patch.orderStatusText = '';
      patch.orderAmountText = '0.00';
    }

    if (code !== this.data.tableCode) {
      patch.tableCode = code;
      // 换了桌台（或彻底没有）：旧的桌台名必须清掉，否则会显示成「继续点餐 · 上一桌」
      patch.tableLabel = code ? '上次的桌台' : '';
    }

    this.setData(patch);
    if (code && code !== this.loadedTableCode) void this.loadTable(code);
  },

  /** 店名与标语：配置没回来前用「扫码点餐」兜底，导航栏标题也换成店名。 */
  async loadBrand(): Promise<void> {
    try {
      const config = await getPublicConfig();
      applyConfig(config);
      const name = String(config.shop_name || '').trim();
      this.setData({
        shopName: name || '扫码点餐',
        slogan: String(config.shop_slogan || '').trim(),
        // 公开配置刚到达，此刻才知道会员功能是否开启
        showProfile: memberEnabled()
      });
      if (name) wx.setNavigationBarTitle({ title: name });
    } catch {
      // 配置失败不挡首页：扫码入口与续用入口都只依赖本地状态，照常可用
      this.setData({ shopName: '扫码点餐' });
    }
  },

  /** 招牌菜橱窗：只展示上架中的招牌菜，失败就不展示这一块。 */
  async loadSignature(): Promise<void> {
    try {
      const menu = await getMenu();
      this.setData({ sigDishes: this.signatureOf(menu) });
    } catch {
      /* 招牌菜只是锦上添花：拉不到就整块不渲染，不打扰顾客 */
    }
  },

  /** 从菜单里挑出招牌菜（跳过没有规格的菜 —— 那种菜根本下不了单）。 */
  signatureOf(menu: MenuCategory[]): SigDish[] {
    const out: SigDish[] = [];
    for (const c of menu || []) {
      for (const dish of (c.dishes || []) as Dish[]) {
        if (Number(dish.signature) !== 1) continue;
        const dishId = Number(dish.dishId) || 0;
        const specs = (dish.specs || []) as DishSpec[];
        if (!dishId || !specs.length) continue;
        const allSoldOut = specs.every(s => Number(s.stock) === 0);
        out.push({
          dishId,
          dishName: String(dish.dishName || ''),
          image: this.failedImgs?.has(dishId) ? '' : mediaUrl(dish.dishImage),
          priceText: money(Math.min(...specs.map(s => Number(s.price) || 0))),
          manySpecs: specs.length > 1,
          soldOut: Number(dish.soldOut) === 1 || allSoldOut
        });
        if (out.length >= SIG_MAX) return out;
      }
    }
    return out;
  },

  /** 拉桌台名：桌台可能已被删除，失败时保留通用文案，入口仍然可用。 */
  async loadTable(code: string): Promise<void> {
    this.loadedTableCode = code;
    try {
      const table = (await getTable(code)) as TableInfo;
      // 请求期间顾客可能又扫了别的桌：结果只回填给仍然匹配的那一桌
      if (this.data.tableCode !== code) return;
      this.setData({ tableLabel: this.tableLabelOf(table) });
    } catch {
      /* 桌台不存在 / 网络不通：真正的错误由菜单页说明，首页不在这里拦人 */
    }
  },

  tableLabelOf(table: TableInfo): string {
    return String(table.tableName || table.tableNo || '').trim() || '上次的桌台';
  },

  /** 扫码点餐：扫桌面点餐码 → 带着桌台码进菜单。 */
  async onScan(): Promise<void> {
    try {
      const code = await scanTableCode();
      if (code === null) return; // 顾客自己取消的，不打扰
      if (!code) {
        toast.info('没识别到桌台码，请扫描桌面上的点餐码');
        return;
      }
      // navigateTo 而不是 redirectTo：点完菜还能退回首页，首页不会从页面栈里消失
      wx.navigateTo({ url: `/pages/index/index?tableCode=${encodeURIComponent(code)}` });
    } catch (e) {
      toast.info((e as Error)?.message || '扫码失败，请用微信「扫一扫」扫描桌面点餐码');
    }
  },

  /** 进行中的订单 → 订单页（session 里已有当前订单，不需要额外参数）。 */
  onResumeOrder(): void {
    wx.navigateTo({ url: '/pages/order/index' });
  },

  /** 继续点餐 → 带上次的桌台码回菜单（菜单页会自己处理加菜 / 清台确认）。 */
  onResumeMenu(): void {
    const code = this.data.tableCode;
    if (!code) return;
    wx.navigateTo({ url: `/pages/index/index?tableCode=${encodeURIComponent(code)}` });
  },

  /** 点招牌菜：没有桌台就先引导扫码，有桌台就进菜单自己挑（规格 / 备注在菜单页）。 */
  onOpenDish(): void {
    if (!this.data.tableCode) {
      toast.info('请先扫描桌面上的点餐码，再去点菜');
      return;
    }
    this.onResumeMenu();
  },

  /** 招牌菜图片加载失败：换成占位块，不留一块破图。 */
  onImgError(e: WechatMiniprogram.CustomEvent): void {
    const dishId = Number(e.currentTarget.dataset.id) || 0;
    if (!dishId) return;
    if (!this.failedImgs) this.failedImgs = new Set<number>();
    this.failedImgs.add(dishId);
    // 只回写命中的那一张（整份 sigDishes 重建没必要）
    const idx = (this.data.sigDishes as SigDish[]).findIndex(d => d.dishId === dishId);
    if (idx < 0) return;
    const patch: Record<string, string> = {};
    patch[`sigDishes[${idx}].image`] = '';
    this.setData(patch);
  }
});
