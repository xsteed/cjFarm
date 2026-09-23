<template>
  <div class="pay-wrap">
    <div class="pay-label">应付金额</div>
    <div class="pay-amt">¥{{ Number(currentOrder.totalAmount).toFixed(2) }}</div>
    <div class="pay-tabs">
      <div
        class="pay-tab"
        :class="{ on: payTab === 'wx' }"
        @click="emit('switch-tab', 'wx')"
      >
        微信支付
      </div>
      <div
        class="pay-tab"
        :class="{ on: payTab === 'ali' }"
        @click="emit('switch-tab', 'ali')"
      >
        支付宝
      </div>
    </div>

    <!-- 在线支付(渠道已启用) -->
    <template v-if="onlineEnabled">
      <div
        v-if="!onlineQr"
        class="online-pay-btn"
        :class="{ disabled: paying }"
        @click="emit('online-pay')"
      >
        {{ paying ? '正在生成支付码…' : `在线支付 ¥${Number(currentOrder.totalAmount).toFixed(2)}` }}
      </div>
      <div
        v-else
        class="pay-block"
      >
        <div class="pay-qr">
          <img
            :src="onlineQr"
            alt="支付二维码"
          />
        </div>
        <div class="pay-tip">请用{{ payTab === 'wx' ? '微信' : '支付宝' }}扫码完成支付</div>
        <div class="pay-tip sub">支付成功后本页面自动更新，请勿关闭</div>
      </div>
      <div
        v-if="currentPayQr"
        class="pay-divider"
      >
        —— 或使用线下收款码 ——
      </div>
    </template>

    <!-- 线下码牌收款(始终保留) -->
    <div
      v-if="currentPayQr"
      class="pay-block"
    >
      <div class="pay-qr">
        <img
          :src="currentPayQr || ''"
          alt="收款码"
        />
      </div>
      <div class="pay-tip">长按识别二维码 或 用{{ payTab === 'wx' ? '微信' : '支付宝' }}扫码付款</div>
      <div class="pay-tip sub">支付完成后收银台确认即可，本页面会自动更新状态</div>
    </div>
    <div
      v-if="!onlineEnabled && !currentPayQr"
      class="qr-empty"
    >
      商家未配置{{ payTab === 'wx' ? '微信' : '支付宝' }}收款码<br />请联系收银员付款
    </div>

    <button
      class="back-btn"
      @click="emit('back')"
    >
      返回订单
    </button>
  </div>
</template>

<script setup lang="ts">
import type { OrderDetail } from '../../types/entities';

defineProps<{
  currentOrder: OrderDetail;
  payTab: 'wx' | 'ali';
  onlineQr: string;
  paying: boolean;
  onlineEnabled: boolean;
  currentPayQr: string | undefined;
}>();

const emit = defineEmits<{
  'switch-tab': [tab: 'wx' | 'ali'];
  'online-pay': [];
  back: [];
}>();
</script>

<style scoped>
/* ---------- 支付视图 ---------- */
.pay-wrap {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 26px 16px;
  overflow-y: auto;
}

.pay-label {
  font-size: 13px;
  color: var(--ink-3);
}

.pay-amt {
  font-size: 34px;
  font-weight: 800;
  color: var(--ink);
  margin-top: 8px;
}

.pay-tabs {
  display: flex;
  gap: 8px;
  margin: 16px 0;
}

.pay-tab {
  padding: 7px 22px;
  border-radius: var(--r-pill);
  font-size: 12.5px;
  background: #f5f5f5;
  color: var(--ink-3);
  font-weight: 500;
  cursor: pointer;
  transition: all 0.15s;
}

.pay-tab.on {
  background: linear-gradient(135deg, var(--brand), var(--brand-deep));
  color: #fff;
  font-weight: 600;
}

/* 收款码区块:二维码与说明文字纵向排列。
   二维码容器必须保持固定方形、且只放图片;提示文字放在容器外层。
   否则 .pay-qr 的 flex 会把文字当兄弟节点横向挤压,导致竖排溢出裁切。 */
.pay-block {
  width: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
}

.pay-qr {
  width: 200px;
  height: 200px;
  flex-shrink: 0;
  border-radius: var(--r-lg);
  border: 1px solid var(--line);
  background: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  box-shadow: var(--shadow-1);
  overflow: hidden;
}

.pay-qr img {
  width: 100%;
  height: 100%;
  object-fit: contain;
  display: block;
}

.qr-empty {
  text-align: center;
  color: var(--ink-3);
  font-size: 12px;
  line-height: 1.8;
  padding: 20px;
}

.pay-tip {
  font-size: 12px;
  color: var(--ink-3);
  margin-top: 14px;
  text-align: center;
  line-height: 1.6;
}

.pay-tip.sub {
  font-size: 11px;
  color: var(--ink-4);
  margin-top: 4px;
}

.online-pay-btn {
  width: 100%;
  height: 44px;
  border-radius: var(--r-pill);
  background: linear-gradient(135deg, var(--brand), var(--brand-deep));
  color: #fff;
  font-size: 14px;
  font-weight: 700;
  display: flex;
  align-items: center;
  justify-content: center;
  margin-top: 6px;
  box-shadow: var(--shadow-3);
  cursor: pointer;
}

.online-pay-btn.disabled {
  opacity: 0.6;
  cursor: default;
}

.pay-divider {
  margin-top: 18px;
  font-size: 11px;
  color: var(--ink-4);
}

.back-btn {
  margin-top: 20px;
  padding: 10px 36px;
  border-radius: var(--r-pill);
  border: 1.5px solid #ffd5c4;
  background: #fff;
  color: var(--brand-deep);
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
}
</style>
