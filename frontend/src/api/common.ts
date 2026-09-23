import { http } from './http';
import type { ApiResponse, PageQuery, PageResult } from '../types/api';
import type {
  AgentTokenResult,
  CategoryPayload,
  ConfigData,
  ConfigPayload,
  Dish,
  DishPayload,
  Id,
  MenuCategory,
  OperLog,
  RemarkOption,
  RemarkPayload,
  TableInfo,
  TablePayload,
  UploadResult
} from '../types/entities';

// 桌台
export const listTables = (params?: PageQuery): Promise<PageResult<TableInfo>> =>
  http.get<unknown, PageResult<TableInfo>>('/admin/table/list', { params });
export const saveTable = (data: TablePayload): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>('/admin/table/save', data);
export const updateTable = (data: TablePayload): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>('/admin/table/update', data);
export const deleteTable = (id: Id): Promise<ApiResponse<unknown>> =>
  http.delete<unknown, ApiResponse<unknown>>(`/admin/table/${id}`);

// 分类
export const listCategories = (): Promise<PageResult<MenuCategory>> =>
  http.get<unknown, PageResult<MenuCategory>>('/admin/category/list');
export const saveCategory = (data: CategoryPayload): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>('/admin/category/save', data);
export const updateCategory = (data: CategoryPayload): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>('/admin/category/update', data);
export const deleteCategory = (id: Id): Promise<ApiResponse<unknown>> =>
  http.delete<unknown, ApiResponse<unknown>>(`/admin/category/${id}`);

// 菜品
export const listDishes = (params?: PageQuery): Promise<PageResult<Dish>> =>
  http.get<unknown, PageResult<Dish>>('/admin/dish/list', { params });
export const getDish = (id: Id): Promise<Dish> => http.get<unknown, Dish>(`/admin/dish/${id}`);
export const saveDish = (data: DishPayload): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>('/admin/dish/save', data);
export const updateDish = (data: DishPayload): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>('/admin/dish/update', data);
export const deleteDish = (id: Id): Promise<ApiResponse<unknown>> =>
  http.delete<unknown, ApiResponse<unknown>>(`/admin/dish/${id}`);

// 备注
export const listRemarkOptions = (): Promise<PageResult<RemarkOption>> =>
  http.get<unknown, PageResult<RemarkOption>>('/admin/remark/list');
export const saveRemark = (data: RemarkPayload): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>('/admin/remark/save', data);
export const updateRemark = (data: RemarkPayload): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>('/admin/remark/update', data);
export const deleteRemark = (id: Id): Promise<ApiResponse<unknown>> =>
  http.delete<unknown, ApiResponse<unknown>>(`/admin/remark/${id}`);

// 配置
export const getConfig = (): Promise<ConfigData> => http.get<unknown, ConfigData>('/admin/config/list');
export const saveConfig = (data: ConfigPayload): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>('/admin/config/save', data);
// 签发打印代理全局令牌:后端生成即落库,明文只在本响应里出现一次。
// 与「手填进表单再点保存」的区别是它不依赖再点一次保存,签发后立即生效。
export const issueAgentToken = (): Promise<AgentTokenResult> =>
  http.post<unknown, AgentTokenResult>('/admin/config/agent/token');

// 操作日志(审计留痕)
export const listOperLogs = (params?: PageQuery): Promise<PageResult<OperLog>> =>
  http.get<unknown, PageResult<OperLog>>('/admin/log/list', { params });
// 清理过期日志:天数只由后端 AUDIT_RETENTION_DAYS 决定,接口不接受任意天数
// (防止持有 log:manage 的人一次抹掉全部历史)
export const cleanOperLogs = (): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>('/admin/log/clean', {});

// 上传
export const uploadFile = (file: File): Promise<UploadResult> => {
  const fd = new FormData();
  fd.append('file', file);
  return http.post<unknown, UploadResult>('/common/upload', fd, {
    headers: { 'Content-Type': 'multipart/form-data' }
  });
};
