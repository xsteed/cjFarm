// ============================================================================
// 小程序设备信息(访问统计的终端直传)
//
// wx.getDeviceInfo 是基础库 2.20.1+ 的新 API(替代已停止维护的
// getSystemInfoSync);老基础库回退到后者,再拿不到就交空串 —— 服务端会
// 用 User-Agent 解析兜底,设备信息缺失只影响终端分布的精度,不影响点餐。
//
// 为什么端上直传:小程序能拿到精确机型(如 "iPhone 15 Pro")与系统版本,
// H5 的 UA 只能解析到品牌档位 —— 能拿准的信息就不该靠猜。
// ============================================================================

export interface MpDeviceInfo {
  /** 操作系统 + 版本,形如 "iOS 16.6"。 */
  system: string;
  /** 平台:ios / android / windows / mac。 */
  platform: string;
  /** 机型,如 "iPhone 15 Pro"。 */
  model: string;
}

/** 取本机设备信息(只读不缓存:调用频率极低,冷启动一次)。 */
export function mpDeviceInfo(): MpDeviceInfo {
  try {
    const d = wx.getDeviceInfo();
    return {
      system: String(d?.system || ''),
      platform: String(d?.platform || ''),
      model: String(d?.model || '')
    };
  } catch {
    /* 新 API 不可用(基础库过旧):回退 getSystemInfoSync */
  }
  try {
    const s = wx.getSystemInfoSync();
    return {
      system: String(s?.system || ''),
      platform: String(s?.platform || ''),
      model: String(s?.model || '')
    };
  } catch {
    return { system: '', platform: '', model: '' };
  }
}
