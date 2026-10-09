<template>
  <div
    class="op-bar"
    :class="{ lifted: showTabbar }"
  >
    <!-- 购物车图标 + 金额合成一个「展开购物车」热区:顾客习惯点整条,
         只有图标响应会让中间一大片点不动。去结算按钮不在这个区里,保持独立。
         空车也允许展开:清空后顾客仍能打开抽屉看空态/重新加菜。 -->
    <div
      class="op-cartarea"
      @click="emit('open')"
    >
      <div
        class="op-cartbtn"
        :class="{ has: totalCount > 0 }"
      >
        <svg
          viewBox="0 0 24 24"
          width="22"
          height="22"
          fill="#fff"
        >
          <path
            d="M7 18a2 2 0 1 0 0 4 2 2 0 0 0 0-4zm10 0a2 2 0 1 0 0 4 2 2 0 0 0 0-4zM3 2h2.6l.9 2H21a1 1 0 0 1 .94 1.34l-2.6 7.3A2 2 0 0 1 17.46 14H8.4l-.4 2H19v2H6.72a1 1 0 0 1-.98-1.2L6.6 12 4.4 4H3a1 1 0 0 1 0-2z"
          />
        </svg>
        <span
          v-if="totalCount > 0"
          class="b"
          >{{ totalCount }}</span
        >
      </div>
      <div class="op-total">
        <div class="l">
          合计
          <!-- 加菜时金额只含本次追加的菜品,与购物车抽屉的「仅加菜」标注同口径 -->
          <span
            v-if="canAppend"
            class="l-tag"
            >（仅加菜）</span
          >
        </div>
        <div class="v"><small>¥</small>{{ totalPrice }}</div>
      </div>
    </div>
    <div
      class="op-checkout"
      :class="{ disabled: !totalCount }"
      @click="emit('open')"
    >
      {{ actionText }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue';

const props = defineProps<{
  showTabbar: boolean;
  totalCount: number;
  totalPrice: number;
  canAppend: boolean;
}>();

const emit = defineEmits<{
  open: [];
}>();

// 按钮文案:空车提示「请选择菜品」;加菜场景是「去加菜」;其余「去结算」。
// 与小程序 cart-bar 的 actionText 口径一致(加菜区分放在按钮上,金额标签统一写「合计」)。
const actionText = computed(() =>
  props.totalCount > 0 ? (props.canAppend ? '去加菜' : '去结算') : '请选择菜品'
);
</script>

<style scoped>
/* ---------- 底部悬浮结算栏 ---------- */
.op-bar {
  position: fixed;
  left: 50%;
  bottom: calc(12px + env(safe-area-inset-bottom));
  transform: translateX(-50%);
  width: calc(100% - 20px);
  max-width: 460px;
  height: 54px;
  background: #fff;
  border-radius: var(--r-pill);
  box-shadow: var(--shadow-2);
  display: flex;
  align-items: center;
  gap: 11px;
  padding: 0 7px;
  z-index: 90;
}

/* 底部有 tabbar(高 58px)时结算栏整体上移,上方留 4px 间隙,减少纵向占用 */
.op-bar.lifted {
  bottom: calc(62px + env(safe-area-inset-bottom));
}

/* 图标 + 金额的热区:整块都能展开购物车(去结算按钮不在此区) */
.op-cartarea {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 11px;
  cursor: pointer;
}

.op-cartbtn {
  position: relative;
  width: 42px;
  height: 42px;
  border-radius: 50%;
  background: #e5e5e5;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  flex-shrink: 0;
  cursor: pointer;
  transition: background 0.2s;
}

.op-cartbtn.has {
  background: linear-gradient(135deg, var(--brand), var(--brand-deep));
}

/* 飞球落袋后的弹性反馈:OrderPage 在 animationend 时挂上 cart-bump 类 */
.op-cartbtn.cart-bump {
  animation: cart-bump 0.35s cubic-bezier(0.3, 1.6, 0.5, 1);
}

@keyframes cart-bump {
  0% {
    transform: scale(1);
  }

  35% {
    transform: scale(1.18);
  }

  100% {
    transform: scale(1);
  }
}

/* 份数角标。
   注意:本工程没有全局 `box-sizing: border-box`,默认的 content-box 会让
   min-width/height 只作用于内容区,border + padding 再往外撑——
   结果是单个数字也被撑成 29px 宽,且 line-height(13px) 小于内容高(17px),
   数字顶在内容区上沿、视觉偏高约 2px。
   这里显式 border-box + flex 居中:尺寸可控,且无论几位数字都真正居中。 */
.op-cartbtn .b {
  position: absolute;
  top: -4px;
  right: -4px;
  display: flex;
  align-items: center;
  justify-content: center;
  box-sizing: border-box;
  min-width: 18px;
  height: 18px;
  padding: 0 5px;
  border-radius: var(--r-pill);
  background: var(--ink);
  border: 2px solid #fff;
  color: #fff;
  font-size: 10px;
  font-weight: 700;
  line-height: 1;
}

.op-total {
  flex: 1;
}

.op-total .l {
  font-size: 9.5px;
  color: var(--ink-4);
}

/* 加菜态的「（仅加菜）」标注:跟在「合计」后,小一号、品牌色 */
.op-total .l-tag {
  font-size: 9px;
  color: var(--brand-deep);
}

.op-total .v {
  font-size: 18px;
  font-weight: 800;
  color: var(--ink);
  line-height: 1.1;
}

.op-total .v small {
  font-size: 11px;
}

.op-checkout {
  height: 42px;
  padding: 0 22px;
  border-radius: var(--r-pill);
  background: linear-gradient(135deg, var(--brand), var(--brand-deep));
  color: #fff;
  font-size: 13px;
  font-weight: 600;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  cursor: pointer;
  transition: opacity 0.2s;
}

.op-checkout.disabled {
  background: #d8d8d8;
  cursor: not-allowed;
}

@media (width <= 360px) {
  .op-checkout {
    padding: 0 18px;
  }
}
</style>
