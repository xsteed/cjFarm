<template>
  <div class="ticket-page">
    <div class="no-print toolbar">
      <t-input v-model="orderNo" placeholder="输入订单号" style="width: 260px" />
      <t-button theme="primary" @click="load">查询</t-button>
      <t-button @click="print">打印</t-button>
    </div>

    <div v-if="order" class="ticket">
      <div class="ticket-shop">{{ shopName }}</div>
      <div class="ticket-sub">食客小票</div>
      <div class="ticket-line">--------------------------------</div>
      <div class="ticket-row"><span>订单号</span><span>{{ order.orderNo }}</span></div>
      <div class="ticket-row"><span>桌号</span><span>{{ order.tableNo }}号桌 · {{ order.tableName }}</span></div>
      <div class="ticket-row"><span>人数</span><span>{{ order.personCount }} 人</span></div>
      <div class="ticket-row"><span>下单时间</span><span>{{ order.createTime }}</span></div>
      <div class="ticket-line">--------------------------------</div>
      <table class="ticket-table">
        <thead>
          <tr><th>菜品</th><th>单价</th><th>数量</th><th>小计</th></tr>
        </thead>
        <tbody>
          <tr v-for="(it, i) in order.items" :key="i">
            <td>{{ it.dishName }}<div class="spec">{{ it.specName }}</div></td>
            <td>{{ $money(it.price) }}</td>
            <td>{{ it.quantity }}</td>
            <td>{{ $money(it.amount) }}</td>
          </tr>
        </tbody>
      </table>
      <div class="ticket-line">--------------------------------</div>
      <div class="ticket-row"><span>菜品金额</span><span>¥{{ $money(order.dishAmount) }}</span></div>
      <div class="ticket-row"><span>餐位费</span><span>¥{{ $money(order.seatFee) }}</span></div>
      <div v-if="order.discountAmount > 0" class="ticket-row"><span>优惠</span><span>-¥{{ $money(order.discountAmount) }}</span></div>
      <div class="ticket-row total"><span>应收金额</span><span>¥{{ $money(order.totalAmount) }}</span></div>
      <div v-if="order.orderRemark" class="ticket-remark">备注：{{ order.orderRemark }}</div>
      <div class="ticket-line">--------------------------------</div>
      <div class="ticket-foot">谢谢惠顾，欢迎再次光临！</div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { getOrderByNo, getPublicConfig } from '../api'

const route = useRoute()
const orderNo = ref(route.query.orderNo || '')
const order = ref(null)
const shopName = ref('')

async function load() {
  if (!orderNo.value) return
  order.value = await getOrderByNo(orderNo.value)
}

function print() {
  window.print()
}

onMounted(async () => {
  const cfg = await getPublicConfig()
  shopName.value = cfg.shop_name
  if (orderNo.value) load()
})
</script>

<style scoped>
.ticket-page {
  max-width: 420px;
  margin: 0 auto;
  padding: 20px;
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
