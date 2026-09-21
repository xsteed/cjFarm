import axios from 'axios'
import { MessagePlugin } from 'tdesign-vue-next'
import { LS_TOKEN, AUTH_KEYS } from '../utils/authKeys'

const http = axios.create({
  baseURL: '/prod-api',
  timeout: 15000
})

// 请求拦截:自动携带登录令牌
http.interceptors.request.use((config) => {
  const token = localStorage.getItem(LS_TOKEN)
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// HTTP 状态码兜底文案。后端业务错误已带中文 msg,这里的映射只在
// 拿不到 msg(网关/代理/超时等)时生效,避免把英文技术文案抛给用户。
const HTTP_FALLBACK_MSG = {
  400: '请求有误，请刷新后重试',
  401: '登录状态已过期，请重新登录',
  403: '没有操作权限',
  404: '请求的内容不存在',
  408: '请求超时，请检查网络后重试',
  500: '服务繁忙，请稍后重试',
  502: '服务正在重启，请稍后重试',
  503: '服务暂时不可用，请稍后重试',
  504: '服务响应超时，请稍后重试'
}

// friendlyMessage 把 axios 抛出的英文技术错误转换为中文可读文案。
// 优先级:业务 msg > 超时 > 状态码映射 > 状态码兜底 > 网络失败。
function friendlyMessage(err) {
  const bizMsg = err?.response?.data?.msg
  if (bizMsg) return bizMsg
  if (err?.code === 'ECONNABORTED') return '请求超时，请检查网络后重试'
  const status = err?.response?.status
  if (status && HTTP_FALLBACK_MSG[status]) return HTTP_FALLBACK_MSG[status]
  if (status) return `请求失败（${status}），请稍后重试`
  return '网络连接失败，请检查网络后重试'
}

// 响应拦截:统一处理业务码与错误
http.interceptors.response.use(
  (res) => {
    const data = res.data
    if (data && data.code === 200) {
      // 分页表格结果:{ code, msg, total, rows }
      if (data.rows !== undefined) return data
      // 带数据的对象结果
      if (data.data !== undefined) return data.data
      // 仅消息结果(如保存成功)
      return data
    }
    const msg = data?.msg || '请求失败'
    MessagePlugin.error(msg)
    return Promise.reject(new Error(msg))
  },
  (err) => {
    if (err.response?.status === 401) {
      // 令牌失效:清掉全部登录态键(含权限/角色缓存)。
      // 只删令牌会留下过期的 admin_perms,下次进管理端菜单会先按旧权限渲染一下。
      for (const k of AUTH_KEYS) {
        localStorage.removeItem(k)
      }
      if (!location.pathname.startsWith('/login')) {
        location.href = '/login'
      }
      return Promise.reject(err)
    }
    const msg = friendlyMessage(err)
    MessagePlugin.error(msg)
    // 覆写 message:调用方 catch(e) 后 e.message 默认是英文技术文案
    // (如 "Request failed with status code 400"),此处统一换成中文。
    err.message = msg
    err.friendlyMessage = msg
    return Promise.reject(err)
  }
)

// 登录
export const login = (data) => http.post('/auth/login', data)

// 用「记住我」令牌静默换取新登录态(免登录 7/30 天);过期与否由后端查库裁决。
export const rememberLogin = (data) => http.post('/auth/remember-login', data)

// 修改密码
export const changePassword = (data) => http.post('/dining/auth/password', data)

// 我的信息与权限(登录态刷新用:返回 username / realName / roleKey / roleName / perms)
export const getProfile = () => http.get('/dining/auth/profile')

// 员工管理(权限: user:view / user:edit)
export const listUsers = (params) => http.get('/dining/user/list', { params })
export const saveUser = (data) => http.post('/dining/user/save', data)
export const updateUser = (data) => http.post('/dining/user/update', data)
export const resetUserPassword = (data) => http.post('/dining/user/resetPassword', data)
export const toggleUserStatus = (data) => http.post('/dining/user/toggleStatus', data)
export const deleteUser = (id) => http.delete(`/dining/user/${id}`)

// 角色与权限(权限: role:view / role:edit)
export const listRoles = () => http.get('/dining/role/list')
export const saveRole = (data) => http.post('/dining/role/save', data)
export const updateRole = (data) => http.post('/dining/role/update', data)
export const deleteRole = (id) => http.delete(`/dining/role/${id}`)
// 权限点目录(按模块分组,含中文名)—— 前端不硬编码权限点,一律从这里取
export const getPermCatalog = () => http.get('/dining/perm/catalog')

// 顾客端
export const getTable = (id) => http.get(`/api/dining/table/${id}`)
export const getMenu = () => http.get('/api/dining/menu')
export const getRemarks = () => http.get('/api/dining/remarks')
export const createOrder = (data) => http.post('/api/dining/order', data)
export const appendOrder = (data) => http.post('/api/dining/order/append', data)
export const getOrderByNo = (no) => http.get(`/api/dining/order/no/${no}`)
// 顾客催菜:请求后厨加急(同一订单 3 分钟冷却,超频后端会返回剩余秒数)
export const urgeOrder = (orderNo) => http.post('/api/dining/order/urge', { orderNo })
export const getPayQr = () => http.get('/api/dining/pay/qr')
// 顾客端公开配置(无需登录):店铺名/餐位费/促销/收款码
export const getPublicConfig = () => http.get('/api/dining/config')

// 在线支付(微信/支付宝)
export const createPay = (data) => http.post('/api/dining/pay/create', data)
export const queryPay = (orderNo) => http.get('/api/dining/pay/query', { params: { orderNo } })

// 桌台
export const listTables = (params) => http.get('/dining/table/list', { params })
export const saveTable = (data) => http.post('/dining/table/save', data)
export const updateTable = (data) => http.post('/dining/table/update', data)
export const deleteTable = (id) => http.delete(`/dining/table/${id}`)

// 分类
export const listCategories = () => http.get('/dining/category/list')
export const saveCategory = (data) => http.post('/dining/category/save', data)
export const updateCategory = (data) => http.post('/dining/category/update', data)
export const deleteCategory = (id) => http.delete(`/dining/category/${id}`)

// 菜品
export const listDishes = (params) => http.get('/dining/dish/list', { params })
export const getDish = (id) => http.get(`/dining/dish/${id}`)
export const saveDish = (data) => http.post('/dining/dish/save', data)
export const updateDish = (data) => http.post('/dining/dish/update', data)
export const deleteDish = (id) => http.delete(`/dining/dish/${id}`)

// 备注
export const listRemarkOptions = () => http.get('/dining/remark/list')
export const saveRemark = (data) => http.post('/dining/remark/save', data)
export const updateRemark = (data) => http.post('/dining/remark/update', data)
export const deleteRemark = (id) => http.delete(`/dining/remark/${id}`)

// 打印机
export const listPrinters = () => http.get('/dining/printer/list')
export const savePrinter = (data) => http.post('/dining/printer/save', data)
export const updatePrinter = (data) => http.post('/dining/printer/update', data)
export const deletePrinter = (id) => http.delete(`/dining/printer/${id}`)
export const testPrinter = (id) => http.post(`/dining/printer/test/${id}`, null, { timeout: 60000 })
// 只测连通性不吐纸(TCP 探端口 / 飞鹅查云端状态)
export const probePrinter = (id) => http.post(`/dining/printer/probe/${id}`, null, { timeout: 60000 })
// 打印实时状态:飞鹅返回在线/缺纸状态与当日打印统计
export const printerStatus = (id) => http.get(`/dining/printer/status/${id}`, { timeout: 60000 })
// 把打印机绑定到当前飞鹅账号(SN#KEY#备注#流量卡)
export const bindFeiePrinter = (data) => http.post('/dining/printer/bind', data, { timeout: 60000 })
// 清空飞鹅云端待打印队列
export const clearPrinterQueue = (id) => http.post(`/dining/printer/clear/${id}`, null, { timeout: 60000 })
// 飞鹅账号配置概况(不回显 UKEY)
export const getFeieInfo = () => http.get('/dining/printer/feie/info')

// 打印日志与补打
export const listPrintLogs = (params) => http.get('/dining/print/log/list', { params })
// 按日志重打(同订单、同打印机、同单据类型)
export const reprintLog = (printId) => http.post('/dining/print/log/reprint', { printId }, { timeout: 60000 })
// 按订单补打;不传 printerId 时后端自动挑一台该类型的启用打印机
export const reprintOrder = (data) => http.post('/dining/order/reprint', data, { timeout: 60000 })

// 操作日志(审计留痕)
export const listOperLogs = (params) => http.get('/dining/log/list', { params })
// 清理过期日志:天数只由后端 AUDIT_RETENTION_DAYS 决定,接口不接受任意天数
// (防止持有 log:manage 的人一次抹掉全部历史)
export const cleanOperLogs = () => http.post('/dining/log/clean', {})

// 配置
export const getConfig = () => http.get('/dining/config/list')
export const saveConfig = (data) => http.post('/dining/config/save', data)

// 订单
export const listOrders = (params) => http.get('/dining/order/list', { params })
export const getOrder = (id) => http.get(`/dining/order/${id}`)
export const getOrderBoard = () => http.get('/dining/order/board')
// 催菜(商家端):催菜列表与处理,用于接单/上菜前查看哪些桌在催
export const listUrges = (params) => http.get('/dining/order/urge/list', { params })
export const handleUrge = (data) => http.post('/dining/order/urge/handle', data)
export const changeOrderStatus = (data) => http.post('/dining/order/status', data)
export const payOrder = (data) => http.post('/dining/order/pay', data)
// 结账:支持正常收款 / 免单 / 挂账(settleType: normal | free | credit)
export const settleOrder = (data) => http.post('/dining/order/settle', data)
// 挂账核销:补收挂账欠款
export const settleCreditOrder = (data) => http.post('/dining/order/credit/settle', data)
// 撤销结算:把已免单/已挂账的订单退回未支付
export const cancelSettle = (data) => http.post('/dining/order/settle/cancel', data)
export const finishOrder = (data) => http.post('/dining/order/finish', data)
export const cancelOrder = (data) => http.post('/dining/order/cancel', data)
export const editOrder = (data) => http.post('/dining/order/edit', data)
export const refundPay = (data) => http.post('/dining/pay/refund', data)
export const queryRefund = (data) => http.post('/dining/pay/refund/query', data)
export const listRefunds = (orderId) => http.get('/dining/pay/refund/list', { params: { orderId } })

// 报表
export const getReportSummary = () => http.get('/dining/report/summary')
export const getDailyTrend = (params) => http.get('/dining/report/dailyTrend', { params })
export const getMonthlyTrend = () => http.get('/dining/report/monthlyTrend')
export const getDishRank = (params) => http.get('/dining/report/dishRank', { params })

// 上传
export const uploadFile = (file) => {
  const fd = new FormData()
  fd.append('file', file)
  return http.post('/common/upload', fd, {
    headers: { 'Content-Type': 'multipart/form-data' }
  })
}

export const ORDER_STATUS = {
  1: { label: '已下单', theme: 'primary' },
  2: { label: '制作中', theme: 'warning' },
  3: { label: '已上齐', theme: 'success' },
  4: { label: '已完成', theme: 'default' },
  5: { label: '已取消', theme: 'danger' }
}

// 结算方式:normal 正常收款 / free 免单 / credit 挂账
export const SETTLE_TYPE = {
  normal: { label: '正常收款', theme: 'success' },
  free: { label: '免单', theme: 'warning' },
  credit: { label: '挂账', theme: 'primary' }
}

// 挂账状态:0 非挂账 1 待收款 2 已结清
export const CREDIT_STATUS = {
  0: { label: '非挂账', theme: 'default' },
  1: { label: '待收款', theme: 'warning' },
  2: { label: '已结清', theme: 'success' }
}

// 退款单状态
export const REFUND_STATUS = {
  0: { label: '退款中', theme: 'warning' },
  1: { label: '已退款', theme: 'success' },
  2: { label: '退款失败', theme: 'danger' }
}

// 打印机接入方式:tcp=网络直连(与后端同局域网) / feie=飞鹅云打印(跨网络可用)
export const PRINTER_PROVIDER = {
  tcp: { label: '网络直连', theme: 'default' },
  feie: { label: '飞鹅云', theme: 'primary' }
}

// 打印机类型
export const PRINTER_TYPE = {
  1: { label: '厨房单', theme: 'danger' },
  2: { label: '食客小票', theme: 'primary' }
}

// 打印单据类型
export const PRINT_DOC_TYPE = {
  kitchen: { label: '厨房单', theme: 'danger' },
  guest: { label: '食客小票', theme: 'primary' },
  test: { label: '测试页', theme: 'default' }
}

// 打印结果:0 失败 1 已送出
export const PRINT_STATUS = {
  0: { label: '失败', theme: 'danger' },
  1: { label: '已送出', theme: 'success' }
}

// 打印触发场景
export const PRINT_TRIGGER = {
  order: '顾客下单',
  append: '顾客加菜',
  settle: '收银结账',
  test: '测试打印',
  reprint: '人工补打'
}

// 操作日志:操作类型(与后端 model.OperType* 一致)
export const OPER_TYPE = {
  insert: { label: '新增', theme: 'primary' },
  update: { label: '修改', theme: 'warning' },
  delete: { label: '删除', theme: 'danger' },
  login: { label: '登录', theme: 'default' },
  grant: { label: '授权', theme: 'danger' },
  print: { label: '打印', theme: 'default' },
  other: { label: '其它', theme: 'default' }
}

// 操作日志:结果(0 失败 / 1 成功)
export const OPER_STATUS = {
  0: { label: '失败', theme: 'danger' },
  1: { label: '成功', theme: 'success' }
}
