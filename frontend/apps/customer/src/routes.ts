import type { RouteRecordRaw } from 'vue-router';

// ============================================================================
// 顾客端路由(公开访问,无需登录)
//
// 这两条是「扫码点餐」的入口,二维码内容由后端配置的 H5 地址拼出来
// (见 apps/admin/src/utils/tableQr.ts):
//   /order/<桌台点餐码>:点餐码为 8 位稳定码,兼容旧的数字桌台 ID
//   /printTicket?orderNo=Dxxx:订单小票打印页
//
// ⚠️ 路径一旦变更,已经印出去的桌牌二维码就会失效,不要随意改。
// ============================================================================
export const customerRoutes: RouteRecordRaw[] = [
  {
    path: '/order/:tableId',
    name: 'OrderPage',
    component: () => import('./OrderPage.vue'),
    meta: { title: '扫码点餐', customer: true }
  },
  {
    path: '/printTicket',
    name: 'PrintTicket',
    component: () => import('./PrintTicket.vue'),
    meta: { title: '打印票据', customer: true }
  }
];
