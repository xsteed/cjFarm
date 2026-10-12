<template>
  <!-- 单张桌台二维码 -->
  <t-dialog
    v-model:visible="dialogVisible"
    :header="qrTitle"
    width="480px"
    :footer="false"
    @close="onQrClose"
  >
    <div class="qr-box">
      <div class="qr-img-wrap">
        <img
          v-if="qrDataUrl"
          :src="qrDataUrl"
          class="qr-img"
          alt="桌台二维码"
        />
        <div
          v-else-if="qrLoading"
          class="qr-placeholder"
        >
          <span class="spin"></span>生成中...
        </div>
        <div
          v-else
          class="qr-placeholder error"
        >
          {{ qrError || '二维码生成失败' }}
        </div>
      </div>

      <div class="qr-meta">
        <div class="qr-table-name">{{ qrTable?.tableName }}（{{ qrTable?.tableNo }}桌）</div>
        <div class="qr-code-line">
          点餐码 <span class="code-chip">{{ qrTable?.tableCode || '-' }}</span>
          <t-button
            variant="text"
            size="small"
            @click="copy(qrTable?.tableCode)"
          >
            复制
          </t-button>
        </div>
        <div
          class="qr-url"
          :title="qrTypeHint"
        >
          {{ qrTypeHint }}
        </div>
        <div
          v-if="isLocalhost"
          class="qr-warn"
        >
          ⚠ 当前为本地地址，手机无法访问。请到「系统配置」把 H5 访问地址改成公网域名或服务器 IP。
        </div>
        <div
          v-if="isTrialCode"
          class="qr-warn"
        >
          ⚠ 当前出的是{{ envLabel }}小程序码，只有体验成员能扫开。印制桌牌前请把「系统配置 →
          小程序出码版本」改回正式版。
        </div>
      </div>

      <div class="qr-opts">
        <t-radio-group v-model="qrType">
          <t-radio-button value="h5"> H5 二维码 </t-radio-button>
          <t-radio-button value="weapp"> 小程序码 </t-radio-button>
        </t-radio-group>
        <t-checkbox
          v-model="qrUseLogo"
          :disabled="isWeapp || !shopLogo"
        >
          中心显示 Logo
        </t-checkbox>
        <t-checkbox
          v-model="qrUseText"
          :disabled="isWeapp || qrUseLogo"
        >
          中心显示桌号
        </t-checkbox>
        <t-select
          v-model="qrSize"
          :options="pixelOptions"
          size="small"
          style="width: 190px"
        />
      </div>
      <div class="qr-card-size">
        <span class="opt-label">卡片尺寸</span>
        <t-select
          v-model="cardKey"
          :options="cardOptions"
          size="small"
          style="width: 220px"
        />
      </div>
      <div
        v-if="!shopLogo"
        class="qr-hint"
      >
        尚未配置店铺 Logo，可到「系统配置 → 店铺 Logo」上传，二维码中间即可显示店铺标志。
      </div>

      <div class="qr-actions">
        <t-button
          theme="primary"
          :disabled="!qrDataUrl"
          @click="downloadQr"
        >
          下载桌牌 PNG
        </t-button>
        <t-button
          variant="outline"
          :loading="bareLoading"
          :disabled="!qrDataUrl"
          @click="downloadQrOnly"
        >
          仅下载二维码
        </t-button>
        <t-button
          variant="outline"
          :disabled="!qrDataUrl"
          @click="printSingle"
        >
          打印
        </t-button>
        <t-button
          variant="outline"
          :disabled="isWeapp"
          @click="copy(qrUrl)"
        >
          复制链接
        </t-button>
      </div>
    </div>
  </t-dialog>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue';
import { MessagePlugin } from 'tdesign-vue-next';
import {
  openPrintWindow,
  renderCardDataUrl,
  renderQrDataUrl,
  QR_PIXEL_PRESETS,
  CARD_PRESETS
} from '../../utils/tableQr';
import { getTableWeappCode } from '@dining/api';
import type { TableInfo } from '@dining/types';

const props = defineProps<{
  visible: boolean;
  row: TableInfo | null;
  h5Base: string;
  shopLogo: string;
  shopTitle: string;
}>();

const emit = defineEmits<{
  'update:visible': [value: boolean];
  copy: [text?: string];
}>();

const escMap: Record<string, string> = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' };
const esc = (s: string): string => s.replace(/[<>&"]/g, c => escMap[c]);

const dialogVisible = computed({
  get: () => props.visible,
  set: (v: boolean) => emit('update:visible', v)
});

const qrLoading = ref(false);
const qrDataUrl = ref('');
const bareLoading = ref(false);
// 小程序码原图缓存:renderSingle 时由后端返回,「仅下载二维码」直接复用,避免重复请求。
const weappCodeUrl = ref('');
const qrUrl = ref('');
const qrError = ref('');
const qrUseLogo = ref(true);
const qrUseText = ref(false);
const qrSize = ref(640);
// 卡片尺寸预设(与批量导出一致):比例 = h/w,使单张与批量打印的卡片形态统一。
const cardKey = ref('60x80');
const cardOptions = CARD_PRESETS.map(c => ({ value: c.key, label: c.label }));
// 码类型。默认 h5:存量桌牌都是这个形态,默认值保持与历史行为一致。
const qrType = ref('h5');
// 出码实际指向的小程序版本,由后端回传;非正式版时必须提醒,否则会把体验版码印到桌牌上。
const qrEnv = ref<'release' | 'trial' | 'develop'>('release');

const qrTable = computed(() => props.row);
const isWeapp = computed(() => qrType.value === 'weapp');
const isTrialCode = computed(() => isWeapp.value && qrEnv.value !== 'release');
const envLabel = computed(() => ({ release: '正式版', trial: '体验版', develop: '开发版' })[qrEnv.value]);
const qrTitle = computed(() => {
  const kind = isWeapp.value ? '桌台小程序码' : '桌台二维码';
  return qrTable.value ? `${kind} - ${qrTable.value.tableName}` : kind;
});
// 小程序码没有「链接」可展示/复制:它的内容由微信封装,这里改为说明它指向哪个版本。
const qrTypeHint = computed(() =>
  isWeapp.value ? `小程序码 · 打开 pages/index/index（${envLabel.value}）` : qrUrl.value
);
const isLocalhost = computed(() => !isWeapp.value && /localhost|127\.0\.0\.1|0\.0\.0\.0/.test(qrUrl.value));

const pixelOptions = QR_PIXEL_PRESETS.map(p => ({ value: p.key, label: p.label }));

// 优先使用桌台稳定码(table_code):创建时生成、永不变更,且不可被枚举遍历。
function tableQrUrl(t: TableInfo): string {
  return `${props.h5Base}/order/${t.tableCode || t.tableId}`;
}

async function renderSingle(): Promise<void> {
  const row = props.row;
  if (!row) return;
  qrLoading.value = true;
  try {
    const preset = CARD_PRESETS.find(c => c.key === cardKey.value) || CARD_PRESETS[0];
    const baseOpts = {
      width: Number(qrSize.value) || 640,
      ratio: preset.h / preset.w,
      shopName: props.shopTitle,
      title: '扫码点餐',
      hint: isWeapp.value ? '微信扫一扫 · 小程序点餐' : '微信扫码 · 自助点餐',
      tableName: row.tableName,
      tableNo: row.tableNo
    };
    if (isWeapp.value) {
      // 小程序码由微信生成、后端缓存;中心是「小程序头像」,因此以图片形式嵌入卡片。
      const res = await getTableWeappCode(String(row.tableCode || row.tableId), Number(qrSize.value) || 640);
      if (!res.dataUrl) throw new Error('微信未返回小程序码图片');
      weappCodeUrl.value = res.dataUrl; // 供「仅下载二维码」复用
      qrDataUrl.value = await renderCardDataUrl({ ...baseOpts, qrImageSrc: res.dataUrl });
      qrEnv.value = res.envVersion || 'release';
    } else {
      qrDataUrl.value = await renderCardDataUrl({
        ...baseOpts,
        text: qrUrl.value,
        logo: qrUseLogo.value && props.shopLogo ? props.shopLogo : '',
        centerText: !qrUseLogo.value && qrUseText.value ? (row.tableNo ?? '') : ''
      });
      qrEnv.value = 'release';
    }
    qrError.value = '';
  } catch (e) {
    qrDataUrl.value = '';
    qrError.value = e instanceof Error && e.message ? e.message : '二维码生成失败';
  } finally {
    qrLoading.value = false;
  }
}

// 打开弹窗 / 切换桌台时重置并重渲染,行为与原 openQr() 保持一致。
watch(
  () => [props.visible, props.row] as const,
  ([visible, row]) => {
    if (!visible || !row) return;
    qrDataUrl.value = '';
    qrError.value = '';
    qrUrl.value = tableQrUrl(row);
    void renderSingle();
  }
);

let renderTimer: ReturnType<typeof setTimeout> | null = null;
watch([qrType, qrUseLogo, qrUseText, qrSize, cardKey], () => {
  if (!props.visible) return;
  if (renderTimer) clearTimeout(renderTimer);
  renderTimer = setTimeout(() => {
    void renderSingle();
  }, 180);
});
// 防抖定时器在途时组件可能被关闭/卸载,清掉避免迟到触发
onUnmounted(() => {
  if (renderTimer) clearTimeout(renderTimer);
});
watch(qrUseLogo, v => {
  if (v) qrUseText.value = false;
});

function onQrClose(): void {
  qrDataUrl.value = '';
  qrUrl.value = '';
  qrError.value = '';
  qrEnv.value = 'release';
  weappCodeUrl.value = '';
}

function downloadQr(): void {
  const row = props.row;
  if (!row || !qrDataUrl.value) return;
  const a = document.createElement('a');
  a.href = qrDataUrl.value;
  // 扩展名跟随图片真实类型:自绘的 H5 码是 PNG,微信回的小程序码是 JPEG。
  // 名字与实际内容不符时,部分看图工具会直接报损坏。
  const ext = qrDataUrl.value.startsWith('data:image/png') ? 'png' : 'jpg';
  a.download = `桌台码_${row.tableNo}_${row.tableName}.${ext}`;
  a.click();
}

// 仅下载裸二维码(不带桌牌卡片装饰):H5 现场生成,小程序码用 renderSingle 缓存的原图。
async function downloadQrOnly(): Promise<void> {
  const row = props.row;
  if (!row) return;
  bareLoading.value = true;
  try {
    let url = '';
    if (isWeapp.value) {
      url = weappCodeUrl.value;
      if (!url) {
        const res = await getTableWeappCode(String(row.tableCode || row.tableId), Number(qrSize.value) || 640);
        if (!res.dataUrl) throw new Error('微信未返回小程序码图片');
        url = res.dataUrl;
        weappCodeUrl.value = url;
      }
    } else {
      url = await renderQrDataUrl(qrUrl.value, {
        size: Number(qrSize.value) || 640,
        logo: qrUseLogo.value && props.shopLogo ? props.shopLogo : '',
        centerText: !qrUseLogo.value && qrUseText.value ? (row.tableNo ?? '') : ''
      });
    }
    const a = document.createElement('a');
    a.href = url;
    const ext = url.startsWith('data:image/png') ? 'png' : 'jpg';
    a.download = `桌台码_${row.tableNo}_${row.tableName}_仅二维码.${ext}`;
    a.click();
  } catch (e) {
    MessagePlugin.error(e instanceof Error && e.message ? e.message : '二维码生成失败');
  } finally {
    bareLoading.value = false;
  }
}

function printSingle(): void {
  const row = props.row;
  if (!row || !qrDataUrl.value) return;
  // 单张已是完整桌牌卡片,直接整图打印到 A4 居中。
  const html = `<!DOCTYPE html><html lang="zh-CN"><head><meta charset="utf-8"><title>桌台码 ${esc(
    row.tableName || ''
  )}</title>
<style>
  * { -webkit-print-color-adjust: exact; print-color-adjust: exact; }
  @page { size: A4; margin: 0; }
  html, body { margin: 0; height: 100%; }
  body { display: flex; align-items: center; justify-content: center; background: #fff; }
  img { width: auto; height: auto; max-width: 88%; max-height: 88%; }
</style></head>
<body><img src="${qrDataUrl.value}" alt="桌台码"></body></html>`;
  if (!openPrintWindow(html)) {
    MessagePlugin.warning('浏览器拦截了弹窗，请改用「下载 PNG」后自行打印');
  }
}

function copy(text?: string): void {
  emit('copy', text);
}
</script>

<style scoped>
.qr-box {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 14px;
  padding: 8px 0 4px;
}

.qr-img-wrap {
  width: 100%;
  max-width: 360px;
  min-height: 200px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: #fff;
  padding: 4px;
}

.qr-img {
  width: 100%;
  height: auto;
  display: block;
  border-radius: 10px;
}

.qr-placeholder {
  color: var(--ink-3);
  font-size: 13px;
  display: flex;
  align-items: center;
  gap: 8px;
}

.qr-placeholder.error {
  color: var(--danger);
}

.qr-meta {
  text-align: center;
  width: 100%;
}

.qr-table-name {
  font-size: 15px;
  font-weight: 600;
  color: var(--ink);
  margin-bottom: 4px;
}

.qr-code-line {
  font-size: 12px;
  color: var(--ink-3);
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  margin-bottom: 4px;
}

.qr-url {
  font-size: 12px;
  color: var(--ink-3);
  word-break: break-all;
}

.qr-warn {
  margin-top: 8px;
  font-size: 12px;
  color: #e6a23c;
  background: #fdf6ec;
  border-radius: 6px;
  padding: 6px 10px;
  text-align: left;
  line-height: 1.5;
}

.qr-opts {
  display: flex;
  align-items: center;
  gap: 16px;
  flex-wrap: wrap;
  justify-content: center;
}

.qr-hint {
  font-size: 12px;
  color: var(--ink-3);
  text-align: center;
  line-height: 1.6;
}

.qr-card-size {
  display: flex;
  align-items: center;
  gap: 8px;
  justify-content: center;
}

.opt-label {
  font-size: 12px;
  color: var(--ink-3);
  line-height: 32px;
}

.qr-actions {
  display: flex;
  gap: 10px;
  /* 4 个按钮合计约 410px：视口 ≤1024px 时弹窗内容区只有约 400px(480px 弹窗
     再扣 TDesign 24px 内边距 + style.css 的 16px 内边距)，手机上仅约 343px。
     不换行就会顶宽 .t-dialog__body(overflow: auto)出现横向滚动条 */
  flex-wrap: wrap;
  justify-content: center;
}

.code-chip {
  display: inline-block;
  font-family: ui-monospace, Menlo, Consolas, monospace;
  font-size: 12px;
  letter-spacing: 1px;
  background: #f3f4f6;
  color: #374151;
  border-radius: 4px;
  padding: 2px 8px;
}

.spin {
  display: inline-block;
  width: 14px;
  height: 14px;
  border: 2px solid var(--line);
  border-top-color: var(--brand);
  border-radius: 50%;
  animation: qrspin 0.8s linear infinite;
}

@keyframes qrspin {
  to {
    transform: rotate(360deg);
  }
}
</style>
