// 小程序入口：创建跨页共享状态 + 装配接口层。
//
// `import './api/client'` 的副作用是在应用启动前把 axios 实例换到 wx.request、
// 指向后端域名并注入提示/401 策略（见该文件说明）。放在 app.ts 里可以保证
// 任何页面发请求之前一定已经装配完成。
import './api/client';
import { restoreMember } from './store/member';
import type { GlobalData } from './types';

App<GlobalData>({
  // 跨页共享状态直接挂在 App 实例上（类型与原因见 src/types.ts）。
  session: {
    tableCode: '',
    tableId: 0,
    table: null,
    shopName: '扫码点餐',
    config: null,
    currentOrder: null,
    personCount: 1
  },
  cart: [],
  member: {
    customerId: 0,
    info: null,
    pending: false
  },

  onLaunch() {
    // 恢复会员登录态（有本地令牌才恢复；没有就保持未登录，点餐不受影响）。
    restoreMember();
  },

  onError(err: string) {
    // 未捕获异常的兜底日志：小程序线上排障只能靠 vConsole 或日志上报，
    // 这里至少把错误打出来，避免「白屏且无任何线索」。
    console.error('[app:onError]', err);
  }
});
