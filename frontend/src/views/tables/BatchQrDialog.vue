<template>
  <!-- 批量导出 / 打印 -->
  <t-dialog
    v-model:visible="dialogVisible"
    header="批量导出桌台点餐码"
    width="660px"
    :footer="false"
  >
    <t-form
      label-width="104px"
      label-align="right"
    >
      <t-form-item label="导出范围">
        <t-radio-group v-model="batch.scope">
          <t-radio-button value="all"> 全部桌台（{{ allCount }}） </t-radio-button>
          <t-radio-button value="query"> 当前查询结果（{{ listCount }}） </t-radio-button>
        </t-radio-group>
      </t-form-item>

      <t-form-item label="排版方式">
        <t-space
          direction="vertical"
          style="align-items: flex-start"
        >
          <t-radio-group v-model="batch.mode">
            <t-radio-button value="sheet"> A4 网格（打印后裁剪） </t-radio-button>
            <t-radio-button value="label"> 每页一张（标签机 / 已裁切桌牌） </t-radio-button>
          </t-radio-group>
          <t-space v-if="batch.mode === 'sheet'">
            <span class="mini-label">每行张数</span>
            <t-radio-group
              v-model="batch.columns"
              variant="default"
            >
              <t-radio-button :value="2"> 2 </t-radio-button>
              <t-radio-button :value="3"> 3 </t-radio-button>
              <t-radio-button :value="4"> 4 </t-radio-button>
            </t-radio-group>
          </t-space>
        </t-space>
      </t-form-item>

      <t-form-item label="卡片尺寸">
        <t-select
          v-model="batch.cardKey"
          :options="cardOptions"
          style="width: 280px"
        />
      </t-form-item>

      <t-form-item label="卡片内容">
        <t-checkbox-group
          v-model="batch.fields"
          :options="fieldOptions"
        />
      </t-form-item>

      <t-form-item label="二维码中心">
        <t-space>
          <t-checkbox
            v-model="batch.useLogo"
            :disabled="!shopLogo"
          >
            店铺 Logo
          </t-checkbox>
          <t-checkbox
            v-model="batch.useTableNo"
            :disabled="batch.useLogo"
          >
            桌号
          </t-checkbox>
        </t-space>
      </t-form-item>

      <t-form-item label="清晰度">
        <t-select
          v-model="batch.size"
          :options="pixelOptions"
          style="width: 240px"
        />
      </t-form-item>
    </t-form>

    <div class="batch-tip">
      点餐码与桌台绑定且永不变更，一次印制长期有效。打印时请在浏览器打印对话框中关闭「页眉和页脚」，并按所选尺寸设置纸张。
    </div>
    <div
      v-if="batch.progress"
      class="batch-progress"
    >
      {{ batch.progress }}
    </div>

    <div class="batch-actions">
      <t-button
        variant="outline"
        :loading="batchBusy"
        @click="runBatch('zip')"
      >
        打包下载 PNG（ZIP）
      </t-button>
      <t-button
        theme="primary"
        :loading="batchBusy"
        @click="runBatch('print')"
      >
        生成打印页
      </t-button>
    </div>
  </t-dialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue';
import { MessagePlugin } from 'tdesign-vue-next';
import { listTables } from '../../api';
import {
  buildPrintSheetHtml,
  CARD_PRESETS,
  openPrintWindow,
  QR_PIXEL_PRESETS,
  renderQrDataUrl
} from '../../utils/tableQr';
import type { PrintSheetOptions, QrCard } from '../../utils/tableQr';
import { dataUrlToBytes, downloadZip } from '../../utils/zip';
import type { CheckboxGroupValue, RadioValue } from 'tdesign-vue-next';
import type { TableInfo } from '../../types/entities';

const props = defineProps<{
  visible: boolean;
  shopLogo: string;
  shopTitle: string;
  h5Base: string;
  allCount: number;
  listCount: number;
  queryTableName: string | number;
}>();

const emit = defineEmits<{
  'update:visible': [value: boolean];
}>();

const dialogVisible = computed({
  get: () => props.visible,
  set: (v: boolean) => emit('update:visible', v)
});

interface BatchState {
  scope: RadioValue;
  mode: RadioValue;
  columns: RadioValue;
  cardKey: string;
  size: number | string;
  useLogo: boolean;
  useTableNo: boolean;
  fields: CheckboxGroupValue;
  progress: string;
}

const batchBusy = ref(false);
const batch = reactive<BatchState>({
  scope: 'all',
  mode: 'sheet',
  columns: 3,
  cardKey: '60x90',
  size: 900,
  useLogo: true,
  useTableNo: false,
  fields: ['shop', 'tableName', 'tableNo', 'hint', 'cutLine'],
  progress: ''
});

const cardOptions = CARD_PRESETS.map(c => ({ value: c.key, label: c.label }));
const pixelOptions = QR_PIXEL_PRESETS.map(p => ({ value: p.key, label: p.label }));
const fieldOptions = [
  { value: 'shop', label: '店铺名称' },
  { value: 'tableName', label: '桌台名称' },
  { value: 'tableNo', label: '桌号' },
  { value: 'hint', label: '底部提示语' },
  { value: 'cutLine', label: '裁剪虚线' }
];

// 每次打开时重置进度,行为与原 openBatch() 一致。
watch(
  () => props.visible,
  v => {
    if (v) batch.progress = '';
  }
);

// 优先使用桌台稳定码(table_code):创建时生成、永不变更,且不可被枚举遍历。
function tableQrUrl(t: TableInfo): string {
  return `${props.h5Base}/order/${t.tableCode || t.tableId}`;
}

// 后端单页上限 500:循环翻页拉全所选范围,不再有「只导出前 500」的静默截断
const FULL_PAGE_SIZE = 500;
// 二维码渲染并发数:每张约 100-300ms,4 路并发把数百张从串行的 1-3 分钟压到半分钟量级
const RENDER_CONCURRENCY = 4;

async function collectCards(): Promise<QrCard[] | null> {
  const params = batch.scope === 'all' ? {} : { tableName: props.queryTableName };
  const rows: TableInfo[] = [];
  for (let page = 1; ; page++) {
    const res = await listTables({ pageNum: page, pageSize: FULL_PAGE_SIZE, ...params });
    rows.push(...(res.items || []));
    if (rows.length >= res.total) break;
  }
  if (!rows.length) {
    MessagePlugin.warning('所选范围内没有桌台');
    return null;
  }
  const cards = new Array<QrCard>(rows.length);
  let next = 0;
  let done = 0;
  async function worker(): Promise<void> {
    while (next < rows.length) {
      const i = next++;
      const t = rows[i];
      const dataUrl = await renderQrDataUrl(tableQrUrl(t), {
        size: Number(batch.size) || 900,
        logo: batch.useLogo && props.shopLogo ? props.shopLogo : '',
        centerText: !batch.useLogo && batch.useTableNo ? (t.tableNo ?? '') : ''
      });
      cards[i] = { tableNo: t.tableNo, tableName: t.tableName, dataUrl };
      done++;
      batch.progress = `正在生成 ${done} / ${rows.length} …`;
    }
  }
  await Promise.all(Array.from({ length: Math.min(RENDER_CONCURRENCY, rows.length) }, () => worker()));
  batch.progress = `已生成 ${cards.length} 张二维码`;
  return cards;
}

function sheetOpts(): PrintSheetOptions {
  const preset = CARD_PRESETS.find(c => c.key === batch.cardKey) || CARD_PRESETS[0];
  return {
    mode: batch.mode as 'sheet' | 'label',
    columns: Number(batch.columns) || 3,
    cardW: preset.w,
    cardH: preset.h,
    gap: 6,
    showShop: batch.fields.includes('shop'),
    shopName: props.shopTitle,
    showTableName: batch.fields.includes('tableName'),
    showTableNo: batch.fields.includes('tableNo'),
    hint: batch.fields.includes('hint') ? '微信扫码 · 自助点餐' : '',
    cutLine: batch.fields.includes('cutLine')
  };
}

async function runBatch(kind: 'print' | 'zip'): Promise<void> {
  batchBusy.value = true;
  batch.progress = '';
  try {
    const cards = await collectCards();
    if (!cards) return;
    if (kind === 'print') {
      const html = buildPrintSheetHtml(cards, sheetOpts());
      if (!openPrintWindow(html)) {
        MessagePlugin.warning('浏览器拦截了弹窗，请允许弹出窗口后重试，或改用「打包下载 PNG」');
        return;
      }
      MessagePlugin.success(`已生成 ${cards.length} 张二维码打印页`);
    } else {
      const files = cards.map(c => ({
        // 去掉 Windows 文件名不允许的字符
        name: `${c.tableNo}_${c.tableName}.png`.replace(/[\\/:*?"<>|]/g, '_'),
        data: dataUrlToBytes(c.dataUrl)
      }));
      const d = new Date();
      const stamp = `${d.getFullYear()}${String(d.getMonth() + 1).padStart(2, '0')}${String(d.getDate()).padStart(2, '0')}`;
      await downloadZip(files, `桌台点餐码_${stamp}.zip`);
      MessagePlugin.success(`已打包 ${cards.length} 张二维码`);
    }
  } catch (e) {
    MessagePlugin.error(e instanceof Error && e.message ? e.message : '生成失败');
  } finally {
    batchBusy.value = false;
  }
}
</script>

<style scoped>
.batch-tip {
  font-size: 12px;
  color: var(--ink-3);
  background: #f7f8fa;
  border-radius: 8px;
  padding: 10px 12px;
  line-height: 1.6;
}

.batch-progress {
  margin-top: 10px;
  font-size: 13px;
  color: var(--brand-deep, #f0481f);
}

.batch-actions {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  margin-top: 16px;
}

.mini-label {
  font-size: 12px;
  color: var(--ink-3);
  line-height: 32px;
}
</style>
