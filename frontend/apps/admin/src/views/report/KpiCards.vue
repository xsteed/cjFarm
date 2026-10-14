<template>
  <!-- 关键指标：今日营业额为主卡，环比基准取昨日 / 上月同期 -->
  <div class="kpi-grid">
    <div class="kpi hero">
      <div class="hero-main">
        <div class="hero-head">
          <span class="hero-label">今日营业额</span>
          <span class="hero-date">{{ today }}</span>
        </div>
        <div class="hero-value">¥{{ money(summary.todayAmount) }}</div>
        <div
          v-if="deltas.todayAmount !== null"
          class="hero-delta"
        >
          <span
            class="badge"
            :class="deltas.todayAmount >= 0 ? 'up' : 'down'"
          >
            {{ arrow(deltas.todayAmount) }} {{ Math.abs(deltas.todayAmount).toFixed(1) }}%
          </span>
          <span class="hero-delta-label">较昨日</span>
        </div>
      </div>
      <div class="hero-stats">
        <div class="hs">
          <div class="hs-top">
            <span class="hs-v">{{ summary.todayOrderCount || 0 }}</span>
            <em
              v-if="deltas.todayOrder !== null"
              class="hs-d"
              :class="deltas.todayOrder >= 0 ? 'up' : 'down'"
            >
              {{ arrow(deltas.todayOrder) }}{{ Math.abs(deltas.todayOrder).toFixed(0) }}%
            </em>
          </div>
          <span class="hs-k">订单</span>
        </div>
        <div class="hs">
          <div class="hs-top">
            <span class="hs-v">{{ summary.todayGuestCount || 0 }}</span>
            <em
              v-if="deltas.todayGuest !== null"
              class="hs-d"
              :class="deltas.todayGuest >= 0 ? 'up' : 'down'"
            >
              {{ arrow(deltas.todayGuest) }}{{ Math.abs(deltas.todayGuest).toFixed(0) }}%
            </em>
          </div>
          <span class="hs-k">到店人次</span>
        </div>
        <div class="hs">
          <div class="hs-top">
            <span class="hs-v">¥{{ money(summary.todayAvgAmount) }}</span>
          </div>
          <span class="hs-k">客单价</span>
        </div>
      </div>
    </div>

    <div class="kpi month">
      <div class="kpi-label">本月营业额</div>
      <div class="kpi-row">
        <div class="kpi-value money">¥{{ money(summary.monthAmount) }}</div>
        <div
          v-if="deltas.monthAmount !== null"
          class="kpi-delta"
        >
          <span
            class="badge"
            :class="deltas.monthAmount >= 0 ? 'up' : 'down'"
          >
            {{ arrow(deltas.monthAmount) }} {{ Math.abs(deltas.monthAmount).toFixed(1) }}%
          </span>
          <span class="kpi-delta-label">较上月同期</span>
        </div>
      </div>
      <div class="kpi-sub">
        本月 {{ summary.monthOrderCount || 0 }} 单 · 客流 {{ summary.monthGuestCount || 0 }} 人次
      </div>
    </div>

    <div class="kpi">
      <div class="kpi-label">今日免单让利</div>
      <div class="kpi-value free">¥{{ money(summary.todayFreeAmount) }}</div>
      <div class="kpi-sub">让利额不计入营业额</div>
    </div>

    <div class="kpi">
      <div class="kpi-label">今日赠送让利</div>
      <div class="kpi-value free">¥{{ money(summary.todayGiftAmount) }}</div>
      <div class="kpi-sub">赠送菜原价合计，不计入营业额</div>
    </div>

    <div class="kpi">
      <div class="kpi-label">挂账待收</div>
      <div class="kpi-value credit">¥{{ money(summary.creditPendingAmount) }}</div>
      <div class="kpi-sub">
        {{ summary.creditPendingCount || 0 }} 笔未结清 ·
        <RouterLink to="/credit"> 去核销 → </RouterLink>
      </div>
    </div>

    <div class="kpi">
      <div class="kpi-label">今日挂账回款</div>
      <div class="kpi-value">¥{{ money(summary.todayCreditSettledAmount) }}</div>
      <div class="kpi-sub">
        核销 {{ summary.todayCreditSettledCount || 0 }} 笔 · 新增挂账 ¥{{ money(summary.todayCreditAmount) }}
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { RouterLink } from 'vue-router';
import type { ReportSummary } from '@dining/types';
import { money, type Deltas } from './reportShared';

defineProps<{
  summary: ReportSummary;
  deltas: Deltas;
  today: string;
}>();

const arrow = (v: number | null): string => ((v ?? 0) >= 0 ? '↑' : '↓');
</script>

<style scoped>
/* 12 栅格：主卡占 2/3、本月占 1/3、其余四张各占 1/4。
   8+4 / 3*4 两行刚好铺满，末行不会再留下空位。 */
.kpi-grid {
  display: grid;
  grid-template-columns: repeat(12, minmax(0, 1fr));
  gap: 16px;
}

.kpi {
  grid-column: span 3;
  display: flex;
  flex-direction: column;
  background: #fff;
  border-radius: var(--r-lg);
  padding: 18px 20px;
  box-shadow: var(--shadow-1);
}

/* 本月营业额是仅次于今日的主指标，占 1/3 */
.kpi.month {
  grid-column: span 4;
}

.kpi-label {
  font-size: 13px;
  color: var(--ink-3);
  margin-bottom: 10px;
}

.kpi-value {
  font-size: 24px;
  font-weight: 800;
  color: var(--ink);
  font-variant-numeric: tabular-nums;
}

.kpi-value.free {
  color: #d48a00;
}

.kpi-value.credit {
  color: #2b6cd4;
}

/* 本月卡更宽，数值同步放大一档 */
.kpi.month .kpi-value {
  font-size: 26px;
}

.kpi-sub {
  font-size: 12px;
  color: var(--ink-4);
  margin-top: 6px;
  line-height: 1.5;
}

.kpi-sub a {
  color: var(--brand-deep);
  text-decoration: none;
}

.kpi-sub a:hover {
  text-decoration: underline;
}

.kpi-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px 14px;
}

/* 今日营业额：品牌渐变主卡，占 2/3 版面，左金额右明细 */
.kpi.hero {
  grid-column: span 8;
  flex-flow: row wrap;
  align-items: center;
  gap: 18px 22px;
  padding: 18px 22px;
  background: var(--grad-brand);
  color: #fff;
  box-shadow: 0 10px 24px rgb(240 72 31 / 22%);
}

.hero-main {
  min-width: 0;
}

.hero-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
}

.hero-label {
  font-size: 13px;
  color: rgb(255 255 255 / 85%);
}

.hero-date {
  font-size: 12px;
  color: rgb(255 255 255 / 80%);
}

.hero-value {
  font-size: 34px;
  font-weight: 800;
  line-height: 1.2;
  margin-top: 8px;
  letter-spacing: 0.5px;
  font-variant-numeric: tabular-nums;
}

.hero-delta {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 8px;
}

.hero-delta-label {
  font-size: 12px;
  color: rgb(255 255 255 / 80%);
}

/* 明细靠右、竖排成三列，用分隔线与金额区隔开 */
.hero-stats {
  display: flex;
  flex-wrap: wrap;
  gap: 10px 28px;
  margin-left: auto;
  padding-left: 22px;
  border-left: 1px solid rgb(255 255 255 / 22%);
}

.hs {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.hs-top {
  display: flex;
  align-items: baseline;
  gap: 4px;
}

.hs-v {
  font-size: 17px;
  font-weight: 700;
}

.hs-k {
  font-size: 12px;
  color: rgb(255 255 255 / 80%);
}

.hs-d {
  font-style: normal;
  font-size: 11px;
  color: rgb(255 255 255 / 90%);
}

/* 环比徽标 */
.badge {
  display: inline-block;
  padding: 2px 8px;
  border-radius: var(--r-pill);
  font-size: 11.5px;
  font-weight: 700;
  line-height: 1.6;
}

.badge.up {
  background: rgb(31 201 126 / 16%);
  color: #0f9d5c;
}

.badge.down {
  background: rgb(245 63 63 / 14%);
  color: #d32f2f;
}

.kpi.hero .badge.up,
.kpi.hero .badge.down {
  background: rgb(255 255 255 / 22%);
  color: #fff;
}

.kpi-delta {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 10px;
}

/* 与金额同行时不额外留上边距 */
.kpi-row .kpi-delta {
  margin-top: 0;
}

.kpi-delta-label {
  font-size: 12px;
  color: var(--ink-4);
}

/* 中屏 6 栅格：主卡与本月各占整行，其余四张两两成行 */
@media (width <= 1200px) {
  .kpi-grid {
    grid-template-columns: repeat(6, minmax(0, 1fr));
  }

  .kpi.month,
  .kpi.hero {
    grid-column: span 6;
  }
}

@media (width <= 767px) {
  .kpi-grid {
    grid-template-columns: minmax(0, 1fr);
    gap: 12px;
  }

  .kpi,
  .kpi.month,
  .kpi.hero {
    grid-column: span 1;
  }

  .kpi {
    padding: 14px 16px;
  }

  .kpi.hero {
    flex-direction: column;
    align-items: stretch;
    gap: 14px;
    padding: 16px;
  }

  .hero-value {
    font-size: 28px;
  }

  /* 单列时明细改为横向排布，分隔线从上边移到左边 */
  .hero-stats {
    gap: 12px 20px;
    margin-left: 0;
    padding-left: 0;
    padding-top: 14px;
    border-left: none;
    border-top: 1px solid rgb(255 255 255 / 22%);
  }
}
</style>
