// ============================================================================
// 小程序运行时配置
//
// ⚠️ 上线前必改：API_ORIGIN 必须是**已 ICP 备案的 HTTPS 域名**，并在
//   微信公众平台 → 开发管理 → 服务器域名 中同时加入：
//    ・request 合法域名   ← 接口调用（/api/customer/*）
//    ・downloadFile 合法域名 ← <image> 加载菜品图与收款码（微信对 image 走这条白名单）
//   小程序不接受 http，也不接受裸 IP —— 详见 docs/miniprogram.md 第三节。
// ============================================================================

/** 后端站点源（含协议与端口，不含路径）。 */
export const API_ORIGIN = 'http://127.0.0.1:8080';

/** 接口前缀，与 Web 端保持一致（后端 /api 分组）。 */
export const API_BASE_URL = `${API_ORIGIN}/api`;

/** 单次请求超时（毫秒），与 Web 端 axios 实例一致。 */
export const REQUEST_TIMEOUT_MS = 15000;

/** 订单状态轮询间隔：顾客端 3s 一拍，与 H5 一致。 */
export const ORDER_POLL_INTERVAL_MS = 3000;

/** 催菜冷却的兜底秒数：后端未返回 cooldown 时使用（与后端默认一致）。 */
export const URGE_COOLDOWN_FALLBACK_S = 180;

/** 本地会话缓存键（桌台码 / 订单号，用于中途退出后恢复）。 */
export const STORAGE_KEY = 'dining_mp_session';

/** 顾客会员令牌的本地缓存键（2h 有效；过期由 store/member.ts 静默重登，顾客无感知）。 */
export const CUSTOMER_TOKEN_KEY = 'dining_mp_customer_token';

/**
 * 把后端返回的图片路径补成绝对地址。
 * 后端存量图片是 `/uploads/xxx` 这种站内相对路径，小程序的 <image> 不认，
 * 必须补成 https 绝对地址（且该域名要在 downloadFile 白名单里）。
 */
export function mediaUrl(path: string | null | undefined): string {
  const v = String(path || '').trim();
  if (!v) return '';
  if (/^https?:\/\//i.test(v)) return v;
  if (v.startsWith('//')) return `https:${v}`;
  return `${API_ORIGIN}${v.startsWith('/') ? v : `/${v}`}`;
}

/**
 * 开发期兜底桌台码。
 *
 * 开发者工具里用「普通编译」进来时没有任何 scene / query 参数，页面会停在
 * 「请扫描餐桌上的二维码进入点餐」。填上本机库中真实存在的桌台码后，
 * 不加编译模式也能直接进菜单，省去每次调工具配置。扫码进入时参数优先，不受影响。
 *
 * 正式版不生效：`devTableCode()` 在 envVersion === 'release' 时恒返回空串。
 * 不需要这个便利时置空即可。
 */
export const DEV_TABLE_CODE = '8YG3Q335';

/** 取开发期兜底桌台码：仅「开发版」返回；体验版与正式版一律返回空串（必须扫码进入）。 */
export function devTableCode(): string {
  if (!DEV_TABLE_CODE) return '';
  try {
    return wx.getAccountInfoSync().miniProgram.envVersion === 'develop' ? DEV_TABLE_CODE : '';
  } catch {
    /* 基础库过旧、或冒烟脚本的极简 wx 桩没有该 API：不兜底，让页面走「请扫码进入」 */
    return '';
  }
}
