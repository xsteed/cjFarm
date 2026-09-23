// H5 地址校验纯函数:从 Tables.vue 迁出,无 DOM 依赖便于单测。
//
// 说明:原实现里 PAGE_ON_LOOPBACK 是模块加载时读 window.location.hostname 得到的
// 常量。为了让它可被单测(不依赖 DOM),这里改成接收可选 hostname 的函数;
// 浏览器中不传参时行为与原来的常量完全一致。

// 回环地址:localhost / 127.0.0.1 / 0.0.0.0 / ::1。
export const LOOPBACK_RE = /^https?:\/\/(?:localhost|127\.0\.0\.1|0\.0\.0\.0|\[::1\])(?::\d+)?(?:\/|$)/i;

// 判断给定 hostname 是否为回环地址(本地开发)。
// 默认取当前页面 hostname;测试可显式传入,避免依赖 window。
export function PAGE_ON_LOOPBACK(hostname?: string): boolean {
  const host = hostname ?? (typeof window !== 'undefined' ? window.location.hostname : '');
  return /^(?:localhost|127\.0\.0\.1|0\.0\.0\.0|\[::1\])$/i.test(host);
}

// 归一化配置里的 H5 地址:补协议、去末尾斜杠。
// 少了这一步,「111.230.154.50」这种裸地址会直接进二维码,手机可能识别不出。
export function normalizeBaseUrl(raw: unknown): string {
  let v = String(raw || '')
    .trim()
    .replace(/\/+$/, '');
  if (v && !/^https?:\/\//i.test(v)) v = 'http://' + v;
  return v;
}
