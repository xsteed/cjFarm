import { ref } from 'vue';
import type { Ref } from 'vue';
import { getPublicConfig } from '@dining/api';

/**
 * 先付模式开关(配置项 pay_first_enabled:'1' 先付 / '0' 后付),管理端各页面共用。
 *
 * 为什么走公开配置接口 /customer/config 而不是 /admin/config/list:收银员往往只有
 * order:settle 而没有 config:read,后者会被 403 挡下;前者无需登录且只回公开子集。
 *
 * 为什么默认后付:后端 settingDefaults 出厂即 '0';请求失败时保持 false,
 * 按钮显示「结算」(改动前的既有行为),不会把后付门店错配成先付。
 *
 * 模块级单例:一次页面生命周期只发一次请求;配置在「基础设置」里改动后刷新页面即生效
 * (改配置本就低频,不值得为它加轮询)。
 */
const payFirst = ref(false);
let requested = false;

export function usePayFirst(): Ref<boolean> {
  if (!requested) {
    requested = true;
    loadPayFirst();
  }
  return payFirst;
}

// 独立成函数:测试对 @dining/api 做整体 mock 时可能没有 getPublicConfig 导出,
// 同步抛错只拖垮这一次加载(保持后付默认),不会影响组件装载。
function loadPayFirst(): void {
  try {
    void getPublicConfig()
      .then(cfg => {
        payFirst.value = cfg.pay_first_enabled === '1';
      })
      .catch(() => {
        /* 读不到配置按后付处理,见 usePayFirst 注释 */
      });
  } catch {
    /* 同上 */
  }
}
