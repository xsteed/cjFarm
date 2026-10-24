import { http } from './http';
import type { ApiResponse, PageQuery, PageResult } from '@dining/types';
import type {
  AgentTokenResult,
  CategoryPayload,
  ConfigData,
  ConfigPayload,
  Dish,
  DishMovePayload,
  DishPayload,
  DishSortRow,
  Id,
  MenuCategory,
  OperLog,
  RemarkOption,
  RemarkPayload,
  TableInfo,
  TablePayload,
  UploadResult,
  WeappCodeResult
} from '@dining/types';

// 桌台
export const listTables = (params?: PageQuery): Promise<PageResult<TableInfo>> =>
  http.get<unknown, PageResult<TableInfo>>('/admin/table/list', { params });
export const saveTable = (data: TablePayload): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>('/admin/table/save', data);
export const updateTable = (data: TablePayload): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>('/admin/table/update', data);
export const deleteTable = (id: Id): Promise<ApiResponse<unknown>> =>
  http.delete<unknown, ApiResponse<unknown>>(`/admin/table/${id}`);
// 设置桌台人工可用性状态:lockStatus 0 正常 / 1 停用 / 2 预约(与订单占用正交)。
export const lockTable = (data: TablePayload): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>('/admin/table/lock', data);
// 强制清台:店家漏点「收款」时桌台会一直挂在占用态,下一桌扫码会被带进上一桌的账单。
// 这个接口把该桌进行中的订单一次性收走并置空闲,action 决定未结账订单的落点
// (settle 补结账 = 钱已在店里收过 / cancel 作废 = 不计收入),两者对账含义相反。
export const releaseTable = (data: TablePayload): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>('/admin/table/release', data);
// 桌台小程序码:后端拿 access_token 向微信换取,并按参数指纹落盘缓存
// (同一桌台第二次出码不会再打微信接口)。未配置 AppSecret 时返回可读的中文错误。
export const getTableWeappCode = (tableCode: string, width?: number): Promise<WeappCodeResult> =>
  http.get<unknown, WeappCodeResult>('/admin/table/weapp-code', { params: { tableCode, width } });

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
// 拖拽排序:只动同分类内的相对顺序,后端返回该分类重排后的 dishId/sortOrder 供回填
export const sortDish = (data: DishMovePayload): Promise<{ items: DishSortRow[] }> =>
  http.post<unknown, { items: DishSortRow[] }>('/admin/dish/sort', data);

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

// 上传。手机原图普遍 3~10MB,弱网下容易超过全局 15s 超时,单独放宽到 2 分钟;
// 其他请求仍用 http 实例的全局 timeout。
export const uploadFile = (file: File): Promise<UploadResult> => {
  const fd = new FormData();
  fd.append('file', file);
  return http.post<unknown, UploadResult>('/common/upload', fd, {
    headers: { 'Content-Type': 'multipart/form-data' },
    timeout: 120000
  });
};
