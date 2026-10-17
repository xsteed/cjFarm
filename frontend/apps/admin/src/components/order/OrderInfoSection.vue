<template>
  <div
    v-if="detail"
    class="od"
  >
    <!-- 关键信息：订单号/状态、桌号/人数/下单时间 -->
    <div class="od-info">
      <div class="od-row">
        <span class="od-k">订单号：</span>
        <span class="od-v mono">{{ detail.orderNo }}</span>
        <span class="od-k od-gap">状态：</span>
        <span class="od-v">
          <t-tag
            :theme="statusOption?.theme"
            variant="light"
          >
            {{ statusOption?.label }}
          </t-tag>
        </span>
      </div>
      <div class="od-row">
        <span class="od-k">桌号：</span>
        <span class="od-v">{{ detail.tableNo }}号</span>
        <span class="od-k od-gap">人数：</span>
        <span class="od-v">{{ detail.personCount }}人</span>
        <span class="od-k od-gap">下单时间：</span>
        <span class="od-v">{{ detail.createTime }}</span>
      </div>
    </div>

    <div
      v-if="detail.pendingUrge"
      class="urge-banner"
    >
      <span>顾客已催菜，请优先跟进</span>
      <t-button
        theme="danger"
        size="small"
        :disabled="busy"
        @click="emit('handle-urge')"
      >
        标记已处理
      </t-button>
    </div>

    <t-table
      :data="detail.items || []"
      :columns="itemCols"
      row-key="itemId"
      size="small"
      :max-height="isMobile ? 220 : 320"
    >
      <template #dishName="{ row }">
        <span
          class="od-dish"
          :class="{ 'od-gift-dish': row.isGift }"
          >{{ row.dishName }}</span
        >
        <span
          v-if="row.specName"
          class="od-spec"
          >{{ row.specName }}</span
        >
        <!-- 加菜批次标签(首单/加菜N):与顾客端同口径,商家能对上「这单加过几次菜」 -->
        <span
          v-if="batchTag(row)"
          class="od-spec od-batch-tag"
          >{{ batchTag(row) }}</span
        >
        <span
          v-if="row.isGift"
          class="od-spec od-gift-tag"
          >赠</span
        >
        <span
          v-if="row.isGift && row.giftReason"
          class="od-gift-reason"
          >{{ row.giftReason }}</span
        >
      </template>
      <template #itemRemark="{ row }">
        <span class="od-iremark">{{ row.itemRemark || '-' }}</span>
      </template>
      <template #price="{ row }"> ¥{{ money(row.price) }} </template>
      <template #amount="{ row }">
        <span class="money">¥{{ money(row.amount) }}</span>
      </template>
    </t-table>

    <div class="od-total">
      <span class="od-t-item"
        ><span class="od-k">菜品</span><b>¥{{ money(detail.dishAmount) }}</b></span
      >
      <span class="od-t-item"
        ><span class="od-k">茶位费（{{ detail.personCount }}人）</span><b>¥{{ money(detail.seatFee) }}</b></span
      >
      <span
        v-if="(detail.discountAmount || 0) > 0"
        class="od-t-item"
      >
        <span class="od-k">优惠</span><b>-¥{{ money(detail.discountAmount) }}</b>
      </span>
      <span
        v-if="(detail.giftAmount || 0) > 0"
        class="od-t-item od-gift-total"
      >
        <span class="od-k">赠送</span><b>-¥{{ money(detail.giftAmount) }}</b>
      </span>
      <span class="od-t-item od-t-sum"
        ><span class="od-k">合计</span><b>¥{{ money(detail.totalAmount) }}</b></span
      >
    </div>

    <div
      v-if="detail.payStatus === 1"
      class="od-settle-line"
    >
      <span>结算：{{ payText(detail) }}</span>
      <span
        >实收 <b>¥{{ money(detail.paidAmount) }}</b></span
      >
      <span
        v-if="(detail.refundAmount || 0) > 0"
        class="od-refunded"
        >已退款 ¥{{ money(detail.refundAmount) }}</span
      >
    </div>

    <div
      v-if="detail.settleType === 'free' && detail.settleRemark"
      class="od-note"
    >
      免单原因：{{ detail.settleRemark }}（操作人 {{ detail.settleOperator || '-' }}）
    </div>
    <div
      v-else-if="detail.settleType === 'credit' && detail.settleRemark"
      class="od-note"
    >
      挂账人/备注：{{ detail.settleRemark }}（操作人 {{ detail.settleOperator || '-' }}）
    </div>
    <div
      v-if="detail.orderRemark"
      class="od-note"
    >
      备注：{{ detail.orderRemark }}
    </div>
    <div
      v-if="detail.cancelReason"
      class="od-note"
    >
      取消原因：{{ detail.cancelReason }}
    </div>

    <!-- 主操作：改单 / 推进状态 / 收款 / 结账。
         移动端(见 scoped media)把它排到最后并 sticky 常驻底部,滚动看明细时主操作始终可点。 -->
    <div class="od-actions od-actions-main">
      <button
        type="button"
        class="ab ab-edit"
        :disabled="!canEditOrder"
        @click="emit('edit')"
      >
        手动改单
      </button>
      <button
        type="button"
        class="ab ab-make"
        :disabled="!canMake || busy"
        @click="emit('set-status', 2)"
      >
        开始制作
      </button>
      <button
        type="button"
        class="ab ab-serve"
        :disabled="!canServe || busy"
        @click="emit('set-status', 3)"
      >
        上齐/用餐中
      </button>
      <button
        type="button"
        class="ab ab-pay"
        :disabled="!canPay"
        @click="emit('settle', 'pay')"
      >
        确认收款
      </button>
      <button
        type="button"
        class="ab ab-settle"
        :disabled="!canPay"
        @click="emit('settle', 'settle')"
      >
        收款并结束用餐
      </button>
    </div>

    <!-- 次要操作一行铺开(按钮按内容自适应宽度,不再拉伸占满整行):
         补打走打印服务出真实热敏纸(小票机在线时用这个),与取消订单互不相干。 -->
    <div class="od-actions od-actions-sub">
      <button
        type="button"
        class="ab ab-re"
        :disabled="!canReprint || busy"
        @click="emit('reprint', 'kitchen')"
      >
        重打厨房单
      </button>
      <button
        type="button"
        class="ab ab-re"
        :disabled="!canReprint || busy"
        @click="emit('reprint', 'guest')"
      >
        重打小票
      </button>
      <button
        type="button"
        class="ab ab-cancel"
        :disabled="!canCancelOrder"
        @click="emit('cancel')"
      >
        取消订单
      </button>
      <button
        type="button"
        class="ab ab-more ab-finish"
        :disabled="!canFinish"
        @click="emit('finish')"
      >
        完成订单
      </button>
    </div>

    <!-- 条件性操作：只有满足条件才出现，平时不占位置 -->
    <div
      v-if="canCreditSettleOrder || canCancelSettleOrder || canRefundOrder"
      class="od-actions od-actions-extra"
    >
      <button
        v-if="canCreditSettleOrder"
        type="button"
        class="ab ab-more"
        @click="emit('credit-settle')"
      >
        核销挂账
      </button>
      <button
        v-if="canCancelSettleOrder"
        type="button"
        class="ab ab-more"
        @click="emit('cancel-settle')"
      >
        撤销结算
      </button>
      <button
        v-if="canRefundOrder"
        type="button"
        class="ab ab-more"
        @click="emit('refund')"
      >
        退款
      </button>
    </div>

    <div
      v-if="detail.payStatus === 1 && canRefundView"
      class="refund-block"
    >
      <div class="refund-title">
        退款记录
        <t-button
          size="small"
          variant="text"
          @click="refreshRefunds"
        >
          刷新
        </t-button>
      </div>
      <t-table
        v-if="refunds.length"
        :data="refunds"
        :columns="refundCols"
        row-key="refundId"
        size="small"
      >
        <template #amount="{ row }">
          <span class="money">¥{{ money(row.amount) }}</span>
        </template>
        <template #status="{ row }">
          <t-tag
            :theme="REFUND_STATUS[row.status]?.theme"
            variant="light"
          >
            {{ REFUND_STATUS[row.status]?.label }}
          </t-tag>
        </template>
        <template #op="{ row }">
          <t-button
            v-if="row.status === 0"
            size="small"
            variant="text"
            @click="emit('sync-refund', row)"
          >
            同步状态
          </t-button>
        </template>
      </t-table>
      <div
        v-else
        class="refund-empty"
      >
        暂无退款记录
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, toRef } from 'vue';
import { ORDER_STATUS, REFUND_STATUS } from '@dining/api';
import { useIsMobile } from '../../utils/useMobile';
import { batchLabels, payStatusText } from '@dining/shared';
import { useOrderActions } from '../../composables/useOrderActions';
import type { OrderDetail, Refund } from '@dining/types';

const props = defineProps<{
  detail: OrderDetail | null;
  refunds: Refund[];
  /** 有操作在途时置灰「改状态/补打/催办」等直接请求按钮,防连点重复触发(补打会真的出纸) */
  busy?: boolean;
}>();

const emit = defineEmits<{
  (e: 'handle-urge'): void;
  (e: 'set-status', status: number): void;
  (e: 'edit'): void;
  (e: 'settle', mode: 'pay' | 'settle'): void;
  (e: 'reprint', docType: 'kitchen' | 'guest'): void;
  (e: 'cancel'): void;
  (e: 'finish'): void;
  (e: 'credit-settle'): void;
  (e: 'cancel-settle'): void;
  (e: 'refund'): void;
  (e: 'refresh-refunds', orderId: number): void;
  (e: 'sync-refund', row: Refund): void;
}>();

interface TableCol {
  colKey: string;
  title: string;
  width?: number;
  ellipsis?: boolean;
}

const money = (v: unknown): string => Number(v || 0).toFixed(2);

const detailRef = toRef(props, 'detail');
const {
  canEditOrder,
  canMake,
  canServe,
  canPay,
  canCancelOrder,
  canFinish,
  canCreditSettleOrder,
  canCancelSettleOrder,
  canRefundOrder,
  canRefundView,
  canRefundOperate,
  canReprint
} = useOrderActions(detailRef);

const { isMobile } = useIsMobile();

const statusOption = computed(() => {
  const status = props.detail?.orderStatus;
  return status != null ? ORDER_STATUS[status] : undefined;
});

// 加菜批次标签(itemId → 首单/加菜N):计算逻辑收敛在 @dining/shared 的 batchLabels,
// 与顾客端 H5/小程序同一口径 —— 只有多批次才打标,单批次订单不打。
const batchTagMap = computed(() => {
  const items = props.detail?.items || [];
  const labels = batchLabels(items);
  const map = new Map<number, string>();
  items.forEach((it, i) => {
    if (it.itemId != null) map.set(it.itemId, labels[i]);
  });
  return map;
});

function batchTag(row: { itemId?: number }): string {
  return row.itemId != null ? (batchTagMap.value.get(row.itemId) || '') : '';
}

// 明细表：窄屏只留「菜品/数量/小计」，规格与单价在收银看单时不是必需，
// 少了它们才不需要横向拖动。
const itemCols = computed<TableCol[]>(() =>
  isMobile.value
    ? [
        { colKey: 'dishName', title: '菜品', ellipsis: true },
        { colKey: 'quantity', title: '数量', width: 46 },
        { colKey: 'amount', title: '小计', width: 74 }
      ]
    : [
        { colKey: 'dishName', title: '菜品' },
        { colKey: 'itemRemark', title: '备注', width: 160 },
        { colKey: 'price', title: '单价', width: 100 },
        { colKey: 'quantity', title: '数量', width: 70 },
        { colKey: 'amount', title: '小计', width: 100 }
      ]
);

const refundCols = computed<TableCol[]>(() => {
  // 窄屏(手机弹窗约 92vw ≈ 345px)放不下 6~7 列,只留核心信息,避免最右「操作」列被挤出视口;
  // 退款单号用 ellipsis 自适应,金额/状态/操作定宽,整体不超一屏。
  if (isMobile.value) {
    const cols: TableCol[] = [
      { colKey: 'refundNo', title: '退款单号', ellipsis: true },
      { colKey: 'amount', title: '金额', width: 86 },
      { colKey: 'status', title: '状态', width: 86 }
    ];
    if (canRefundOperate.value) cols.push({ colKey: 'op', title: '操作', width: 86 });
    return cols;
  }
  const cols: TableCol[] = [
    { colKey: 'refundNo', title: '退款单号', ellipsis: true },
    { colKey: 'channel', title: '渠道', width: 90 },
    { colKey: 'amount', title: '金额', width: 90 },
    { colKey: 'status', title: '状态', width: 90 },
    { colKey: 'reason', title: '原因', ellipsis: true },
    { colKey: 'createTime', title: '时间', width: 160 }
  ];
  if (canRefundOperate.value) cols.push({ colKey: 'op', title: '操作', width: 90 });
  return cols;
});

// 结算状态文案与「订单管理」列表共用 @dining/shared 里 orderDisplay 的实现,不再各写一份。
const payText = payStatusText;

function refreshRefunds(): void {
  const id = props.detail?.orderId;
  if (id != null) emit('refresh-refunds', id);
}
</script>

<style scoped>
.od {
  display: flex;
  flex-direction: column;
}

.od-info {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--line);
}

.od-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  font-size: 14px;
  color: var(--ink);
}

.od-k {
  color: var(--ink-3);
  font-size: 13px;
}

.od-v {
  font-weight: 600;
}

.od-gap {
  margin-left: 20px;
}

.mono {
  font-family: ui-monospace, Menlo, Consolas, monospace;
  font-size: 13px;
  letter-spacing: 0.2px;
}

.od-dish {
  font-weight: 600;
}

/* 规格小标签：截图里规格挂在菜名右侧，而不是单占一列 */
.od-spec {
  display: inline-block;
  margin-left: 8px;
  font-size: 11px;
  line-height: 16px;
  padding: 0 6px;
  border-radius: 4px;
  background: #f2f3f5;
  color: #7c7f85;
  vertical-align: 1px;
}

/* 赠送标记 */
.od-gift-dish {
  color: var(--ink-3);
}

/* 加菜批次标签(首单/加菜N):浅橙底与「赠」标呼应,又和灰底规格标区分开 */
.od-batch-tag {
  background: #fff3e0;
  color: #e8850c;
}

.od-gift-tag {
  background: #fff3e0;
  color: #e8850c;
  font-weight: 700;
}

.od-gift-reason {
  display: block;
  margin-top: 4px;
  font-size: 12px;
  color: #e8850c;
}

.od-gift-total b {
  color: #e8850c;
}

.od-iremark {
  font-size: 13px;
  color: var(--ink-2);
}

.od-total {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  align-items: baseline;
  gap: 8px 22px;
  margin-top: 14px;
  font-size: 14px;
}

.od-t-item .od-k {
  margin-right: 6px;
}

.od-t-sum b {
  font-size: 19px;
  font-weight: 800;
  color: var(--brand-deep);
}

.od-settle-line {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px 20px;
  margin-top: 8px;
  font-size: 13px;
  color: var(--ink-2);
}

.od-settle-line b {
  color: var(--ink);
}

.od-refunded {
  color: #d4380d;
}

.od-note {
  margin-top: 10px;
  padding: 8px 12px;
  background: #fff7e6;
  border-radius: 6px;
  font-size: 13px;

  /* 原来的 var(--brand-deep) #F0481F 落在浅黄底上只有 ~3.4:1，低于 WCAG AA 的 4.5:1，
     收银场景里备注(免单原因/挂账人)又是要看清的重点，这里压深一档。 */
  color: #a8340b;
  font-weight: 600;
}

.urge-banner {
  margin-top: 12px;
  padding: 10px 14px;
  border-radius: 10px;
  background: var(--danger-soft, #feeeee);
  color: var(--danger, #f53f3f);
  font-size: 13px;
  font-weight: 600;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

/* ---- 操作按钮 ---- */
.od-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 18px;
}

/* 主操作组保持等宽铺满(工具条观感);次要操作按内容自适应,不拉伸占满整行 */
.od-actions-sub {
  margin-top: 12px;
}

.od-actions-sub .ab {
  flex: 0 0 auto;
  min-width: 0;
}

.od-actions-extra {
  margin-top: 10px;
}

.ab {
  flex: 1 1 0;
  min-width: 100px;
  height: 32px;
  padding: 0 14px;
  border-radius: 7px;
  border: 1px solid transparent;
  font-size: 13px;
  font-weight: 600;
  white-space: nowrap;
  cursor: pointer;
  background: #fff;
  transition:
    filter 0.15s ease,
    opacity 0.15s ease;
}

.ab:not(:disabled):hover {
  filter: brightness(0.95);
}

.ab:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.ab-edit {
  color: #d97b06;
  border-color: #f3c98a;
  background: #fffaf1;
}

.ab-make {
  color: #fff;
  background: #ff9f26;
}

.ab-serve {
  color: #fff;
  background: #46b662;
}

.ab-pay {
  color: #fff;
  background: #3b8cff;
}

.ab-settle {
  color: #fff;
  background: #f4512e;
}

.ab-re {
  color: var(--ink);
  border-color: #dcdcdc;
}

.ab-cancel {
  color: #e34d59;
  border-color: #f7c4c7;
}

.ab-more {
  flex: 0 1 auto;
  min-width: 88px;
  color: var(--ink-2);
  border-color: #dcdcdc;
}

.ab-finish {
  color: #2ba471;
  border-color: #a3ddc6;
}

/* ---- 退款记录 ---- */
.refund-block {
  margin-top: 20px;
  padding-top: 12px;
  border-top: 1px dashed #e5e6eb;
}

.refund-title {
  font-size: 14px;
  font-weight: 600;
  margin-bottom: 8px;
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.refund-empty {
  font-size: 13px;
  color: #999;
}

/* 移动端:操作按钮放大、两列排布,拇指好点;金额区左右分布更清晰 */
@media (width <= 767px) {
  .od-actions {
    gap: 8px;
    margin-top: 12px;
  }

  /* 主操作组:排到内容末尾并常驻弹层底部,滚动查看明细时收款/结算/制作始终可点。
     负外边距 + 等量内边距让它横向铺满弹层(否则两侧 16px 内边距会露出滚动内容)。 */
  .od-actions-main {
    order: 1;
    position: sticky;
    bottom: 0;
    z-index: 3;
    gap: 8px;
    margin: 14px -16px 0;
    padding: 10px 16px 2px;
    background: #fff;
    border-top: 1px solid var(--line);
  }

  .ab {
    flex: 1 1 calc(50% - 4px);
    min-width: 0;
    height: 44px;
    font-size: 14px;
  }

  /* 移动端次要按钮恢复两列等宽大按钮(覆盖桌面端的按内容自适应) */
  .od-actions-sub .ab {
    flex: 1 1 calc(50% - 4px);
  }

  .ab-more {
    flex: 1 1 calc(50% - 4px);
    height: 40px;
  }

  .od-total {
    justify-content: space-between;
    gap: 6px 12px;
  }
}
</style>
