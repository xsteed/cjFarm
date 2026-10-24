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
    <template v-else-if="hasContent">
      <!-- 多段平铺:每段一张独立的票(打印机管理页同时展示点菜单与结账小票),
           段与段之间留白并用细线分隔;单票场景(打印记录/示例预览)只有一段。 -->
      <div class="preview-sections">
        <section
          v-for="(b, bi) in blocks"
          :key="bi"
          class="preview-section"
        >
          <div
            v-if="b.title || b.note"
            class="preview-section-head"
          >
            <span class="preview-section-title">{{ b.title }}</span>
            <span
              v-if="b.note"
              class="preview-section-note"
              >{{ b.note }}</span
            >
          </div>
          <div class="paper-scroll">
            <div
              v-for="(chunk, i) in b.chunks || []"
              :key="i"
              class="paper-group"
            >
              <div
                v-if="(b.chunks?.length ?? 0) > 1"
                class="preview-tip chunk-label"
              >
                第 {{ i + 1 }} / {{ b.chunks?.length }} 张纸 · 实际出纸时按此顺序逐张送出
              </div>
              <div
                class="ticket-paper"
                :style="{ '--cols': b.lineWidth ?? 48 }"
              >
                <template
                  v-for="(line, li) in chunk"
                  :key="li"
                >
                  <!-- 二维码行:票据实际出的是打印机生成的二维码图形,这里用同内容
                       的二维码图片还原,字符网格画不出来。
                       当前没有票据会产出该行(小票二维码已下线:桌台上有点餐码),
                       但整条链路保留 —— 票尾位置预留给小程序码(见后端
                       print/appendGuestTicketTail 的实现提醒),届时预览同样按图片渲染。 -->
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
                  <!-- 放大行(店名/夹单桌号/整张放大的结账小票):出纸时是倍宽倍高。
                       仍然走同一套字符网格,只是每格与字号一起翻倍(见 .ticket-line.big)——
                       整张票放大后「菜名 —— 金额」的两列对齐全靠 pad 出来的空格,
                       若退化成普通文本流,连续空格会被 HTML 折叠,金额就贴到菜名后面。
                       是否居中看 bigLeft。 -->
                  <div
                    v-else
                    class="ticket-line"
                    :class="{ bold: line.bold, center: line.center, big: line.big, left: line.bigLeft }"
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
          <div
            v-if="(b.copies ?? 1) > 1"
            class="preview-tip chunk-label"
          >
            该打印机一次打印 {{ b.copies }} 张,预览只展示其中一份。
          </div>
        </section>
      </div>
      <!-- 「形态还原」提示:只靠看纸面看不出来,留在纸面下方 -->
      <div
        v-if="approximate"
        class="preview-note"
      >
        <div>
          这条记录没有存下票面快照(升级前打的),加菜小票只能还原票据形态:
          <b>当时新增了哪些菜</b>已无从追溯,此处菜品区按订单<b>当前全部菜品</b>渲染, 金额是当前整单合计。
        </div>
      </div>
      <div
        class="preview-tip paper-meta"
        :class="{ 'paper-meta-stale': metaState === 'replayed' }"
      >
        <slot name="meta">
          <template v-if="metaState === 'snapshot'">
            由后端存下的票面快照回放,与当时实际出纸 1:1;纸宽 {{ paperSize }}。
          </template>
          <template v-else-if="metaState === 'replayed'">
            这条记录是升级前打的,没有存下票面快照;内容按当前订单与配置重新渲染,可能与当时不同。纸宽
            {{ paperSize }}。
          </template>
          <template v-else> 由后端「出纸同一份渲染代码」生成,与实际小票 1:1;纸宽 {{ paperSize }}。 </template>
        </slot>
      </div>
    </template>
  </t-dialog>
</template>

<script setup lang="ts">
// 纸带式票据预览:逐字符按「字符网格」渲染,精确复刻热敏机的对齐模型
// (全角字符占 2 格、半角占 1 格),浏览器字体宽度差异不会造成两列错位。
// 加粗行(店名/标题/合计)与 ESC/POS 出纸的 ESC E 强调一致;放大行
// (GS ! 倍宽倍高 / 飞鹅 <B>)同样走网格,只是每格宽与字号翻倍。
// 打印记录页(回放某条记录当时存下的票面快照)与系统配置页(示例模板预览)共用。
import { computed, ref, watch } from 'vue';
import { renderQrDataUrl } from '../utils/tableQr';
import type { TicketLine, TicketPreviewSection } from '@dining/types';

const props = defineProps<{
  visible: boolean;
  title: string;
  loading?: boolean;
  /** 分段后的文本行:每段就是实际吐出的一张纸,按先后顺序排列 */
  chunks?: TicketLine[][] | null;
  /**
   * 多段平铺:每段是一张独立的票(如顾客小票机的「点菜单」与「结账小票」),
   * 按数组顺序上下排列、段间留白。传了它就忽略 chunks/lineWidth/copies
   * (单票场景继续用那三个参数,调用方无需改动)。
   */
  sections?: TicketPreviewSection[] | null;
  /** 每行可容纳的半角字符数(58mm=32,80mm=48) */
  lineWidth?: number;
  /** 打印机配置的打印份数;>1 时提示「预览只展示其中一张」 */
  copies?: number;
  /**
   * 内容是否来自台账存下的票面快照。
   * 为真(常态):回放,与实际出纸一字不差,底部文案据实说明「快照回放」;
   * 为假:升级前的老记录没有快照,按当前数据重渲染,文案要明确提示可能已与当时不同。
   */
  fromSnapshot?: boolean;
  /** 为 true 表示形态还原而非内容复现(加菜小票),需要显式说明 */
  approximate?: boolean;
}>();

defineEmits<{ 'update:visible': [v: boolean] }>();

// 纸宽文案:58mm 与 80mm 的行宽唯一确定,不给调用方再传一个「58mm/80mm」参数的口子。
const paperSize = computed(() => (props.lineWidth === 32 ? '58mm' : '80mm'));

// 底部说明的三种口径,由 fromSnapshot 区分:
//   true    —— 按台账存下的票面快照回放,与当时出纸一字不差;
//   false   —— 升级前的老记录没有快照,按当前订单与配置重渲染,必须显式提示可能不同;
//   undefined —— 调用方不涉及历史记录(示例模板 / 测试页预览),用中性文案。
// 不用「默认当成老记录」:那样示例预览会莫名背上「升级前」的告示。
const metaState = computed<'snapshot' | 'replayed' | 'plain'>(() => {
  if (props.fromSnapshot === true) return 'snapshot';
  if (props.fromSnapshot === false) return 'replayed';
  return 'plain';
});

// 渲染用的分段:多段平铺直接用 sections;单票场景包成一段,
// 让模板只有一条渲染路径(少一份重复的纸面标记,也就少一处会漂移的地方)。
const blocks = computed<TicketPreviewSection[]>(() => {
  if (props.sections?.length) return props.sections;
  return [
    {
      chunks: props.chunks ?? null,
      lineWidth: props.lineWidth,
      copies: props.copies
    }
  ];
});

// 没有任何一段有内容时不画空纸(加载失败/后端返回空数据)。
const hasContent = computed(() => blocks.value.some(b => (b.chunks?.length ?? 0) > 0));

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
  // 必须监听渲染用的 blocks(它汇总了 sections 与 chunks 两种入参):只监听 props.chunks
  // 会让 sections 模式(Printers.vue 的票据预览正用这种方式传参)永远拿不到二维码 ——
  // 那种模式下 props.chunks 恒为 null,watcher 一次都不会触发,占位文案顶掉二维码。
  () => blocks.value,
  sections => {
    const todo = new Set<string>();
    for (const sec of sections) {
      for (const ck of sec.chunks ?? []) {
        for (const line of ck) {
          if (line.qr) todo.add(line.qr);
        }
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

/* 多段平铺:打印机管理页把点菜单与结账小票同时铺开。
   段与段之间留白 + 一条细虚线,一眼能认出是两张不同的票而不是同一张的两页。 */
.preview-section + .preview-section {
  margin-top: 26px;
  padding-top: 20px;
  border-top: 1px dashed #e4e4e4;
}

.preview-section-head {
  display: flex;
  align-items: baseline;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 8px;
}

.preview-section-title {
  font-size: 13px;
  font-weight: 600;
  color: #333;
}

.preview-section-note {
  font-size: 12px;
  color: #999;
}

/* 多段时整块滚动(两段各自 70vh 会让弹窗高到不可用),
   横向仍由 .paper-scroll 负责,窄屏下能左右滑看完整纸宽。 */
.preview-sections {
  max-height: 74vh;
  overflow-y: auto;
}

.preview-section .paper-scroll {
  max-height: none;
  overflow: visible auto;
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
  overflow: auto;

  /* 手机弹窗被 92vw 压窄时,纸面按真实宽度渲染并横向滚动,保证与热敏小票 1:1 不被裁切 */
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

/* 纸带:白底、等宽网格。内容区宽 = 行宽(半角格数) × 格宽,与出纸行宽严格一致;
   内边距与边框在盒模型外叠加(content-box),不占格子 —— 移动端媒体查询改内边距
   也不会挤占网格。 */
.ticket-paper {
  --cols: 48;
  --cell: 0.62em; /* 半角格宽;中文占 2 格,字形在格子内居中 */

  font-family: Menlo, Consolas, 'Courier New', monospace;
  font-size: 14px;
  line-height: 1.5;
  margin: 0;
  padding: 16px 18px 18px;
  background: linear-gradient(180deg, #fdfdfd 0%, #fff 12%);
  color: #1a1a1a;
  border: 1px solid #e4e4e4;
  border-radius: 3px;

  /* 双层阴影:贴近感 + 环境光,更像一张平放的纸 */
  box-shadow:
    0 1px 2px rgb(0 0 0 / 8%),
    0 6px 20px rgb(0 0 0 / 7%);

  /* 内容区 = 行宽 × 格宽(48 格即 48 格,一行不多一行不少)。
     曾把内边距 36px 也加进 width:默认 content-box 下那 36px 全落进内容区,
     网格变成 52 格 —— 左对齐的细线只占左边 48 格、右边空 4 格,而机器居中的
     粗线在 52 格里居中、整体右移约 2 格,同一张票上两种行因此错位。
     min-width: max-content 兜底防撑破:任何行都不该超过 48 格。 */
  width: calc(var(--cell) * var(--cols));

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

/* 机器居中行:单据名副标题(【顾客小票】等)。出纸走 ESC a 1 / 飞鹅 <C>,
   预览按同样观感居中,不再依赖补位空格的字符网格对齐。 */
.ticket-line.center {
  text-align: center;
}

/* 放大行:店名/夹单桌号/菜品行/整张放大的结账小票。
   出纸走 GS ! 倍宽倍高(飞鹅 <C><B> 或 <B>);预览仍按字符网格渲染,
   只是字号与格宽一起翻倍 —— 格宽 0.62em 是相对本行字号算的,
   2em 下每格正好变成 2 格宽,24 个半角字仍精确铺满 48 格纸。
   这也是「整张小票放大」在预览里两列仍对齐的前提:pad 出的连续空格
   按格子逐个占位;若改用普通文本流,HTML 会把连续空格折叠成一个。 */
.ticket-line.big {
  font-weight: 700;
  font-size: 2em;
  line-height: 1.25;

  /* 高度沿用 .ticket-line 的 1.5em(相对 2em 字号 = 两倍行高):
     空行也占位,和出纸时空行仍走一行纸的动作一致。 */
  padding: 2px 0;
  text-align: center;
}

/* 左对齐放大:整单厨房单的菜品清单(逐条编号)。出纸时编号在行首对齐,
   预览若跟着居中,后厨按预览核对的观感就和出纸对不上了。 */
.ticket-line.big.left {
  text-align: left;
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
@media (width <= 520px) {
  .ticket-paper {
    font-size: 11px;
    padding: 10px 12px 12px;
  }
}

.paper-meta {
  margin: 10px auto 0;

  /* 弹窗是 width:fit-content,meta 不限宽的话一行长文案会把整个弹窗撑到超出屏幕;
     限宽后长说明在此折行,弹窗宽度始终由纸面决定。 */
  max-width: min(560px, 86vw);
  text-align: center;
  word-break: break-word;
}

/* 没有票面快照的老记录:重渲染的结果可能已与当时不同,这句提示必须被看见,
   不能和「与当时实际出纸 1:1」的常态文案长一个颜色。 */
.paper-meta-stale {
  color: #ed7b2f;
}
</style>
