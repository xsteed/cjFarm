// ============================================================================
// 小程序运行时配置
//
// 为什么这里的域名不是「一个手改的常量」：
//   Web 端前后端同域，请求走相对路径 `/api`，本地靠 vite proxy —— 它压根不需要
//   环境配置。小程序没有「同域」这回事，必须写死绝对 HTTPS 域名，于是「本地联调」
//   与「线上」天然是两个值。
//
//   最早的做法是留一个 API_ORIGIN 让人上线前手改，再用 scripts/precheck-release.mjs
//   拦住忘了改的情况。但那个动作本身才是风险源：改源码切环境，改漏一次就是全站
//   请求失败（真机上没有任何降级路径），且来回切会在 git 里留下噪音。
//
//   现在改成**按 envVersion 自动切换**——这个判据代码里本来就在用（devTableCode）。
//   开发者工具/开发版 → 本机；体验版/正式版 → 生产。不需要任何手工动作，
//   也就不存在「忘了改」。
//
// 上线前仍必须做的事（代码侧无法替代，`npm run mp:precheck` 会检查前两项）：
//   1) 把下面 PROD_API_ORIGIN 换成已 ICP 备案的真实 HTTPS 域名；
//   2) 在微信公众平台 → 开发管理 → 服务器域名 中同时加入：
//        · request 合法域名      ← 接口调用（/api/customer/*）
//        · downloadFile 合法域名 ← <image> 加载菜品图与收款码（微信对 image 走这条白名单）
//      小程序不接受 http，也不接受裸 IP —— 详见 docs/miniprogram.md 第三节。
// ============================================================================
import { LS_CUSTOMER_TOKEN } from './shared/keys';

/** 开发期后端地址：开发者工具连本机后端。 */
const DEV_API_ORIGIN = 'http://127.0.0.1:8080';

/**
 * 生产 / 体验环境后端地址。
 *
 * ⚠️ 上线前必须换成真实域名。占位符（含 `example.com`）会被 `npm run mp:precheck` 拦下，
 * 所以带着它提审不会「静默失败」，而是根本过不了预检。
 */
const PROD_API_ORIGIN = 'https://YOUR-DOMAIN.example.com';

/** 微信的运行环境档位（`wx.getAccountInfoSync().miniProgram.envVersion`）。 */
export type EnvVersion = 'develop' | 'trial' | 'release';

/**
 * 当前环境档位。
 *
 * 兜底到 `develop` 是刻意的：`getAccountInfoSync` 只在极老基础库或测试桩里不可用。
 * 那种情况下宁可去连本机（现象是「连不上」，一眼就能发现），也绝不能在开发阶段
 * 误连生产 —— 后者会往真实账单里写测试数据，比报错难收拾得多。
 */
export function envVersion(): EnvVersion {
  try {
    const v = wx.getAccountInfoSync?.()?.miniProgram?.envVersion;
    return v === 'release' || v === 'trial' ? v : 'develop';
  } catch {
    return 'develop';
  }
}

/** 是否为开发版（开发者工具 / 真机调试）。`devTableCode()` 与域名切换共用它。 */
export function isDevelopEnv(): boolean {
  return envVersion() === 'develop';
}

/** 后端站点源（含协议与端口，不含路径）。按环境自动切换，不需要手工改。 */
export const API_ORIGIN = isDevelopEnv() ? DEV_API_ORIGIN : PROD_API_ORIGIN;

/**
 * 生产域名是否还是占位符（还没拿到已备案的域名）。
 *
 * `mp:precheck` 会在提审前拦住它，但**体验版可能先于预检被打出来**：那时全站请求
 * 100% 失败，控制台只有一堆 `request:fail`，排查的人要翻到 runtimeConfig 才能定位。
 * app.ts 启动时用它打一句明确的中文线索（见 onLaunch）。
 */
export function isProdOriginPending(): boolean {
  return /example\.com|your-?domain/i.test(PROD_API_ORIGIN);
}

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

/**
 * 顾客会员令牌的本地缓存键（2h 有效；过期由 store/member.ts 静默重登，顾客无感知）。
 *
 * 键名定义在 shared/keys.ts：App / 网站 / 小程序共用同一个常量，
 * 请求拦截器（packages/api/src/http.ts）也按它取令牌，不允许各端自定义。
 */
export const CUSTOMER_TOKEN_KEY = LS_CUSTOMER_TOKEN;

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
 * 正式版不生效：`devTableCode()` 在非 develop 环境恒返回空串，所以这个值
 * **可以安全地留在仓库里** —— 它不需要在提审前手工清空（清空只会白白牺牲开发便利）。
 */
export const DEV_TABLE_CODE = '8YG3Q335';

/**
 * 取开发期兜底桌台码：仅「开发版」返回；体验版与正式版一律返回空串（必须扫码进入）。
 *
 * ⚠️ 这里**刻意不复用** `isDevelopEnv()`：后者把「拿不到环境信息」当作 develop
 * （为了开发时宁可连不上本机也不误连生产）。但兜底桌台码的失效方向相反 ——
 * 一旦误用，顾客会在没扫码的情况下被带进某张桌、把菜点到别人的桌上。
 * 所以「环境信息拿不到」在这里按「不是开发版」处理，与改造前的行为一致。
 */
export function devTableCode(): string {
  if (!DEV_TABLE_CODE) return '';
  try {
    return wx.getAccountInfoSync?.()?.miniProgram?.envVersion === 'develop' ? DEV_TABLE_CODE : '';
  } catch {
    /* 基础库过旧、或冒烟脚本的极简 wx 桩没有该 API：不兜底，让页面走「请扫码进入」 */
    return '';
  }
}
