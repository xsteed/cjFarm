import { ref } from 'vue'

/**
 * 全局响应式断点:<= 767px 视为移动端。
 *
 * 用模块级单例 + 只绑一次 matchMedia 监听,而不是每个组件各绑一份:
 * 十几个页面同时挂载时不会重复注册监听器,且路由切换期间状态始终一致。
 * 用 matchMedia 而不是 window.innerWidth,是为了顺带覆盖「旋转屏幕 / 桌面浏览器
 * 拖动窗口」这类 resize 事件之外的变化,也避免在 resize 里反复读取布局属性。
 */
const MOBILE_QUERY = '(max-width: 767px)'

const isMobile = ref(false)
let bound = false

function syncMatches() {
  isMobile.value = window.matchMedia(MOBILE_QUERY).matches
}

export function useIsMobile() {
  if (!bound && typeof window !== 'undefined' && typeof window.matchMedia === 'function') {
    syncMatches()
    const mql = window.matchMedia(MOBILE_QUERY)
    // Safari 13 及更早只支持已废弃的 addListener
    if (typeof mql.addEventListener === 'function') {
      mql.addEventListener('change', syncMatches)
    } else if (typeof mql.addListener === 'function') {
      mql.addListener(syncMatches)
    }
    bound = true
  }
  return { isMobile }
}
