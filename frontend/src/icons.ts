// ⚠️ import 列表与下方 iconRegistry 必须逐项一致:漏一项会让 app.component(name, undefined)
//    静默失败,菜单图标直接消失。加图标时两处一起改。
import {
  DashboardIcon,
  GridViewIcon,
  ViewListIcon,
  RiceIcon,
  OrderAdjustmentColumnIcon,
  PrintIcon,
  SettingIcon,
  ChartBarIcon,
  ChevronDownIcon,
  QrcodeIcon,
  UserCircleIcon,
  AddIcon,
  RefreshIcon,
  Edit2Icon,
  SearchIcon,
  MoneyIcon,
  FileIcon,
  UserIcon,
  LockOnIcon,
  MenuFoldIcon,
  MenuUnfoldIcon,
  EllipsisIcon,
  HistoryIcon,
  FilterIcon,
  MemberIcon
} from 'tdesign-icons-vue-next';

// 单一图标清单：main.ts 全局注册与 router 的 meta.icon 都从这里取。
// 键名即组件名，与 router 侧栏菜单 / AdminLayout tabbar 里的字符串一一对应。
export const iconRegistry = {
  DashboardIcon,
  GridViewIcon,
  ViewListIcon,
  RiceIcon,
  OrderAdjustmentColumnIcon,
  PrintIcon,
  SettingIcon,
  ChartBarIcon,
  ChevronDownIcon,
  QrcodeIcon,
  UserCircleIcon,
  AddIcon,
  RefreshIcon,
  Edit2Icon,
  SearchIcon,
  MoneyIcon,
  FileIcon,
  UserIcon,
  LockOnIcon,
  MenuFoldIcon,
  MenuUnfoldIcon,
  EllipsisIcon,
  HistoryIcon,
  FilterIcon,
  MemberIcon
};
