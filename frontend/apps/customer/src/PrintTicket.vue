<template>
  <div class="ticket-page">
    <div class="no-print toolbar">
      <t-input
        v-model="orderNo"
        placeholder="输入订单号"
        style="width: 260px"
      />
      <t-button
        theme="primary"
        @click="load"
      >
        查询
      </t-button>
      <t-button @click="print"> 打印 </t-button>
    </div>

    <div
      v-if="loadError"
      class="ticket-error"
    >
      {{ loadError }}
    </div>

    <div
      v-if="order"
      class="ticket"
    >
      <div class="ticket-shop">
        {{ shopName }}
      </div>
      <div class="ticket-sub">顾客小票</div>
      <div class="ticket-line">--------------------------------</div>
      <div class="ticket-row">
        <span>订单号</span><span>{{ order.orderNo }}</span>
      </div>
      <div class="ticket-row">
        <span>桌号</span><span>{{ order.tableNo }}号桌 · {{ order.tableName }}</span>
      </div>
      <div class="ticket-row">
        <span>人数</span><span>{{ order.personCount }} 人</span>
      </div>
      <div class="ticket-row">
        <span>下单时间</span><span>{{ order.createTime }}</span>
      </div>
      <div class="ticket-line">--------------------------------</div>
      <table class="ticket-table">
        <thead>
          <tr>
            <th>菜品</th>
            <th>单价</th>
            <th>数量</th>
            <th>小计</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="(it, i) in order.items"
            :key="i"
          >
            <td>
              <b
                v-if="it.isGift"
                class="gift"
                >赠 </b
              >{{ it.dishName }}
              <div class="spec">
                {{ it.specName }}
              </div>
            </td>
            <td>{{ it.isGift ? '赠' : $money(it.price) }}</td>
            <td>{{ it.quantity }}</td>
            <td>{{ it.isGift ? '赠' : $money(it.amount) }}</td>
          </tr>
        </tbody>
      </table>
      <div class="ticket-line">--------------------------------</div>
      <div class="ticket-row">
        <span>菜品金额</span><span>¥{{ $money(order.dishAmount) }}</span>
      </div>
      <div class="ticket-row">
        <span>茶位费（{{ order.personCount }}人）</span><span>¥{{ $money(order.seatFee) }}</span>
      </div>
      <div
        v-if="(order.discountAmount ?? 0) > 0"
        class="ticket-row"
      >
        <span>优惠</span><span>-¥{{ $money(order.discountAmount) }}</span>
      </div>
      <div class="ticket-row total">
        <span>应收金额</span><span>¥{{ $money(order.totalAmount) }}</span>
      </div>
      <div
        v-if="order.orderRemark"
        class="ticket-remark"
      >
        备注：{{ order.orderRemark }}
      </div>

      <!-- 结算信息:与热敏机打出来的那张小票保持同一套内容。
           顾客扫码看到的就是这张票,若与纸质小票不一致会直接引发争议。 -->
      <div
        v-if="order.settleType === 'free'"
        class="ticket-row highlight"
      >
        <span>【免单】实收</span><span>¥0.00</span>
      </div>
      <div
        v-else-if="order.creditStatus === 1"
        class="ticket-row highlight"
      >
        <span>【挂账】待收</span><span>¥{{ $money(order.creditAmount) }}</span>
      </div>
      <div
        v-else-if="order.creditStatus === 2"
        class="ticket-row highlight"
      >
        <span>【挂账已结】</span><span>¥{{ $money(order.creditAmount) }}</span>
      </div>
      <div
        v-if="order.payType"
        class="ticket-row"
      >
        <span>支付方式</span><span>{{ order.payType }}</span>
      </div>
      <div
        v-if="order.settleTime"
        class="ticket-row"
      >
        <span>结账时间</span><span>{{ order.settleTime }}</span>
      </div>
      <div
        v-if="order.creditSettleTime"
        class="ticket-row"
      >
        <span>核销时间</span><span>{{ order.creditSettleTime }}</span>
      </div>
      <div
        v-if="paidRow"
        class="ticket-row"
      >
        <span>实收</span><span>¥{{ $money(order.paidAmount) }}</span>
      </div>
      <div
        v-if="(order.refundAmount ?? 0) > 0"
        class="ticket-row"
      >
        <span>已退款</span><span>-¥{{ $money(order.refundAmount) }}</span>
      </div>
      <div
        v-if="order.settleRemark"
        class="ticket-remark"
      >
        {{ order.settleType === 'free' ? '免单原因' : '挂账人/备注' }}：{{ order.settleRemark }}
      </div>
      <div class="ticket-line">--------------------------------</div>
      <div class="ticket-foot">谢谢惠顾，欢迎再次光临！</div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, onMounted } from 'vue';
import { useRoute } from 'vue-router';
import { getOrderByNo, getPublicConfig } from '@dining/api';
import { money } from '@dining/shared';
import type { OrderDetail } from '@dining/types';
import { setCustomerTitle } from './title';

// 模板里 $money 与全局属性共用 @dining/shared 里 money 的实现(TS 模板消费不到全局属性,须显式导入)。
const $money = money;

const route = useRoute();
const orderNo = ref<string>(String(route.query.orderNo || ''));
const order = ref<OrderDetail | null>(null);
const shopName = ref<string>('');

const loadError = ref('');

// 实收只在与应收不一致时单列(与纸质小票同一规则):正常收款下两者相等,
// 再印一遍只是啰嗦;发生部分退款时这一行才是顾客真正支付的金额。
const paidRow = computed(() => {
  const paid = Number(order.value?.paidAmount ?? 0);
  const total = Number(order.value?.totalAmount ?? 0);
  return paid > 0 && paid !== total;
});

async function load(): Promise<void> {
  if (!orderNo.value) return;
  loadError.value = '';
  try {
    order.value = await getOrderByNo(orderNo.value);
  } catch {
    // 失败原因由拦截器统一 toast,页面给出可操作的提示而不是空白
    loadError.value = '订单查询失败，请核对订单号后重试';
  }
}

function print(): void {
  window.print();
}

onMounted(async () => {
  try {
    const cfg = await getPublicConfig();
    shopName.value = cfg.shop_name || '';
    setCustomerTitle(cfg.shop_name);
  } catch {
    /* 店名拉取失败不阻塞票据查询,拦截器已提示 */
  }
  if (orderNo.value) load();
});
</script>

<style scoped>
.ticket-page {
  max-width: 420px;
  margin: 0 auto;
  padding: 20px;
}

.ticket-error {
  background: #fff;
  padding: 24px;
  text-align: center;
  color: #d54941;
}

.toolbar {
  display: flex;
  gap: 8px;
  margin-bottom: 20px;
}

.ticket {
  background: #fff;
  padding: 24px;
  font-family: 'Courier New', monospace;
  font-size: 13px;
}

.ticket-shop {
  text-align: center;
  font-size: 18px;
  font-weight: bold;
}

.ticket-sub {
  text-align: center;
  font-size: 12px;
  color: #666;
  margin-top: 4px;
}

.ticket-line {
  color: #999;
  margin: 10px 0;
}

.ticket-row {
  display: flex;
  justify-content: space-between;
  margin: 4px 0;
}

.ticket-row.total {
  font-weight: bold;
  font-size: 15px;
  margin-top: 8px;
}

.ticket-table {
  width: 100%;
  border-collapse: collapse;
}

.ticket-table th,
.ticket-table td {
  text-align: left;
  padding: 4px 0;
}

.ticket-table .spec {
  font-size: 11px;
  color: #999;
}

.ticket-table .gift {
  color: #e8850c;
  font-weight: 700;
}

/* 免单 / 挂账标注:纸质小票上这几行是加粗的,这里同样强调,避免顾客漏看。 */
.ticket-row.highlight {
  margin-top: 8px;
  font-weight: bold;
}

.ticket-remark {
  margin-top: 8px;
  color: #666;
}

.ticket-foot {
  text-align: center;
  margin-top: 16px;
  color: #666;
}

@media print {
  .no-print {
    display: none;
  }
}
</style>
