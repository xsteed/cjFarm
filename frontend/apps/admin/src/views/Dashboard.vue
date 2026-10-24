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
        <!-- 站点访问量:与营业额口径独立(没下单的扫码访客也计入),看「今天多少人来了」 -->
        <div class="kpi">
          <div class="l">今日访问</div>
          <div class="v">{{ visit.todayUv || 0 }} <small>人</small></div>
          <div class="d">{{ visit.todayPv || 0 }} 次访问 · 新访客 {{ visit.todayNewUv || 0 }}</div>
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

      <!-- 待处理催菜。
           其余 KPI 都是系统算出来的「结果指标」,只有这一条是顾客亲手按下按钮产生的
           「不满信号」—— 菜没上、等太久,不处理就在几十分钟内升级成客诉与差评。
           它是全页唯一一个有分钟级 SLA 的指标,因此占一张常驻卡。
           点击进订单管理而不是就地标记已处理:催菜的正解是「把菜端上去」
           (订单流转到已上齐时后端会自动消解催菜),在卡片上点一下等于把提醒按掉、
           菜照样没上,反而把真实问题藏起来了。 -->
      <button
        type="button"
        class="kpi urge"
        :class="{ 'urge-alert': urgeTableCount > 0 }"
        :aria-label="`待处理催菜 ${urgeTableCount} 桌`"
        @click="goOrders"
      >
        <div class="l">待处理催菜</div>
        <div class="v">{{ urgeTableCount }} <small>桌</small></div>
        <div class="d">{{ urgeTableCount ? urgeSubText : '暂无催菜' }}</div>
      </button>
    </div>

    <!-- 未结账超时告警:开关与阈值都由后端下发(见 handler.OrderStale)。
         它不改变任何状态,只是把「可能被忘掉的那几桌」推到店员眼前。 -->
    <div
      v-if="stale.items.length"
      class="stale-bar"
    >
      <div class="sh">
        <span class="t">有 {{ stale.items.length }} 笔订单还没结账，去核对一下</span>
        <router-link
          class="l"
          to="/orders"
        >
          订单管理 →
        </router-link>
      </div>
      <button
        v-for="it in stale.items"
        :key="it.orderId"
        type="button"
        class="stale-line"
        :aria-label="`${it.tableName || it.tableNo} ${it.shortNo || it.orderNo} 已开台 ${durationText(it.minutes)}`"
        @click="goOrders"
      >
        <span class="tb">{{ it.tableName || it.tableNo }}</span>
        <span class="no">#{{ it.shortNo || (it.orderNo || '').slice(-6) }}</span>
        <span class="tm">已 {{ durationText(it.minutes) }}</span>
        <!-- 跨日遗留单的桌台已被自动清空,看板上什么都看不到 —— 这里必须标出来,
             否则店家会以为「桌上没人,怎么还有单」 -->
        <span
          v-if="it.legacy"
          class="lg"
          >跨日</span
        >
        <span class="am">¥{{ $money(it.totalAmount) }}</span>
      </button>
    </div>

    <div class="panels">
      <!-- 桌台看板 -->
      <div class="panel">
        <div class="ph">
          <div class="t">桌台看板</div>
          <router-link
            class="l"
            to="/tables"
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
            to="/orders"
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
          :aria-label="`订单 ${o.shortNo || (o.orderNo || '').slice(-6)},${statusLabel(o.orderStatus)}`"
          @click="goOrders"
        >
          <div class="no">#{{ o.shortNo || (o.orderNo || '').slice(-6) }}</div>
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
import { ref, computed, onMounted, onUnmounted } from 'vue';
import { useRouter } from 'vue-router';
import { getReportSummary, getOrderBoard, getStaleOrders, getVisitStats, listUrges } from '@dining/api';
import { hasPerm } from '@dining/auth';
import { durationText, money } from '@dining/shared';
import type { OrderSummary, ReportSummary, StaleOrderAlert, TableInfo, Urge, VisitStats } from '@dining/types';
import { useIsMobile } from '../utils/useMobile';
import { usePullRefresh } from '../composables/usePullRefresh';
import PullRefresh from '../components/PullRefresh.vue';

// 模板里 $money 与全局属性共用 @dining/shared 里 money 的实现(TS 模板消费不到全局属性,须显式导入)。
const $money = money;

const router = useRouter();
const { isMobile } = useIsMobile();

// 移动端下拉刷新看板实时数据
const { distance: refreshDistance, refreshing: refreshRefreshing, pulling: refreshPulling } = usePullRefresh(loadOnce);

// 看板上的桌台 / 订单点一下直达对应管理页,减少一级查找
function goTables(): void {
  router.push('/tables');
}

function goOrders(): void {
  router.push('/orders');
}

// 报表类 KPI(营业额/客单价/订单数)属 report:view;没有就只展示桌台看板。
const canReport = computed(() => hasPerm('report:view'));

const summary = ref<ReportSummary>({});
// 访问统计只取 today 字段,daily 数组不用于本页(看趋势去报表页)。
const visit = ref<NonNullable<VisitStats['summary']>>({});
const board = ref<TableInfo[]>([]);
// 未结账超时告警。接口类型里三个字段都是可选的(兼容老后端),这里落地成必填:
// items 恒为数组,模板就能直接 .length / v-for,不必到处写 || []。
type StaleState = { enabled: boolean; hours: number; items: StaleOrderAlert[] };
const stale = ref<StaleState>({ enabled: false, hours: 0, items: [] });
// 待处理催菜(status=0),与订单管理页「已催办」按钮同口径。
const urges = ref<Urge[]>([]);

// durationText 已抽到 @dining/shared(桌台管理页也用),此处直接导入。

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

// ---- 待处理催菜卡片 ----
// 顾客端催菜有 3 分钟冷却,同一桌仍可能连催多次;按「桌」去重才是店长关心的
// 「几桌在催」,直接算条数会把一桌的重复催促当成多桌。
const urgeTables = computed(() => {
  const seen = new Map<string, string>();
  urges.value.forEach(u => {
    // 口径是「几桌在催」:按桌去重 —— 同一桌多笔进行中单同时催菜只算一桌。
    // 按桌+单去重会把一桌的连催算成多桌,文案还会出现「大厅01桌 · 大厅01桌」。
    const key = String(u.tableId ?? u.tableName ?? u.tableNo ?? '');
    const label = u.tableName || u.tableNo || '';
    if (label && !seen.has(key)) seen.set(key, label);
  });
  return [...seen.values()];
});

const urgeTableCount = computed(() => urgeTables.value.length);
// 副行最多列 3 桌:再多会撑破卡片,剩下的量由主数字表达。
const urgeTableText = computed(() => urgeTables.value.slice(0, 3).join(' · '));

// 催了多久。后端下发服务端时间字符串,Safari 不认 'YYYY-MM-DD HH:mm:ss',统一斜杠化。
function urgeMinutes(createTime?: string): number {
  const t = new Date(String(createTime || '').replace(/-/g, '/')).getTime();
  if (!Number.isFinite(t)) return 0;
  return Math.max(0, Math.round((Date.now() - t) / 60_000));
}

// 取「等得最久」的那一条 —— 决定今天服务口碑的不是催了几次,而是最久的那桌等了多久。
const urgeOldestMinutes = computed(() => urges.value.reduce((max, u) => Math.max(max, urgeMinutes(u.createTime)), 0));

// 副行文案。刚催出来不满 1 分钟时说「刚刚」,避免出现「最久 0 分钟」这种别扭说法。
const urgeSubText = computed(() => {
  if (!urgeTableCount.value) return '';
  const wait = urgeOldestMinutes.value ? `最久 ${durationText(urgeOldestMinutes.value)}` : '刚刚';
  return `${urgeTableText.value} 在催 · ${wait}`;
});

const STATUS: Record<number, string> = { 1: '已下单', 2: '制作中', 3: '已上齐', 4: '已完成', 5: '已取消' };
function statusLabel(s?: number): string {
  return s === undefined ? '-' : STATUS[s] || '-';
}

async function load(): Promise<void> {
  try {
    // 无 report:view 时跳过报表请求,避免一次必然 403 的调用弹「没有操作权限」。
    if (canReport.value) {
      summary.value = await getReportSummary();
      // 只取近 2 天:本页只用 today 字段,不必为一张卡拉整月逐日数据。
      // 与 summary 串行是刻意的 —— 访问统计失败不该影响经营 KPI 的展示。
      try {
        const v = await getVisitStats({ days: 2 });
        visit.value = v?.summary || {};
      } catch {
        /* 报表接口失败已由拦截器提示,这里保经营指标可用 */
      }
    }
    // order/board 返回桌台数组(每桌带当前进行中订单),与 TableInfo 同形。
    board.value = await getOrderBoard();
    // 告警开关关着时后端会直接返回 enabled=false 且不带数据,前端不必先判断配置。
    const alerts = await getStaleOrders();
    stale.value = { enabled: !!alerts.enabled, hours: Number(alerts.hours) || 0, items: alerts.items || [] };
    // 催菜单独包一层:拿不到催菜不该让桌台看板和超时告警跟着空掉。
    try {
      // pageSize 给足:卡片只要「几桌在催 + 最久等多久」,不做分页。
      // 真出现几十条未处理的极端情况,前 50 条也足以说明问题。
      const u = await listUrges({ status: 0, pageSize: 50 });
      urges.value = u?.items || [];
    } catch {
      urges.value = [];
    }
  } catch {
    /* 失败已由拦截器统一 toast */
  }
}

// ---- 停留在本页时自动刷新 ----
// 看板是实时数据(桌台占用/进行中订单/超时告警),停留时每 15s 自动拉一次;
// 切后台/锁屏暂停轮询(省流量与后端 QPS),回到可见立即补一轮并恢复节奏
// (与订单管理/桌台管理页同策略)。
const AUTO_REFRESH_MS = 15_000;
let autoTimer: ReturnType<typeof setInterval> | null = null;

// 看板没有 loading 态,用「共享在途 Promise」去重:自动刷新、回到前台、下拉刷新、
// 首屏加载都可能同时触发 —— 此前只有定时器那一拍有守卫,其余路径仍会并发多份,
// 既重复打后端,也让先到的旧快照被后到的覆盖。
let inflightLoad: Promise<void> | null = null;

function loadOnce(): Promise<void> {
  if (inflightLoad) return inflightLoad;
  inflightLoad = load().finally(() => {
    inflightLoad = null;
  });
  return inflightLoad;
}

function startAutoRefresh(): void {
  stopAutoRefresh();
  autoTimer = setInterval(() => {
    void loadOnce();
  }, AUTO_REFRESH_MS);
}

function stopAutoRefresh(): void {
  if (autoTimer) {
    clearInterval(autoTimer);
    autoTimer = null;
  }
}

function onVisibilityChange(): void {
  if (document.hidden) {
    stopAutoRefresh();
  } else {
    void loadOnce();
    startAutoRefresh();
  }
}

onMounted(() => {
  void loadOnce();
  startAutoRefresh();
  document.addEventListener('visibilitychange', onVisibilityChange);
});

onUnmounted(() => {
  stopAutoRefresh();
  document.removeEventListener('visibilitychange', onVisibilityChange);
});
</script>

<style scoped>
/* KPI */
.kpis {
  display: grid;

  /* 有 report:view 时共 6 张卡(营业额/订单/客单价/今日访问/桌台占用/待处理催菜),等宽平分 */
  grid-template-columns: repeat(6, 1fr);
  gap: 16px;
  margin-bottom: 16px;
}

/* 无 report:view 时只剩「桌台占用」+「待处理催菜」两张,让它们保持正常卡片宽度而不是撑满整行。
   (.kpis.solo 的选择器权重高于下面的媒体查询,窄屏那一档要显式再覆盖一次) */
.kpis.solo {
  grid-template-columns: repeat(2, minmax(0, 320px));
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

/* 待处理催菜:整卡可点,直达订单管理。
   常态刻意与其它白卡片保持一致 —— 没人催的时候不该抢注意力;
   一旦有人催,换成告警底色 + 呼吸描边,把它从一排白卡片里拎出来。 */
.kpi.urge {
  display: block;
  width: 100%;

  /* 本工程没有全局 border-box(见 CartBar / Printers 里的同样处理):
     width:100% 叠上 .kpi 的左右内边距与本卡的 1px 边框,content-box 下会戳出格子约 42px,
     把 6 列网格挤乱。 */
  box-sizing: border-box;
  font: inherit;
  color: inherit;
  text-align: left;

  /* 常态就占好 1px 边框的位置:切到告警态时不会因多出边框而抖动 */
  border: 1px solid transparent;
  cursor: pointer;
  transition:
    border-color 0.2s,
    background 0.2s;
}

.kpi.urge:hover {
  border-color: var(--brand-soft);
}

.kpi.urge:focus-visible {
  outline: 2px solid var(--brand);
  outline-offset: 2px;
}

.kpi.urge-alert {
  background: var(--danger-soft);
  border-color: var(--danger);
  animation: urge-card-breathe 1.6s ease-in-out infinite;
}

.kpi.urge-alert .l,
.kpi.urge-alert .v,
.kpi.urge-alert .v small {
  color: var(--danger);
}

.kpi.urge-alert:hover {
  border-color: var(--danger);
}

/* 呼吸的只有描边,底色与文字保持稳定 —— 整卡闪烁会干扰旁边 KPI 的阅读 */
@keyframes urge-card-breathe {
  0%,
  100% {
    box-shadow: var(--shadow-1);
  }

  50% {
    box-shadow: 0 0 0 3px var(--danger-soft);
  }
}

/* 未结账超时告警:用告警色把它从一屏白卡片里拎出来,但不做成弹窗 ——
   它是「提醒」不是「阻断」,店家可以先把手上这单忙完再回来处理。 */
.stale-bar {
  background: #fff8e6;
  border: 1px solid #ffe1a8;
  border-radius: 14px;
  padding: 14px 16px;
  margin-bottom: 16px;
}

.sh {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}

.sh .t {
  font-size: 13px;
  font-weight: 700;
  color: #a86a00;
}

.sh .l {
  flex-shrink: 0;
  font-size: 12px;
  color: #a86a00;
  text-decoration: none;
}

.sh .l:hover {
  text-decoration: underline;
}

.stale-line {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 9px 0;
  border: none;
  border-top: 1px solid #ffe1a8;
  background: transparent;
  color: inherit;
  font-family: inherit;
  font-size: 12.5px;
  text-align: left;
  cursor: pointer;
}

.stale-line:active {
  opacity: 0.72;
}

.stale-line:focus-visible {
  outline: 2px solid var(--brand);
  outline-offset: 2px;
}

.stale-line .tb {
  font-weight: 600;
  color: var(--ink);
}

.stale-line .no {
  color: var(--ink-3);
  font-size: 11.5px;
}

.stale-line .tm {
  flex: 1;
  color: #a86a00;
  font-weight: 600;
  font-size: 11.5px;
}

/* 「跨日」标记:这类单的桌台已被清空,与还在占台的超时单区分开 */
.stale-line .lg {
  flex-shrink: 0;
  padding: 1px 6px;
  border-radius: var(--r-pill);
  background: #ffe1a8;
  color: #a86a00;
  font-size: 10.5px;
  font-weight: 700;
}

.stale-line .am {
  font-weight: 700;
  color: var(--brand-deep);
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

  /* 必须是 border-box:本工程没有全局 reset(见 CartBar / Printers 里的同样处理),
     content-box 下 width:100% 会把 12px 水平内边距和 3px 边框加在列宽之外 ——
     每个瓦片比网格列宽 15px,4 列时相邻瓦片重叠 5px、最右一个戳出面板。 */
  box-sizing: border-box;
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

/* 6 张卡在中屏排 3 列:排 2 列要占 3 行,会把下面的桌台看板顶出首屏 */
@media (width <= 900px) {
  .kpis {
    grid-template-columns: repeat(3, 1fr);
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

/* 手机窄屏:3 列会把「¥8,000」这类金额压到换行,回到 2 列。
   .kpis.solo 权重高于本块里的 .kpis,故这里必须连带覆盖一次。 */
@media (width <= 560px) {
  .kpis,
  .kpis.solo {
    grid-template-columns: repeat(2, minmax(0, 1fr));
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
