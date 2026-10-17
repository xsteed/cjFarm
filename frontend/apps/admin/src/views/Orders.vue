<template>
  <div class="page-card">
    <!-- 桌面端工具栏:移动端由下方 App 式筛选栏替代 -->
    <div
      v-if="!isMobile"
      class="toolbar"
    >
      <div class="toolbar-left">
        <t-input
          v-model="query.orderNo"
          placeholder="订单号 / 短号后6位"
          clearable
          style="width: 180px"
        />
        <t-input
          v-model="query.tableNo"
          placeholder="桌号"
          clearable
          style="width: 120px"
        />
        <!-- 桌台名称模糊筛选:桌号只是编号,收银认的是「大厅01桌」这类名称;
             桌台可改名,历史单留的是改名前的快照,故走后端模糊匹配 -->
        <t-input
          v-model="query.tableName"
          placeholder="桌台名称"
          clearable
          style="width: 130px"
        />
        <t-select
          v-model="query.orderStatus"
          placeholder="订单状态"
          clearable
          style="width: 140px"
        >
          <t-option
            v-for="(v, k) in ORDER_STATUS"
            :key="k"
            :value="String(k)"
            :label="v.label"
          />
        </t-select>
        <t-select
          v-model="query.payStatus"
          placeholder="支付状态"
          clearable
          style="width: 140px"
        >
          <t-option
            value="0"
            label="未支付"
          />
          <t-option
            value="1"
            label="已支付"
          />
        </t-select>
        <t-select
          v-model="query.settleFilter"
          placeholder="结算方式"
          clearable
          style="width: 150px"
        >
          <t-option
            value="normal"
            label="正常收款"
          />
          <t-option
            value="free"
            label="免单"
          />
          <t-option
            value="creditPending"
            label="挂账待收"
          />
          <t-option
            value="creditSettled"
            label="挂账已结"
          />
        </t-select>
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
      <t-button
        theme="primary"
        @click="load"
      >
        <template #icon>
          <refresh-icon />
        </template>
        刷新
      </t-button>
    </div>

    <!-- 移动端:App 式订单流(下拉刷新 + 搜索 + 状态药丸 + 卡片 + 底部筛选/操作弹层) -->
    <template v-if="isMobile">
      <PullRefresh
        :distance="refreshDistance"
        :refreshing="refreshRefreshing"
        :pulling="refreshPulling"
      />
      <div class="m-bar">
        <div class="m-search">
          <t-input
            v-model="query.orderNo"
            placeholder="搜索订单号 / 桌号"
            clearable
            @enter="onSearch"
          >
            <template #prefix-icon>
              <search-icon />
            </template>
          </t-input>
          <t-button
            theme="primary"
            variant="outline"
            class="m-filter-btn"
            aria-label="筛选"
            @click="filterSheetVisible = true"
          >
            <template #icon>
              <filter-icon />
            </template>
          </t-button>
        </div>
        <div class="m-seg">
          <button
            v-for="t in statusTabs"
            :key="t.value"
            type="button"
            class="m-seg-item"
            :class="{ on: query.orderStatus === t.value }"
            @click="onStatusTab(t.value)"
          >
            {{ t.label }}
          </button>
        </div>
      </div>

      <div class="m-list">
        <div
          v-if="!list.length"
          class="m-empty"
        >
          {{ loading ? '加载中…' : '暂无订单' }}
        </div>
        <!-- 点整卡看详情;底部按钮用 @click.stop 阻断冒泡。键盘可达(Enter/Space)。 -->
        <div
          v-for="row in list"
          :key="row.orderId"
          class="ocard ocard-tap"
          role="button"
          tabindex="0"
          @click="openDetail(row)"
          @keydown.enter="openDetail(row)"
          @keydown.space.prevent="openDetail(row)"
        >
          <div class="ocard-top">
            <span class="ocard-no"
              ><span class="ocard-no-label">短号(取餐号):</span
              >#{{ row.shortNo || row.orderNo.slice(-6) }}</span
            >
            <t-tag
              :theme="ORDER_STATUS[row.orderStatus]?.theme"
              variant="light"
            >
              {{ ORDER_STATUS[row.orderStatus]?.label }}
            </t-tag>
          </div>
          <!-- 完整订单号:移动端没有桌面端表格的 title 悬浮提示,直接铺在卡上方便对账报号 -->
          <div class="ocard-orderNo">单号: {{ row.orderNo }}</div>
          <div class="ocard-meta">
            <!-- 桌台优先展示名称(收银认的是「大厅01桌」而非桌号),名称为空的历史单回退桌号;加粗突出桌台 -->
            <span class="ocard-table">{{ row.tableName || row.tableNo + ' 号桌' }}</span> · {{ row.personCount }} 人 ·
            {{ row.createTime }} 下单
            <t-tag
              v-if="row.pendingUrge"
              theme="danger"
              variant="light"
              size="small"
              class="urgent-mini"
            >
              催菜
            </t-tag>
            <!-- 加菜角标:与桌面列表/详情弹窗同口径 -->
            <t-tag
              v-if="(row.batchCount || 0) > 1"
              theme="warning"
              variant="light"
              size="small"
              class="urgent-mini"
            >
              加菜×{{ row.batchCount! - 1 }}
            </t-tag>
          </div>
          <div class="ocard-amt">
            <span class="money">¥{{ $money(row.totalAmount) }}</span>
            <!-- 结算状态(免单/挂账待收/挂账已结/已支付)统一由下方 payText 标签展示,
                 与桌面端表格的「支付状态」列同口径;移动端卡片没有独立状态列,
                 不再另铺 settle-flag 角标,否则免单会出现两个重复标记 -->
            <!-- 退款中优先展示:refundAmount 只在退款成功时累加,处理中的退款单不会体现在
                 已退款金额里 —— 不单独标出来,收银会看不出该单正在退款（并会去点「退款」） -->
            <t-tag
              v-if="(row.refundingAmount || 0) > 0"
              theme="warning"
              variant="light"
            >
              退款中 ¥{{ $money(row.refundingAmount || 0) }}
            </t-tag>
            <t-tag
              v-else-if="row.refundAmount > 0"
              :theme="row.refundAmount >= row.totalAmount ? 'danger' : 'warning'"
              variant="light"
            >
              {{ row.refundAmount >= row.totalAmount ? '已退款' : '部分退款' }}
            </t-tag>
            <t-tag
              v-else
              :theme="payTheme(row)"
              variant="light"
            >
              {{ payText(row) }}
            </t-tag>
          </div>
          <!-- 所有可执行操作直接铺在卡片上(不再收进「更多」),一眼看清、一键直达 -->
          <div
            class="ocard-ft"
            @click.stop
          >
            <t-button
              v-for="a in cardActions(row)"
              :key="a.key"
              :theme="a.theme"
              variant="outline"
              :disabled="a.disabled"
              @click="a.run()"
            >
              {{ a.label }}
            </t-button>
          </div>
        </div>

        <!-- 移动端底部分页器:只要有数据就常驻(一页时显示「共 N 条数据 · 1 ›」),
             0 单空态才隐藏。此时仍可把「10 条/页」切成 5 条,一页变两页浏览;
             点页码数字即跳转到该页。事件口径与菜品页一致(current-change 首参即新页码),
             另补 page-size-change —— 菜品页漏绑该事件导致条数选择器改了不生效。 -->
        <div
          v-if="pagination.total > 0"
          class="m-pager"
        >
          <t-pagination
            :current="pagination.current"
            :page-size="pagination.pageSize"
            :total="pagination.total"
            @current-change="onPageChange"
            @page-size-change="onPageSizeChange"
          />
        </div>
      </div>
    </template>

    <t-table
      v-else
      :data="list"
      :columns="columns"
      row-key="orderId"
      :loading="loading"
      :pagination="pagination"
      @page-change="onTablePageChange"
    >
      <template #orderNo="{ row }">
        <span
          class="ono ono-link"
          :title="'完整订单号 ' + row.orderNo"
          role="button"
          tabindex="0"
          @click="openDetail(row)"
          @keydown.enter="openDetail(row)"
          @keydown.space.prevent="openDetail(row)"
          >#{{ row.shortNo || row.orderNo.slice(-6) }}</span
        >
        <t-tag
          v-if="row.pendingUrge"
          theme="danger"
          variant="light"
          size="small"
          style="margin-left: 6px"
        >
          催菜
        </t-tag>
        <!-- 加菜角标:batchCount>1 即本单加过菜,N=加菜次数;与详情弹窗的批次标签同口径 -->
        <t-tag
          v-if="(row.batchCount || 0) > 1"
          theme="warning"
          variant="light"
          size="small"
          style="margin-left: 6px"
        >
          加菜×{{ row.batchCount! - 1 }}
        </t-tag>
        <!-- 完整订单号:与移动端卡片同口径,短号(取餐号)报给顾客,完整单号留给对账/查询;
             桌面端不再只藏在 title 悬浮提示里 -->
        <div class="ono-full">{{ row.orderNo }}</div>
      </template>
      <template #tableNo="{ row }">
        <!-- 桌号列同时展示桌台名称:收银认的是「大厅01桌」这类名称(与移动端卡片/详情
             弹窗同口径),名称为空的历史单回退只显示桌号;桌名过长时在单元格内折行,
             title 悬浮给全称 -->
        <span
          class="tbl"
          :title="row.tableNo ? row.tableNo + '号桌' + (row.tableName ? ' · ' + row.tableName : '') : ''"
        >{{ row.tableNo ? row.tableNo + '号桌' : '-' }}<span
          v-if="row.tableName"
          class="tbl-name"
        >
            · {{ row.tableName }}</span
        ></span>
      </template>
      <template #orderStatus="{ row }">
        <t-tag
          :theme="ORDER_STATUS[row.orderStatus]?.theme"
          variant="light"
        >
          {{ ORDER_STATUS[row.orderStatus]?.label }}
        </t-tag>
      </template>
      <template #payStatus="{ row }">
        <t-tag
          v-if="row.refundAmount > 0"
          :theme="row.refundAmount >= row.totalAmount ? 'danger' : 'warning'"
          variant="light"
        >
          {{ row.refundAmount >= row.totalAmount ? '已退款' : '部分退款' }} ¥{{ $money(row.refundAmount) }}
        </t-tag>
        <t-tag
          v-else
          :theme="payTheme(row)"
          variant="light"
        >
          {{ payText(row) }}
        </t-tag>
      </template>
      <template #dishAmount="{ row }">
        <span class="money">¥{{ $money(row.dishAmount) }}</span>
      </template>
      <template #seatFee="{ row }">
        <span class="money">¥{{ $money(row.seatFee) }}</span>
      </template>
      <template #totalAmount="{ row }">
        <!-- 结算状态(免单/挂账待收/挂账已结)已在「支付状态」列展示,金额列只展示金额,不再重复打角标 -->
        <span class="money">¥{{ $money(row.totalAmount) }}</span>
      </template>
      <template #mealEndTime="{ row }">
        <!-- 结束时间:meal_end_time 仅在订单到终态(已完成/取消)时写入,进行中订单为空 -->
        {{ row.mealEndTime || '-' }}
      </template>
      <template #op="{ row }">
        <t-space>
          <t-button
            theme="primary"
            variant="text"
            size="small"
            @click="openDetail(row)"
          >
            详情
          </t-button>
          <t-button
            theme="danger"
            variant="text"
            size="small"
            :disabled="!canUrgeRow(row)"
            @click="onHandleUrge(row)"
          >
            已催办
          </t-button>
          <t-button
            theme="warning"
            variant="text"
            size="small"
            :disabled="!canMakeRow(row)"
            @click="setStatus(row, 2)"
          >
            制作
          </t-button>
          <t-button
            theme="warning"
            variant="text"
            size="small"
            :disabled="!canServeRow(row)"
            @click="setStatus(row, 3)"
          >
            上菜
          </t-button>
          <t-button
            theme="success"
            variant="text"
            size="small"
            :disabled="!canSettleRow(row)"
            @click="openSettle(row, settleMode)"
          >
            {{ settleLabel }}
          </t-button>
          <t-button
            theme="danger"
            variant="text"
            size="small"
            :disabled="!canCancelRow(row)"
            @click="onCancel(row)"
          >
            取消
          </t-button>
          <t-button
            theme="danger"
            variant="text"
            size="small"
            :disabled="!canRefund(row)"
            @click="openRefund(row)"
          >
            退款
          </t-button>
        </t-space>
      </template>
    </t-table>

    <!-- 移动端高级筛选:桌号 / 桌台名称 / 支付状态 / 结算方式收进底部弹层 -->
    <BottomSheet
      v-model="filterSheetVisible"
      title="筛选订单"
    >
      <div class="ff-list">
        <label class="ff">
          <span class="ff-k">桌号</span>
          <t-input
            v-model="query.tableNo"
            placeholder="如 01"
            clearable
          />
        </label>
        <label class="ff">
          <span class="ff-k">桌台名称</span>
          <t-input
            v-model="query.tableName"
            placeholder="如 大厅01桌"
            clearable
          />
        </label>
        <label class="ff">
          <span class="ff-k">支付状态</span>
          <t-select
            v-model="query.payStatus"
            placeholder="全部"
            clearable
          >
            <t-option
              value="0"
              label="未支付"
            />
            <t-option
              value="1"
              label="已支付"
            />
          </t-select>
        </label>
        <label class="ff">
          <span class="ff-k">结算方式</span>
          <t-select
            v-model="query.settleFilter"
            placeholder="全部"
            clearable
          >
            <t-option
              value="normal"
              label="正常收款"
            />
            <t-option
              value="free"
              label="免单"
            />
            <t-option
              value="creditPending"
              label="挂账待收"
            />
            <t-option
              value="creditSettled"
              label="挂账已结"
            />
          </t-select>
        </label>
      </div>
      <template #footer>
        <t-button
          theme="default"
          @click="resetFilters"
        >
          重置
        </t-button>
        <t-button
          theme="primary"
          @click="applyFilters"
        >
          <template #icon>
            <search-icon />
          </template>
          查询
        </t-button>
      </template>
    </BottomSheet>

    <!-- 订单详情：与「桌台管理」页点击桌台共用同一个组件，收款/退款/改单等子弹窗都在组件内，
         避免两处各写一套后长期跑偏。列表行的快捷按钮通过 ref 调用组件暴露的方法。 -->
    <OrderDetailDialog
      ref="detailRef"
      v-model:visible="detailVisible"
      :order-id="detailOrderId"
      @changed="load"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, onUnmounted } from 'vue';
import { MessagePlugin, DialogPlugin } from 'tdesign-vue-next';
import { listOrders, changeOrderStatus, cancelOrder, finishOrder, handleUrge, ORDER_STATUS } from '@dining/api';
import { useIsMobile } from '../utils/useMobile';
import { usePullRefresh } from '../composables/usePullRefresh';
import { usePayFirst } from '../composables/usePayFirst';
import { hasPerm } from '@dining/auth';
import { payStatusText, payStatusTheme, money } from '@dining/shared';
import OrderDetailDialog from '../components/OrderDetailDialog.vue';
import BottomSheet from '../components/BottomSheet.vue';
import PullRefresh from '../components/PullRefresh.vue';
import type { PageInfo } from 'tdesign-vue-next';
import type { OrderSummary, PageQuery } from '@dining/types';

// 模板里 $money 与全局属性共用 @dining/shared 里 money 的实现(TS 模板消费不到全局属性,须显式导入)。
const $money = money;

// 列表行在模板里要 .slice 订单号、拿 orderStatus 当下标、对 refundAmount/totalAmount
// 做数值比较;OrderSummary 字段全可选,这里把这几项收紧为必填,避免模板报「可能未定义」。
type OrderRow = OrderSummary & {
  orderId: number;
  orderNo: string;
  orderStatus: number;
  refundAmount: number;
  totalAmount: number;
  payChannel: string;
};

// ---- 权限位:每个写操作按钮都对应后端一个权限点(见 docs/user-permission-design.md 4.3)。
// 前端隐藏只是体验优化,后端 RequirePerm 仍会 403。
const canOperate = computed(() => hasPerm('order:operate')); // 制作/上菜/已催办
const canSettle = computed(() => hasPerm('order:settle')); // 收款/结账(弹窗在详情组件内)
const canCancel = computed(() => hasPerm('order:cancel')); // 取消订单
const canRefundOperate = computed(() => hasPerm('refund:operate')); // 发起退款/同步退款状态

// 先付模式(pay_first_enabled='1')下「结算」是个坑:后端结算(/order/settle)只出结账小票、
// 不出厨房单,而先付订单的厨单只在「收款成功」那一刻补出(printKitchenOnPaid),
// 且结算会直接拒绝已支付订单。因此未支付先付单唯一正确的收款入口是
// 「确认收款」(/order/pay,只标记收款不改状态)—— 按钮随之改显「确认收款」;
// 后付模式(默认)显示「后付款结账」(收款/免单/挂账并一步归档、释放桌台)。
const payFirst = usePayFirst();
const settleMode = computed<'pay' | 'settle'>(() => (payFirst.value ? 'pay' : 'settle'));
const settleLabel = computed(() => (payFirst.value ? '确认收款' : '后付款结账'));

const list = ref<OrderRow[]>([]);
const loading = ref(false);

interface OrderQuery {
  orderNo: string;
  tableNo: string;
  tableName: string;
  orderStatus: string;
  payStatus: string;
  settleFilter: string;
}

const query = reactive<OrderQuery>({ orderNo: '', tableNo: '', tableName: '', orderStatus: '', payStatus: '', settleFilter: '' });
const pagination = reactive({ current: 1, pageSize: 10, total: 0 });
const { isMobile } = useIsMobile();
// 移动端下拉刷新当前列表(沿用当前筛选与页码)
const { distance: refreshDistance, refreshing: refreshRefreshing, pulling: refreshPulling } = usePullRefresh(load);
// 列表内直接请求操作(制作/上齐/已催办)的在途标记:防连点重复提交
const acting = ref(false);

// 详情弹窗已抽成共享组件 components/OrderDetailDialog.vue ——
// 详情展示、收款/结算、挂账核销、退款、手动改单全在组件内实现；
// 列表行的「结算」「退款」快捷按钮通过 detailRef 调用组件暴露的方法，
// 保证每种操作全局只有一份代码(桌台管理页点击桌台也复用同一个组件)。
const detailRef = ref<InstanceType<typeof OrderDetailDialog> | null>(null);
const detailVisible = ref(false);
const detailOrderId = ref(0);

// 结算状态文案/主题与订单详情共用 @dining/shared 里 orderDisplay 的实现,不再各写一份。
const payText = payStatusText;
const payTheme = payStatusTheme;

const columns = computed(() => {
  const cols = [
    // 该列实际渲染两行:首行取餐号/短号(#shortNo),第二行完整订单号(ono-full),
    // 标题取「取餐号/单号」覆盖两者,避免只叫「订单号」让人以为漏了完整单号。
    { colKey: 'orderNo', title: '取餐号/单号', width: 200 },
    // 桌台列同时展示桌号与桌台名称(如「01号桌 · 大厅01桌」):收银认的是名称而非
    // 桌号(与移动端卡片/详情弹窗同口径),名称为空的历史单回退只显示桌号。
    // 「01号桌 · 包间牡丹厅」约 150px,桌名更长的(如「二楼VIP-牡丹厅」)允许折行,
    // 不再硬撑宽度把整表拉宽。
    { colKey: 'tableNo', title: '桌台', width: 150 },
    { colKey: 'personCount', title: '人数', width: 70 },
    { colKey: 'dishAmount', title: '菜品金额', width: 100 },
    { colKey: 'seatFee', title: '茶位费', width: 90 },
    { colKey: 'totalAmount', title: '应收金额', width: 110 },
    { colKey: 'orderStatus', title: '订单状态', width: 100 },
    { colKey: 'payStatus', title: '支付状态', width: 150 },
    // 后端时间格式 conf.TimeLayout = "2006-01-02 15:04:05",共 19 个半角字符。
    // 14px 字号下文字约需 140px,加单元格左右各 16px 内边距共约 172px —— 原 170 在数字
    // 稍宽的字体下会折成两行,这里放宽到 195 留出余量。
    { colKey: 'createTime', title: '下单时间', width: 195 },
    // 结束时间:取 meal_end_time(订单到终态 4已完成/5已取消 时写入,取消单同样有),
    // 进行中的订单该列为 NULL,展示「-」。与详情弹窗「结束时间」同口径。
    { colKey: 'mealEndTime', title: '结束时间', width: 195 }
  ];
  // 操作列改为固定渲染全部 7 个按钮(不满足条件时置灰而非隐藏),宽度按最坏情况
  // 「详情/已催办/制作/上菜/后付款结账/取消/退款」全可点估算(后付模式的
  // 「后付款结账」比原「结算」多 3 字;先付模式为「确认收款」,少 1 字):
  // 12px 字号文字约 333px + 6 个 8px 间距 + 32px 内边距 ≈ 413px,留余量取 430。
  cols.push({ colKey: 'op', title: '操作', width: 430 });
  return cols;
});

// canRefund 仅在线支付(微信/支付宝)且已支付、未全额退款、且没有处理中退款的订单可发起在线退款。
// 另需 refund:operate —— 本期该权限只授予超级管理员。
// refundingAmount（处理中的退款单）同样占用可退额度，必须排除：否则按钮可点、点了被后端拒。
function canRefund(row: OrderRow): boolean {
  return (
    canRefundOperate.value &&
    row &&
    row.payStatus === 1 &&
    ['wxpay', 'alipay'].includes(row.payChannel) &&
    !((row.refundingAmount || 0) > 0) &&
    (row.refundAmount || 0) < row.totalAmount
  );
}

// ---- 操作列各按钮的行级可用性:操作列固定渲染全部按钮,能否点击由「行状态 + 权限位」
// 共同决定,不满足时按钮 disabled 置灰(而非 v-if 隐藏),好处有三:
//   1. 收银一眼看到该单「还能做什么/为什么不能做」,不必猜按钮是否被权限藏了;
//   2. 行与行的操作位对齐,不会因按钮数量不同而错位;
//   3. 权限缺失只在前端置灰,后端 RequirePerm 仍会 403,不影响安全边界。
// 命名带 Row 后缀以区别于上面的权限位 computed(canSettle / canCancel)。
function canUrgeRow(row: OrderRow): boolean {
  return !!row?.pendingUrge && canOperate.value;
}
function canMakeRow(row: OrderRow): boolean {
  return row?.orderStatus === 1 && canOperate.value;
}
function canServeRow(row: OrderRow): boolean {
  return row?.orderStatus === 2 && canOperate.value;
}
function canSettleRow(row: OrderRow): boolean {
  return !!row && [1, 2, 3].includes(row.orderStatus) && row.payStatus === 0 && canSettle.value;
}
// 完成(归档收尾)与详情弹窗 canFinish 同口径:任意进行中状态 + 已支付。
// 未支付订单点了仍会被后端拒绝(先收款/结账),所以这里只管状态与收款两道。
function canFinishRow(row: OrderRow): boolean {
  return !!row && [1, 2, 3].includes(row.orderStatus) && row.payStatus === 1 && canOperate.value;
}
function canCancelRow(row: OrderRow): boolean {
  // 已支付订单不可直接取消(后端 CancelOrder 会拒绝并要求先退款),
  // 少这道条件按钮可点但点了必然报错。
  return !!row && [1, 2, 3].includes(row.orderStatus) && row.payStatus === 0 && canCancel.value;
}

// 请求序号:快速连点查询/翻页时,仅最新一次请求可写回列表,防止旧页响应乱序覆盖新页
let loadSeq = 0;

// silent=true 用于定时自动刷新:不点亮全表 loading 遮罩,避免每轮轮询都闪一下。
// 手动刷新(按钮/下拉/筛选)仍走默认 false,保留可见的加载反馈。
async function load(silent = false): Promise<void> {
  if (!silent) loading.value = true;
  const seq = ++loadSeq;
  try {
    const params: PageQuery = { pageNum: pagination.current, pageSize: pagination.pageSize };
    if (query.orderNo) params.orderNo = query.orderNo;
    if (query.tableNo) params.tableNo = query.tableNo;
    // 桌台名称模糊匹配:名称可改名,历史单留改名前快照,必须服务端过滤
    if (query.tableName) params.tableName = query.tableName;
    if (query.orderStatus) params.orderStatus = query.orderStatus;
    if (query.payStatus) params.payStatus = query.payStatus;
    // 结算方式筛选:挂账需区分待收 / 已结
    if (query.settleFilter === 'creditPending') {
      params.settleType = 'credit';
      params.creditStatus = 1;
    } else if (query.settleFilter === 'creditSettled') {
      params.settleType = 'credit';
      params.creditStatus = 2;
    } else if (query.settleFilter) {
      params.settleType = query.settleFilter;
    }
    const res = await listOrders(params);
    if (seq !== loadSeq) return;
    // 后端条目字段全可选(OrderSummary),这里归一化成列表渲染依赖的必填形态,
    // 避免缺字段时模板 row.orderNo.slice(...) 直接抛运行时错误。
    list.value = (res.items || []).map(r => ({
      ...r,
      orderId: Number(r.orderId) || 0,
      orderNo: r.orderNo || '',
      orderStatus: Number(r.orderStatus) || 0,
      payStatus: Number(r.payStatus) || 0,
      payChannel: r.payChannel || '',
      totalAmount: Number(r.totalAmount) || 0,
      refundAmount: Number(r.refundAmount) || 0
    }));
    pagination.total = res.total;
    // 筛选 / 自动刷新后总数可能缩水(进行中的订单被完成或取消后掉出当前筛选),
    // 当前页码随之超出新的总页数,而后端不纠偏(越界页码直接返回空列表)——
    // 夹回最后一页并重拉一次,避免停在「第 3/5 页却一片空白」的状态。
    // 页码已夹到合法范围,不会再次触发,也就不会递归。
    const maxPage = Math.max(1, Math.ceil(pagination.total / pagination.pageSize));
    if (pagination.current > maxPage) {
      pagination.current = maxPage;
      load();
    }
  } catch {
    /* 失败已由拦截器统一 toast */
  } finally {
    if (seq === loadSeq) loading.value = false;
  }
}

function onSearch(): void {
  pagination.current = 1;
  load();
}

// 移动端底部分页器:current-change 首参就是新页码数字(与菜品页同口径,见 t-pagination 事件签名)
function onPageChange(page: number): void {
  pagination.current = page;
  load();
}

// 每页条数变更:t-pagination 自带条数选择器(一页数据时靠它切 5 条/页分成两页浏览)。
// 改完必须回第 1 页 —— 沿用旧页码可能越过新的总页数,后端不纠偏会返回空列表。
function onPageSizeChange(size: number): void {
  pagination.pageSize = size;
  pagination.current = 1;
  load();
}

// t-table 的 page-change 给的是 PageInfo(current + pageSize),语义与底部分页器不同,分开处理
function onTablePageChange(pageInfo: PageInfo): void {
  pagination.current = pageInfo.current;
  pagination.pageSize = pageInfo.pageSize;
  load();
}

// openDetail 打开详情弹窗:只给组件 orderId,详情与退款记录由组件自行拉取。
function openDetail(row: OrderRow): void {
  detailOrderId.value = Number(row.orderId) || 0;
  detailVisible.value = true;
}

// afterRowAction 列表行操作后的收尾:详情弹窗开着就顺带刷新它(组件内部会 emit changed
// 触发本页 load()),否则直接刷列表,避免同一操作触发两次列表请求。
async function afterRowAction(orderId: number): Promise<void> {
  if (detailVisible.value && detailRef.value) {
    await detailRef.value.refresh(orderId);
  } else {
    load();
  }
}

async function setStatus(row: OrderRow, status: number): Promise<void> {
  if (acting.value) return;
  acting.value = true;
  try {
    await changeOrderStatus({ orderId: row.orderId, orderStatus: status });
    MessagePlugin.success('操作成功');
    await afterRowAction(row.orderId);
  } catch {
    /* 失败已由拦截器统一 toast */
  } finally {
    acting.value = false;
  }
}

// onHandleUrge 消解该订单全部待处理催菜(已加急/已解释),角标随之消失。
async function onHandleUrge(row: OrderRow): Promise<void> {
  if (acting.value) return;
  acting.value = true;
  try {
    await handleUrge({ orderId: row.orderId });
    MessagePlugin.success('催菜已标记为已处理');
    await afterRowAction(row.orderId);
  } catch {
    /* 失败已由拦截器统一 toast */
  } finally {
    acting.value = false;
  }
}

// 列表行的收款入口直接复用详情组件里的弹窗:
// openSettle mode=pay 仅标记收款(先付模式下未支付单的唯一正确入口,会补出厨房单),
// mode=settle 可选择免单/挂账并完成订单(后付模式一步归档、释放桌台)。
function openSettle(row: OrderRow, mode: 'pay' | 'settle'): void {
  detailRef.value?.openSettle(row, mode);
}

function openRefund(row: OrderRow): void {
  detailRef.value?.openRefund(row);
}

function onCancel(row: OrderRow): void {
  const dlg = DialogPlugin.confirm({
    header: '确认取消',
    body: `确认取消订单【${row.orderNo}】？`,
    onConfirm: async () => {
      try {
        await cancelOrder({ orderId: row.orderId, cancelReason: '商家取消' });
        MessagePlugin.success('订单已取消');
        dlg.hide();
        await afterRowAction(row.orderId);
      } catch {
        dlg.hide();
      }
    }
  });
}

// onFinish 完成订单:归档、计入今日统计并释放桌台。与详情弹窗的「完成订单」同一接口,
// 确认文案保持一致;先付模式下顾客付款后单子常停在「已下单」,卡片上给个直接出口。
function onFinish(row: OrderRow): void {
  if (acting.value) return;
  const dlg = DialogPlugin.confirm({
    header: '确认完成',
    body: '确认后订单将归档并计入今日统计，桌台释放为空闲。',
    onConfirm: async () => {
      acting.value = true;
      try {
        await finishOrder({ orderId: row.orderId });
        MessagePlugin.success('订单已完成');
        dlg.hide();
        await afterRowAction(row.orderId);
      } catch {
        dlg.hide();
      } finally {
        acting.value = false;
      }
    }
  });
}

// ---- 移动端订单卡片的操作按钮 ----
// 直接铺在卡片上,不再有「更多」二级面板:能做什么一眼可见、一键直达。
interface CardAction {
  key: string;
  label: string;
  theme: 'default' | 'primary' | 'success' | 'warning' | 'danger';
  run: () => void;
  disabled?: boolean;
}

function cardActions(row: OrderRow): CardAction[] {
  // 先付模式:卡片收敛为收银四键,按 详情 → 确认收款 → 取消订单 → 完成订单 排列
  // (与移动端详情弹窗的主动作行同口径)。制作/上齐/催办/退款一律进详情弹窗,
  // 不占卡片 —— 先付单的收款入口不能被制作流程按钮挤到后面,完成订单(归档、
  // 释放桌台)也给个卡片上的直接出口,不必为了点它先打开详情。
  // 各键按行状态显隐:确认收款/取消订单仅未支付,完成订单仅已支付。
  if (payFirst.value) {
    const items: CardAction[] = [{ key: 'detail', label: '订单详情', theme: 'primary', run: () => openDetail(row) }];
    if ([1, 2, 3].includes(row.orderStatus) && row.payStatus === 0 && canSettle.value)
      items.push({
        key: 'settle',
        label: settleLabel.value,
        theme: 'success',
        run: () => openSettle(row, settleMode.value)
      });
    if ([1, 2, 3].includes(row.orderStatus) && row.payStatus === 0 && canCancel.value)
      items.push({ key: 'cancel', label: '取消订单', theme: 'danger', run: () => onCancel(row) });
    if (canFinishRow(row))
      items.push({ key: 'finish', label: '完成订单', theme: 'primary', run: () => onFinish(row), disabled: acting.value });
    return items;
  }
  // 后付模式:完整操作铺在卡片上(制作/上齐是厨房流程,催办/退款按行状态出现)。
  const items: CardAction[] = [{ key: 'detail', label: '订单详情', theme: 'primary', run: () => openDetail(row) }];
  if (row.pendingUrge && canOperate.value)
    items.push({ key: 'urge', label: '已催办', theme: 'danger', run: () => onHandleUrge(row), disabled: acting.value });
  if (row.orderStatus === 1 && canOperate.value)
    items.push({
      key: 'make',
      label: '开始制作',
      theme: 'warning',
      run: () => setStatus(row, 2),
      disabled: acting.value
    });
  if (row.orderStatus === 2 && canOperate.value)
    items.push({ key: 'serve', label: '已上齐', theme: 'warning', run: () => setStatus(row, 3), disabled: acting.value });
  if ([1, 2, 3].includes(row.orderStatus) && row.payStatus === 0 && canSettle.value)
    items.push({
      key: 'settle',
      label: settleLabel.value,
      theme: 'success',
      run: () => openSettle(row, settleMode.value)
    });
  if ([1, 2, 3].includes(row.orderStatus) && row.payStatus === 0 && canCancel.value)
    items.push({ key: 'cancel', label: '取消订单', theme: 'danger', run: () => onCancel(row) });
  if (canRefund(row)) items.push({ key: 'refund', label: '退款', theme: 'danger', run: () => openRefund(row) });
  return items;
}

// ---- 移动端 App 式筛选 ----
// 状态药丸:全部 + ORDER_STATUS 全部状态;点击即查,不再需要一个「查询」按钮。
const statusTabs = computed(() => [
  { label: '全部', value: '' },
  ...Object.entries(ORDER_STATUS).map(([k, v]) => ({ label: v.label, value: String(k) }))
]);

function onStatusTab(value: string): void {
  query.orderStatus = value;
  pagination.current = 1;
  load();
}

// 高级筛选弹层
const filterSheetVisible = ref(false);

function applyFilters(): void {
  filterSheetVisible.value = false;
  onSearch();
}

function resetFilters(): void {
  query.tableNo = '';
  query.tableName = '';
  query.payStatus = '';
  query.settleFilter = '';
  // 订单号搜索框与桌面端共用同一个 query.orderNo,漏清会让「重置」后关键词仍在;
  // 同时回到第一页,否则重置完仍停在第 N 页,看起来「没生效」。
  query.orderNo = '';
  pagination.current = 1;
}

// ---- 停留在本页时自动刷新 ----
// 订单状态(新单/制作/上菜/支付)变化频繁,靠人工点「刷新」容易漏单。
// 每 10s 静默拉一次当前筛选与页码下的列表;切后台/锁屏暂停轮询(省流量与后端 QPS),
// 回到可见立即补一轮并恢复节奏(与「打印管理」页的代理轮询同策略)。
const AUTO_REFRESH_MS = 10_000;
let autoTimer: ReturnType<typeof setInterval> | null = null;

function startAutoRefresh(): void {
  stopAutoRefresh();
  autoTimer = setInterval(() => {
    // 详情弹窗打开或行操作进行中时跳过本轮,避免后台刷新与用户正在进行的操作互相干扰。
    if (detailVisible.value || acting.value || loading.value) return;
    load(true);
  }, AUTO_REFRESH_MS);
}

function stopAutoRefresh(): void {
  if (autoTimer) {
    clearInterval(autoTimer);
    autoTimer = null;
  }
}

function onVisibilityChange(): void {
  if (document.hidden) {
    stopAutoRefresh();
  } else {
    load(true);
    startAutoRefresh();
  }
}

onMounted(() => {
  load();
  startAutoRefresh();
  document.addEventListener('visibilitychange', onVisibilityChange);
});

onUnmounted(() => {
  stopAutoRefresh();
  document.removeEventListener('visibilitychange', onVisibilityChange);
});
</script>

<style scoped>
/* 短号：列表统一展示可口头报读的短号，避免收银喊单时报错流水号 */
.ono {
  font-weight: 600;
  color: var(--ink, #1a1a1a);
  letter-spacing: 0.4px;
}

/* 桌面端短号可点开详情:与「详情」按钮同能力;默认即链接蓝,hover/focus 加下划线 */
.ono-link {
  cursor: pointer;
  color: var(--td-brand-color, #0052d9);
}

.ono-link:hover,
.ono-link:focus-visible {
  text-decoration: underline;
}

/* 订单号单元格第二行:完整订单号,与移动端卡片 ocard-orderNo 同口径 */
.ono-full {
  margin-top: 2px;
  font-size: 11.5px;
  color: var(--ink-3, #8a8f99);
  letter-spacing: 0.3px;
  word-break: break-all;
}

/* 桌台列:桌号是定位依据(主色加粗),桌台名称是收银口语(弱化一档);
   长桌名在单元格内折行,title 悬浮给全称,避免把整表撑宽 */
.tbl {
  font-weight: 600;
  color: var(--ink, #1a1a1a);
}

.tbl-name {
  font-weight: 400;
  font-size: 12px;
  color: var(--ink-2, #5b6270);
}

/* 移动端卡片里的催菜角标:贴住短号右侧,不另起一行 */
.urgent-mini {
  margin-left: 6px;
  vertical-align: 1px;
}

/* 卡片左上角短号前的「短号」字样:小号弱化,号码本身仍是视觉主体;
   与下方 ocard-orderNo 的完整「单号」对应,一眼分清两个号各是什么 */
.ocard-no-label {
  margin-right: 3px;
  font-size: 11px;
  font-weight: 500;
  color: var(--ink-3, #8a8f99);
}

/* 卡片信息行里的桌台名字:加粗突出(收银先认桌再认人) */
.ocard-table {
  font-weight: 700;
  color: var(--ink, #1a1a1a);
}

/* 移动端搜索栏右侧的筛选图标按钮:正方形,与搜索输入框同高。
   输入框高度走 --td-comp-size-m(默认 32px),这里直接取同一 token 而不是写死像素,
   换主题/字号时两边自动保持一致;原来固定 40px 会把整条搜索栏撑高、与输入框错位 */
.m-filter-btn {
  width: var(--td-comp-size-m);
  height: var(--td-comp-size-m);
  flex-shrink: 0;
  padding: 0;
}

/* 筛选弹层里的字段:标签在上、控件占满整行 */
.ff-list {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.ff {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.ff-k {
  font-size: 13px;
  color: var(--ink-2);
  font-weight: 500;
}
</style>
