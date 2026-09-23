import { http } from './http';
import type { PageQuery } from '../types/api';
import type {
  DailyTrendRow,
  DishRankRow,
  HourlyRow,
  MonthlyTrendRow,
  ReportSummary,
  SettleMixRow
} from '../types/entities';

// 报表
// summary 汇总(含昨日/上月同期,供前端算环比);
// dailyTrend / hourly / settleMix 支持 days=N 或 start&end 自定义区间。
export const getReportSummary = (): Promise<ReportSummary> => http.get<unknown, ReportSummary>('/admin/report/summary');
export const getDailyTrend = (params?: PageQuery): Promise<DailyTrendRow[]> =>
  http.get<unknown, DailyTrendRow[]>('/admin/report/dailyTrend', { params });
export const getMonthlyTrend = (): Promise<MonthlyTrendRow[]> =>
  http.get<unknown, MonthlyTrendRow[]>('/admin/report/monthlyTrend');
export const getDishRank = (params?: PageQuery): Promise<DishRankRow[]> =>
  http.get<unknown, DishRankRow[]>('/admin/report/dishRank', { params });
// 时段分布(排班/备货参考)
export const getHourlyReport = (params?: PageQuery): Promise<HourlyRow[]> =>
  http.get<unknown, HourlyRow[]>('/admin/report/hourly', { params });
// 结算方式构成(正常收款/免单/挂账)
export const getSettleMix = (params?: PageQuery): Promise<SettleMixRow[]> =>
  http.get<unknown, SettleMixRow[]>('/admin/report/settleMix', { params });
