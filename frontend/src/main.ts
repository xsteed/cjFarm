import { createApp } from 'vue';
// 组件 JS 由 unplugin-vue-components 按需自动导入(见 vite.config.ts);这里只保留全量样式
// 与函数式组件(MessagePlugin/DialogPlugin 各自显式 import)所需的样式兜底。
import 'tdesign-vue-next/es/style/index.css';
import { iconRegistry } from './icons';
import { formatMoney } from './utils/money';
import App from './App.vue';
import router from './router';
import './style.css';

const app = createApp(App);
app.use(router);

// 本地注册图标组件（避免 t-icon 依赖腾讯 CDN，内网可正常显示）
Object.entries(iconRegistry).forEach(([name, comp]) => app.component(name, comp));

// 全局金额格式化(唯一实现在 utils/money.ts,页面本地替代函数共用同一份)
app.config.globalProperties.$money = formatMoney;

app.mount('#app');
