import { http } from './http';
import type { ApiResponse, PageQuery, PageResult } from '../types/api';
import type {
  AgentInfo,
  AgentPayload,
  AgentSaveResult,
  FeieInfo,
  Id,
  PrintAgent,
  Printer,
  PrinterBindPayload,
  PrinterPayload,
  PrinterStatus,
  PrintLog,
  TicketPreview
} from '../types/entities';

// 打印机
export const listPrinters = (): Promise<PageResult<Printer>> =>
  http.get<unknown, PageResult<Printer>>('/admin/printer/list');
export const savePrinter = (data: PrinterPayload): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>('/admin/printer/save', data);
export const updatePrinter = (data: PrinterPayload): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>('/admin/printer/update', data);
export const deletePrinter = (id: Id): Promise<ApiResponse<unknown>> =>
  http.delete<unknown, ApiResponse<unknown>>(`/admin/printer/${id}`);
export const testPrinter = (id: Id): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>(`/admin/printer/test/${id}`, null, { timeout: 60000 });
// 只测连通性不吐纸(TCP 探端口 / 飞鹅查云端状态)
export const probePrinter = (id: Id): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>(`/admin/printer/probe/${id}`, null, { timeout: 60000 });
// 打印实时状态:飞鹅返回在线/缺纸状态与当日打印统计
export const printerStatus = (id: Id): Promise<PrinterStatus> =>
  http.get<unknown, PrinterStatus>(`/admin/printer/status/${id}`, { timeout: 60000 });
// 把打印机绑定到当前飞鹅账号(SN#KEY#备注#流量卡)
export const bindFeiePrinter = (data: PrinterBindPayload): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>('/admin/printer/bind', data, { timeout: 60000 });
// 清空待打印队列(飞鹅云端队列 / 本地代理队列)
export const clearPrinterQueue = (id: Id): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>(`/admin/printer/clear/${id}`, null, { timeout: 60000 });
// 飞鹅账号配置概况(不回显 UKEY)
export const getFeieInfo = (): Promise<FeieInfo> => http.get<unknown, FeieInfo>('/admin/printer/feie/info');
// 本地打印代理概况(不回显代理令牌;含代理是否在线、队列积压与 v2 代理身份列表)
export const getAgentInfo = (): Promise<AgentInfo> => http.get<unknown, AgentInfo>('/admin/printer/agent/info');

// 打印日志与补打
export const listPrintLogs = (params?: PageQuery): Promise<PageResult<PrintLog>> =>
  http.get<unknown, PageResult<PrintLog>>('/admin/print/log/list', { params });
// 按日志重打(同订单、同打印机、同单据类型)
export const reprintLog = (printId: Id): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>('/admin/print/log/reprint', { printId }, { timeout: 60000 });
// 票据预览(后端重放渲染,不出纸):返回与出纸同一份代码生成的等宽文本行
export const previewPrintLog = (printId: Id): Promise<TicketPreview> =>
  http.get<unknown, TicketPreview>('/admin/print/preview', { params: { printId } });

// 示例模板预览:不依赖真实订单,用示例数据 + 配置渲染「预计打印模板」;
// 请求体可携带表单里未保存的值(footer/开关)即时覆盖,所见即所改。
export interface SamplePreviewPayload {
  docType?: string;
  paperWidth?: number;
  footer?: string;
  showSeatFee?: boolean;
  showDiscount?: boolean;
  kitchenShowPrice?: boolean;
}

export const previewSample = (data: SamplePreviewPayload): Promise<TicketPreview> =>
  http.post<unknown, TicketPreview>('/admin/print/preview/sample', data);

// ==================== 打印代理身份管理(per-agent 令牌) ====================

// 列表加载失败静默(老后端无此接口时区块优雅降级,不打扰整页配置功能)。
export const listAgents = (): Promise<PageResult<PrintAgent>> =>
  http.get<unknown, PageResult<PrintAgent>>('/admin/printer/agent/list', { silent: true });

// 新增代理身份:令牌明文仅在本次响应中返回一次。
export const saveAgent = (data: AgentPayload): Promise<AgentSaveResult> =>
  http.post<unknown, AgentSaveResult>('/admin/printer/agent/save', data);

// 启用(1) / 吊销(0) 代理身份。
export const setAgentStatus = (id: Id, status: number): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>(`/admin/printer/agent/status/${id}`, { status });

// 修改代理名称与授权范围(不重新签发令牌);printerIds 留空 = 授权全部打印机。
// 后端保存时会剔除已删除打印机的残留 id,故它同时承担「清理历史打印机」的作用。
export const updateAgent = (id: Id, data: AgentPayload): Promise<ApiResponse<unknown>> =>
  http.post<unknown, ApiResponse<unknown>>(`/admin/printer/agent/update/${id}`, data);

// 删除代理身份:只接受已吊销的代理,连同令牌哈希一起清掉(启用中的必须先吊销)。
export const deleteAgent = (id: Id): Promise<ApiResponse<unknown>> =>
  http.delete<unknown, ApiResponse<unknown>>(`/admin/printer/agent/${id}`);
