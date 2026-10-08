// 底部结算栏：已选份数 + 合计金额 + 去结算。
// 对应 Web 端 apps/customer/src/order/CartBar.vue。
Component({
  properties: {
    /** 已选总份数 */
    count: { type: Number, value: 0 },
    /** 已格式化的合计金额文案（如 "88.00"），避免组件里再引一次金额格式化 */
    totalText: { type: String, value: '0.00' },
    /** 按钮文案：首次下单是「去结算」，加菜场景由页面传「去加菜」之类 */
    actionText: { type: String, value: '去结算' },
    /** 底部还有主导航（tab-bar）时置 true：结算栏整体上移，避免把导航压住 */
    lifted: { type: Boolean, value: false }
  },

  methods: {
    onTap(): void {
      if (this.data.count <= 0) return;
      this.triggerEvent('open');
    }
  }
});
