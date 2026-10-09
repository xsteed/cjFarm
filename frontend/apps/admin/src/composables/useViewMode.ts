import { ref, watch } from 'vue';
import { safeStorage } from '@dining/shared';
import { getUsername } from '@dining/auth';
import type { RadioValue } from 'tdesign-vue-next';

// ============================================================================
// 「列表 / 卡片」视图偏好的本地缓存
//
// 按「用户名 + 页面」分键:前台/收银常共用一台机器、多人轮流登录,
// 若全局只存一个键,A 切成列表后 B 登录也被带成列表。分键后各记各的。
//
// 存 localStorage 而不是后端:这纯属本机展示偏好,换台电脑重选一次无伤大雅,
// 不值得为它加接口和字段。读写都走 safeStorage —— Safari 隐私模式下
// localStorage 的读写会抛 SecurityError,这里不能让整页白屏。
// ============================================================================

const PREFIX = 'admin_view_mode';

/** 拿不到用户名(理论上进不了后台页)时落到 anon 命名空间,避免出现 `::` 空段键名。 */
function storeKey(page: string): string {
  return `${PREFIX}:${getUsername() || 'anon'}:${page}`;
}

/**
 * 记住某个页面的视图模式,返回值直接给 t-radio-group 的 v-model:
 * 初次进入读上次的选择(无缓存或值已失效则回落 fallback),切换时自动落盘。
 *
 * @param page 页面标识(如 dishes / tables),不同页面互不干扰
 * @param fallback 无缓存时的默认视图
 */
export function useViewMode(page: string, fallback: RadioValue = 'card') {
  const cached = safeStorage.get(storeKey(page));
  // 只认仍在用的两个值:将来新增/删掉某个视图时,旧缓存不会把页面卡成空白
  const initial: RadioValue = cached === 'table' || cached === 'card' ? cached : fallback;
  const viewMode = ref<RadioValue>(initial);

  // watch 建在 setup 期间,随组件卸载自动停止
  watch(viewMode, v => safeStorage.set(storeKey(page), String(v)));

  return viewMode;
}
