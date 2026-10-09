<template>
  <!-- 顶部总览:基于全量桌台(忽略分页/搜索)统计,一眼看清当前楼面桌况 -->
  <div class="stat-row">
    <div class="stat">
      <div class="label">桌台总数</div>
      <div class="num">
        {{ summary.total }}
      </div>
    </div>
    <div class="stat">
      <div class="label">占用 / 空闲</div>
      <div class="num">
        <span class="n-occ">{{ summary.occupied }}</span>
        <span class="nslash">/</span>
        <span class="n-idle">{{ summary.idle }}</span>
      </div>
    </div>
    <div class="stat">
      <div class="label">停用 / 预约</div>
      <div class="num">
        <span class="n-dis">{{ summary.disabled }}</span>
        <span class="nslash">/</span>
        <span class="n-res">{{ summary.reserved }}</span>
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
  // 人工设置的「停用 / 预约」桌台数(与占用正交,不计入空闲)。
  disabled: number;
  reserved: number;
  rate: number;
  capacity: number;
}

defineProps<{
  summary: TableSummary;
}>();
</script>

<style scoped>
/* 顶部总览:桌面一排统计卡自适应折行;移动端见下方媒体查询(2 列紧凑网格) */
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

/* 合并卡「占用 / 空闲」:两个数字同色系区分,中间斜杠弱化 */
.stat .num .n-occ {
  color: #e6772e;
}

.stat .num .n-idle {
  color: #2ba24a;
}

/* 停用(灰) / 预约(蓝) 两个数字:与占用/空闲同款区分 */
.stat .num .n-dis {
  color: #8a8f99;
}

.stat .num .n-res {
  color: #3d6ff0;
}

.stat .num .nslash {
  margin: 0 4px;
  color: var(--ink-3);
  font-weight: 400;
}

/* 移动端(≤768px):卡片整体收紧,2 列网格平铺,4 张卡 2×2 一屏看全,
   不再折成 3 行或需要横向滚动。 */
@media (width <= 768px) {
  .stat-row {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 8px;
    margin-bottom: 10px;
  }

  .stat {
    padding: 10px 12px;
  }

  .stat .label {
    font-size: 11px;
  }

  .stat .num {
    font-size: 20px;
    margin-top: 4px;
  }
}
</style>
