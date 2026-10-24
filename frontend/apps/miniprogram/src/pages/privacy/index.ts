// 隐私授权页。
//
// 这不是一个「顾客会主动逛到」的页面：只有当某个**隐私接口**被调用、而顾客还没
// 同意《用户隐私保护指引》时，微信才会回调 app.ts 注册的 wx.onNeedPrivacyAuthorization
// 把顾客导航到这里（见 store/privacy.ts 与 app.ts）。
//
// 做成独立页面而不是往 8 个页面里各塞一个弹窗组件：后者只要漏掉一个页面，
// 那个页面上的隐私按钮就是「点了没反应」，而且每次新增页面都得记得补。
// 一个页面 + 一个全局监听，覆盖了全部调用点。
//
// ⚠️ 只在 match 以下前提下有效：微信公众平台「设置 → 服务内容声明 → 用户隐私保护指引」
//    里已经提交并通过了《 Он 指引》。指引没配，这个页面根本不会被唤起，
//    对应的隐私接口会直接 fail —— 那是平台侧动作，代码侧无从替代。
import { AGREEMENT_ROUTE, privacyCollects, privacyRights, type PrivacyItem } from '../../shared/privacy-text';
import { sessionState } from '../../store/session';
import { reportPrivacyExposure, takeAllPrivacyResolvers } from '../../store/privacy';

Page({
  data: {
    shopName: '本店',
    /** 我们采集哪些信息、用来做什么。 */
    collects: [] as PrivacyItem[],
    /** 顾客对自己信息有哪些支配权。 */
    rights: [] as PrivacyItem[]
  },

  /** 已经触发过一次离开：顾客可能双击同意，也可能抢先用左上角返回 */
  leaving: false,

  onLoad() {
    // 曝光告知：官方建议在把弹窗展示出来时调一次，后台才看得到「弹了但没同意」的漏斗。
    // 它不消耗 resolver，放这里最合适（早于顾客做任何选择）。
    reportPrivacyExposure();

    const name = String(sessionState().config?.shop_name || '').trim();
    this.setData({
      shopName: name || '本店',
      collects: privacyCollects(),
      rights: privacyRights()
    });
  },

  onUnload() {
    // 顾客用左上角返回离开 = 没有做出选择，按「不同意」收尾（全部挂起的接口一起）。
    // 这一步不能省：resolver 不被兑现就永远挂在内存里，此后每次点隐私按钮都不会再
    // 触发回调，顾客会发现「绑定手机号」彻底失灵，而我们连重 exhibited 的机会都没有。
    for (const resolve of takeAllPrivacyResolvers()) resolve({ event: 'disagree' });
  },

  /**
   * 「同意」按钮：open-type="agreePrivacyAuthorization" 由基础库接管，
   * 它把这个 button 的 id 暴露出来，我们拿它去兑现 resolver。
   *
   * buttonId 必须与 WXML 里那个按钮的 id **完全一致**：基础库会校验「这个按钮真的
   * 被点过」，随手写一个不存在的 id 会让这次授权静默失败。
   */
  onAgree(): void {
    // 一次选择兑现全部：平台侧会释放所有被挂起的隐私接口，只兑现一个会让其余的挂起
    for (const resolve of takeAllPrivacyResolvers()) resolve({ event: 'agree', buttonId: 'agree-btn' });
    this.leave();
  },

  onDisagree(): void {
    for (const resolve of takeAllPrivacyResolvers()) resolve({ event: 'disagree' });
    this.leave();
  },

  /** 同意/不同意以外的第三种出口：先看完整的协议与隐私政策，再回来决定。 */
  onOpenAgreement(): void {
    wx.navigateTo({ url: `/${AGREEMENT_ROUTE}?type=privacy` });
  },

  leave(): void {
    if (this.leaving) return;
    this.leaving = true;
    // 正常路径：从某个页面被 navigateTo 过来，栈里一定有来处，直接返回。
    // 兜底 reLaunch 到首页：万一这个页面成了栈底（极端的直接打开场景），
    // navigateBack 无处可退，顾客会被困在这里。
    if (getCurrentPages().length > 1) wx.navigateBack();
    else wx.reLaunch({ url: '/pages/home/index' });
  }
});
