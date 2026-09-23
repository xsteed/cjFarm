<template>
  <!-- 运营概览：一眼看清在途、空闲与风险项 -->
  <div class="meta-card">
    <div class="meta-item">
      <span class="mk">在途订单</span>
      <span class="mv">{{ summary.activeOrderCount || 0 }}<em>单</em></span>
    </div>
    <div class="meta-item">
      <span class="mk">空闲桌台</span>
      <span class="mv"
        >{{ summary.freeTableCount || 0 }}<em>/ {{ summary.tableCount || 0 }} 桌</em></span
      >
    </div>
    <div class="meta-item">
      <span class="mk">翻台率</span>
      <span class="mv">{{ turnover }}<em>次/桌</em></span>
    </div>
    <div class="meta-item">
      <span class="mk">今日退款</span>
      <span
        class="mv"
        :class="{ warn: (summary.todayRefundAmount || 0) > 0 }"
      >
        ¥{{ money(summary.todayRefundAmount) }}
      </span>
    </div>
    <div class="meta-item">
      <span class="mk">今日取消</span>
      <span
        class="mv"
        :class="{ warn: (summary.todayCancelCount || 0) > 0 }"
      >
        {{ summary.todayCancelCount || 0 }}<em>单</em>
      </span>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { ReportSummary } from '../../types/entities';
import { money } from './reportShared';

defineProps<{
  summary: ReportSummary;
  turnover: string;
}>();
</script>

<style scoped>
/* ---------- 运营概览条 ---------- */
.meta-card {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  gap: 12px;
  margin-top: 16px;
  background: #fff;
  border-radius: var(--r-lg);
  padding: 14px 20px;
  box-shadow: var(--shadow-1);
}

.meta-item {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}

.mk {
  font-size: 12px;
  color: var(--ink-3);
}

.mv {
  font-size: 18px;
  font-weight: 700;
  color: var(--ink);
}

.mv em {
  font-style: normal;
  font-size: 12px;
  font-weight: 500;
  color: var(--ink-4);
  margin-left: 3px;
}

.mv.warn {
  color: var(--danger);
}

@media (width <= 1200px) {
  .meta-card {
    grid-template-columns: repeat(3, minmax(0, 1fr));
    row-gap: 16px;
  }
}

@media (width <= 767px) {
  .meta-card {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    padding: 14px 16px;
  }
}

/* 超窄屏(320~360px):运营概览条 2 列金额数字收紧,避免 ¥ 金额换行 */
@media (width <= 360px) {
  .meta-card {
    gap: 8px;
    padding: 12px;
  }

  .mv {
    font-size: 16px;
  }
}
</style>
