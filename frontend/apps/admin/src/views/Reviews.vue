<template>
  <div>
    <!-- 汇总卡:均分大数字 + 评分分布 + 差评率。与列表同一筛选条件(近 7 日均分除外,
         它始终是全局趋势锚点,不受筛选影响) -->
    <div class="stat-grid">
      <div class="stat-card acc">
        <div class="stat-label">平均评分</div>
        <div class="stat-value review-score">
          <span class="score-num">{{ summary.avgScore?.toFixed(2) || '0.00' }}</span>
          <span class="score-stars">{{ starText(summary.avgScore || 0) }}</span>
        </div>
        <div class="stat-sub">共 {{ summary.total || 0 }} 条评价</div>
      </div>
      <div class="stat-card">
        <div class="stat-label">差评率(1-2星)</div>
        <div class="stat-value">{{ badRateText }}</div>
        <div class="stat-sub">{{ summary.badCount || 0 }} 条差评</div>
      </div>
      <div class="stat-card">
        <div class="stat-label">近 7 日均分</div>
        <div class="stat-value">{{ summary.last7Avg?.toFixed(2) || '—' }}</div>
        <div class="stat-sub">与 {{ summary.avgScore?.toFixed(2) || '—' }} 全期对比看趋势</div>
      </div>
      <div class="stat-card">
        <div class="stat-label">评分分布</div>
        <div class="dist-rows">
          <div
            v-for="n in [5, 4, 3, 2, 1]"
            :key="n"
            class="dist-row"
          >
            <span class="dist-star">{{ n }}★</span>
            <div class="dist-bar">
              <div
                class="dist-fill"
                :style="{ width: distPercent(n) + '%' }"
              ></div>
            </div>
            <span class="dist-n">{{ summary.dist?.[String(n)] || 0 }}</span>
          </div>
        </div>
      </div>
    </div>

    <!-- 标签云:差在哪/好在哪一眼可见 -->
    <div
      v-if="summary.tags?.length"
      class="page-card"
    >
      <div class="tag-cloud">
        <span
          v-for="t in summary.tags"
          :key="t.name"
          class="tag-chip"
          :class="{ bad: BAD_TAG_SET.has(t.name || '') }"
          @click="onTagClick(t.name || '')"
          >{{ t.name }} <b>{{ t.count }}</b></span
        >
      </div>
    </div>

    <div class="page-card">
      <div
        v-if="!isMobile"
        class="toolbar"
      >
        <div class="toolbar-left">
          <t-date-range-picker
            v-model="dateRange"
            allow-input
            clearable
            style="width: 240px"
            @change="onSearch"
          />
          <t-select
            v-model="ratingTab"
            style="width: 120px"
            @change="onSearch"
          >
            <t-option
              value="all"
              label="全部评分"
            />
            <t-option
              value="good"
              label="好评 4-5 星"
            />
            <t-option
              value="mid"
              label="中评 3 星"
            />
            <t-option
              value="bad"
              label="差评 1-2 星"
            />
          </t-select>
          <t-checkbox v-model="hasContent">仅看有文字</t-checkbox>
          <t-select
            v-model="imagesTab"
            style="width: 120px"
            @change="onSearch"
          >
            <t-option
              value="all"
              label="全部图片"
            />
            <t-option
              value="with"
              label="仅看有图"
            />
            <t-option
              value="without"
              label="仅看无图"
            />
          </t-select>
          <t-input
            v-model="keyword"
            placeholder="内容 / 标签 / 桌号"
            clearable
            style="width: 180px"
            @enter="onSearch"
          />
          <t-button
            theme="primary"
            @click="onSearch"
          >
            <template #icon>
              <search-icon />
            </template>
            查询
          </t-button>
        </div>
      </div>

      <!-- 移动端筛选条 -->
      <div
        v-else
        class="m-toolbar"
      >
        <t-select
          v-model="ratingTab"
          style="width: 130px"
          @change="onSearch"
        >
          <t-option
            value="all"
            label="全部评分"
          />
          <t-option
            value="good"
            label="好评 4-5 星"
          />
          <t-option
            value="mid"
            label="中评 3 星"
          />
          <t-option
            value="bad"
            label="差评 1-2 星"
          />
        </t-select>
        <t-checkbox v-model="hasContent">有文字</t-checkbox>
        <t-select
          v-model="imagesTab"
          style="width: 110px"
          @change="onSearch"
        >
          <t-option
            value="all"
            label="全部图片"
          />
          <t-option
            value="with"
            label="有图"
          />
          <t-option
            value="without"
            label="无图"
          />
        </t-select>
      </div>

      <t-table
        v-if="!isMobile"
        row-key="reviewId"
        :data="list"
        :columns="columns"
        :loading="loading"
        :pagination="pagination"
        hover
        @page-change="onPageChange"
      >
        <template #customer="{ row }">
          <span :class="{ muted: row.customerType === 'anonymous' }">{{ customerText(row) }}</span>
        </template>
        <template #time="{ row }">
          <span class="row-time">{{ row.createTime }}</span>
        </template>
        <template #rating="{ row }">
          <span
            class="row-stars"
            :class="{ bad: Number(row.rating) <= 2 }"
            >{{ starText(Number(row.rating) || 0) }}</span
          >
        </template>
        <template #tags="{ row }">
          <span
            v-for="t in row.tags"
            :key="t"
            class="row-tag"
            :class="{ bad: BAD_TAG_SET.has(t) }"
            >{{ t }}</span
          >
          <span
            v-if="!row.tags?.length"
            class="muted"
            >—</span
          >
        </template>
        <template #content="{ row }">
          <span :class="{ muted: !row.content }">{{ row.content || '—' }}</span>
        </template>
        <template #orderNo="{ row }">
          <t-link
            theme="primary"
            hover="color"
            @click="openReviewDetail(row)"
            >#{{ row.shortNo || row.orderNo }}</t-link
          >
        </template>
        <template #op="{ row }">
          <t-link
            theme="primary"
            hover="color"
            @click="openOrder(row)"
            >订单</t-link
          >
          <t-popconfirm
            content="删除后不可恢复，确认删除这条评价？"
            @confirm="onDelete(row)"
          >
            <t-link
              theme="danger"
              hover="color"
              style="margin-left: 8px"
              >删除</t-link
            >
          </t-popconfirm>
        </template>
      </t-table>

      <!-- 移动端卡片列表 -->
      <div
        v-else
        class="m-list"
      >
        <div
          v-for="row in list"
          :key="row.reviewId"
          class="m-card"
        >
          <div class="m-card-head">
            <span
              class="row-stars"
              :class="{ bad: Number(row.rating) <= 2 }"
              >{{ starText(Number(row.rating) || 0) }}</span
            >
            <span class="m-time">{{ row.createTime }}</span>
          </div>
          <div
            v-if="row.content"
            class="m-content"
          >
            {{ row.content }}
          </div>
          <div
            v-if="row.images?.length"
            class="m-imgs"
          >
            <a
              v-for="u in row.images"
              :key="u"
              :href="u"
              target="_blank"
            >
              <img
                :src="u"
                alt="评价图片"
                loading="lazy"
              />
            </a>
          </div>
          <div
            v-if="row.tags?.length"
            class="m-tags"
          >
            <span
              v-for="t in row.tags"
              :key="t"
              class="row-tag"
              :class="{ bad: BAD_TAG_SET.has(t) }"
              >{{ t }}</span
            >
          </div>
          <div class="m-meta">
            <span
              class="m-no"
              @click="openReviewDetail(row)"
              >#{{ row.shortNo || row.orderNo }} · {{ row.tableName || row.tableNo }} · {{ row.personCount }}人</span
            >
            <span>{{ customerText(row) }}</span>
          </div>
          <div class="m-ops">
            <t-button
              size="small"
              variant="outline"
              @click="openOrder(row)"
              >看订单</t-button
            >
            <t-button
              v-if="canEdit"
              size="small"
              variant="outline"
              theme="danger"
              @click="onDeleteMobile(row)"
              >删除</t-button
            >
          </div>
        </div>
        <div
          v-if="!list.length && !loading"
          class="m-empty"
        >
          还没有评价
        </div>
        <div class="m-pager">
          <t-button
            size="small"
            variant="outline"
            :disabled="pagination.current <= 1"
            @click="goPage(pagination.current - 1)"
            >上一页</t-button
          >
          <span class="m-page-no">{{ pagination.current }} / {{ Math.max(1, Math.ceil(pagination.total / pagination.pageSize)) }}</span>
          <t-button
            size="small"
            variant="outline"
            :disabled="pagination.current >= Math.ceil(pagination.total / pagination.pageSize)"
            @click="goPage(pagination.current + 1)"
            >下一页</t-button
          >
        </div>
      </div>
    </div>

    <!-- 评价详情弹层:内容全文 + 图片大图 + 顾客信息;订单号列点击打开 -->
    <t-dialog
      v-model:visible="reviewDetailVisible"
      :header="reviewDetailHeader"
      :footer="false"
      width="520px"
    >
      <div
        v-if="reviewDetail"
        class="rd-body"
      >
        <div class="rd-stars">
          <span
            class="rd-star-big"
            :class="{ bad: Number(reviewDetail.rating) <= 2 }"
            >{{ starText(Number(reviewDetail.rating) || 0) }}</span
          >
          <span class="rd-time">{{ reviewDetail.createTime }}</span>
        </div>
        <div
          v-if="reviewDetail.tags?.length"
          class="m-tags"
        >
          <span
            v-for="t in reviewDetail.tags"
            :key="t"
            class="row-tag"
            :class="{ bad: BAD_TAG_SET.has(t) }"
            >{{ t }}</span
          >
        </div>
        <p
          v-if="reviewDetail.content"
          class="rd-content"
        >
          {{ reviewDetail.content }}
        </p>
        <p
          v-else
          class="rd-content muted"
        >
          未填写文字
        </p>
        <div
          v-if="reviewDetail.images?.length"
          class="rd-imgs"
        >
          <a
            v-for="u in reviewDetail.images"
            :key="u"
            :href="u"
            target="_blank"
            title="点击查看原图"
          >
            <img
              :src="u"
              alt="评价图片"
              loading="lazy"
            />
          </a>
        </div>
        <div class="rd-info">
          <div class="rd-info-row">
            <span>桌台</span><span>{{ reviewDetail.tableName || reviewDetail.tableNo || '—' }} · {{ reviewDetail.personCount }} 人</span>
          </div>
          <div class="rd-info-row">
            <span>顾客</span><span>{{ customerText(reviewDetail) }}</span>
          </div>
          <div class="rd-info-row">
            <span>营业日</span><span>{{ reviewDetail.bizDate || '—' }}</span>
          </div>
        </div>
        <div class="rd-foot">
          <t-button
            variant="outline"
            @click="openOrder(reviewDetail)"
            >查看订单详情</t-button
          >
          <t-button
            v-if="canEdit"
            theme="danger"
            variant="outline"
            @click="onDeleteFromDetail"
            >删除该评价</t-button
          >
        </div>
      </div>
    </t-dialog>

    <!-- 订单详情弹层:评价的上下文(吃了什么、金额、经办人)在这里看 -->
    <OrderDetailDialog
      v-model:visible="detailVisible"
      :order-id="detailOrderId"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import { DialogPlugin, MessagePlugin } from 'tdesign-vue-next';
import { SearchIcon } from 'tdesign-icons-vue-next';
import { listReviews, deleteReview, getOrder } from '@dining/api';
import { hasPerm } from '@dining/auth';
import { useIsMobile } from '../utils/useMobile';
import OrderDetailDialog from '../components/OrderDetailDialog.vue';
import type { Review, ReviewSummary } from '@dining/types';
import type { PageInfo } from 'tdesign-vue-next';

// 标签好坏分组与两端评价弹层的固定两组同源(前端云着色用,不做过滤依据)
const BAD_TAG_SET = new Set(['不合口味', '上菜慢', '分量少', '服务差', '环境差', '不够干净', '偏贵']);

const list = ref<Review[]>([]);
const loading = ref(false);
const summary = ref<ReviewSummary>({});
const pagination = reactive({ current: 1, pageSize: 10, total: 0 });
const { isMobile } = useIsMobile();

const dateRange = ref<[string, string] | null>(null);
const ratingTab = ref('all');
const hasContent = ref(false);
const imagesTab = ref<'all' | 'with' | 'without'>('all');
const keyword = ref('');
// 标签点击 = 快捷关键词筛选(与后端 keyword 的标签匹配同口径)
const canEdit = computed(() => hasPerm('review:edit'));

const ratingMin = computed(() => (ratingTab.value === 'good' ? 4 : ratingTab.value === 'mid' ? 3 : ratingTab.value === 'bad' ? 1 : undefined));
const ratingMax = computed(() => (ratingTab.value === 'good' ? 5 : ratingTab.value === 'mid' ? 3 : ratingTab.value === 'bad' ? 2 : undefined));

function starText(score: number): string {
  const n = Math.round(Number(score) || 0);
  return '★'.repeat(n) + '☆'.repeat(5 - n);
}

const badRateText = computed(() => {
  const r = Number(summary.value.badRate) || 0;
  return (r * 100).toFixed(0) + '%';
});

function distPercent(n: number): number {
  const total = Number(summary.value.total) || 0;
  if (!total) return 0;
  return Math.round(((summary.value.dist?.[String(n)] || 0) / total) * 100);
}

function customerText(row: Review): string {
  if (row.customerType === 'anonymous') return '匿名顾客';
  if (row.customerType === 'deleted') return `已注销会员${row.customerId ? ' #' + row.customerId : ''}`;
  const phone = row.phone ? ' ' + String(row.phone).replace(/^(\d{3})\d{4}(\d{4})$/, '$1****$2') : '';
  return (row.nickname || '会员') + phone;
}

async function load(): Promise<void> {
  loading.value = true;
  try {
    const [startDay, endDay] = dateRange.value || ['', ''];
    const res = await listReviews({
      pageNum: pagination.current,
      pageSize: pagination.pageSize,
      startDay: startDay || undefined,
      endDay: endDay || undefined,
      ratingMin: ratingMin.value,
      ratingMax: ratingMax.value,
      hasContent: hasContent.value || undefined,
      hasImages: imagesTab.value === 'all' ? undefined : imagesTab.value === 'with' ? '1' : '0',
      keyword: keyword.value || undefined
    });
    list.value = res.items || [];
    pagination.total = res.total || 0;
    summary.value = res.summary || {};
  } finally {
    loading.value = false;
  }
}

function onSearch(): void {
  pagination.current = 1;
  void load();
}

function onPageChange(info: PageInfo): void {
  pagination.current = info.current || 1;
  pagination.pageSize = info.pageSize || 10;
  void load();
}

function goPage(p: number): void {
  pagination.current = p;
  void load();
}

function onTagClick(name: string): void {
  keyword.value = name;
  onSearch();
}

// ---- 评价详情弹层(订单号列点击打开) ----
const reviewDetailVisible = ref(false);
const reviewDetail = ref<Review | null>(null);
const reviewDetailHeader = computed(() =>
  reviewDetail.value ? `评价详情 · #${reviewDetail.value.shortNo || reviewDetail.value.orderNo}` : '评价详情'
);

function openReviewDetail(row: Review): void {
  reviewDetail.value = row;
  reviewDetailVisible.value = true;
}

function onDeleteFromDetail(): void {
  if (!reviewDetail.value) return;
  const row = reviewDetail.value;
  reviewDetailVisible.value = false;
  onDelete(row);
}

// ---- 订单详情弹层 ----
const detailVisible = ref(false);
const detailOrderId = ref<number | null>(null);

async function openOrder(row: Review): Promise<void> {
  // 评价行只带 order_id;详情弹层按 ID 拉全量(菜品/金额/经办)。
  if (!row.orderId) return;
  detailOrderId.value = row.orderId;
  detailVisible.value = true;
}

// ---- 删除(物理删除 + 操作审计留痕;删除前先取原文供审计,由后端负责) ----
function onDelete(row: Review): void {
  void deleteReview(row.reviewId!).then(() => {
    MessagePlugin.success('已删除');
    void load();
  });
}

function onDeleteMobile(row: Review): void {
  const dialog = DialogPlugin.confirm({
    header: '删除评价',
    body: '删除后不可恢复，确认删除这条评价？',
    confirmBtn: '删除',
    onConfirm: () => {
      onDelete(row);
      dialog.destroy();
    }
  });
}

// 桌面表格列:cell 用 h 渲染 customerId 列省略 —— 顾客列/星级/标签/操作都走插槽
// 订单号第一列且为链接色:评价详情(含图片大图)从这里点开,不再单独设图片列
const columns = [
  { colKey: 'shortNo', title: '订单号', width: 130, cell: 'orderNo' },
  { colKey: 'createTime', title: '时间', width: 170, cell: 'time' },
  { colKey: 'tableName', title: '桌台', width: 100, cell: (_h: unknown, { row }: { row: Review }) => row.tableName || row.tableNo || '—' },
  { colKey: 'personCount', title: '人数', width: 60 },
  { colKey: 'rating', title: '评分', width: 120, cell: 'rating' },
  { colKey: 'tags', title: '标签', cell: 'tags' },
  { colKey: 'content', title: '内容', ellipsis: true, cell: 'content' },
  { colKey: 'customer', title: '顾客', width: 160, cell: 'customer' },
  { colKey: 'op', title: '操作', width: 132, cell: 'op' }
];
void h;

onMounted(load);
</script>

<style scoped>
/* 卡片纵向节奏:与 Credit 页同款 16px,三个区块(汇总卡/标签云/列表)统一间距 */
.page-card {
  margin-top: 16px;
}

.stat-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 16px;
}

@media (max-width: 768px) {
  .stat-grid {
    grid-template-columns: repeat(2, 1fr);
  }
}

.stat-card {
  background: #fff;
  border-radius: var(--r-md, 9px);
  padding: 14px 16px;
  box-shadow: var(--shadow-1, 0 1px 4px rgb(0 0 0 / 6%));
}

.stat-card.acc {
  background: linear-gradient(135deg, var(--brand, #e85d3d), var(--brand-deep, #c94a2e));
  color: #fff;
}

.stat-card.acc .stat-label,
.stat-card.acc .stat-sub {
  color: rgb(255 255 255 / 80%);
}

.stat-label {
  font-size: 12px;
  color: var(--ink-3, #999);
}

.stat-value {
  font-size: 22px;
  font-weight: 700;
  margin: 4px 0 2px;
}

.stat-sub {
  font-size: 12px;
  color: var(--ink-3, #999);
}

.review-score {
  display: flex;
  align-items: baseline;
  gap: 8px;
}

.score-num {
  font-size: 26px;
}

.score-stars {
  font-size: 13px;
  color: #ffd166;
  letter-spacing: 1px;
}

.dist-rows {
  display: flex;
  flex-direction: column;
  gap: 3px;
  margin-top: 2px;
}

.dist-row {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 11px;
  color: var(--ink-3, #999);
}

.dist-star {
  width: 26px;
  flex-shrink: 0;
}

.dist-bar {
  flex: 1;
  height: 6px;
  background: #f0f0f0;
  border-radius: 3px;
  overflow: hidden;
}

.dist-fill {
  height: 100%;
  background: #ffb400;
  border-radius: 3px;
}

.dist-n {
  width: 24px;
  text-align: right;
}

/* 标签云 */
.tag-cloud {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.tag-chip {
  padding: 5px 14px;
  border-radius: 999px;
  background: #fff5f1;
  color: var(--brand-deep, #c94a2e);
  font-size: 12.5px;
  cursor: pointer;
}

.tag-chip b {
  margin-left: 2px;
}

.tag-chip.bad {
  background: #fdecec;
  color: var(--danger, #d54941);
}

/* 时间列:单行不折行(170px 已容下 YYYY-MM-DD HH:mm:ss) */
.row-time {
  white-space: nowrap;
}

/* 行内元素 */
.row-stars {
  color: #ffb400;
  letter-spacing: 1px;
  font-size: 13px;
}

.row-stars.bad {
  color: var(--danger, #d54941);
}

.row-tag {
  display: inline-block;
  padding: 1px 8px;
  margin-right: 4px;
  border-radius: 999px;
  background: #fff5f1;
  color: var(--brand-deep, #c94a2e);
  font-size: 11px;
  line-height: 1.8;
}

.row-tag.bad {
  background: #fdecec;
  color: var(--danger, #d54941);
}

.muted {
  color: var(--ink-4, #bbb);
}

/* 评价详情弹层 */
.rd-body {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.rd-stars {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.rd-star-big {
  font-size: 20px;
  letter-spacing: 2px;
  color: #ffb400;
}

.rd-star-big.bad {
  color: var(--danger, #d54941);
}

.rd-time {
  font-size: 12px;
  color: var(--ink-4, #bbb);
}

.rd-content {
  margin: 0;
  font-size: 14px;
  line-height: 1.7;
  color: var(--ink, #333);
  white-space: pre-wrap;
}

.rd-imgs {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.rd-imgs img {
  width: 120px;
  height: 120px;
  object-fit: cover;
  border-radius: 8px;
  border: 1px solid var(--line, #eee);
  display: block;
}

.rd-info {
  border-top: 1px solid var(--line, #eee);
  padding-top: 10px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.rd-info-row {
  display: flex;
  justify-content: space-between;
  font-size: 13px;
  color: var(--ink-2, #4a4a4a);
}

.rd-info-row span:first-child {
  color: var(--ink-3, #999);
}

.rd-foot {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  padding-top: 4px;
}

/* 移动卡片订单号:链接色提示可点 */
.m-no {
  color: var(--td-brand-color, #0052d9);
  cursor: pointer;
}

.m-no:active {
  opacity: 0.7;
}

.m-imgs {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
  margin-top: 8px;
}

.m-imgs img {
  width: 88px;
  height: 88px;
  object-fit: cover;
  border-radius: 8px;
  border: 1px solid var(--line, #eee);
  display: block;
}

.toolbar {
  display: flex;
  justify-content: space-between;
  margin-bottom: 12px;
  flex-wrap: wrap;
  gap: 8px;
}

.toolbar-left {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

/* 移动端 */
.m-toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 10px;
}

.m-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.m-card {
  border: 1px solid var(--line, #eee);
  border-radius: var(--r-md, 9px);
  padding: 12px;
}

.m-card-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.m-time {
  font-size: 11px;
  color: var(--ink-4, #bbb);
}

.m-content {
  margin-top: 6px;
  font-size: 13.5px;
  color: var(--ink, #333);
  line-height: 1.6;
}

.m-tags {
  margin-top: 6px;
}

.m-meta {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  margin-top: 8px;
  font-size: 11.5px;
  color: var(--ink-3, #999);
}

.m-ops {
  display: flex;
  gap: 8px;
  margin-top: 8px;
}

.m-empty {
  text-align: center;
  color: var(--ink-4, #bbb);
  padding: 30px 0;
}

.m-pager {
  display: flex;
  justify-content: center;
  align-items: center;
  gap: 14px;
  padding: 6px 0;
}

.m-page-no {
  font-size: 12.5px;
  color: var(--ink-3, #999);
}
</style>
