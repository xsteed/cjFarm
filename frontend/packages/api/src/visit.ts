import { http } from './http';
import type { PageQuery, VisitLogPage, VisitStats, VisitTrackPayload } from '@dining/types';

// 站点访问统计(见 docs/design-docs/visit-analytics/spec.md)。
//
// 打点接口挂在顾客公开组下:顾客端(H5/小程序)调用,**故意不带登录态拦截**——
// http 的请求拦截器只在有 admin_token 时才附 Authorization,顾客端本地没有这个键,
// 因此匿名打点天然干净;小程序端已登录会员时,该请求会由会员拦截器自动带上顾客令牌
// (见 apps/miniprogram/src/api/client.ts),后端据此把访客升级为「会员身份」。
//
// 响应拦截器对非 200 会弹全局错误 toast —— 埋点失败绝不能打扰正在点餐的顾客,
// 因此这里显式传 silent,由调用方 .catch 静默消化。
export const reportVisit = (payload: VisitTrackPayload): Promise<unknown> =>
  http.post('/customer/visit', payload, { silent: true });

// 管理端报表(需 report:view):入参与既有报表一致(days=N 或 start&end)。
export const getVisitStats = (params?: PageQuery): Promise<VisitStats> =>
  http.get<unknown, VisitStats>('/admin/report/visits', { params });

// 访问明细下钻(报表页访问统计弹窗):区间 + 渠道/入口/终端筛选,device/os/browser
// 传「未知」即筛空值行(与分布图的归一口径一致)。
export const getVisitLogs = (params?: PageQuery): Promise<VisitLogPage> =>
  http.get<unknown, VisitLogPage>('/admin/report/visitLogs', { params });
