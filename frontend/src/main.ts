import { createApp } from 'vue';
// 组件 JS 由 unplugin-vue-components 按需自动导入(见 vite.config.ts);这里只保留全量样式
// 与函数式组件(MessagePlugin/DialogPlugin 各自显式 import)所需的样式兜底。
import 'tdesign-vue-next/es/style/index.css';
import { MessagePlugin } from 'tdesign-vue-next';
import { formatMoney, setUiNotifier } from '@dining/shared';
import { iconRegistry } from './icons';
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

// 本地注册图标组件（避免 t-icon 依赖腾讯 CDN，内网可正常显示）
Object.entries(iconRegistry).forEach(([name, comp]) => app.component(name, comp));

// 全局金额格式化(唯一实现在 @dining/shared 的 money.ts,页面本地替代函数共用同一份)
app.config.globalProperties.$money = formatMoney;

app.mount('#app');
