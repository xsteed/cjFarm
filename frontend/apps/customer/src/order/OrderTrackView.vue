<template>
  <div
    class="track-body"
    :class="{ lifted: showTabbar }"
  >
    <!-- 状态卡:下单后顾客第一眼要回答的是「我的单到哪一步了」。
         用一张带品牌色渐层的卡承载状态大字 + 已等时长 + 出餐三步进度,
         菜品卡只负责「点了什么、多少钱」,两块信息各归各位。 -->
    <div
      class="st-hero"
      :class="'st-hero--' + heroTheme"
    >
      <div class="sh-top">
        <div class="sh-text">
          <div class="sh-title">{{ headline }}</div>
          <div class="sh-sub">{{ subline }}</div>
        </div>
        <div class="sh-illo">
          <!-- 等出餐:餐碗 + 三缕错拍升腾的热气 -->
          <svg
            v-if="heroIcon === 'bowl'"
            viewBox="0 0 48 48"
            fill="none"
            aria-hidden="true"
          >
            <path
              class="steam s1"
              d="M17.5 17.5c2-2.3-2-4 0-6.3"
              stroke="currentColor"
              stroke-width="2.3"
              stroke-linecap="round"
            />
            <path
              class="steam s2"
              d="M24 15.5c2-2.3-2-4 0-6.3"
              stroke="currentColor"
              stroke-width="2.3"
              stroke-linecap="round"
            />
            <path
              class="steam s3"
              d="M30.5 17.5c2-2.3-2-4 0-6.3"
              stroke="currentColor"
              stroke-width="2.3"
              stroke-linecap="round"
            />
            <path
              d="M8 24h32a16 16 0 0 1-32 0Z"
              fill="currentColor"
              fill-opacity="0.14"
            />
            <path
              d="M8 24h32a16 16 0 0 1-32 0Z"
              stroke="currentColor"
              stroke-width="2.3"
              stroke-linejoin="round"
            />
            <path
              d="M19 40h10"
              stroke="currentColor"
              stroke-width="2.3"
              stroke-linecap="round"
            />
          </svg>

          <!-- 已上齐 / 已完成:圆底对勾 -->
          <svg
            v-else-if="heroIcon === 'check'"
            viewBox="0 0 48 48"
            fill="none"
            aria-hidden="true"
          >
            <circle
              cx="24"
              cy="24"
              r="15"
              fill="currentColor"
              fill-opacity="0.14"
            />
            <path
              d="M16.5 24.4l5.2 5.2 10.2-10.6"
              stroke="currentColor"
              stroke-width="3"
              stroke-linecap="round"
              stroke-linejoin="round"
            />
          </svg>

          <!-- 已取消:圆底叉 -->
          <svg
            v-else-if="heroIcon === 'cross'"
            viewBox="0 0 48 48"
            fill="none"
            aria-hidden="true"
          >
            <circle
              cx="24"
              cy="24"
              r="15"
              fill="currentColor"
              fill-opacity="0.14"
            />
            <path
              d="M17.5 17.5l13 13M30.5 17.5l-13 13"
              stroke="currentColor"
              stroke-width="3"
              stroke-linecap="round"
            />
          </svg>

          <!-- 先付待付款:一张待刷的卡 -->
          <svg
            v-else
            viewBox="0 0 48 48"
            fill="none"
            aria-hidden="true"
          >
            <rect
              x="7"
              y="15"
              width="34"
              height="21"
              rx="4.5"
              stroke="currentColor"
              stroke-width="2.3"
            />
            <path
              d="M7 22h34"
              stroke="currentColor"
              stroke-width="2.3"
            />
            <path
              d="M13 30h7"
              stroke="currentColor"
              stroke-width="2.3"
              stroke-linecap="round"
            />
          </svg>
        </div>
      </div>

      <!-- 出餐三步:节点事实来自后端 order_status,不承诺时间、不做倒计时。
           先付未付款时后厨还没收到单,整块不渲染(否则等于告诉顾客「已下单」) -->
      <div
        v-if="showSteps"
        class="st-steps"
      >
        <div
          v-for="(s, i) in steps"
          :key="s.label"
          class="step"
          :class="{ on: s.on, now: s.now }"
        >
          <div class="step-track">
            <span
              class="step-bar l"
              :class="{ on: i > 0 && s.on }"
            ></span>
            <span class="step-dot"></span>
            <span
              class="step-bar r"
              :class="{ on: s.bar }"
            ></span>
          </div>
          <span class="step-txt">{{ s.label }}</span>
        </div>
      </div>

      <!-- 进度免责小字:状态是商家手动点的,可能与实际上菜有先后,提前把预期说软 -->
      <div
        v-if="showSteps"
        class="st-steps-hint"
      >
        出餐进度由商家更新，可能与实际上菜略有先后
      </div>

      <!-- 结束态说明:已完成 / 已取消 / 已结账,措辞与小程序逐字一致 -->
      <div
        v-if="tipText"
        class="st-tip"
        :class="{ 'st-tip--cancel': canceled }"
      >
        {{ tipText }}
      </div>
    </div>

    <!-- 菜品明细卡:顾客最关心「点了什么、多少钱」。每行带缩略图,
         名称/规格/备注在左,数量与金额右对齐 -->
    <div class="st-card">
      <div class="st-card-head">
        <div class="tt">菜品明细</div>
        <div class="st-count">共 {{ totalQty }} 件</div>
      </div>

      <div
        v-for="(it, idx) in renderItems"
        :key="it.itemId ?? idx"
        class="st-item"
      >
        <div class="si-main">
          <span class="si-name"
            ><b
              v-if="it.batchLabel"
              class="si-batch"
              >{{ it.batchLabel }}</b
            ><b
              v-if="it.isGift"
              class="si-gift"
              >赠 </b
            >{{ it.dishName
            }}<em
              v-if="it.specName"
              class="si-spec"
              >{{ it.specName }}</em
            ></span
          >
          <span
            v-if="it.itemRemark"
            class="si-remark"
            >{{ it.itemRemark }}</span
          >
        </div>
        <span class="si-qty">×{{ it.quantity }}</span>
        <span
          class="si-amount"
          :class="{ 'si-gift': it.isGift }"
          >{{ it.isGift ? '赠' : '¥' + Number(it.amount).toFixed(2) }}</span
        >
      </div>

      <!-- 整单备注:顾客提交的整体要求(如不要辣),位置与小程序一致(明细之后、账单之前) -->
      <div
        v-if="currentOrder.orderRemark"
        class="st-order-remark"
      >
        订单备注：{{ currentOrder.orderRemark }}
      </div>

      <!-- 账单明细:菜品小计 + 加价项,与小程序一致摊开。
           末行给出「合计 + 支付状态」:合计只在这里出现一次,与上面几行前因后果对得上。 -->
      <div class="st-bill">
        <div class="st-bill-row">
          <span>菜品小计</span>
          <span>¥{{ Number(currentOrder.dishAmount || 0).toFixed(2) }}</span>
        </div>
        <div
          v-if="Number(currentOrder.seatFee) > 0"
          class="st-bill-row"
        >
          <span>茶位费（{{ currentOrder.personCount }}人）</span>
          <span>¥{{ Number(currentOrder.seatFee).toFixed(2) }}</span>
        </div>
        <div
          v-if="Number(currentOrder.discountAmount) > 0"
          class="st-bill-row st-discount"
        >
          <span>满减优惠</span>
          <span>-¥{{ Number(currentOrder.discountAmount).toFixed(2) }}</span>
        </div>
        <div class="st-bill-row st-bill-total">
          <span>合计</span>
          <span class="st-bill-total-right">
            <span
              class="st-tag"
              :class="'st-tag--' + payTheme"
              >{{ payText }}</span
            >
            <span class="st-bill-total-value">¥{{ Number(currentOrder.totalAmount).toFixed(2) }}</span>
          </span>
        </div>
      </div>

      <!-- 待付款引导:点明「先付模式」与下一步动作,紧跟账单、与下方「去支付」按钮连成一条线。
           这里刻意不带金额 —— 金额已在账单末行给出,重复出现反而像两笔账 -->
      <div
        v-if="payFirst && view === 'order' && isActive && Number(currentOrder.payStatus) === 0"
        class="st-tip st-payfirst-tip"
      >
        💳 本店为先付模式：点击下方「去支付」完成付款，后厨即刻开工～
      </div>

      <!-- 进行中的订单:按钮区只留「加菜」(门店欢迎的动作)。
           settled 视图也要判断:先付模式付款后轮询会切到 settled,此时菜往往还在做;
           settled 时已付款,加菜因 canAppend 要求未支付而自然隐藏,按钮区整块不渲染。
           催菜不再进按钮区 —— 见账单下方那行小字 -->
      <div
        v-if="(view === 'order' || view === 'settled') && isActive && canAppend"
        class="st-actions"
      >
        <div
          class="st-append"
          @click="emit('append')"
        >
          <svg
            viewBox="0 0 24 24"
            width="14"
            height="14"
            fill="none"
            stroke="currentColor"
            stroke-width="2.5"
            stroke-linecap="round"
          >
            <path d="M12 5v14M5 12h14" />
          </svg>
          加菜
        </div>
      </div>

      <!-- 按钮只给动作，金额由上方账单末行的「合计」承担，避免同屏出现两次 -->
      <div
        v-if="view === 'order' && Number(currentOrder.payStatus) === 0"
        class="st-paybtn"
        @click="emit('pay')"
      >
        <!-- 先付模式下后厨还没收到这颗单,付款才是关键动作,用火苗催一下;
             后付模式只是结账,不加这个紧迫感 -->
        {{ payFirst ? '🔥 去支付' : '去支付' }}
      </div>
      <!-- 结束态按钮组:去评价与再点一单并排(用户反馈:tip 条看不出可点)。
           评价按钮只在已付款且上齐后出现;已评变灰只读。canceled 只剩重新点菜 -->
      <div
        v-if="canReview || canceled || settled || done"
        class="st-endbtns"
      >
        <div
          v-if="canReview"
          class="st-btn st-review-btn"
          :class="{ done: reviewDone }"
          @click="!reviewDone && emit('review')"
        >
          {{ reviewDone ? '已评价 ✓' : '去评价' }}
        </div>
        <div
          class="st-btn st-neworder"
          @click="emit('newOrder')"
        >
          {{ newOrderText }}
        </div>
      </div>

      <!-- 催菜:整张卡最后一行小字,放在按钮下方 ——
           它是「等太久」才用一次的兜底动作,做成按钮、或压在按钮上面,都会跟
           「加菜 / 去支付」抢视线(等于变相请顾客催单)。催过之后整句换成安抚文案、
           链接自动收起,刻意不留倒计时:秒数会让人盯着数,冷却一过再来催一次 -->
      <div
        v-if="(view === 'order' || view === 'settled') && isActive && canUrge"
        class="st-urge-line"
      >
        <template v-if="urging">正在通知后厨…</template>
        <template v-else-if="urgeCooldown > 0">已通知后厨加急，马上就来～</template>
        <template v-else
          >菜等好久还没来？<span
            class="st-urge"
            @click="emit('urge')"
            >催一下后厨</span
          ></template
        >
      </div>
    </div>

    <!-- 订单信息:单号 / 人数 / 时间 / 支付凭证,顾客核对单据时才看,放菜品之后不再挤在首屏 -->
    <div class="st-info">
      <div class="st-info-title">订单信息</div>
      <div class="st-info-grid">
        <div class="st-info-cell">
          <span class="k">单号</span>
          <span class="v">{{ currentOrder.shortNo || currentOrder.orderNo }}</span>
        </div>
        <div class="st-info-cell">
          <span class="k">用餐人数</span>
          <span class="v">{{ currentOrder.personCount }} 人</span>
        </div>
        <div
          v-if="createdAtText"
          class="st-info-cell"
        >
          <span class="k">下单时间</span>
          <span class="v">{{ createdAtText }}</span>
        </div>
      </div>
      <!-- 支付凭证行:结账后留一条可核对的记录(渠道 + 时间),不再只有一个「已结账」标签 -->
      <div
        v-if="Number(currentOrder.payStatus) === 1"
        class="st-info-paid"
      >
        <span class="st-info-paid-dot"></span>{{ payText }}<template v-if="paidAtText"> · {{ paidAtText }}</template>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue';
import type { OrderDetail, OrderItem } from '@dining/types';
import type { ViewState } from '@dining/customer-core';
import { payStatusText, payStatusTheme, batchLabels, durationText } from '@dining/shared/orderDisplay';

const props = defineProps<{
  view: ViewState;
  currentOrder: OrderDetail;
  isActive: boolean;
  canAppend: boolean;
  urging: boolean;
  urgeCooldown: number;
  showTabbar: boolean;
  /** 门店先付模式:未支付时提示「付款后才开做」 */
  payFirst: boolean;
  /** 本单是否已评价(后端查得):已评则入口变只读的「已评价 ✓」 */
  reviewDone?: boolean;
}>();

const emit = defineEmits<{
  append: [];
  urge: [];
  pay: [];
  newOrder: [];
  review: [];
}>();

// 状态主题:与小程序 ORDER_STATUS 字典一致,驱动状态卡的强调色(橙=等接单/琥珀=制作中/绿=用餐中)
const STATUS_THEME: Record<number, string> = { 1: 'primary', 2: 'warning', 3: 'success', 4: 'default', 5: 'danger' };
const orderStatus = computed(() => Number(props.currentOrder.orderStatus) || 1);
const statusTheme = computed(() => STATUS_THEME[orderStatus.value] || 'primary');

// 催菜只对「后厨已经收到厨单」的订单有意义:先付模式下未支付时后厨还没收到单,
// 顾客点催菜只会得到一个「已催」的假反馈,反而以为菜在做 —— 这类订单先引导付款。
const canUrge = computed(() => !props.payFirst || Number(props.currentOrder.payStatus) === 1);

// 结束态:与小程序 order 页的 settled / done / canceled 三态一致
const done = computed(() => props.view === 'done');
const canceled = computed(() => props.view === 'canceled');
const settled = computed(() => props.view === 'settled');

// 先付模式下「还没给钱」:后厨没收到单,顾客要做的第一件事是付款,状态卡也据此换口径
const payFirstUnpaid = computed(
  () => props.payFirst && Number(props.currentOrder.payStatus) === 0 && !canceled.value && !done.value
);

// 评价入口的显示条件:已付款 + 已上齐/已完成(3/4),不依赖视图 ——
// 后付刚结账(settled)固然是主路径,但顾客从「本桌正在出餐」列表、刷新或换设备
// 回到订单视图(view=order)时同样是吃完待反馈的状态,入口必须同样出现。
const canReview = computed(
  () => Number(props.currentOrder.payStatus) === 1 && [3, 4].includes(Number(props.currentOrder.orderStatus))
);

// 「再点一单」按钮文案按状态区分(行为都是开新单,见 spec「一桌多单」):
// 已结账但还在吃(settled)→「再加点菜」——先付模式付款后想加菜,正规路径就是
// 开一单新的继续点(spec 附录 A:不做已支付差额补付,账务不变式不动);
// 完成→「再点一单」;取消→「重新点菜」。
const newOrderText = computed(() => {
  if (props.view === 'canceled') return '重新点菜';
  if (props.view === 'settled') return '再加点菜';
  return '再点一单';
});

// 支付状态文案/主题:复用共享实现,与小程序同源
const payText = computed(() => payStatusText(props.currentOrder));
const payTheme = computed(() => payStatusTheme(props.currentOrder));

// 付款时间:后端是「2026-10-09 12:03:07」这类字符串,截到分钟即可,不当时间解析用
const paidAtText = computed(() => {
  const t = props.currentOrder.payTime;
  return t ? String(t).replace('T', ' ').slice(0, 16) : '';
});

// 下单时间:同在订单信息卡里按「月-日 时:分」展示 —— 秒数对顾客没有意义,
// 年份在订单页也用不上,三列信息格里「10-25 03:20」正好一行放得下
const createdAtText = computed(() => {
  const t = props.currentOrder.createTime;
  return t ? String(t).replace('T', ' ').slice(5, 16) : '';
});

// 取消提示:后端已对取消原因做可展示性收口(通用标签「商家取消」会置空),
// 有真实原因就带上,没有就给标准说明。取消只可能发生在未支付订单上(后端强制 pay_status=0),
// 所以能明确告诉顾客「不产生费用」,不必让顾客去猜钱到底扣没扣。
const cancelTip = computed(() =>
  props.currentOrder.cancelReason
    ? `订单已取消：${props.currentOrder.cancelReason}`
    : '订单已取消，本单未支付、不会产生任何费用'
);

// ---- 状态卡文案 ----
// 大字只说「此刻该知道的一件事」:等餐 → 走到哪一步;先付未付 → 去付款;结束态 → 结果。
// 付款与否不改变后厨进度(见 spec:出餐是商家维度),所以 settled 仍按 order_status 说话。
const headline = computed(() => {
  if (canceled.value) return '订单已取消';
  if (done.value) return '本单已完成';
  if (payFirstUnpaid.value) return '等待付款';
  switch (orderStatus.value) {
    case 2:
      return '后厨正在制作';
    case 3:
      // 不写「已上齐」:状态由商家手动维护、可能滞后于实际上菜,只说过程不打包票
      return '餐品陆续上桌，请慢用';
    default:
      return '已下单，等待接单';
  }
});

// 图标与底色随状态走:等出餐/用餐中=餐碗冒热气,完成=对勾,取消=叉,待付款=卡片。
// 「用餐中」(3)不用对勾:那是「全部上齐」的强断言,商家可能还没点完状态,对勾只留给已完成
const heroIcon = computed<'bowl' | 'check' | 'cross' | 'card'>(() => {
  if (canceled.value) return 'cross';
  if (done.value) return 'check';
  if (payFirstUnpaid.value) return 'card';
  return 'bowl';
});

const heroTheme = computed(() => {
  if (canceled.value) return 'danger';
  // 已完成也用成功色:整页进入"收尾"基调,灰色会把完成后的三步进度画得像失效
  if (done.value) return 'success';
  if (payFirstUnpaid.value) return 'warning';
  return statusTheme.value;
});

// 已等时长:每分钟量一次(30s 一拍足够),数据全来自订单自身的 createTime,
// 不承诺出餐时间、不做倒计时 —— 后厨改状态才是唯一事实来源。
const nowTs = ref(Date.now());
let tickTimer: ReturnType<typeof setInterval> | null = null;

/** 后端时间串("YYYY-MM-DD HH:MM:SS" 或 ISO)转时间戳;iOS 不认带「-」的日期串,统一换「/」。 */
function parseTime(t?: string | null): number {
  if (!t) return 0;
  const s = String(t).replace('T', ' ').slice(0, 19).replace(/-/g, '/');
  const v = new Date(s).getTime();
  return Number.isFinite(v) ? v : 0;
}

const subline = computed(() => {
  if (canceled.value) return '本单不会再出餐';
  if (payFirstUnpaid.value) return '付款后后厨即刻开工';
  const start = parseTime(props.currentOrder.createTime);
  if (!start) return '订单已提交';
  const mins = Math.floor((nowTs.value - start) / 60000);
  // 时钟漂移/跨日遗留单不做无意义的「已等 3000 分钟」
  if (mins < 0 || mins > 60 * 24) return '订单已提交';
  if (mins < 1) return '刚刚下单';
  return `${done.value ? '用餐' : '已等'} ${durationText(mins)}`;
});

// 出餐三步:只映射后厨的三个真实节点(1已下单 → 2制作中 → 3用餐中),完成的单三步全亮。
// 第三步不写「已上齐」:商家未必准时维护状态,「用餐中」是软性阶段词,滞后也不算说错。
// 先付未付款时后厨压根没收到单,不画进度(进度条会让人以为已经在做)。
const showSteps = computed(() => !canceled.value && !payFirstUnpaid.value && orderStatus.value <= 4);
const stepIdx = computed(() => (done.value ? 3 : Math.min(Math.max(orderStatus.value - 1, 0), 2)));
const steps = computed(() => [
  { label: '已下单', on: true, bar: stepIdx.value >= 1, now: stepIdx.value === 0 },
  { label: '制作中', on: stepIdx.value >= 1, bar: stepIdx.value >= 2, now: stepIdx.value === 1 },
  { label: '用餐中', on: stepIdx.value >= 2, bar: false, now: stepIdx.value === 2 }
]);

// 结束态才出说明块:进行中的单没有额外话可说,进度条已经回答完了
const tipText = computed(() => {
  if (canceled.value) return cancelTip.value;
  if (done.value) return '🎉 这顿吃完啦，欢迎下次光临';
  if (settled.value) return '已结账，后厨正在做啦～出餐进度会自动更新；想加的菜随时可以再点';
  return '';
});

// 菜品明细行:与小程序一致,给「加过菜」的批次打行内小标签(首单 / 加菜N);单批次不标。
// 编号口径收敛在 @dining/shared 的 batchLabels(与管理端订单详情共用)。
type RenderItem = OrderItem & { batchLabel: string };

const renderItems = computed<RenderItem[]>(() => {
  const list = props.currentOrder.items || [];
  const labels = batchLabels(list);
  return list.map((it, i) => ({ ...it, batchLabel: labels[i] }));
});

// 明细行刻意不放菜品图:下单后顾客是来「核对点了什么」的,
// 图片会让人重新对着照片评判菜品,反而削弱核对这件事本身。
const totalQty = computed(() =>
  (props.currentOrder.items || []).reduce((s, it) => s + (Number(it.quantity) || 0), 0)
);

onMounted(() => {
  tickTimer = setInterval(() => {
    nowTs.value = Date.now();
  }, 30000);
});

onUnmounted(() => {
  if (tickTimer) clearInterval(tickTimer);
  tickTimer = null;
});
</script>

<style scoped>
/* 订单视图滚动区:整页是一张张白卡(状态 → 菜品明细 → 订单信息),
   与菜单页「白卡上移压住 hero」不同,这里首张卡与横幅之间留出边距 */
.track-body {
  flex: 1;
  padding: 8px 14px 24px;
  overflow-y: auto;
  position: relative;
  z-index: 1;
}

/* 底部 tabbar 会遮住卡片底部,加菜/催菜按钮需要额外让位 */
.track-body.lifted {
  padding-bottom: 86px;
}

/* ---------- 状态卡 ---------- */
.st-hero {
  /* 主题色由 --accent 驱动:进度节点、图标、副文案高亮都跟着它,
     底色是同一色系的极浅渐层,白卡上不会抢走菜品明细的注意力 */
  --accent: var(--brand);
  --accent-soft: var(--brand-soft);

  position: relative;
  margin-top: 6px;

  /* 状态卡只是「一眼看进度」的横幅,不做第二个信息中心:
     内边距、字号、图标都按紧凑档取值,给下面的菜品明细留出屏幕 */
  padding: 12px 14px;
  border-radius: var(--r-lg);
  background: linear-gradient(152deg, var(--accent-soft) 0%, #fff 72%);
  box-shadow: var(--shadow-1);
  animation: hero-in 0.28s ease-out;
}

.st-hero--warning {
  --accent: #e8930c;
  --accent-soft: var(--warning-soft);
}

.st-hero--success {
  --accent: var(--success);
  --accent-soft: var(--success-soft);
}

.st-hero--danger {
  --accent: var(--danger);
  --accent-soft: var(--danger-soft);
}

.st-hero--default {
  --accent: var(--ink-3);
  --accent-soft: #f4f4f4;
}

@keyframes hero-in {
  from {
    opacity: 0;
    transform: translateY(6px);
  }

  to {
    opacity: 1;
    transform: translateY(0);
  }
}

.sh-top {
  display: flex;
  align-items: center;
  gap: 10px;
}

.sh-text {
  flex: 1;
  min-width: 0;
}

/* 状态大字:整页最大的一行字,顾客抬眼就知道走到哪了 */
.sh-title {
  font-size: 16px;
  font-weight: 800;
  line-height: 1.3;
  letter-spacing: 0.2px;
  color: var(--ink);
}

.sh-sub {
  margin-top: 2px;
  font-size: 11.5px;
  line-height: 1.4;
  color: var(--ink-3);
}

.sh-illo {
  flex-shrink: 0;
  width: 38px;
  height: 38px;
  border-radius: 50%;
  background: #fff;
  color: var(--accent);
  display: flex;
  align-items: center;
  justify-content: center;
  box-shadow: 0 3px 10px rgb(26 26 26 / 8%);
}

.sh-illo svg {
  display: block;
  width: 26px;
  height: 26px;
}

/* 热气:三缕错拍升腾,让「正在做」有活着的感觉 */
.sh-illo .steam {
  animation: sh-steam 2.2s ease-in-out infinite;
}

.sh-illo .steam.s2 {
  animation-delay: 0.65s;
}

.sh-illo .steam.s3 {
  animation-delay: 1.3s;
}

@keyframes sh-steam {
  0%,
  100% {
    opacity: 0.3;
    transform: translateY(1px);
  }

  50% {
    opacity: 1;
    transform: translateY(-2px);
  }
}

/* ---------- 出餐进度 ---------- */
.st-steps {
  display: flex;
  margin-top: 9px;
}

/* 进度免责小字:弱化到第三层级,说清「进度是商家更新的」又不抢主信息 */
.st-steps-hint {
  margin-top: 8px;
  font-size: 10.5px;
  line-height: 1.4;
  text-align: center;
  color: var(--ink-4);
}

.step {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
}

.step-track {
  display: flex;
  align-items: center;
  width: 100%;
}

/* 连接线左右各半段:首节点左边、末节点右边隐藏,拼起来是一条连续细线 */
.step-bar {
  flex: 1;
  height: 2px;
  background: #ececec;
  border-radius: 1px;
}

.step-bar.on {
  background: var(--accent);
}

.step:first-child .step-bar.l,
.step:last-child .step-bar.r {
  visibility: hidden;
}

.step-dot {
  flex-shrink: 0;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #e0e0e0;
}

.step.on .step-dot {
  background: var(--accent);
}

/* 当前节点:外圈呼吸,余光也能看出「停在这一步」。
   光圈用主题自带的浅色(--accent-soft),避免依赖 color-mix 在旧 WebView 上的支持 */
.step.now .step-dot {
  box-shadow: 0 0 0 4px var(--accent-soft);
  animation: step-pulse 1.8s ease-in-out infinite;
}

@keyframes step-pulse {
  0%,
  100% {
    box-shadow: 0 0 0 3px var(--accent-soft);
  }

  50% {
    box-shadow: 0 0 0 6px var(--accent-soft);
  }
}

.step-txt {
  font-size: 10.5px;
  line-height: 1.2;
  color: var(--ink-4);
  white-space: nowrap;
}

.step.on .step-txt {
  color: var(--ink-2);
  font-weight: 600;
}

.step.now .step-txt {
  color: var(--accent);
  font-weight: 700;
}

/* 订单信息卡:单号 / 人数 / 时间。放菜品之后,顾客核对单据时才看(桌台由 HeroHeader 展示)。
   内边距与 .st-card 一致,几张卡的留白口径统一 */
.st-info {
  background: #fff;
  border-radius: var(--r-lg);
  padding: 16px;
  margin-top: 12px;
  box-shadow: var(--shadow-1);
}

.st-info-title {
  font-size: 13px;
  font-weight: 700;
  color: var(--ink);
  margin-bottom: 10px;
}

/* 三列信息格:标签在上、值在下,长单号自然折行不挤掉邻居 */
.st-info-grid {
  display: flex;
  flex-wrap: wrap;
  gap: 10px 12px;
}

.st-info-cell {
  flex: 1 1 30%;
  min-width: 88px;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.st-info-cell .k {
  font-size: 11px;
  color: var(--ink-4);
}

.st-info-cell .v {
  font-size: 13px;
  font-weight: 600;
  color: var(--ink-2);
  word-break: break-all;
}

/* 支付凭证:绿点 + 「已支付（微信）· 2026-10-09 12:03」,可核对可回看 */
.st-info-paid {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 10px;
  padding-top: 10px;
  border-top: 1px dashed var(--line);
  color: var(--success);
  font-size: 12px;
}

.st-info-paid-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--success);
}

/* 状态 / 支付标签:对齐小程序 app.wxss 的 .tag 体系 */
.st-tag {
  display: inline-flex;
  align-items: center;
  height: 22px;
  padding: 0 10px;
  border-radius: var(--r-pill);
  font-size: 11px;
  line-height: 1;
}

.st-tag--primary {
  background: var(--brand-soft);
  color: var(--brand-deep);
}

.st-tag--success {
  background: var(--success-soft);
  color: var(--success);
}

.st-tag--warning {
  background: var(--warning-soft);
  color: #b7791f;
}

.st-tag--danger {
  background: var(--danger-soft);
  color: var(--danger);
}

.st-tag--default {
  background: #f2f2f2;
  color: var(--ink-3);
}

/* 状态说明块:已完成 / 已取消 / 已结账,内嵌于状态卡,对齐小程序。
   白底 + 一圈发丝线:状态卡底色随主题变(橙/绿/红),说明块不能跟着染色,
   否则绿卡上贴一块粉色会显得两个系统在打架 */
.st-tip {
  padding: 8px 10px;
  border-radius: var(--r-md);
  background: #fff;
  box-shadow: inset 0 0 0 1px rgb(26 26 26 / 6%);
  color: var(--ink-2);
  font-size: 12px;
  line-height: 1.55;
}

.st-hero > .st-tip {
  margin-top: 10px;
}

.st-tip--cancel {
  background: var(--danger-soft);
  box-shadow: none;
  color: var(--danger);
}

/* 先付模式提示:紧贴账单,给「去支付」按钮做铺垫。
   这张卡在白色菜品卡里,沿用品牌浅底才和「待办」的分量相称 */
.st-payfirst-tip {
  margin-top: 12px;
  background: var(--brand-ghost);
  box-shadow: none;
}

.st-card {
  background: #fff;
  border-radius: var(--r-lg);
  padding: 16px;
  margin-top: 12px;
  box-shadow: var(--shadow-1);
}

/* 卡头:标题 + 右侧件数 */
.st-card-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 8px;
}

.st-card .tt {
  font-size: 14px;
  font-weight: 700;
  margin-bottom: 8px;
}

.st-card-head .tt {
  margin-bottom: 0;
}

/* 件数:一眼能看到「这单有多少东西」,与状态卡的已等时长互补 */
.st-count {
  font-size: 12px;
  color: var(--ink-3);
}

/* 加菜批次行内标签(首单 / 加菜N):与小程序 row-batch 一致 */
.si-batch {
  display: inline-block;
  margin-right: 4px;
  padding: 0 4px;
  border-radius: var(--r-sm);
  background: #f2f2f2;
  color: var(--ink-3);
  font-size: 10px;
  font-weight: 400;
  line-height: 1.7;
}

/* 菜品行:名称(含备注) | 数量 | 金额 三栏(每行上边框,首行也有分隔线)。
   纯文字核对单,不放菜品图 —— 下单后顾客是来对单的,不是来重新挑菜的。
   字号对齐全站菜品口径(菜单 .op-name 14px / 购物车 14.5px) */
.st-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 11px 0;
  border-top: 1px solid #f5f5f5;
  font-size: 13.5px;
}

.si-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.si-name {
  color: var(--ink);
  font-weight: 600;
}

.si-spec {
  font-style: normal;
  margin-left: 6px;
  padding: 1px 6px;
  border-radius: var(--r-sm);
  background: #f4f4f4;
  color: var(--ink-3);
  font-size: 11.5px;
  font-weight: 400;
}

.si-remark {
  font-size: 12px;
  color: var(--ink-3);
  line-height: 1.5;
}

.si-qty {
  flex-shrink: 0;
  color: var(--ink-3);
  font-size: 12.5px;
}

.si-amount {
  flex-shrink: 0;
  min-width: 60px;
  text-align: right;
  color: var(--ink);
  font-size: 13.5px;
  font-weight: 600;
}

.si-gift {
  color: #e8850c;
  font-weight: 700;
}

/* 账单明细:菜品小计 / 茶位费 / 满减,与小程序一致的摊开式呈现 */
.st-bill {
  margin-top: 10px;
  padding-top: 10px;
  border-top: 1px dashed #f5f5f5;
}

.st-bill-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 5px 0;
  color: var(--ink-3);
  font-size: 12.5px;
}

.st-bill-row.st-discount {
  color: var(--brand-deep);
}

/* 合计行:账单收尾,金额与「未支付 / 已支付」标签同排。
   虚线上下的留白对齐(上一行 5px 下内边距 + 7px 外边距 = 12px,下方 12px) */
.st-bill-total {
  margin-top: 7px;
  padding: 12px 0 0;
  border-top: 1px dashed var(--line);
  color: var(--ink);
  font-size: 13.5px;
  font-weight: 600;
}

/* 窄屏兜底:标签文案较长(如「挂账待收 ¥12.00」)时也不把金额挤变形 */
.st-bill-total-right {
  display: flex;
  flex-shrink: 0;
  align-items: center;
  gap: 8px;
}

.st-bill-total-value {
  color: var(--brand-deep);
  font-size: 17px;
  font-weight: 700;
}

/* 支付按钮:与卡内「加菜」同高,去掉重投影 ——
   主次靠底色(实心品牌渐变 vs 描边)区分,不再靠体积压住菜品明细 */
.st-paybtn {
  width: 100%;
  height: 44px;
  border-radius: var(--r-pill);
  background: linear-gradient(135deg, var(--brand), var(--brand-deep));
  box-shadow: 0 6px 16px rgb(240 72 31 / 22%);
  color: #fff;
  font-size: 14.5px;
  font-weight: 600;
  display: flex;
  align-items: center;
  justify-content: center;
  margin-top: 12px;
  cursor: pointer;
}

.st-paybtn:active {
  transform: translateY(1px);
  box-shadow: 0 3px 10px rgb(240 72 31 / 22%);
}

/* 结束态按钮组:去评价 | 再点一单 并排,各占一半 */
.st-endbtns {
  display: flex;
  gap: 10px;
  margin-top: 12px;
}

.st-btn {
  flex: 1;
  height: 42px;
  border-radius: var(--r-pill);
  font-size: 14px;
  font-weight: 600;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
}

/* 去评价:主色描边(与加菜按钮同款视觉),已评变灰只读 */
.st-review-btn {
  border: 1.5px solid #ffd5c4;
  background: var(--brand-soft);
  color: var(--brand-deep);
}

.st-review-btn:active {
  background: #ffe3d4;
}

.st-review-btn.done {
  border-color: var(--line);
  background: #f5f5f5;
  color: var(--ink-3);
  font-weight: 400;
  cursor: default;
}

/* 顾客侧已结束(已结账/完成/取消)后的「再点一单」出口:与去评价并排各半 */
.st-neworder {
  border: 1.5px solid #ffd5c4;
  background: #fff;
  color: var(--brand-deep);
}

.st-neworder:active {
  background: var(--brand-soft);
}

/* 整单备注回显:顾客提交的整体要求(不要辣等),下单后必须能看到。
   样式与小程序「订单备注」一致:虚线上方纯文本,不再用图标高亮块 */
.st-order-remark {
  margin-top: 10px;
  padding-top: 10px;
  border-top: 1px dashed #f5f5f5;
  color: var(--ink-2);
  font-size: 12.5px;
  line-height: 1.6;
}

/* 订单卡片操作区:只剩「加菜」这一个按钮(催菜已收进下面那行小字) */
.st-actions {
  display: flex;
  gap: 10px;
  margin-top: 12px;
}

.st-append {
  flex: 1;
  height: 42px;
  border: 1.5px solid #ffd5c4;
  border-radius: var(--r-pill);
  background: #fff;
  color: var(--brand-deep);
  font-size: 14px;
  font-weight: 600;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 5px;
  cursor: pointer;
}

.st-append:active {
  background: var(--brand-soft);
}

/* 催菜:整张卡最后一行小字,排在按钮下方,不是按钮 —— 顾客等久了往下看时会读到,
   不需要用色块把它推到眼前(那等于替门店催单)。与小程序 .urge-line 同口径 */
.st-urge-line {
  margin-top: 12px;
  color: var(--ink-3);
  font-size: 12.5px;
  line-height: 1.6;
}

/* 只有「催一下后厨」这几个字可点:同色同字号,靠一条细下划线示意可点,
   点下去的反馈交给整句文案(换成「已通知后厨加急」) */
.st-urge {
  color: var(--ink-3);
  font-weight: 500;
  text-decoration: underline;
  text-decoration-color: var(--ink-4);
  text-underline-offset: 2px;
  cursor: pointer;
}

.st-urge:active {
  color: var(--ink-2);
  text-decoration-color: var(--ink-2);
}

/* 无障碍:系统开了「减少动态效果」时,热气与节点呼吸都停掉 */
@media (prefers-reduced-motion: reduce) {
  .st-hero,
  .sh-illo .steam,
  .step.now .step-dot {
    animation: none;
  }
}

/* 超窄屏(320px):三列信息格改成两列,单号不被压成竖排 */
@media (width <= 360px) {
  .st-info-cell {
    flex: 1 1 44%;
  }

  .sh-title {
    font-size: 17px;
  }
}
</style>
