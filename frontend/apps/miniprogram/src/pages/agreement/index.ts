// 用户服务协议 / 隐私政策。
//
// 存在的理由不是「好看」，而是三件硬要求：
//   1) 《个人信息保护法》第十七条：处理个人信息前必须以显著方式、清晰易懂的语言
//      告知处理者名称、处理目的、方式、种类、保存期限等；
//   2) 微信小程序提审会检查敏感权限背后有没有可查阅的协议与隐私政策入口；
//   3) 隐私授权页（pages/privacy）需要一个「看完整版」的落点。
//
// 正文来自 shared/privacy-text.ts —— 授权页的摘要与这里的表述必须同源，分开放必漂移。
// tab 由 query 的 type 决定（type=privacy 直接进隐私政策），约定同样收在那里。
import {
  DOC_UPDATE_AT,
  privacySections,
  renderContact,
  termsSections,
  type DocSection
} from '../../shared/privacy-text';
import { sessionState } from '../../store/session';

type DocKey = 'terms' | 'privacy';

Page({
  data: {
    active: 'terms' as DocKey,
    shopName: '本店',
    updateAt: DOC_UPDATE_AT,
    sections: [] as DocSection[]
  },

  onLoad(options: Record<string, string | undefined>) {
    // 「我的」页的两个入口各自带 type：没有或非法值一律落到服务协议（第一个 tab）。
    const type = String(options?.type || '').trim();
    this.setData({
      active: type === 'privacy' ? 'privacy' : 'terms',
      shopName: String(sessionState().config?.shop_name || '').trim() || '本店'
    });
    this.render();
  },

  /** 切 tab：data-type 只有两档，非法值退到服务协议（与 onLoad 同一口径）。 */
  onSwitch(e: WechatMiniprogram.TouchEvent): void {
    const next = String(e.currentTarget.dataset.type || '') === 'privacy' ? 'privacy' : 'terms';
    if (next === this.data.active) return;
    this.setData({ active: next });
    this.render();
    wx.pageScrollTo({ scrollTop: 0, duration: 0 });
  },

  render(): void {
    const name = this.data.shopName;
    const raw = this.data.active === 'privacy' ? privacySections(name) : termsSections(name);
    const contact = renderContact(name);
    this.setData({
      sections: raw.map(s => ({
        key: s.key,
        title: s.title,
        // 联系方式在正文里是占位符：门店渠道由配置驱动，写死在条款里必然过期
        lines: s.lines.map(l => (l === 'CONTACT_PLACEHOLDER' ? contact : l))
      }))
    });
  }
});
