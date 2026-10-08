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

/** 会员卡信息（/api/customer/auth/* 返回的展示字段）。 */
export interface MemberInfo {
  customerId: number;
  /** 未填昵称时后端返回「微信用户」。 */
  nickname: string;
  avatarUrl: string;
  /** 入会日期。 */
  memberSince: string;
  orderCount: number;
}

/** 会员登录态。挂在 App 实例上跨页共享；令牌另存本地存储供冷启动恢复。 */
export interface MemberState {
  /** 会员 ID；0 表示未登录。 */
  customerId: number;
  info: MemberInfo | null;
  /** 登录流程进行中（防重复点击与并发静默重登）。 */
  pending: boolean;
}

/** 「我的订单」列表行（后端 dao.CustomerOrderRow 的响应形态）。 */
export interface MemberOrderRow {
  orderId: number;
  orderNo: string;
  pickupNo: number;
  tableId: number;
  tableName: string;
  orderStatus: number;
  payStatus: number;
  /** 金额单位是分，展示用 money() 换算。 */
  totalAmount: number;
  createTime: string;
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
