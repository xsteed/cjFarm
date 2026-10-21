<template>
  <!--
    规格 / 单项备注弹层（对齐小程序 components/spec-sheet）。
    规格多到行内放不下时由它接手：一次把规格、口味、其他要求都定好再加进购物车，
    不用先加进去再跑到购物车里补备注。
  -->
  <div
    class="spec-mask"
    @click.self="emit('close')"
  >
    <div class="spec-sheet">
      <div class="spec-head">
        <span class="spec-title">{{ dishName }}</span>
        <span
          class="spec-close"
          @click="emit('close')"
          >×</span
        >
      </div>

      <div class="spec-body">
        <div
          v-if="specs.length > 1"
          class="spec-sec"
        >
          <div class="sec-title">规格</div>
          <div class="chips">
            <span
              v-for="s in specs"
              :key="s.specId"
              class="chip"
              :class="{ on: Number(s.specId) === selectedSpecId, off: Number(s.stock) === 0 }"
              @click="emit('pick-spec', Number(s.specId))"
              >{{ s.specName }}<template v-if="Number(s.stock) === 0"> 售罄</template
              ><template v-else> ¥{{ money(s.price) }}</template></span
            >
          </div>
        </div>

        <div
          v-if="remarks.length"
          class="spec-sec"
        >
          <div class="sec-title">口味备注</div>
          <div class="chips">
            <span
              v-for="r in remarks"
              :key="r.remarkId"
              class="chip"
              :class="{ on: selected.includes(r.optionName || '') }"
              @click="emit('toggle-remark', r.optionName || '')"
              >{{ r.optionName }}</span
            >
          </div>
        </div>

        <div class="spec-sec">
          <div class="sec-title">其他要求</div>
          <input
            :value="custom"
            class="spec-input"
            placeholder="如：不要辣、少放盐"
            maxlength="50"
            @input="onCustomInput"
          />
        </div>
      </div>

      <!-- 已在购物车里的菜再点开：明确告知「确认添加」是加几份，改备注请去购物车 -->
      <div
        v-if="inCartQty"
        class="spec-hint"
      >
        购物车已有 {{ inCartQty }} 份，点「确认添加」会再加 {{ qty }} 份
      </div>

      <div class="spec-foot">
        <!-- 数量步进器单独一行:左「数量」标签,右步进器(默认 1,最小 1,最大 99)。
             加减号用 SVG 短棒而不是字体「− / +」：字形跟着基线走，不同机型上下偏心 ——
             与菜单页 op-stepper 同一画法同一尺寸，几何居中不依赖字体度量。 -->
        <div class="ft-qty-row">
          <span class="ft-qty-label">数量</span>
          <div class="ft-stepper">
            <span
              class="ft-minus"
              :class="{ off: qty <= 1 }"
              @click="setQty(qty - 1)"
              ><svg
                viewBox="0 0 24 24"
                width="13"
                height="13"
                fill="none"
                stroke="currentColor"
                stroke-width="2.5"
                stroke-linecap="round"
              >
                <path d="M5 12h14" /></svg
            ></span>
            <span class="ft-num">{{ qty }}</span>
            <span
              class="ft-plus"
              @click="setQty(qty + 1)"
              ><svg
                viewBox="0 0 24 24"
                width="13"
                height="13"
                fill="none"
                stroke="currentColor"
                stroke-width="2.5"
                stroke-linecap="round"
              >
                <path d="M12 5v14M5 12h14" /></svg
            ></span>
          </div>
        </div>
        <div class="ft-bottom">
          <div class="ft-price"><i>¥</i>{{ priceText }}</div>
          <button
            class="sp-btn"
            @click="emit('confirm')"
          >
            确认添加
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import { money } from '@dining/shared/money';
import type { DishSpec, RemarkOption } from '@dining/types';

const props = defineProps<{
  dishName: string;
  specs: DishSpec[];
  remarks: RemarkOption[];
  selectedSpecId: number;
  selected: string[];
  custom: string;
  inCartQty: number;
  /** 弹层内购买数量:父层 useCart.sheetQty,默认 1 */
  qty: number;
}>();

const emit = defineEmits<{
  'pick-spec': [specId: number];
  'toggle-remark': [name: string];
  'update:custom': [value: string];
  'update:qty': [value: number];
  close: [];
  confirm: [];
}>();

// 与小程序 syncPrice 同口径：单价 × 数量。改规格与改数量都要跟着变
const cur = computed(() => props.specs.find(s => Number(s.specId) === props.selectedSpecId));
const priceText = computed(() => money((Number(cur.value?.price) || 0) * props.qty));

const QTY_MIN = 1;
const QTY_MAX = 99;
function setQty(n: number): void {
  const v = Math.max(QTY_MIN, Math.min(QTY_MAX, Math.floor(n) || QTY_MIN));
  emit('update:qty', v);
}

function onCustomInput(e: Event): void {
  emit('update:custom', (e.target as HTMLInputElement).value);
}
</script>

<style scoped>
/* 层级同 RemarkSheet:要高过购物车抽屉(TDesign .t-drawer 是 1500) */
.spec-mask {
  position: fixed;
  inset: 0;
  background: rgb(0 0 0 / 50%);
  z-index: 1600;
  display: flex;
  align-items: flex-end;
  justify-content: center;
}

.spec-sheet {
  display: flex;
  flex-direction: column;
  width: 100%;
  max-width: 480px;
  max-height: 82vh;
  background: #fff;
  border-radius: var(--r-lg) var(--r-lg) 0 0;
  padding: 20px 18px calc(20px + env(safe-area-inset-bottom));
  box-sizing: border-box;
}

.spec-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-shrink: 0;
}

.spec-title {
  font-size: 16px;
  font-weight: 700;
  color: var(--ink);
}

.spec-close {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  margin-right: -6px;
  color: var(--ink-4);
  font-size: 20px;
  line-height: 1;
  cursor: pointer;
}

/* 内容区自己滚,页脚与价格始终露在外面 */
.spec-body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  margin: 0 -4px;
  padding: 0 4px;
}

.spec-sec {
  margin-top: 18px;
}

.sec-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--ink-2);
}

.chips {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 10px;
}

.chip {
  padding: 8px 16px;
  border-radius: var(--r-pill);
  border: 1.5px solid var(--line);
  background: #fafafa;
  color: var(--ink-2);
  font-size: 13px;
  cursor: pointer;
  transition: all 0.12s;
}

.chip.on {
  border-color: var(--brand);
  background: var(--brand-soft);
  color: var(--brand-deep);
  font-weight: 600;
}

/* 售罄规格：灰显点不动（useCart.pickSheetSpec 再拦一道） */
.chip.off {
  border-color: #eee;
  background: #f6f6f6;
  color: var(--ink-4);
  cursor: not-allowed;
}

.spec-input {
  width: 100%;
  box-sizing: border-box;
  margin-top: 10px;
  padding: 10px 14px;
  border: 1.5px solid var(--line);
  border-radius: var(--r-md);
  font-size: 13.5px;
  color: var(--ink);
  outline: none;
}

.spec-input:focus {
  border-color: var(--brand);
}

/* 加购提示：避免把「改备注」误当成「再加一份」 */
.spec-hint {
  flex-shrink: 0;
  margin-top: 14px;
  padding: 8px 12px;
  border-radius: var(--r-md);
  background: var(--brand-ghost);
  color: var(--ink-3);
  font-size: 12px;
}

.spec-foot {
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-top: 16px;
}

/* 数量行:左「数量」标签,右步进器 */
.ft-qty-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.ft-qty-label {
  font-size: 13px;
  color: var(--ink-2);
}

/* 数量步进器:左栏固定,价格占剩余,「确认添加」靠右(与小程序 spec-sheet 底栏布局一致) */
.ft-stepper {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: 8px;
}

/* 与菜单页 op-stepper 同尺寸同画法（22px 圆 + SVG 短棒）：弹层是从菜品行
   点进来的，同一套控件两处观感必须一致。字体「− / +」的字形随基线走、
   不同机型上下偏心，弃用；SVG 以几何中心绘制，天然居中。 */
.ft-minus,
.ft-plus {
  width: 22px;
  height: 22px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  cursor: pointer;
  user-select: none;
  -webkit-user-select: none;
}

.ft-minus {
  border: 1.5px solid #ffd5c4;
  color: var(--brand);
  background: #fff;
}

.ft-plus {
  background: linear-gradient(135deg, var(--brand), var(--brand-deep));
  color: #fff;
}

/* 已到最小份:减号置灰且点不动,不允许把下单数减到 0(要减去购物车里改) */
.ft-minus.off {
  border-color: #e3e0da;
  color: #cfc9c0;
  cursor: not-allowed;
}

.ft-num {
  min-width: 14px;
  text-align: center;
  font-size: 12.5px;
  font-weight: 700;
  color: var(--ink);
}

/* 金额行:左下角总金额(单价 × 数量),右侧确定按钮 */
.ft-bottom {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.ft-price {
  color: var(--brand-deep);
  font-size: 20px;
  font-weight: 700;
}

.ft-price i {
  font-size: 13px;
  font-style: normal;
  margin-right: 1px;
}

.sp-btn {
  flex-shrink: 0;
  height: 44px;
  padding: 0 34px;
  border: none;
  border-radius: var(--r-pill);
  background: linear-gradient(135deg, var(--brand), var(--brand-deep));
  color: #fff;
  font-size: 15.5px;
  font-weight: 600;
  cursor: pointer;
}
</style>
