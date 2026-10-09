<!-- 手动改单弹窗
     对接 POST /api/admin/order/edit —— 该接口后端早已实现(仅「进行中且未支付」的订单可改)，
     但前端此前一直没有入口，本组件补上。

     提交的 items 只需 dishId / specId / quantity：菜名、规格名与单价一律由后端按
     service.ResolveOrderItems 从数据库回读，防止前端被篡改价格或手滑写错名称；
     菜品金额 / 茶位费 / 优惠 / 合计也全部由 service.RecalcAmount 重算，
     因此本弹窗里的金额只是「预估」，保存后以服务端返回为准。 -->
<template>
  <t-dialog
    :visible="visible"
    header="手动改单"
    width="840px"
    :footer="false"
    @update:visible="(v: boolean) => emit('update:visible', v)"
    @closed="onClosed"
  >
    <div
      v-if="form"
      class="oe"
    >
      <div class="oe-head">
        <span class="oe-hi"
          ><span class="k">订单号</span><span class="v mono">{{ order?.orderNo }}</span></span
        >
        <span class="oe-hi"
          ><span class="k">桌号</span><span class="v">{{ order?.tableNo }}号</span></span
        >
        <span class="oe-hi">
          <span class="k">用餐人数</span>
          <t-input-number
            v-model="form.personCount"
            :min="1"
            :max="20"
            size="small"
            style="width: 110px"
          />
          <!-- 上限 20 与顾客端(H5 抽屉/小程序会话)统一:茶位费按人数算,口径不一致会产生异常大额 -->
        </span>
      </div>

      <div class="oe-sec">菜品明细</div>
      <t-table
        :data="form.items"
        :columns="cols"
        row-key="_key"
        size="small"
        :max-height="300"
      >
        <template #dishName="{ row }">
          <span
            class="oe-dish"
            :class="{ 'oe-gift-dish': row.isGift }"
            >{{ row.dishName }}</span
          >
          <span
            v-if="row.specName"
            class="chip"
            >{{ row.specName }}</span
          >
          <!-- 加菜批次标签(首单/加菜N):与顾客端/详情弹窗同口径,改单时能看清批次归属 -->
          <span
            v-if="batchTag(row)"
            class="chip oe-batch-chip"
            >{{ batchTag(row) }}</span
          >
          <span
            v-if="row.isGift"
            class="chip oe-gift-chip"
            >赠</span
          >
          <t-input
            v-if="row.isGift"
            v-model="row.giftReason"
            class="oe-gift-reason"
            size="small"
            placeholder="赠送原因(可选)"
            :maxlength="60"
          />
        </template>
        <template #price="{ row }"> ¥{{ money(row.price) }} </template>
        <template #quantity="{ row }">
          <span class="step">
            <button
              type="button"
              class="step-b"
              :disabled="row.quantity <= 1"
              @click="row.quantity -= 1"
            >
              −
            </button>
            <span class="step-n">{{ row.quantity }}</span>
            <button
              type="button"
              class="step-b"
              :disabled="row.quantity >= 99"
              @click="row.quantity += 1"
            >
              +
            </button>
          </span>
        </template>
        <template #amount="{ row }">
          <span class="money">¥{{ money(row.price * row.quantity) }}</span>
        </template>
        <template #op="{ row }">
          <t-button
            :theme="row.isGift ? 'warning' : 'default'"
            variant="text"
            size="small"
            @click="toggleGift(row)"
          >
            {{ row.isGift ? '取消赠送' : '赠送' }}
          </t-button>
          <t-button
            theme="danger"
            variant="text"
            size="small"
            @click="removeItem(row)"
          >
            删除
          </t-button>
        </template>
      </t-table>

      <div class="oe-add">
        <t-select
          v-model="add.dishId"
          class="oe-add-dish"
          placeholder="搜索并按分类选择菜品"
          :options="dishOptions"
          filterable
          clearable
          :loading="menuLoading"
          @change="onPickDish"
        />
        <t-select
          v-model="add.specId"
          placeholder="规格"
          :options="specOptions"
          :disabled="!add.dishId"
          style="width: 150px"
        />
        <t-button
          theme="primary"
          variant="outline"
          size="small"
          :disabled="!add.specId"
          @click="doAdd"
        >
          加入订单
        </t-button>
      </div>

      <div class="oe-field">
        <span class="k">整单备注</span>
        <t-input
          v-model="form.orderRemark"
          class="oe-remark"
          placeholder="如：不要香菜、先上凉菜"
          :maxlength="60"
        />
      </div>

      <div class="oe-sum">
        <span
          >菜品金额 <b>¥{{ money(dishTotal) }}</b></span
        >
        <span
          >茶位费(预估·{{ form?.personCount || 0 }}人) <b>¥{{ money(seatPreview) }}</b></span
        >
        <span
          v-if="giftTotal > 0"
          class="oe-gift-total"
          >赠送 <b>¥{{ money(giftTotal) }}</b></span
        >
        <span class="oe-hint">优惠与合计由服务端按当前促销规则重算</span>
      </div>

      <div class="oe-actions">
        <t-button
          variant="outline"
          @click="emit('update:visible', false)"
        >
          取消
        </t-button>
        <t-button
          theme="primary"
          :loading="saving"
          @click="submit"
        >
          保存并重算金额
        </t-button>
      </div>
    </div>
  </t-dialog>
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch } from 'vue';
import { MessagePlugin, DialogPlugin } from 'tdesign-vue-next';
import { getMenu, editOrder } from '@dining/api';
import { batchLabels } from '@dining/shared';
import { useIsMobile } from '../utils/useMobile';
import type { Dish, MenuCategory, OrderDetail } from '@dining/types';

interface EditFormItem {
  _key: number;
  dishId?: number;
  specId?: number;
  dishName?: string;
  specName?: string;
  price: number;
  quantity: number;
  itemRemark?: string;
  // 加菜批次:随提交原样回传,改单重建明细后顾客的加菜批次不丢;
  // 商家新加的菜归入「下一个批次」(见 nextBatchNo),后厨能按批次出菜。
  batchNo?: number;
  isGift?: number;
  giftReason?: string;
}

interface EditForm {
  personCount: number;
  orderRemark: string;
  items: EditFormItem[];
}

const props = withDefaults(
  defineProps<{
    visible: boolean;
    // 订单详情(含 items),由父级详情弹窗传入刚拉取的最新数据
    order: OrderDetail | null;
  }>(),
  {
    visible: false,
    order: null
  }
);
const emit = defineEmits<{
  (e: 'update:visible', value: boolean): void;
  (e: 'saved'): void;
}>();

const form = ref<EditForm | null>(null);
const saving = ref(false);
const add = reactive<{ dishId: number | undefined; specId: number | undefined }>({
  dishId: undefined,
  specId: undefined
});
const menu = ref<MenuCategory[]>([]);
const menuLoading = ref(false);
let seq = 0;

const { isMobile } = useIsMobile();

const money = (v: unknown): string => Number(v || 0).toFixed(2);

interface Col {
  colKey: string;
  title: string;
  width?: number;
  ellipsis?: boolean;
}

// 窄屏必须砍列：下面宽版的固定列宽合计 410px，而手机上弹窗可用宽度只有 ~330px，
// 摆不下就会变成「必须横向拖动才能看到删除按钮」。改份数才是改单的主操作，
// 单价与小计在收银机器上意义不大，优先保留它们之外的列。
const cols = computed<Col[]>(() =>
  isMobile.value
    ? [
        { colKey: 'dishName', title: '菜品', ellipsis: true },
        { colKey: 'quantity', title: '数量', width: 118 },
        { colKey: 'op', title: '操作', width: 150 }
      ]
    : [
        { colKey: 'dishName', title: '菜品' },
        { colKey: 'price', title: '单价', width: 100 },
        { colKey: 'quantity', title: '数量', width: 130 },
        { colKey: 'amount', title: '小计', width: 100 },
        { colKey: 'op', title: '操作', width: 150 }
      ]
);

// 把分类 → 菜品拍平成一张可搜索的下拉表:菜单通常只有几十道菜,
// 一个 filterable 下拉比「先选分类再选菜」少一次点击。
const dishOptions = computed(() => {
  const out: { value: number; label: string }[] = [];
  for (const c of menu.value) {
    for (const d of c.dishes || []) {
      out.push({ value: Number(d.dishId), label: `${c.categoryName} / ${d.dishName}` });
    }
  }
  return out;
});

const specOptions = computed(() => {
  const d = findDish(add.dishId);
  return (d?.specs || []).map(s => ({
    value: Number(s.specId),
    label: `${s.specName} ¥${Number(s.price || 0).toFixed(2)}`
  }));
});

// 菜品金额只累计「非赠送」项;赠送项按原价累计到 giftTotal,仅作让利展示。
const dishTotal = computed(() =>
  (form.value?.items || []).reduce((s, it) => (it.isGift ? s : s + Number(it.price || 0) * Number(it.quantity || 0)), 0)
);

const giftTotal = computed(() =>
  (form.value?.items || []).reduce((s, it) => (it.isGift ? s + Number(it.price || 0) * Number(it.quantity || 0) : s), 0)
);

// 茶位费按「当前订单的人均茶位费 × 新人数」估算。茶位费开关与单价都在服务端配置里,
// 前端读不到也不该读(pay 相关配置属敏感区),所以用现有订单反推人均值。
const seatPreview = computed(() => {
  const o = props.order;
  const pc = form.value?.personCount || 0;
  if (!o || !o.personCount || !o.seatFee) return 0;
  const unit = Number(o.seatFee) / Number(o.personCount);
  return Math.round(unit * pc * 100) / 100;
});

function findDish(id: number | undefined): Dish | null {
  for (const c of menu.value) {
    const d = (c.dishes || []).find(x => x.dishId === id);
    if (d) return d;
  }
  return null;
}

async function loadMenu(): Promise<void> {
  if (menu.value.length || menuLoading.value) return;
  menuLoading.value = true;
  try {
    menu.value = (await getMenu()) || [];
  } catch {
    /* 菜单拉取失败时仍可改份数/删项,只是加不了新菜 */
  } finally {
    menuLoading.value = false;
  }
}

function init(): void {
  const o = props.order;
  if (!o) return;
  form.value = {
    personCount: o.personCount || 1,
    orderRemark: o.orderRemark || '',
    items: (o.items || []).map(it => ({
      _key: ++seq,
      dishId: it.dishId,
      specId: it.specId,
      dishName: it.dishName,
      specName: it.specName,
      price: Number(it.price || 0),
      quantity: it.quantity || 1,
      itemRemark: it.itemRemark || '',
      batchNo: it.batchNo || 1,
      isGift: it.isGift || 0,
      giftReason: it.giftReason || ''
    }))
  };
  add.dishId = undefined;
  add.specId = undefined;
  loadMenu();
}

watch(
  () => props.visible,
  v => {
    if (v) init();
  }
);

function onClosed(): void {
  form.value = null;
}

function onPickDish(id: unknown): void {
  const d = findDish(Number(id));
  // 单规格菜品直接选中;多规格留空强制操作者显式选择,避免默认打成错的规格。
  const specs = d?.specs || [];
  add.specId = specs.length === 1 ? specs[0].specId : undefined;
}

// 加菜批次标签(_key → 首单/加菜N):与详情弹窗同一实现(@dining/shared 的 batchLabels)。
// 商家在改单里新加的菜会拿到新批次号,标签随之出现,提交前后所见一致。
const batchTagMap = computed(() => {
  const items = form.value?.items || [];
  const labels = batchLabels(items);
  const map = new Map<number, string>();
  items.forEach((it, i) => map.set(it._key, labels[i]));
  return map;
});

function batchTag(row: EditFormItem): string {
  return batchTagMap.value.get(row._key) || '';
}

// 商家改单新加的菜归入「下一个批次」:与顾客加菜同一编号逻辑(上次最大批次 +1),
// 后厨按批次出菜时,商家加的菜和顾客加的菜一视同仁。
function nextBatchNo(): number {
  return (form.value?.items || []).reduce((m, it) => Math.max(m, it.batchNo || 1), 0) + 1;
}

function doAdd(): void {
  const f = form.value;
  if (!f) return;
  const d = findDish(add.dishId);
  const s = (d?.specs || []).find(x => x.specId === add.specId);
  if (!d || !s) return;
  const hit = f.items.find(it => it.dishId === d.dishId && it.specId === s.specId);
  if (hit) {
    hit.quantity = Math.min(99, hit.quantity + 1);
    MessagePlugin.success(`【${d.dishName}】数量 +1`);
  } else {
    f.items.push({
      _key: ++seq,
      dishId: d.dishId,
      specId: s.specId,
      dishName: d.dishName,
      specName: s.specName,
      price: Number(s.price || 0),
      quantity: 1,
      itemRemark: '',
      batchNo: nextBatchNo(),
      isGift: 0,
      giftReason: ''
    });
    MessagePlugin.success(`已加入【${d.dishName}】`);
  }
  add.dishId = undefined;
  add.specId = undefined;
}

function removeItem(row: EditFormItem): void {
  const f = form.value;
  if (!f) return;
  // 最后一道菜必须在「点删除之前」拦住并说明原因。
  // 原来的写法是先删掉发现空了再塞回去，结果就是：点了删除、菜还在、也没任何提示 ——
  // 看上去就像删除按钮坏了。
  if (f.items.length <= 1) {
    MessagePlugin.warning('订单至少要保留一道菜；整单不要请用详情里的「取消订单」');
    return;
  }
  const label = row.specName ? `${row.dishName}（${row.specName}）` : row.dishName;
  const dlg = DialogPlugin.confirm({
    header: '移除菜品',
    body: `确认从订单中移除【${label}】？`,
    onConfirm: () => {
      f.items = f.items.filter(it => it._key !== row._key);
      dlg.hide();
    }
  });
}

// 切换「赠送」标记:赠送菜不计入应收(金额重算时由服务端记 0),
// 原价仍计入订单赠送额用于让利统计;原因可选,切换为赠送时清空重填。
function toggleGift(row: EditFormItem): void {
  row.isGift = row.isGift ? 0 : 1;
  if (row.isGift) row.giftReason = '';
}

async function submit(): Promise<void> {
  const f = form.value;
  const o = props.order;
  if (!f || !o) return;
  if (!f.items.length) {
    MessagePlugin.warning('订单明细不能为空');
    return;
  }
  saving.value = true;
  try {
    await editOrder({
      orderId: o.orderId,
      personCount: f.personCount || 1,
      orderRemark: f.orderRemark || '',
      items: f.items.map(it => ({
        dishId: it.dishId,
        specId: it.specId,
        quantity: it.quantity,
        itemRemark: it.itemRemark,
        batchNo: it.batchNo || 1,
        isGift: it.isGift || 0,
        giftReason: it.giftReason || ''
      }))
    });
    MessagePlugin.success('改单成功，金额已重算');
    emit('saved');
    emit('update:visible', false);
  } catch {
    /* 失败原因(如「菜品【X】已下架」)由请求拦截器统一提示,弹窗保持打开供继续修改 */
  } finally {
    saving.value = false;
  }
}
</script>

<style scoped>
.oe {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.oe-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px 24px;
  font-size: 14px;
}

.oe-hi {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.oe-hi .k {
  color: var(--ink-3);
  font-size: 13px;
}

.oe-hi .v {
  color: var(--ink);
  font-weight: 600;
}

.mono {
  font-family: ui-monospace, Menlo, Consolas, monospace;
  font-size: 13px;
}

.oe-sec {
  font-size: 13px;
  font-weight: 700;
  color: var(--ink-2);
}

.oe-dish {
  font-weight: 600;
}

.chip {
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

/* 赠送标记与原因输入 */
.oe-gift-dish {
  color: var(--ink-3);
}

.oe-gift-chip {
  background: #fff3e0;
  color: #e8850c;
  font-weight: 700;
}

/* 加菜批次标签:与「赠」标同为浅橙底,区别是不加粗(非强调,是归属信息) */
.oe-batch-chip {
  background: #fff3e0;
  color: #e8850c;
}

.oe-gift-reason {
  margin-top: 6px;
  width: 100%;
}

.oe-gift-total {
  color: #e8850c;
}

.step {
  display: inline-flex;
  align-items: center;
  border: 1px solid var(--line);
  border-radius: 6px;
  overflow: hidden;
}

.step-b {
  width: 26px;
  height: 26px;
  border: 0;
  background: #fafafa;
  color: var(--ink-2);
  font-size: 15px;
  line-height: 1;
  cursor: pointer;
}

.step-b:disabled {
  color: var(--ink-4);
  cursor: not-allowed;
}

.step-n {
  min-width: 34px;
  text-align: center;
  font-size: 13px;
  font-weight: 600;
}

.oe-add {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border-radius: 10px;
  background: #f7f8fa;
}

.oe-add-dish {
  flex: 1 1 240px;
  min-width: 200px;
}

.oe-field {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
}

.oe-field .k {
  flex: 0 0 auto;
  font-size: 13px;
  color: var(--ink-3);
}

/* min-width:0 是关键：flex 子项默认 min-width:auto，长内容会把容器顶开导致横向溢出 */
.oe-remark {
  flex: 1 1 220px;
  min-width: 0;
}

.oe-sum {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 6px 20px;
  font-size: 13px;
  color: var(--ink-2);
  justify-content: flex-end;
}

.oe-sum b {
  color: var(--brand-deep);
  font-size: 15px;
}

.oe-hint {
  font-size: 12px;
  color: var(--ink-3);
}

.oe-actions {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
}
</style>
