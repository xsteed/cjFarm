<template>
  <t-dialog
    :visible="visible"
    :header="title"
    width="fit-content"
    :footer="false"
    :close-on-overlay-click="true"
    @close="$emit('update:visible', false)"
    @update:visible="($event: boolean) => $emit('update:visible', $event)"
  >
    <div
      v-if="loading"
      class="preview-loading"
    >
      正在生成预览…
    </div>
    <template v-else-if="chunks && chunks.length">
      <div class="paper-scroll">
        <div
          v-for="(chunk, i) in chunks"
          :key="i"
          class="paper-group"
        >
          <div
            v-if="chunks.length > 1"
            class="preview-tip chunk-label"
          >
            第 {{ i + 1 }} / {{ chunks.length }} 段(超长票据实际分段送出)
          </div>
          <div
            class="ticket-paper"
            :style="{ '--cols': lineWidth }"
          >
            <div
              v-for="(line, li) in chunk"
              :key="li"
              class="ticket-line"
              :class="{ bold: line.bold }"
            >
              <span
                v-for="(cell, ci) in cellsOf(line.text)"
                :key="ci"
                class="cell"
                :class="{ wide: cell.wide }"
                >{{ cell.ch }}</span
              >
            </div>
          </div>
        </div>
      </div>
      <div class="preview-tip paper-meta">
        <slot name="meta">
          由后端「出纸同一份渲染代码」生成,与实际小票 1:1;纸宽
          {{ lineWidth === 32 ? '58mm' : '80mm' }}。
        </slot>
      </div>
    </template>
  </t-dialog>
</template>

<script setup lang="ts">
// 纸带式票据预览:逐字符按「字符网格」渲染,精确复刻热敏机的对齐模型
// (全角字符占 2 格、半角占 1 格),浏览器字体宽度差异不会造成两列错位。
// 加粗行(店名/标题/合计)与 ESC/POS 出纸的 ESC E 强调一致。
// 打印记录页(重放某条记录)与系统配置页(示例模板预览)共用。
import type { TicketLine } from '../types/entities';

defineProps<{
  visible: boolean;
  title: string;
  loading?: boolean;
  /** 分段后的文本行(每段一张纸带,与实际入队的任务段一一对应) */
  chunks?: TicketLine[][] | null;
  /** 每行可容纳的半角字符数(58mm=32,80mm=48) */
  lineWidth?: number;
}>();

defineEmits<{ 'update:visible': [v: boolean] }>();

interface CharCell {
  ch: string;
  wide: boolean;
}

// 与后端 displayWidth 同一条判定规则:码点 > 0x2E80(中日韩与全角标点)按全角计。
const WIDE_FROM = 0x2e80;

const cellCache = new Map<string, CharCell[]>();
// 共享组件长期打开不同票据会不断累积不同行文本,设上限整体清空(命中率不受影响,
// 一次预览的行数远小于上限)。
const CELL_CACHE_MAX = 500;

/** 把一行文本拆成字符单元格(全角 2 格、半角 1 格);按行缓存避免重复拆分。 */
function cellsOf(line: string): CharCell[] {
  const cached = cellCache.get(line);
  if (cached) return cached;
  const cells: CharCell[] = [];
  for (const r of line) {
    cells.push({ ch: r, wide: (r.codePointAt(0) ?? 0) > WIDE_FROM });
  }
  if (cellCache.size >= CELL_CACHE_MAX) cellCache.clear();
  cellCache.set(line, cells);
  return cells;
}
</script>

<style scoped>
.preview-tip {
  color: #999;
  font-size: 12px;
}

.preview-loading {
  padding: 24px 8px;
  color: #999;
  font-size: 13px;
  text-align: center;
  min-width: 200px;
}

.paper-scroll {
  max-height: 70vh;
  overflow-y: auto;
}

.paper-group {
  display: flex;
  flex-direction: column;
  align-items: center;
}

.chunk-label {
  margin: 4px 0;
}

/* 纸带:白底、等宽网格。宽度 = 行宽(半角格数) × 格宽 + 内边距。 */
.ticket-paper {
  --cols: 48;
  --cell: 0.62em; /* 半角格宽;中文占 2 格,字形在格子内居中 */
  font-family: 'Menlo', 'Consolas', 'Courier New', monospace;
  font-size: 14px;
  line-height: 1.5;
  margin: 0;
  padding: 16px 18px 18px;
  background: linear-gradient(180deg, #fdfdfd 0%, #ffffff 12%);
  color: #1a1a1a;
  border: 1px solid #e4e4e4;
  border-radius: 3px;
  /* 双层阴影:贴近感 + 环境光,更像一张平放的纸 */
  box-shadow:
    0 1px 2px rgba(0, 0, 0, 0.08),
    0 6px 20px rgba(0, 0, 0, 0.07);
  width: calc(var(--cell) * var(--cols) + 36px);
  max-width: 100%;
}

/* 每行一个网格行;空行也保留高度(分隔线之间的空隙)。 */
.ticket-line {
  height: 1.5em;
  white-space: nowrap;
  font-weight: 400;
}

/* 加粗行:店名/标题/合计,与 ESC/POS 出纸的 ESC E 重打强调一致。 */
.ticket-line.bold {
  font-weight: 700;
}

/* 字符单元格:半角 1 格、全角 2 格,超出字形裁切不换行 —— 保证任何字体下
   网格对齐都与热敏机一致,后端 pad 出的空格因此精确对位。 */
.cell {
  display: inline-block;
  width: var(--cell);
  overflow: hidden;
  vertical-align: bottom;
  text-align: center;
}

.cell.wide {
  width: calc(var(--cell) * 2);
}

/* 小屏(手机)上缩字号让 80mm 票整行可见,避免横向滚动。 */
@media (max-width: 520px) {
  .ticket-paper {
    font-size: 11px;
    padding: 10px 12px 12px;
  }
}

.paper-meta {
  margin-top: 10px;
  text-align: center;
}
</style>
