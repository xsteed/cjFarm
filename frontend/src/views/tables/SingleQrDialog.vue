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
          :title="qrUrl"
        >
          {{ qrUrl }}
        </div>
        <div
          v-if="isLocalhost"
          class="qr-warn"
        >
          ⚠ 当前为本地地址，手机无法访问。请到「系统配置」把 H5 访问地址改成公网域名或服务器 IP。
        </div>
      </div>

      <div class="qr-opts">
        <t-checkbox
          v-model="qrUseLogo"
          :disabled="!shopLogo"
        >
          中心显示 Logo
        </t-checkbox>
        <t-checkbox
          v-model="qrUseText"
          :disabled="qrUseLogo"
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
          下载 PNG
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
import { buildPrintSheetHtml, openPrintWindow, renderQrDataUrl, QR_PIXEL_PRESETS } from '../../utils/tableQr';
import type { TableInfo } from '../../types/entities';

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

const dialogVisible = computed({
  get: () => props.visible,
  set: (v: boolean) => emit('update:visible', v)
});

const qrLoading = ref(false);
const qrDataUrl = ref('');
const qrUrl = ref('');
const qrError = ref('');
const qrUseLogo = ref(true);
const qrUseText = ref(false);
const qrSize = ref(640);

const qrTable = computed(() => props.row);
const qrTitle = computed(() => (qrTable.value ? `桌台二维码 - ${qrTable.value.tableName}` : '桌台二维码'));
const isLocalhost = computed(() => /localhost|127\.0\.0\.1|0\.0\.0\.0/.test(qrUrl.value));

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
    qrDataUrl.value = await renderQrDataUrl(qrUrl.value, {
      size: Number(qrSize.value) || 640,
      logo: qrUseLogo.value && props.shopLogo ? props.shopLogo : '',
      centerText: !qrUseLogo.value && qrUseText.value ? (row.tableNo ?? '') : ''
    });
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
watch([qrUseLogo, qrUseText, qrSize], () => {
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
}

function downloadQr(): void {
  const row = props.row;
  if (!row || !qrDataUrl.value) return;
  const a = document.createElement('a');
  a.href = qrDataUrl.value;
  a.download = `桌台码_${row.tableNo}_${row.tableName}.png`;
  a.click();
}

function printSingle(): void {
  const row = props.row;
  if (!row || !qrDataUrl.value) return;
  const card = { tableNo: row.tableNo, tableName: row.tableName, dataUrl: qrDataUrl.value };
  const html = buildPrintSheetHtml([card], {
    mode: 'sheet',
    columns: 1,
    cardW: 90,
    cardH: 120,
    showShop: true,
    shopName: props.shopTitle,
    hint: '微信扫码 · 自助点餐'
  });
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
  width: 260px;
  height: 260px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 1px dashed var(--line);
  border-radius: 10px;
  background: #fff;
  overflow: hidden;
}

.qr-img {
  width: 100%;
  height: 100%;
  object-fit: contain;
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

.qr-actions {
  display: flex;
  gap: 10px;
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
