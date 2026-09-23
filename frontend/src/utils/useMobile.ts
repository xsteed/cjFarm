import { ref } from 'vue';
import type { Ref } from 'vue';

/**
 * 全局响应式断点。用模块级单例 + 只绑一次 matchMedia 监听,而不是每个组件各绑一份:
 * 十几个页面同时挂载时不会重复注册监听器,且路由切换期间状态始终一致。
 * 用 matchMedia 而不是 window.innerWidth,是为了顺带覆盖「旋转屏幕 / 桌面浏览器
 * 拖动窗口」这类 resize 事件之外的变化,也避免在 resize 里反复读取布局属性。
 *
 * 断点按主流 iOS / 安卓机型的逻辑像素宽度标定(覆盖范围见下方注释):
 *   - isNarrow  <= 360px   超窄屏:iPhone SE(1代) 320 / 小安卓 360
 *   - isMobile  <= 1024px  紧凑布局主开关:手机(iOS 375~430 / 安卓 360~412)
 *                          与平板竖屏(iPad mini 768 / iPad 810 / iPad Air 834 /
 *                          iPad Pro 1024)一并覆盖 —— 表格改卡片流、抽屉侧栏等
 *                          结构切换都以此为准
 *   - isTablet  768~1024px 平板竖屏细分:需要区分「手机」与「平板」时用
 *   - (超过 1024px 为桌面,含 iPad 横屏)
 */
const BREAKPOINTS: Record<'narrow' | 'mobile' | 'tablet', string> = {
  narrow: '(max-width: 360px)',
  mobile: '(max-width: 1024px)',
  tablet: '(min-width: 768px) and (max-width: 1024px)'
};

const isNarrow = ref(false);
const isMobile = ref(false);
const isTablet = ref(false);

let bound = false;

function queryMatches(q: string): boolean {
  return typeof window !== 'undefined' && typeof window.matchMedia === 'function'
    ? window.matchMedia(q).matches
    : false;
}

function syncMatches(): void {
  isNarrow.value = queryMatches(BREAKPOINTS.narrow);
  isMobile.value = queryMatches(BREAKPOINTS.mobile);
  isTablet.value = queryMatches(BREAKPOINTS.tablet);
}

export function useIsMobile(): { isMobile: Ref<boolean>; isTablet: Ref<boolean>; isNarrow: Ref<boolean> } {
  if (!bound && typeof window !== 'undefined' && typeof window.matchMedia === 'function') {
    syncMatches();
    for (const q of Object.values(BREAKPOINTS)) {
      const mql = window.matchMedia(q);
      // Safari 13 及更早只支持已废弃的 addListener
      if (typeof mql.addEventListener === 'function') {
        mql.addEventListener('change', syncMatches);
      } else if (typeof mql.addListener === 'function') {
        mql.addListener(syncMatches);
      }
    }
    bound = true;
  }
  return { isMobile, isTablet, isNarrow };
}
