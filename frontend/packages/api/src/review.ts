// 订单评价管理(仅商家可见的内部反馈,见 docs/design-docs/order-review/spec.md):
// 列表 + 同口径汇总一次返回;删除不当内容。权限 review:view / review:edit。
import { http } from './http';
import type { ApiResponse, Id, PageQuery, ReviewListResult } from '@dining/types';

export interface ReviewListParams extends PageQuery {
  startDay?: string;
  endDay?: string;
  ratingMin?: number;
  ratingMax?: number;
  hasContent?: boolean | string;
  /** 图片筛选:"1" 仅看有图 / "0" 仅看无图 / 缺省全部。 */
  hasImages?: string;
  keyword?: string;
}

export const listReviews = (params?: ReviewListParams): Promise<ReviewListResult> =>
  http.get<unknown, ReviewListResult>('/admin/review/list', { params });

export const deleteReview = (id: Id): Promise<ApiResponse<unknown>> =>
  http.delete<unknown, ApiResponse<unknown>>(`/admin/review/${id}`);
