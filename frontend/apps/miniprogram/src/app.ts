// 小程序入口：创建跨页共享状态 + 装配接口层。
//
// `import './api/client'` 的副作用是在应用启动前把 axios 实例换到 wx.request、
// 指向后端域名并注入提示/401 策略（见该文件说明）。放在 app.ts 里可以保证
// 任何页面发请求之前一定已经装配完成。
import './api/client';
import { reportVisit } from '@dining/api';
import { restoreMember } from './store/member';
import { mpVisitorId } from './utils/visitor';
import { resolveLaunchTableCode } from './utils/scene';
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
    personCount: 1,
    orderRemark: ''
  },
  cart: [],
  member: {
    customerId: 0,
    info: null,
    pending: false
  },

  onLaunch(options) {
    // 恢复会员登录态（有本地令牌才恢复；没有就保持未登录，点餐不受影响）。
    restoreMember();

    // 站点访问统计：每次**冷启动**记一次 PV（口径对齐微信官方「启动次数」）。
    // 不在 onShow 埋点 —— 前后台切换会重复计数，把数字灌虚。
    //
    // entry 带本次进入的桌台码：扫桌台小程序码 / 扫普通二维码跳小程序都会带上它，
    // 后台「扫码入口 Top」才知道是哪张桌引来的客人。解析逻辑与页面 onLoad 共用
    // resolveTableCode（scene / query 两种形态都覆盖）；**没带码的直接打开**解析
    // 结果为空串，属正常情况 —— 那次访问确实不是从桌台码进来的。
    // 已登录会员的请求会由 api/client.ts 的拦截器自动带上顾客令牌，
    // 后端据此把访客从匿名升级为会员身份（m:<customerId>），会员 UV 才精确。
    // 埋点失败静默：统计不是顾客的诉求，绝不能挡住点餐。
    try {
      void reportVisit({
        visitorId: mpVisitorId(),
        channel: 'miniprogram',
        entry: resolveLaunchTableCode(options)
      }).catch(() => {
        /* 静默失败 */
      });
    } catch {
      /* 同步异常同样静默 */
    }
  },

  onError(err: string) {
    // 未捕获异常的兜底日志：小程序线上排障只能靠 vConsole 或日志上报，
    // 这里至少把错误打出来，避免「白屏且无任何线索」。
    console.error('[app:onError]', err);
  }
});
