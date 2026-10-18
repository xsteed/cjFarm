// 小程序入口：创建跨页共享状态 + 装配接口层。
//
// `import './api/client'` 的副作用是在应用启动前完成接口层装配
// （baseURL / 提示实现，见该文件说明）。放在 app.ts 里可以保证
// 任何页面发请求之前一定已经装配完成。
import './api/client';
import { installMemberUnauthorizedHandler, restoreMember } from './store/member';
import { reportVisit } from './api/client';
import { mpVisitorId } from './utils/visitor';
import { resolveLaunchTableCode } from './utils/scene';
import { PRIVACY_ROUTE, setPrivacyResolver } from './store/privacy';
import type { PrivacyResolver } from './store/privacy';
import { isDevelopEnv, isProdOriginPending } from './config';
import type { GlobalData } from './types';

// 401 处置策略：会员专属接口令牌过期时静默重登（见 store/member.ts）。
// 必须在任何请求发出之前注册 —— 放在模块加载期，早于所有页面生命周期。
installMemberUnauthorizedHandler();

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
    personChosen: false,
    orderRemark: ''
  },
  cart: [],
  member: {
    customerId: 0,
    info: null,
    pending: false
  },

  onLaunch(options) {
    // 生产域名还没换成真实备案域名就先打了体验版/正式版：请求会 100% 失败，
    // 而控制台只会显示一行行 request:fail —— 这里补一句能直接定位的中文线索。
    // 开发版不受影响（走本机地址），所以这条告警不会打扰日常联调。
    if (!isDevelopEnv() && isProdOriginPending()) {
      console.error(
        '[config] PROD_API_ORIGIN 仍是占位符，所有后端请求都会失败。' +
          '请替换为已备案的 HTTPS 域名，详见 docs/miniprogram.md 3.1'
      );
    }

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

    checkUpdate();
  },

  onError(err: string) {
    // 未捕获异常的兜底日志：小程序线上排障只能靠 vConsole 或日志上报，
    // 这里至少把错误打出来，避免「白屏且无任何线索」。
    console.error('[app:onError]', err);
  }
});

/**
 * 版本更新：小程序的顾客是**不会自己更新**的 —— 他下次扫码进来用的是本地缓存的
 * 旧包。不加这一段，修完线上 bug 后仍会有一批顾客停留在旧版本上好几天
 * （微信对新版本的收敛本身也有延迟），同一个问题会被反复投诉。
 *
 * 只在检测到新版本时才打扰顾客：下载完成后弹一次确认框，顾客点「立即重启」才
 * 生效；正忙着结账时可以选「稍后」，下次冷启动会再问一次。
 *
 * 写成模块函数而不是 App 实例方法：App.Options<T> 只接受 T 的字段 + 生命周期，
 * 挂自定义方法过不了类型检查，而这里本来就不需要跨页调用。
 */
function checkUpdate(): void {
  // 基础库 1.9.90+。真机上一定有；冒烟脚本的极简 wx 桩没有这个 API。
  if (typeof wx.getUpdateManager !== 'function') return;
  const manager = wx.getUpdateManager();
  manager.onUpdateReady(() => {
    wx.showModal({
      title: '更新提示',
      content: '小程序已升级到新版本，重启后即可使用最新功能。',
      confirmText: '立即重启',
      cancelText: '稍后',
      success: r => {
        if (r.confirm) manager.applyUpdate();
      }
    });
  });
  manager.onUpdateFailed(() => {
    // 下载失败不需要顾客做任何事：下次冷启动会自然重试。
    console.warn('[app:update] 新版本下载失败，下次启动时重试');
  });
}

// ---------------------------------------------------------------------------
// 隐私授权监听（2023-10-17 起强制）
//
// 顾客点 pages/profile 的 `<button open-type="getPhoneNumber">` 而尚未同意
// 《用户隐私保护指引》时，基础库会挂起这次调用并回调这里。我们弹出 pages/privacy，
// 由那页把顾客的「同意 / 不同意」兑现回 resolve（见 store/privacy.ts）。
//
// 放在模块加载期而不是 onLaunch：注册动作越早越好，晚于第一个隐私接口调用就漏了。
// ---------------------------------------------------------------------------
if (typeof wx.onNeedPrivacyAuthorization === 'function') {
  wx.onNeedPrivacyAuthorization(res => {
    // 类型库把这个 listener 的首个参数标成了 `GeneralCallbackResult`，但**运行时
    // 它是 resolve 函数**（官方 JSDoc 也是这么描述的 —— res({ event: 'agree' })）。
    // 这里是唯一一处适配：把它收窄成精确类型后交给 store/privacy.ts，
    // 上层就不必到处跟这个不准确的签名纠缠。
    setPrivacyResolver(res as unknown as PrivacyResolver);
    // 已经停在授权页了（顾客在同一页又点了别的隐私按钮）：那页会把这次也一并处理。
    const top = getCurrentPages().slice(-1)[0] as { route?: string } | undefined;
    if (top?.route === PRIVACY_ROUTE) return;
    wx.navigateTo({ url: `/${PRIVACY_ROUTE}` });
  });
}
