// 扫桌面点餐码 → 桌台码。
//
// 首页（还没进桌时）与菜单页（会话里没有桌台时）都要这个动作，
// 而「用户取消 / 扫到无关内容 / 基础库不支持」三种结果的处理方式必须一致，
// 所以收在这里，不给两处各写一份提示语的机会。
import { parseScannedTableCode } from './scene';

/**
 * 调起扫码并解析出桌台码。
 *
 * 返回值三种：
 *  ・有效桌台码      → 进菜单 / 就地加载菜单
 *  ・`''`           → 扫到了，但不是点餐码（调用方提示「请扫描桌面上的点餐码」）
 *  ・`null`          → 顾客主动取消，调用方什么都别做（弹一句提示反而是噪音）
 *
 * 真正的失败（基础库不支持 / 相机不可用）才 reject，文案可直接展示。
 */
export function scanTableCode(): Promise<string | null> {
  return new Promise((resolve, reject) => {
    if (typeof wx.scanCode !== 'function') {
      reject(new Error('当前微信版本不支持扫码，请用微信「扫一扫」扫描桌面点餐码'));
      return;
    }
    wx.scanCode({
      // 只认二维码/条码；相册选图打开（onlyFromCamera=false）—— 桌牌拍照、截图也能识别
      scanType: ['qrCode', 'barCode'],
      onlyFromCamera: false,
      success: res => resolve(parseScannedTableCode(res.result, res.path)),
      fail: err => {
        const msg = String((err as { errMsg?: string } | undefined)?.errMsg || '');
        // 主动取消不是错误：返回 null，别弹一个「扫码失败」吓人
        if (/cancel/i.test(msg)) {
          resolve(null);
          return;
        }
        reject(new Error('扫码失败，请用微信「扫一扫」扫描桌面点餐码'));
      }
    });
  });
}
