// 小程序侧的共享类型。
//
// ⚠️ 为什么这些状态不放在「模块级单例」里：
// 构建产物是 CommonJS 且不做代码分割（见 build.mjs 说明），每个页面/组件的
// .js 都是**自包含**的，`src/store/cart.ts` 会被分别打进菜单页与购物车页各一份，
// 模块级变量因此有两个互不相干的副本 —— 菜单页加进购物车的东西，购物车页看不见。
// 小程序的惯用解法是把跨页状态挂在**唯一的 App 实例**上（globalData），
// 它由运行时创建一次，所有页面共享同一份引用。
import type { OrderDetail, PublicConfig, TableInfo } from '@dining/types';

/** 本次进入的桌台与订单。 */
export interface SessionState {
  /** 桌台稳定码（二维码里的 8 位点餐码）。 */
  tableCode: string;
  /** 数字桌台 ID，下单接口需要。 */
  tableId: number;
  table: TableInfo | null;
  shopName: string;
  /** 顾客端公开配置：茶位费 / 促销 / 收款码等。 */
  config: PublicConfig | null;
  /** 当前桌台进行中的订单；null 表示尚未下单。 */
  currentOrder: OrderDetail | null;
  /**
   * 就餐人数。放在会话里而不是各页面自己存：茶位费按人数计算，
   * 菜单页底部结算栏与购物车页的合计必须一致，否则同一批菜在两页显示两个价。
   */
  personCount: number;
  /**
   * 是否已「显式选择」过人数（选人数面板确认 / 购物车步进 / 订单页改人数）。
   * 与 personCount 分开：personCount 可能来自购物车恢复或订单现值，
   * 不能据此判断顾客有没有做过选择 —— 茶位费门店进入菜单时必须选一次，
   * 选过（本会话内）就不再弹。
   */
  personChosen: boolean;
  /**
   * 整单备注。同样放会话里：购物车要能跨冷启动恢复（见 store/cartPersist），
   * 备注若只存在购物车页的 data 里，页面一销毁就丢，恢复出来的车等于少了一半信息。
   */
  orderRemark: string;
}

/** 购物车行：同一道菜的不同规格算两行。 */
export interface CartLine {
  /** `dishId:specId`。 */
  key: string;
  dishId: number;
  dishName: string;
  specId: number;
  specName: string;
  price: number;
  image: string;
  quantity: number;
  itemRemark: string;
}

// 会员卡与「我的订单」的响应形态直接复用接口层（@dining/api）的定义。
// 两端各维护一份结构相同的 interface 迟早会漂移 —— App/网站接进来时更是三份。
//
// ⚠️ MemberOrderRow.totalAmount 的单位是**元**（与订单页/支付页同口径），
// 直接 `money()` 展示即可。它曾被误标成分、且后端确实下发过分，
// 导致 ¥28.00 的单显示成 ¥2800.00（后端已统一为下发元）。
import type { MemberInfo } from '@dining/api';
export type { MemberInfo, MemberOrderRow } from '@dining/api';

/** 会员登录态。挂在 App 实例上跨页共享；令牌另存本地存储供冷启动恢复。 */
export interface MemberState {
  /** 会员 ID；0 表示未登录，-1 表示「有本地令牌但档案尚未拉取」。 */
  customerId: number;
  info: MemberInfo | null;
  /** 登录流程进行中（防重复点击与并发静默重登）。 */
  pending: boolean;
}

/**
 * 挂在 App 实例上的跨页状态。
 *
 * 注意它**不是** `globalData` 字段的子结构：miniprogram-api-typings 的
 * `App.Options<T> = Partial<Option> & T`，即 T 的字段会直接摊到 App 实例上，
 * 因此写法是 `App<GlobalData>({ session, cart, ... })` 与
 * `getApp<GlobalData>().cart`（equivalent to 传统 globalData，但带类型）。
 */
export interface GlobalData {
  session: SessionState;
  cart: CartLine[];
  member: MemberState;
}
