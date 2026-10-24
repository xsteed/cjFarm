import { createApp } from 'vue';
// 组件 JS 由 unplugin-vue-components 按需自动导入(见 vite.config.ts);这里只保留全量样式
// 与函数式组件(MessagePlugin/DialogPlugin 各自显式 import)所需的样式兜底。
import 'tdesign-vue-next/es/style/index.css';
import { MessagePlugin } from 'tdesign-vue-next';
import { formatMoney, setUiNotifier } from '@dining/shared';
import { setupDialogSheetGesture } from './composables/dialogSheetGesture';
import App from './App.vue';
import router from './router';
import './style.css';

// 逻辑包(@dining/api、@dining/customer-core)不依赖任何 UI 库,只调用 @dining/shared 的 ui;
// 「具体怎么提示」由各端入口在这里注入。行为与拆分前完全一致(都是 TDesign 的全局轻提示)。
// 小程序端会用 setUiNotifier({ success: wx.showToast, ... }) 换成自己的实现。
setUiNotifier({
  success: (message: string) => MessagePlugin.success(message),
  warning: (message: string) => MessagePlugin.warning(message),
  error: (message: string) => MessagePlugin.error(message)
});

const app = createApp(App);
app.use(router);

// 全局金额格式化(唯一实现在 @dining/shared 的 money.ts,页面本地替代函数共用同一份)
app.config.globalProperties.$money = formatMoney;

// 本地注册图标组件（避免 t-icon 依赖腾讯 CDN，内网可正常显示）。
//
// 这 28 个图标只有管理端用得到 —— 侧栏 / 底栏 / ActionSheet 都是按字符串
// `<component :is="'XxxIcon'" />` 渲染。顾客扫码点菜(order / printTicket)那两页
// 一个都不渲染,却要为它们多下一次图标表。所以按路由的 meta.customer 决定是否加载:
//   · 管理端:先取图标表再挂载,首屏侧栏图标不会闪一下;
//   · 顾客端:直接挂载,图标表连请求都不会发。
// resolve() 是同步的,不需要等初始导航完成就能读到 meta。
const entryRoute = router.resolve(window.location.pathname + window.location.search);

void (async () => {
  if (!entryRoute.meta.customer) {
    const { iconRegistry } = await import('./icons');
    Object.entries(iconRegistry).forEach(([name, comp]) => app.component(name, comp));
  }
  app.mount('#app');
})();

// 移动端把 t-dialog 改造成贴底弹层后,顶部横杠支持点击/下拉关闭(见 style.css 移动端断点)
setupDialogSheetGesture();
