import { computed, onMounted, ref } from 'vue';
import { getPublicConfig } from '@dining/api';

// 店铺品牌名在管理端多处展示(侧栏左上角品牌区、登录页标题),取数与
// 「店铺名称 · 点餐系统」这个拼接格式统一收在这里 —— 格式反复调整过几次,
// 复制成两份必然会漂移,改一处忘一处。
//
// 模块级单例:多个组件同时挂载只会发一次请求(与 @dining/auth 的 permSet 同一做法)。
const shopName = ref('');
let inflight: Promise<void> | null = null;

/**
 * 拉取店铺名。并发去重;失败静默 —— 保持默认品牌名,不阻塞页面渲染。
 *
 * 走的是顾客端公开配置接口(无需登录),所以登录页也能用。
 */
export function loadShopBrand(): Promise<void> {
  if (inflight) return inflight;
  inflight = getPublicConfig()
    .then(cfg => {
      shopName.value = (cfg?.shop_name || '').trim();
    })
    .catch(() => {
      /* 拉取失败保持默认品牌名 */
    })
    .finally(() => {
      inflight = null;
    });
  return inflight;
}

/**
 * 品牌文案:配置了店铺名时为「店铺名称 · 点餐系统」,未配置时用调用方给的兜底文案
 * (侧栏与登录页的历史文案不同,故兜底由调用方决定)。
 *
 * 挂载时自动拉取一次;调用方若需要「改完店铺名立刻生效」,可再自行调用 loadShopBrand()。
 */
export function useShopBrand(fallback: string) {
  const brandName = computed(() => (shopName.value ? `${shopName.value} · 点餐系统` : fallback));
  onMounted(loadShopBrand);
  return { brandName, loadShopBrand };
}
