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
            第 {{ i + 1 }} / {{ chunks.length }} 张纸 · 实际出纸时按此顺序逐张送出
          </div>
          <div
            class="ticket-paper"
            :style="{ '--cols': lineWidth }"
          >
            <template
              v-for="(line, li) in chunk"
              :key="li"
            >
              <!-- 二维码行:票据实际出的是打印机生成的二维码图形,这里用同内容
                   的二维码图片还原,字符网格画不出来 -->
              <div
                v-if="line.qr"
                class="ticket-line qr-line"
              >
                <img
                  v-if="qrOf(line.qr)"
                  :src="qrOf(line.qr)"
                  class="qr-img"
                  alt="小票二维码"
                />
                <span
                  v-else
                  class="qr-placeholder"
                  >{{ line.text }}</span
                >
              </div>
              <!-- 放大行(夹单桌号):出纸时是倍宽倍高 + 居中,字符网格已失效,
                   这里直接用大字居中渲染,不按格拆分 -->
              <div
                v-else-if="line.big"
                class="ticket-line big"
              >
                {{ line.text }}
              </div>
              <div
                v-else
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
            </template>
          </div>
        </div>
      </div>
      <!-- 份数与「形态还原」提示:这两件事只靠看纸面是看不出来的 -->
      <div
        v-if="copiesHint || approximate"
        class="preview-note"
      >
        <div v-if="copiesHint">该打印机一次打印 {{ copiesHint }},预览只展示其中一张。</div>
        <div v-if="approximate">
          加菜小票只能还原票据形态:<b>当时新增了哪些菜</b>已无从追溯(订单只增不减、打印日志不存渲染快照),
          此处菜品区按订单<b>当前全部菜品</b>渲染,金额是当前整单合计。
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
import { computed, ref, watch } from 'vue';
import { renderQrDataUrl } from '../utils/tableQr';
import type { TicketLine } from '@dining/types';

const props = defineProps<{
  visible: boolean;
  title: string;
  loading?: boolean;
  /** 分段后的文本行:每段就是实际吐出的一张纸,按先后顺序排列 */
  chunks?: TicketLine[][] | null;
  /** 每行可容纳的半角字符数(58mm=32,80mm=48) */
  lineWidth?: number;
  /** 打印机配置的打印份数;>1 时提示「预览只展示其中一张」 */
  copies?: number;
  /** 为 true 表示形态还原而非内容复现(加菜小票),需要显式说明 */
  approximate?: boolean;
}>();

defineEmits<{ 'update:visible': [v: boolean] }>();

// 份数提示只在真的会多打时才出现(配 1 份时说它是废话)。
const copiesHint = computed(() => {
  const n = props.copies ?? 1;
  return n > 1 ? `${n} 张` : '';
});

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

// ---- 二维码行 ----
// 票据末端的二维码由打印机(GS ( k)或飞鹅云(<QR>)生成,后端只在文本行里给出
// 内容。字符网格画不出二维码,这里按同一内容在前端生成一张图补上,
// 保证「预览即所得」,而不是丢一行占位文字让商家猜。
const qrMap = ref<Record<string, string>>({});

function qrOf(content: string): string {
  return qrMap.value[content] || '';
}

async function ensureQR(content: string): Promise<void> {
  if (qrMap.value[content]) return;
  try {
    // 纠错 H:小票会被折叠、沾油,冗余越高越扫得出;与桌牌二维码同一套参数。
    qrMap.value[content] = await renderQrDataUrl(content, { size: 240, margin: 1 });
  } catch {
    // 生成失败就留占位文本,不阻塞整张预览
  }
}

watch(
  () => props.chunks,
  chunks => {
    if (!chunks) return;
    const todo = new Set<string>();
    for (const ck of chunks) {
      for (const line of ck) {
        if (line.qr) todo.add(line.qr);
      }
    }
    for (const c of todo) void ensureQR(c);
  },
  { immediate: true, deep: true }
);
</script>

<style scoped>
.preview-tip {
  color: #999;
  font-size: 12px;
}

/* 「预览看不出来」的两件事:份数与内容近似。放在纸面下方,不干扰票据本身。 */
.preview-note {
  margin-top: 8px;
  padding: 8px 10px;
  border-radius: 4px;
  background: #fff8e6;
  border: 1px solid #ffe0a3;
  color: #8a5a00;
  font-size: 12px;
  line-height: 1.6;
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
  /* 手机弹窗被 92vw 压窄时,纸面按真实宽度渲染并横向滚动,保证与热敏小票 1:1 不被裁切 */
  overflow-x: auto;
  -webkit-overflow-scrolling: touch;
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
  /* 不被弹窗宽度压窄:保持真实纸宽,由外层 .paper-scroll 横向滚动查看 */
  max-width: none;
  min-width: max-content;
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

/* 放大行:夹单桌号。出纸走 GS ! 倍宽倍高 + 居中(飞鹅通道 <C><B>),
   预览按同样观感放大居中,不再参与字符网格对齐。 */
.ticket-line.big {
  font-weight: 700;
  font-size: 1.7em;
  line-height: 1.3;
  height: auto;
  padding: 2px 0;
  text-align: center;
  white-space: normal;
}

/* 二维码行:热敏机在票尾居中出一枚二维码,预览里同样居中放图。 */
.qr-line {
  display: flex;
  justify-content: center;
  align-items: center;
}

.qr-img {
  width: 40%;
  min-width: 88px;
  max-width: 160px;
  height: auto;
  margin: 4px 0;
  image-rendering: pixelated;
}

/* 二维码生成失败时的兜底:与后端占位文案一致,至少能看出这里会出码。 */
.qr-placeholder {
  color: #999;
  font-size: 12px;
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
