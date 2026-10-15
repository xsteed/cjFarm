<template>
  <!-- 批量导出 / 打印 -->
  <t-dialog
    v-model:visible="dialogVisible"
    header="批量导出桌台点餐码"
    width="760px"
    :footer="false"
  >
    <div class="batch-layout">
      <t-form
        class="batch-form"
        :label-width="isMobile ? 'auto' : '104px'"
        :label-align="isMobile ? 'top' : 'right'"
      >
        <t-form-item label="导出范围">
          <t-radio-group v-model="batch.scope">
            <t-radio-button value="all"> 全部桌台（{{ allCount }}） </t-radio-button>
            <t-radio-button value="query"> 当前查询结果（{{ listCount }}） </t-radio-button>
          </t-radio-group>
        </t-form-item>

        <t-form-item label="码类型">
          <t-space
            direction="vertical"
            style="align-items: flex-start"
          >
            <t-radio-group v-model="batch.codeType">
              <t-radio-button value="h5"> H5 二维码（扫开浏览器点餐） </t-radio-button>
              <t-radio-button value="weapp"> 小程序码（扫开小程序） </t-radio-button>
            </t-radio-group>
            <span
              v-if="batch.codeType === 'weapp'"
              class="code-tip"
            >
              小程序码由微信生成：中心是「小程序头像」（在微信公众平台更换即可），因此下方「二维码中心」不再生效
            </span>
          </t-space>
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
            :style="{ width: isMobile ? '100%' : '280px' }"
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
              :disabled="isWeapp || !shopLogo"
            >
              店铺 Logo
            </t-checkbox>
            <t-checkbox
              v-model="batch.useTableNo"
              :disabled="isWeapp || batch.useLogo"
            >
              桌号
            </t-checkbox>
          </t-space>
        </t-form-item>

        <t-form-item label="清晰度">
          <t-select
            v-model="batch.size"
            :options="pixelOptions"
            :style="{ width: isMobile ? '100%' : '240px' }"
          />
        </t-form-item>
      </t-form>

      <!-- 预览:与批量导出走同一条渲染链路,所见即所得 -->
      <aside class="batch-preview">
        <div class="preview-head">
          效果预览
          <span class="preview-badge">样张</span>
        </div>
        <div class="preview-frame">
          <img
            v-if="previewUrl"
            :src="previewUrl"
            class="preview-img"
            alt="桌牌样张预览"
          />
          <div
            v-else-if="previewLoading"
            class="preview-ph"
          >
            <span class="spin"></span>生成中...
          </div>
          <div
            v-else-if="previewError"
            class="preview-ph error"
          >
            {{ previewError }}
          </div>
          <div
            v-else
            class="preview-ph"
          >
            所选范围内没有桌台
          </div>
        </div>
        <div class="preview-label">{{ previewLabel }}</div>
        <div class="preview-size">{{ currentPreset.label }}</div>
        <div class="preview-note">预览与导出结果完全一致；「清晰度」只影响导出分辨率。</div>
      </aside>
    </div>

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
        {{ isWeapp ? '打包下载图片（ZIP）' : '打包下载 PNG（ZIP）' }}
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
import { computed, onUnmounted, reactive, ref, watch } from 'vue';
import { MessagePlugin } from 'tdesign-vue-next';
import { useIsMobile } from '../../utils/useMobile';
import { listTables } from '@dining/api';
import {
  buildCardImagesSheetHtml,
  CARD_PRESETS,
  preparePrintWindow,
  QR_PIXEL_PRESETS,
  renderCardDataUrl,
  writePrintWindow
} from '../../utils/tableQr';
import type { QrCard } from '../../utils/tableQr';
import { dataUrlToBytes, downloadZip } from '../../utils/zip';
import { getTableWeappCode } from '@dining/api';
import type { CheckboxGroupValue, RadioValue } from 'tdesign-vue-next';
import type { TableInfo } from '@dining/types';

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

// 窄屏适配:标签顶置、下拉全宽、单选组换行(见模板与样式)。
const { isMobile } = useIsMobile();

interface BatchState {
  scope: RadioValue;
  // 码类型:h5 = 自己渲染的 H5 二维码(旧桌牌继续用);weapp = 微信生成的小程序码。
  // 默认 h5:存量桌牌都是这个形态,默认值保持与历史行为一致。
  codeType: RadioValue;
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
  codeType: 'h5',
  mode: 'sheet',
  columns: 3,
  cardKey: '60x80',
  size: 900,
  useLogo: true,
  useTableNo: false,
  fields: ['shop', 'tableName', 'tableNo', 'hint', 'cutLine'],
  progress: ''
});

// 是否走小程序码:小程序码的中心由微信固定为「小程序头像」,中心 Logo/桌号一律不适用。
const isWeapp = computed(() => batch.codeType === 'weapp');

const cardOptions = CARD_PRESETS.map(c => ({ value: c.key, label: c.label }));
const pixelOptions = QR_PIXEL_PRESETS.map(p => ({ value: p.key, label: p.label }));
const fieldOptions = [
  { value: 'shop', label: '店铺名称' },
  { value: 'tableName', label: '桌台名称' },
  { value: 'tableNo', label: '桌号' },
  { value: 'hint', label: '底部提示语' },
  { value: 'cutLine', label: '裁剪虚线' }
];

// ============ 样张预览 ============
// 取所选范围内的第一张桌台,按当前选项渲染出一张「所见即所得」的桌牌。
// 与批量导出共用 makeCodeDataUrl,版式/文案/Logo 与导出结果逐像素一致;
// 小程序码也用相同的 width 去请求,后端按「参数指纹」落盘缓存 —— 预览顺手
// 把批量里那张码预热好,不会因此多消耗一次微信额度。
const previewUrl = ref('');
const previewLoading = ref(false);
const previewError = ref('');
const previewTable = ref<TableInfo | null>(null);
// 选项连点会连续触发渲染:用递增 token 作废旧结果,避免慢的那次覆盖新图
let previewToken = 0;

const currentPreset = computed(() => CARD_PRESETS.find(c => c.key === batch.cardKey) || CARD_PRESETS[0]);
const previewLabel = computed(() => {
  const t = previewTable.value;
  return t ? `示例：${t.tableName}（${t.tableNo}桌）` : '示例：—';
});

async function loadPreview(): Promise<void> {
  const params = batch.scope === 'all' ? {} : { tableName: props.queryTableName };
  try {
    // 预览只需要一张桌台:pageSize=1 让后端少查一次,也避免把整页数据拉进内存
    const res = await listTables({ pageNum: 1, pageSize: 1, ...params });
    previewTable.value = res.items?.[0] || null;
  } catch (e) {
    previewTable.value = null;
    previewError.value = e instanceof Error && e.message ? e.message : '桌台信息读取失败';
  }
  await renderPreview();
}

async function renderPreview(): Promise<void> {
  const t = previewTable.value;
  const token = ++previewToken;
  if (!t) {
    previewUrl.value = '';
    previewError.value = '';
    return;
  }
  previewLoading.value = true;
  try {
    const url = await makeCodeDataUrl(t);
    if (token !== previewToken) return;
    previewUrl.value = url;
    previewError.value = '';
  } catch (e) {
    if (token !== previewToken) return;
    previewUrl.value = '';
    // 出码/渲染失败在预览阶段就原样透出(如「小程序页面不存在」):
    // 否则用户要等几百张渲染完才看到报错
    previewError.value = e instanceof Error && e.message ? e.message : '预览生成失败';
  } finally {
    if (token === previewToken) previewLoading.value = false;
  }
}

// 每次打开时重置进度,行为与原 openBatch() 一致;同时渲染一张样张预览。
// 注意:这个 immediate 的 watch 必须排在预览状态(refs)声明之后,
// 否则 setup 阶段同步回调会撞上 const 的暂时性死区。
watch(
  () => props.visible,
  v => {
    if (!v) {
      // 关闭即释放预览图(几百 KB 的 dataURL)并作废在途渲染:
      // 这个弹窗接下来可能一口气生成几百张卡片,内存余量很宝贵
      previewToken++;
      previewUrl.value = '';
      previewTable.value = null;
      previewError.value = '';
      return;
    }
    batch.progress = '';
    previewUrl.value = '';
    previewError.value = '';
    void loadPreview();
  },
  // immediate:弹窗组件常驻挂载(visible 初值 false),这里要覆盖「一挂载就已打开」的场景
  { immediate: true }
);

// 导出范围变了,示例桌台(第一张)也可能变,需要重新取一张
watch(
  () => batch.scope,
  () => {
    if (props.visible) void loadPreview();
  }
);

// 影响卡片外观的选项变化后重渲染预览;180ms 防抖与单张弹窗一致,
// 避免勾选「卡片内容」时每点一下就重画一次。
// batchBusy 时不重渲染:批量生成本身已是 4 路并发,再插一张只会互相拖慢。
let previewTimer: ReturnType<typeof setTimeout> | null = null;
watch(
  () => [batch.cardKey, batch.codeType, batch.size, batch.useLogo, batch.useTableNo, batch.fields.join(',')],
  () => {
    if (!props.visible || batchBusy.value || !previewTable.value) return;
    if (previewTimer) clearTimeout(previewTimer);
    previewTimer = setTimeout(() => {
      void renderPreview();
    }, 180);
  }
);

// 防抖定时器在途时弹窗可能已关闭/组件卸载,清掉避免迟到触发
onUnmounted(() => {
  if (previewTimer) clearTimeout(previewTimer);
});

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
      const dataUrl = await makeCodeDataUrl(t);
      cards[i] = { tableNo: t.tableNo, tableName: t.tableName, dataUrl };
      done++;
      batch.progress = `正在生成 ${done} / ${rows.length} …`;
    }
  }
  await Promise.all(Array.from({ length: Math.min(RENDER_CONCURRENCY, rows.length) }, () => worker()));
  batch.progress = `已生成 ${cards.length} 张二维码`;
  return cards;
}

// 单张码图的来源:一律产出「完整桌牌卡片」(含商家名/扫码提醒/桌号),与单张导出一致。
// 小程序码:后端带 access_token 向微信换取并按桌台码落盘缓存,同一桌台只有第一次
// 真正打微信接口;几十张批量导出时这里是 4 路并发的若干个「本店后端」请求,不是
// 几十次微信调用。任一张失败会整体中断并向用户抛出可操作的错误(如「页面未发布」)。
async function makeCodeDataUrl(t: TableInfo): Promise<string> {
  const preset = CARD_PRESETS.find(c => c.key === batch.cardKey) || CARD_PRESETS[0];
  const width = Number(batch.size) || 900;
  const ratio = preset.h / preset.w;
  const wantShop = batch.fields.includes('shop');
  const wantName = batch.fields.includes('tableName');
  const wantNo = batch.fields.includes('tableNo');
  const wantHint = batch.fields.includes('hint');
  const common = {
    width,
    ratio,
    // 必须显式 scale:1。renderCardCanvas 默认 scale=2,即画布是 width 的两倍
    // (900 → 1800×2400),单张解码位图 17MB。桌牌只有 60~100mm,300dpi 下
    // 700~1200px 已绰绰有余;上百张时多出来的像素会吃掉 GB 级内存,
    // Windows 上打印预览光栅化全部页面时直接卡死(表现就是点了没反应)。
    scale: 1,
    shopName: wantShop ? props.shopTitle : '',
    title: '扫码点餐',
    // 小程序码是「微信扫一扫 → 直接进小程序」,与 H5 码的提示语故意区分开,
    // 免得顾客看到「自助点餐」却不知道会跳到哪里。
    hint: wantHint ? (isWeapp.value ? '微信扫一扫 · 进小程序点餐' : '微信扫码 · 自助点餐') : '',
    tableName: wantName ? t.tableName : '',
    tableNo: wantNo ? t.tableNo : ''
  };
  if (isWeapp.value) {
    const res = await getTableWeappCode(String(t.tableCode || t.tableId), width);
    if (!res.dataUrl) throw new Error('微信未返回小程序码图片');
    return renderCardDataUrl({ ...common, qrImageSrc: res.dataUrl });
  }
  return renderCardDataUrl({
    ...common,
    text: tableQrUrl(t),
    logo: batch.useLogo && props.shopLogo ? props.shopLogo : '',
    centerText: !batch.useLogo && batch.useTableNo ? (t.tableNo ?? '') : ''
  });
}

async function runBatch(kind: 'print' | 'zip'): Promise<void> {
  // 渲染几百张码要几十秒,而 Chrome 的 transient activation 只保留 5 秒:
  // 等渲染完再 window.open 会被当成「非用户触发的弹窗」拦截,压根没有打印页。
  // 因此这里(点击任务内、仍带用户激活)先把窗口开出来,内容就绪后再写进去。
  const printWin = kind === 'print' ? preparePrintWindow() : null;
  if (kind === 'print' && !printWin) {
    MessagePlugin.warning('浏览器拦截了弹窗，请允许弹出窗口后重试，或改用「打包下载 PNG」');
    return;
  }
  batchBusy.value = true;
  batch.progress = '';
  try {
    const cards = await collectCards();
    if (!cards) {
      printWin?.close();
      return;
    }
    if (kind === 'print') {
      const preset = CARD_PRESETS.find(c => c.key === batch.cardKey) || CARD_PRESETS[0];
      const html = buildCardImagesSheetHtml(
        cards.map(c => ({ dataUrl: c.dataUrl, name: `${c.tableNo}_${c.tableName}` })),
        {
          mode: batch.mode as 'sheet' | 'label',
          columns: Number(batch.columns) || 3,
          cardW: preset.w,
          cardH: preset.h,
          gap: 6,
          cutLine: batch.fields.includes('cutLine'),
          shopName: batch.fields.includes('shop') ? props.shopTitle : ''
        }
      );
      if (printWin) writePrintWindow(printWin, html);
      MessagePlugin.success(`已生成 ${cards.length} 张二维码打印页`);
    } else {
      const files = cards.map(c => {
        // 卡片统一为 PNG(H5 码与嵌入小程序码的卡片都经 canvas 导出)
        const ext = c.dataUrl.startsWith('data:image/png') ? 'png' : 'jpg';
        return {
          // 去掉 Windows 文件名不允许的字符
          name: `${c.tableNo}_${c.tableName}.${ext}`.replace(/[\\/:*?"<>|]/g, '_'),
          data: dataUrlToBytes(c.dataUrl)
        };
      });
      const d = new Date();
      const stamp = `${d.getFullYear()}${String(d.getMonth() + 1).padStart(2, '0')}${String(d.getDate()).padStart(2, '0')}`;
      await downloadZip(files, `桌台点餐码_${stamp}.zip`);
      MessagePlugin.success(`已打包 ${cards.length} 张二维码`);
    }
  } catch (e) {
    // 失败时别把开出来的空窗口留在那儿
    printWin?.close();
    MessagePlugin.error(e instanceof Error && e.message ? e.message : '生成失败');
  } finally {
    batchBusy.value = false;
  }
}
</script>

<style scoped>
/* 左侧选项、右侧样张:弹窗从 660px 加宽到 760px 才有足够的两栏宽度 */
.batch-layout {
  display: flex;
  align-items: flex-start;
  gap: 20px;
}

.batch-form {
  flex: 1;

  /* 不加 min-width:0 时,flex 子项会被内容(如 280px 的下拉)撑破并挤出预览栏 */
  min-width: 0;
}

.batch-tip {
  font-size: 12px;
  color: var(--ink-3);
  background: #f7f8fa;
  border-radius: 8px;
  padding: 10px 12px;
  line-height: 1.6;
}

/* ============ 样张预览 ============ */
.batch-preview {
  flex: 0 0 190px;
  width: 190px;
  position: sticky;

  /* 弹窗内容区(t-dialog__body)才是滚动容器,钉住它才能在长表单滚动时始终可见 */
  top: 0;
}

.preview-head {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  font-weight: 600;
  color: var(--ink);
  margin-bottom: 8px;
}

.preview-badge {
  font-size: 11px;
  font-weight: 400;
  color: var(--brand-deep, #f0481f);
  background: rgb(240 72 31 / 10%);
  border-radius: 4px;
  padding: 1px 6px;
}

.preview-frame {
  /* 底色与卡片自身的外框底色一致,预览时看不出「贴纸」的边界 */
  background: #f5f6f8;
  border: 1px solid var(--line);
  border-radius: 10px;
  padding: 8px;
  min-height: 150px;
  display: flex;
  align-items: center;
  justify-content: center;
}

/* 卡片比例跨度很大(50×50 小方标 ~ 90×130 大立牌),按 max-* 缩放而不是定宽定高,
   否则横版会被拉变形、竖版会撑破预览栏 */
.preview-img {
  max-width: 100%;
  max-height: 250px;
  width: auto;
  height: auto;
  display: block;
  border-radius: 8px;
}

.preview-ph {
  color: var(--ink-3);
  font-size: 12px;
  line-height: 1.6;
  text-align: center;
  padding: 8px 4px;
  display: flex;
  align-items: center;
  gap: 6px;
}

.preview-ph.error {
  color: var(--danger);
}

.preview-label {
  font-size: 12px;
  color: var(--ink);
  margin-top: 8px;
}

.preview-size {
  font-size: 12px;
  color: var(--ink-3);
}

.preview-note {
  font-size: 11px;
  color: var(--ink-3);
  line-height: 1.6;
  margin-top: 4px;
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

/* 码类型的说明文字可能折成两行,不能用 mini-label 的 32px 行高 */
.code-tip {
  font-size: 12px;
  color: var(--ink-3);
  line-height: 1.6;
  max-width: 420px;
}

.spin {
  display: inline-block;
  width: 14px;
  height: 14px;
  border: 2px solid var(--line);
  border-top-color: var(--brand);
  border-radius: 50%;
  animation: batchspin 0.8s linear infinite;
}

@keyframes batchspin {
  to {
    transform: rotate(360deg);
  }
}

/* 窄屏适配:让长文案单选组换行、预览改为置顶整宽、底部两个操作按钮竖向铺满,避免溢出与挤压 */
@media (width <= 768px) {
  :deep(.t-radio-group) {
    flex-wrap: wrap;
    gap: 8px;
  }

  /* 窄屏没有并排空间:预览挪到选项上方,改选项时仍能第一时间看到效果 */
  .batch-layout {
    flex-direction: column;
    gap: 14px;
  }

  .batch-preview {
    order: -1;
    flex: none;
    width: 100%;
    position: static;
  }

  .preview-frame {
    padding: 10px;
  }

  .preview-img {
    max-height: 220px;
  }

  .batch-actions {
    flex-direction: column;
    align-items: stretch;
  }
}
</style>
