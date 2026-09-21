import { createApp } from 'vue'
import TDesign from 'tdesign-vue-next'
import 'tdesign-vue-next/es/style/index.css'
import {
  DashboardIcon, GridViewIcon, ViewListIcon, RiceIcon,
  OrderAdjustmentColumnIcon, PrintIcon, ChatIcon, SettingIcon,
  ChartBarIcon, QrcodeIcon, UserCircleIcon, AddIcon, RefreshIcon,
  Table2Icon, Edit2Icon, SearchIcon, MoneyIcon, ShopIcon, CheckCircleIcon,
  FileIcon, UserIcon, LockOnIcon, EllipsisIcon, HistoryIcon
} from 'tdesign-icons-vue-next'
import App from './App.vue'
import router from './router'
import './style.css'

const app = createApp(App)
app.use(TDesign)
app.use(router)

// 本地注册图标组件（避免 t-icon 依赖腾讯 CDN，内网可正常显示）
const icons = {
  DashboardIcon, GridViewIcon, ViewListIcon, RiceIcon,
  OrderAdjustmentColumnIcon, PrintIcon, ChatIcon, SettingIcon,
  ChartBarIcon, QrcodeIcon, UserCircleIcon, AddIcon, RefreshIcon,
  Table2Icon, Edit2Icon, SearchIcon, MoneyIcon, ShopIcon, CheckCircleIcon,
  FileIcon, UserIcon, LockOnIcon, EllipsisIcon, HistoryIcon
}
Object.entries(icons).forEach(([name, comp]) => app.component(name, comp))

// 全局金额格式化
app.config.globalProperties.$money = (v) => {
  const n = Number(v || 0)
  return n.toFixed(2)
}

app.mount('#app')
