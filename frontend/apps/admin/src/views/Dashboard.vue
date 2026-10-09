<template>
  <div>
    <!-- 移动端下拉刷新:看板是实时数据,随手下拉即可刷新 -->
    <PullRefresh
      v-if="isMobile"
      :distance="refreshDistance"
      :refreshing="refreshRefreshing"
      :pulling="refreshPulling"
    />

    <!-- KPI 卡片。
         注意:营业额/客单价/今日订单都来自 report/summary 接口(report:view)。
         收银员与员工只有 order:view,若照旧调用会稳定 403 —— 所以这里按权限降级:
         没有 report:view 就只留「桌台占用」,数据从 order/board 本地算,不发报告请求。 -->
    <div
      class="kpis"
      :class="{ solo: !canReport }"
    >
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
        <div class="v">
          {{ busyCount }} <small>/ {{ board.length }}</small>
        </div>
        <div class="d">
          <template v-if="canReport">
            空闲 {{ summary.freeTableCount || 0 }} 桌 · 进行中 {{ summary.activeOrderCount || 0 }} 单
          </template>
          <template v-else> 空闲 {{ board.length - busyCount }} 桌 </template>
        </div>
      </div>
    </div>

    <div class="panels">
      <!-- 桌台看板 -->
      <div class="panel">
        <div class="ph">
          <div class="t">桌台看板</div>
          <router-link
            class="l"
            to="/dining/tables"
          >
            全部桌台 →
          </router-link>
        </div>
        <div class="table-grid">
          <!-- 点桌台瓦片直达桌台管理,少一层查找;催菜角标保持醒目 -->
          <button
            v-for="t in board"
            :key="t.tableId"
            type="button"
            class="tg"
            :class="[t.status === 1 ? 'busy' : 'free', { urging: hasUrge(t) }]"
            :aria-label="`${t.tableNo} 号桌,${t.status === 1 ? '用餐中' : '空闲'}`"
            @click="goTables"
          >
            <div class="n">
              {{ t.tableNo }}
            </div>
            <div class="s">
              {{ t.status === 1 ? '用餐中' : '空闲' }}
            </div>
            <span
              v-if="hasUrge(t)"
              class="urge-badge"
              >催</span
            >
          </button>
          <div
            v-if="!board.length"
            class="empty"
          >
            暂无桌台
          </div>
        </div>
      </div>

      <!-- 最新订单 -->
      <div class="panel">
        <div class="ph">
          <div class="t">进行中订单</div>
          <router-link
            class="l"
            to="/dining/orders"
          >
            订单管理 →
          </router-link>
        </div>
        <div
          v-if="!activeOrders.length"
          class="empty"
        >
          暂无进行中订单
        </div>
        <button
          v-for="o in activeOrders"
          :key="o.orderId"
          type="button"
          class="orderline"
          :aria-label="`订单 ${o.shortNo || o.orderNo.slice(-6)},${statusLabel(o.orderStatus)}`"
          @click="goOrders"
        >
          <div class="no">#{{ o.shortNo || o.orderNo.slice(-6) }}</div>
          <div class="meta">{{ o.tableName || o.tableNo }} · {{ o.personCount }}人</div>
          <div class="amount">¥{{ $money(o.totalAmount) }}</div>
          <span
            v-if="o.pendingUrge"
            class="urge-tag"
            >催菜</span
          >
          <span
            class="st"
            :class="'s' + o.orderStatus"
            >{{ statusLabel(o.orderStatus) }}</span
          >
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue';
import { useRouter } from 'vue-router';
import { getReportSummary, getOrderBoard } from '@dining/api';
import { hasPerm } from '@dining/auth';
import { money } from '@dining/shared';
import type { OrderSummary, ReportSummary, TableInfo } from '@dining/types';
import { useIsMobile } from '../utils/useMobile';
import { usePullRefresh } from '../composables/usePullRefresh';
import PullRefresh from '../components/PullRefresh.vue';

// 模板里 $money 与全局属性共用 @dining/shared 里 money 的实现(TS 模板消费不到全局属性,须显式导入)。
const $money = money;

const router = useRouter();
const { isMobile } = useIsMobile();

// 移动端下拉刷新看板实时数据
const { distance: refreshDistance, refreshing: refreshRefreshing, pulling: refreshPulling } = usePullRefresh(load);

// 看板上的桌台 / 订单点一下直达对应管理页,减少一级查找
function goTables(): void {
  router.push('/dining/tables');
}

function goOrders(): void {
  router.push('/dining/orders');
}

// 报表类 KPI(营业额/客单价/订单数)属 report:view;没有就只展示桌台看板。
const canReport = computed(() => hasPerm('report:view'));

const summary = ref<ReportSummary>({});
const board = ref<TableInfo[]>([]);

const busyCount = computed(() => board.value.filter(t => t.status === 1).length);

// 进行中订单行在模板里要 .slice 订单号;OrderSummary 字段全可选,这里收紧为必填。
type ActiveOrder = OrderSummary & { orderNo: string };

const activeOrders = computed<ActiveOrder[]>(() => {
  const arr: OrderSummary[] = [];
  board.value.forEach(t => {
    // 一桌多单:看板每桌返回 orders 数组(付款后新单与出餐中旧单并存)。
    (t.orders || []).forEach(o => {
      if (o.orderStatus !== undefined && [1, 2, 3].includes(o.orderStatus)) {
        arr.push({ ...o, tableName: t.tableName, tableNo: t.tableNo });
      }
    });
  });
  return arr.slice(0, 6) as ActiveOrder[];
});

// 桌台是否有待处理催菜(任一进行中订单在催)。
function hasUrge(t: TableInfo): boolean {
  return !!(t.orders && t.orders.some(o => o.pendingUrge));
}

const STATUS: Record<number, string> = { 1: '已下单', 2: '制作中', 3: '已上齐', 4: '已完成', 5: '已取消' };
function statusLabel(s?: number): string {
  return s === undefined ? '-' : STATUS[s] || '-';
}

async function load(): Promise<void> {
  try {
    // 无 report:view 时跳过报表请求,避免一次必然 403 的调用弹「没有操作权限」。
    if (canReport.value) {
      summary.value = await getReportSummary();
    }
    // order/board 返回桌台数组(每桌带当前进行中订单),与 TableInfo 同形。
    board.value = await getOrderBoard();
  } catch {
    /* 失败已由拦截器统一 toast */
  }
}

onMounted(load);
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
  color: rgb(255 255 255 / 85%);
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
  width: 100%;
  border-radius: 10px;
  padding: 12px 6px;
  text-align: center;
  font-family: inherit;
  font-size: 11px;
  color: inherit;
  border: 1.5px solid var(--line);
  background: #fff;
  cursor: pointer;
  transition: all 0.15s;
}

/* 可点反馈:按压变淡、键盘聚焦有描边 */
.tg:active,
.orderline:active {
  opacity: 0.72;
}

.tg:focus-visible,
.orderline:focus-visible {
  outline: 2px solid var(--brand);
  outline-offset: 2px;
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
  0%,
  100% {
    opacity: 1;
  }

  50% {
    opacity: 0.45;
  }
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
  width: 100%;
  padding: 11px 0;
  border: none;
  border-bottom: 1px solid #f5f5f5;
  background: transparent;
  color: inherit;
  font-family: inherit;
  font-size: 12.5px;
  text-align: left;
  cursor: pointer;
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

@media (width <= 900px) {
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

@media (width <= 767px) {
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

  .panels {
    gap: 12px;
  }

  .panel {
    padding: 14px;
    border-radius: 12px;
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

/* 超窄屏(iPhone SE 1代 320 / 小安卓 360):3 列桌号卡再收紧,桌号字号下调避免溢出 */
@media (width <= 360px) {
  .tg {
    padding: 8px 2px;
  }

  .tg .n {
    font-size: 14px;
  }

  .tg .s {
    font-size: 9px;
  }
}
</style>
