// ============================================================================
// 隐私授权（2023-10-17 起微信强制）
//
// 背景：微信把「手机号」列为隐私接口。顾客点 pages/profile 里的
// `<button open-type="getPhoneNumber">` 时，若尚未同意《用户隐私保护指引》，
// 基础库会**挂起**这次调用，并回调我们在 app.ts 里注册的
// wx.onNeedPrivacyAuthorization。必须把它给的 resolve 兑现回一次，
// 被挂起的接口才会继续执行 —— 不注册这个监听，「绑定手机号」按钮点了就是没反应
// （拿不到 e.detail.code，pages/profile 的回调直接 return）。
//
// 为什么 resolve 不放 GlobalData：它是一次性闭包，与 session / cart / member
// 那些「跨页共享业务状态」性质完全不同，混进去只会让那份语义变脏。
// 而且 resolver 必须「取走即失效」：同意与不同意只能兑现一次。
// ============================================================================

/** 隐私授权页的路由（app.ts 跳转与「当前是否已在本页」的判断共用同一份字符串）。 */
export const PRIVACY_ROUTE = 'pages/privacy/index';

/**
 * wx.onNeedPrivacyAuthorization 回调里给出的函数。
 *
 * event：
 *   ・ agree                  用户点击了「同意」按钮（需带 buttonId，基础库会校验这个按钮真被点过）
 *   ・ disagree               用户拒绝，原隐私接口 fail（privacy permission is not authorized）
 *   ・ exposureAuthorization  仅告知平台「弹窗已曝光」，**不消耗** resolver，可多次调用
 */
export type PrivacyResolver = (param: {
  event: 'agree' | 'disagree' | 'exposureAuthorization';
  buttonId?: string;
}) => void;

let pending: PrivacyResolver | null = null;

/** app.ts 在 wx.onNeedPrivacyAuthorization 回调里登记待兑现的 resolver。 */
export function setPrivacyResolver(fn: PrivacyResolver): void {
  pending = fn;
}

export function hasPendingPrivacy(): boolean {
  return pending !== null;
}

/**
 * 取走待处理的 resolver——注意是「取走」：第二次调用返回 null。
 * 这样同一个 resolver 不可能被两处各兑现一遍（并发双击时尤其要紧）。
 */
export function takePrivacyResolver(): PrivacyResolver | null {
  const fn = pending;
  pending = null;
  return fn;
}

/**
 * 告知平台隐私弹窗已曝光：不计入最终决定，所以**不清空** pending。
 * 失败静默：这只影响后台的曝光统计，不能因为它把授权流程卡住。
 */
export function reportPrivacyExposure(): void {
  try {
    pending?.({ event: 'exposureAuthorization' });
  } catch {
    /* 静默 */
  }
}
