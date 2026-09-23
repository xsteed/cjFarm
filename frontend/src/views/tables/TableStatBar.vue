<template>
  <!-- 顶部总览:基于全量桌台(忽略分页/搜索)统计,一眼看清当前楼面桌况 -->
  <div class="stat-row">
    <div class="stat">
      <div class="label">桌台总数</div>
      <div class="num">
        {{ summary.total }}
      </div>
    </div>
    <div class="stat occ">
      <div class="label">占用中</div>
      <div class="num">
        {{ summary.occupied }}
      </div>
    </div>
    <div class="stat idle">
      <div class="label">空闲</div>
      <div class="num">
        {{ summary.idle }}
      </div>
    </div>
    <div class="stat">
      <div class="label">占用率</div>
      <div class="num">{{ summary.rate }}<span class="unit">%</span></div>
    </div>
    <div class="stat">
      <div class="label">总容纳人数</div>
      <div class="num">{{ summary.capacity }}<span class="unit">人</span></div>
    </div>
  </div>
</template>

<script setup lang="ts">
interface TableSummary {
  total: number;
  occupied: number;
  idle: number;
  rate: number;
  capacity: number;
}

defineProps<{
  summary: TableSummary;
}>();
</script>

<style scoped>
/* 顶部总览:一排统计卡,响应式自动折行 */
.stat-row {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: 12px;
  margin-bottom: 12px;
}

.stat {
  background: #fff;
  border: 1px solid var(--line);
  border-radius: var(--r-md, 10px);
  padding: 14px 16px;
}

.stat .label {
  font-size: 12px;
  color: var(--ink-3);
}

.stat .num {
  font-size: 24px;
  font-weight: 700;
  color: var(--ink);
  margin-top: 6px;
  line-height: 1;
}

.stat .num .unit {
  font-size: 13px;
  font-weight: 500;
  color: var(--ink-3);
  margin-left: 3px;
}

.stat.occ .num {
  color: #e6772e;
}

.stat.idle .num {
  color: #2ba24a;
}

/* 超窄屏(320~360px):顶部总览 5 张统计卡收紧,数字字号下调避免换行 */
@media (width <= 360px) {
  .stat-row {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 8px;
  }

  .stat {
    padding: 12px;
  }

  .stat .num {
    font-size: 20px;
  }
}
</style>
