// 底部主导航（点餐 / 订单）。
//
// 对应 Web 端 apps/customer/src/order/BottomTabBar.vue，有一处刻意的差异：
// H5 是在同一个页面里切视图，且「已有订单才显示」；小程序端点餐与订单是两个独立页面，
// 所以这里常驻显示 —— 未下单时也能切到订单页（那边渲染空态）。
//
// 没有用原生 tabBar（app.json 的 tabBar 字段）：它要求列表页全局常驻、自动切页，
// 而这里「支付页需要专注、订单页可能是空态」都靠页面自己决定，做成组件更直接。
//
// 切页规则见 utils/nav.ts；样式自包含（自定义组件默认样式隔离，token 在 index.wxss 里自带一份）。
import { goToMenu, goToOrder, goToProfile } from '../../utils/nav';

type TabKey = 'menu' | 'order' | 'profile';

Component({
  properties: {
    /** 当前页所属的 tab：menu（点餐）| order（订单）| profile（我的） */
    active: { type: String, value: 'menu' },
    /** 订单 tab 的角标（已下单份数），0 表示不显示 */
    orderedCount: { type: Number, value: 0 },
    /**
     * 是否显示「我的」（会员）tab。跟随后端开关：系统配置里配了小程序
     * AppID/AppSecret 才显示；关闭时回到两档导航，会员入口整体消失。
     */
    showProfile: { type: Boolean, value: true }
  },

  methods: {
    onTapMenu(): void {
      this.switchTab('menu');
    },

    onTapOrder(): void {
      this.switchTab('order');
    },

    onTapProfile(): void {
      this.switchTab('profile');
    },

    switchTab(tab: TabKey): void {
      // 点当前 tab 不做事，否则会往页面栈里再压一份同样的页面
      if (tab === this.data.active) return;
      if (tab === 'menu') goToMenu();
      else if (tab === 'order') goToOrder();
      else goToProfile();
    }
  }
});
