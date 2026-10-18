import { http } from './http';

// ============================================================================
// 管理端:顾客会员
//
// 与 /customer/auth/* 的区别:那一组是顾客自己用的(注册/登录/会员卡),
// 这一组是后台员工用的(查会员、启停用),走管理端令牌与 member:* 权限点。
//
// 后台刻意没有「新建会员」:档案只能由顾客自助注册产生(需要一条登录方式),
// 后台代建会留下谁也登不进去的孤儿档案。
// ============================================================================

/** 会员列表行(与后端 dao.MemberRow 字段一一对应)。 */
export interface MemberRow {
  customerId: number;
  /** 未填昵称时为空串 —— 后端不做「微信用户」兜底(那是顾客端会员卡的口径)。 */
  nickname: string;
  avatarUrl: string;
  /** 1 启用 / 0 停用。 */
  status: number;
  orderCount: number;
  lastOrderTime: string;
  createTime: string;
  lastLoginTime: string;
  /** 已绑定的登录方式,逗号分隔(如 "wx_mp,phone");未绑定时为空串。 */
  providers: string;
  /** 绑定的手机号(完整号码,仅管理端可见;未绑定为空串)。 */
  phone: string;
  /** 储值余额(元,与订单金额同一口径)。 */
  balance: number;
}

export interface MemberListQuery {
  keyword?: string;
  /** 1 启用 / 0 停用;不传表示不限(注意 0 是有效筛选值,不能当"不传"处理)。 */
  status?: number;
  pageNum: number;
  pageSize: number;
}

export const listMembers = (params: MemberListQuery): Promise<{ total: number; items: MemberRow[] }> =>
  http.get<unknown, { total: number; items: MemberRow[] }>('/admin/member/list', { params });

/** 启用(1)/停用(0)会员。停用后该会员的现有令牌在下一次请求时立即失效。 */
export const setMemberStatus = (customerId: number, status: number): Promise<void> =>
  http.post<unknown, void>('/admin/member/status', { customerId, status });

/** 登录方式的中文名。新增 provider 时在这里补一行(与后端 po.CustomerProvider* 对应)。 */
export const MEMBER_PROVIDER_LABELS: Record<string, string> = {
  wx_mp: '微信小程序',
  wx_oa: '微信公众号',
  wx_app: '微信 App',
  phone: '手机号',
  password: '账号密码',
  apple: 'Apple'
};

/** 把 "wx_mp,phone" 渲染成 "微信小程序 / 手机号";未知 provider 原样展示,不吞掉。 */
export function providerText(providers: string): string {
  return String(providers || '')
    .split(',')
    .map(p => p.trim())
    .filter(Boolean)
    .map(p => MEMBER_PROVIDER_LABELS[p] || p)
    .join(' / ');
}
