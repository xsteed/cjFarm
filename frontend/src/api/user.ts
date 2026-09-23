import { http } from './http';
import type { ApiResponse, PageQuery, PageResult } from '../types/api';
import type { Id, PermCatalog, Role, UserInfo, UserPayload, RolePayload } from '../types/entities';

// 员工管理(权限: user:view / user:edit)
export const listUsers = (params?: PageQuery): Promise<PageResult<UserInfo>> =>
  http.get<unknown, PageResult<UserInfo>>('/admin/user/list', { params });
export const saveUser = (data: UserPayload): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>('/admin/user/save', data);
export const updateUser = (data: UserPayload): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>('/admin/user/update', data);
export const resetUserPassword = (data: UserPayload): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>('/admin/user/resetPassword', data);
export const toggleUserStatus = (data: UserPayload): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>('/admin/user/toggleStatus', data);
export const deleteUser = (id: Id): Promise<ApiResponse<unknown>> =>
  http.delete<unknown, ApiResponse<unknown>>(`/admin/user/${id}`);

// 角色与权限(权限: role:view / role:edit)
export const listRoles = (): Promise<PageResult<Role>> => http.get<unknown, PageResult<Role>>('/admin/role/list');
export const saveRole = (data: RolePayload): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>('/admin/role/save', data);
export const updateRole = (data: RolePayload): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>('/admin/role/update', data);
export const deleteRole = (id: Id): Promise<ApiResponse<unknown>> =>
  http.delete<unknown, ApiResponse<unknown>>(`/admin/role/${id}`);
// 权限点目录(按模块分组,含中文名)—— 前端不硬编码权限点,一律从这里取
export const getPermCatalog = (): Promise<PermCatalog> => http.get<unknown, PermCatalog>('/admin/perm/catalog');
