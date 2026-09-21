<template>
  <div>
    <!-- KPI 卡片。
         注意:营业额/客单价/今日订单都来自 report/summary 接口(report:view)。
         收银员与员工只有 order:view,若照旧调用会稳定 403 —— 所以这里按权限降级:
         没有 report:view 就只留「桌台占用」,数据从 order/board 本地算,不发报告请求。 -->
    <div class="kpis" :class="{ solo: !canReport }">
      <template v-if="canReport">
        <div class="kpi acc">
          <div class="l">今日营业额</div>
          <div class="v">¥{{ $money(summary.todayAmount) }}</div>
          <div class="d">本月累计 ¥{{ $money(summary.monthAmount) }}</div>
        </div>
        <div class="kpi">
          <div class="l">今日订单</div>
          <div class="v">{{ summary.todayOrderCount || 0 }} <small>单</small></div>
          <div class="d up">已完成 {{ summary.todayFinishedCount || 0 }} 单</div>
        </div>
        <div class="kpi">
          <div class="l">客单价</div>
          <div class="v">¥{{ $money(summary.todayAvgAmount) }}</div>
          <div class="d">今日 {{ summary.todayGuestCount || 0 }} 人次到店</div>
        </div>
      </template>
      <div class="kpi">
        <div class="l">桌台占用</div>
        <div class="v">{{ busyCount }} <small>/ {{ board.length }}</small></div>
        <div class="d">
          <template v-if="canReport">空闲 {{ summary.freeTableCount || 0 }} 桌 · 进行中 {{ summary.activeOrderCount || 0 }} 单</template>
          <template v-else>空闲 {{ board.length - busyCount }} 桌</template>
        </div>
      </div>
    </div>

    <div class="panels">
      <!-- 桌台看板 -->
      <div class="panel">
        <div class="ph">
          <div class="t">桌台看板</div>
          <router-link class="l" to="/dining/tables">全部桌台 →</router-link>
        </div>
        <div class="table-grid">
          <div
            v-for="t in board"
            :key="t.tableId"
            class="tg"
            :class="[t.status === 1 ? 'busy' : 'free', { urging: t.order && t.order.pendingUrge }]"
          >
            <div class="n">{{ t.tableNo }}</div>
            <div class="s">{{ t.status === 1 ? '用餐中' : '空闲' }}</div>
            <span v-if="t.order && t.order.pendingUrge" class="urge-badge">催</span>
          </div>
          <div v-if="!board.length" class="empty">暂无桌台</div>
        </div>
      </div>

      <!-- 最新订单 -->
      <div class="panel">
        <div class="ph">
          <div class="t">进行中订单</div>
          <router-link class="l" to="/dining/orders">订单管理 →</router-link>
        </div>
        <div v-if="!activeOrders.length" class="empty">暂无进行中订单</div>
        <div v-for="o in activeOrders" :key="o.orderId" class="orderline">
          <div class="no">#{{ o.shortNo || o.orderNo.slice(-6) }}</div>
          <div class="meta">{{ o.tableName || o.tableNo }} · {{ o.personCount }}人</div>
          <div class="amount">¥{{ $money(o.totalAmount) }}</div>
          <span v-if="o.pendingUrge" class="urge-tag">催菜</span>
          <span class="st" :class="'s' + o.orderStatus">{{ statusLabel(o.orderStatus) }}</span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { getReportSummary, getOrderBoard } from '../api'
import { hasPerm } from '../utils/perm'

// 报表类 KPI(营业额/客单价/订单数)属 report:view;没有就只展示桌台看板。
const canReport = computed(() => hasPerm('report:view'))

const summary = ref({})
const board = ref([])

const busyCount = computed(() => board.value.filter((t) => t.status === 1).length)
const activeOrders = computed(() => {
  const arr = []
  board.value.forEach((t) => {
    if (t.order && [1, 2, 3].includes(t.order.orderStatus)) arr.push({ ...t.order, tableName: t.tableName, tableNo: t.tableNo })
  })
  return arr.slice(0, 6)
})

const STATUS = { 1: '已下单', 2: '制作中', 3: '已上齐', 4: '已完成', 5: '已取消' }
function statusLabel(s) {
  return STATUS[s] || '-'
}

async function load() {
  // 无 report:view 时跳过报表请求,避免一次必然 403 的调用弹「没有操作权限」。
  if (canReport.value) {
    summary.value = await getReportSummary()
  }
  board.value = await getOrderBoard()
}

onMounted(load)
</script>

<style scoped>
/* KPI */
.kpis {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 16px;
  margin-bottom: 16px;
}
/* 无 report:view 时只剩「桌台占用」一张卡,让它保持正常卡片宽度而不是撑满整行。
   (.kpis.solo 的选择器权重高于下面的媒体查询,不必再逐档覆盖) */
.kpis.solo {
  grid-template-columns: minmax(0, 320px);
}
.kpi {
  background: #fff;
  border-radius: 14px;
  padding: 18px 20px;
  box-shadow: var(--shadow-1);
}
.kpi .l {
  font-size: 12px;
  color: var(--ink-3);
}
.kpi .v {
  font-size: 26px;
  font-weight: 800;
  margin-top: 8px;
  color: var(--ink);
}
.kpi .v small {
  font-size: 13px;
  font-weight: 600;
  color: var(--ink-3);
}
.kpi .d {
  font-size: 11.5px;
  margin-top: 8px;
  color: var(--ink-3);
}
.kpi .d.up {
  color: var(--success);
}
.kpi.acc {
  background: var(--grad-brand);
  color: #fff;
  border: none;
}
.kpi.acc .l,
.kpi.acc .d {
  color: rgba(255, 255, 255, 0.85);
}
.kpi.acc .v,
.kpi.acc .v small {
  color: #fff;
}

/* 面板 */
.panels {
  display: grid;
  grid-template-columns: 1.6fr 1fr;
  gap: 16px;
}
.panel {
  background: #fff;
  border-radius: 14px;
  padding: 18px;
  box-shadow: var(--shadow-1);
}
.ph {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}
.ph .t {
  font-size: 14px;
  font-weight: 700;
  color: var(--ink);
}
.ph .l {
  font-size: 12px;
  color: var(--brand-deep);
  text-decoration: none;
}
.ph .l:hover {
  text-decoration: underline;
}

/* 桌台看板 */
.table-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 10px;
}
.tg {
  position: relative;
  border-radius: 10px;
  padding: 12px 6px;
  text-align: center;
  font-size: 11px;
  border: 1.5px solid var(--line);
  background: #fff;
  transition: all 0.15s;
}
.tg .n {
  font-weight: 800;
  font-size: 15px;
  color: var(--ink);
}
.tg .s {
  font-size: 10px;
  color: var(--ink-3);
  margin-top: 2px;
}
.tg.busy {
  border-color: #ffd5c4;
  background: var(--brand-soft);
}
.tg.busy .n {
  color: var(--brand-deep);
}
.tg.busy .s {
  color: var(--brand-deep);
}
.tg.free {
  border-color: #d4f2e3;
  background: var(--success-soft);
}
.tg.free .n {
  color: var(--success);
}
.tg.free .s {
  color: var(--success);
}

/* 催菜提醒:桌台角标 + 订单行标签,让服务员一眼看到哪桌在催 */
.tg.urging {
  border-color: var(--danger);
  box-shadow: 0 0 0 2px var(--danger-soft);
}
.urge-badge {
  position: absolute;
  top: -6px;
  right: -6px;
  min-width: 18px;
  height: 18px;
  padding: 0 4px;
  border-radius: 9px;
  background: var(--danger);
  color: #fff;
  font-size: 10px;
  font-weight: 700;
  line-height: 18px;
  text-align: center;
  animation: urge-blink 1.2s ease-in-out infinite;
}
@keyframes urge-blink {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.45; }
}
.urge-tag {
  flex-shrink: 0;
  padding: 1px 7px;
  border-radius: var(--r-pill);
  background: var(--danger-soft);
  color: var(--danger);
  font-size: 10.5px;
  font-weight: 700;
}

/* 订单 */
.orderline {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 11px 0;
  border-bottom: 1px solid #f5f5f5;
  font-size: 12.5px;
}
.orderline:last-child {
  border-bottom: none;
}
.orderline .no {
  font-weight: 600;
  color: var(--ink);
}
.orderline .meta {
  flex: 1;
  color: var(--ink-3);
  font-size: 11.5px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.orderline .amount {
  font-weight: 700;
  color: var(--brand-deep);
}
.st {
  font-size: 10.5px;
  padding: 3px 9px;
  border-radius: var(--r-pill);
  font-weight: 600;
  flex-shrink: 0;
}
.st.s1 {
  background: var(--warning-soft);
  color: #d48a00;
}
.st.s2 {
  background: var(--brand-soft);
  color: var(--brand-deep);
}
.st.s3 {
  background: var(--success-soft);
  color: var(--success);
}
.st.s4 {
  background: #f5f5f5;
  color: var(--ink-3);
}

.empty {
  padding: 30px 0;
  text-align: center;
  color: var(--ink-4);
  font-size: 13px;
}

@media (max-width: 900px) {
  .kpis {
    grid-template-columns: repeat(2, 1fr);
  }
  .panels {
    grid-template-columns: 1fr;
  }
  .table-grid {
    grid-template-columns: repeat(3, 1fr);
  }
}

@media (max-width: 767px) {
  .kpis {
    gap: 10px;
    margin-bottom: 12px;
  }
  .kpi {
    padding: 14px;
    border-radius: 12px;
  }
  .kpi .v {
    font-size: 22px;
    margin-top: 6px;
  }
  /* minmax(0,1fr) 阻止桌号卡内容把网格撑宽;padding 收窄给内容让位 */
  .table-grid {
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 8px;
  }
  .tg {
    padding: 10px 3px;
  }
}
</style>
