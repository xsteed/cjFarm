<template>
  <button
    ref="el"
    type="button"
    class="m-fab"
    :class="{ 'm-fab-dragging': dragging, 'm-fab-anim': !dragging }"
    :style="fabStyle"
    @pointerdown="onDown"
    @click="onClick"
  >
    <slot />
  </button>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import { clampFabPosition, nearestFabSide, snappedFabX } from '../utils/fabPosition';
import type { FabLayout, FabPoint } from '../utils/fabPosition';

// 可拖拽的悬浮主操作按钮:
// - 拖动可把它挪开,避免挡住页面上的操作位置;松手后吸附到最近的左/右边缘;
// - 位置记在 localStorage,下次进入沿用;
// - 位移小于阈值视为「点按」,照常触发 click(拖动结束的那次 click 不算)。

const emit = defineEmits<{ (e: 'click'): void }>();

const STORAGE_KEY = 'admin_mobile_fab_pos';
const DEFAULT_TABBAR_H = 54; // 拿不到 tabbar 高度时的兜底值

const el = ref<HTMLButtonElement | null>(null);
const pos = ref<{ x: number; y: number } | null>(null);
const dragging = ref(false);

let pointerId: number | null = null;
let startX = 0;
let startY = 0;
let originX = 0;
let originY = 0;
let moved = false;

const fabStyle = computed(() => {
  if (!pos.value) return undefined; // 未初始化时沿用 CSS 默认(右下)
  return { left: `${pos.value.x}px`, top: `${pos.value.y}px`, right: 'auto', bottom: 'auto' };
});

function size(): { w: number; h: number } {
  const rect = el.value?.getBoundingClientRect();
  return { w: rect?.width || 80, h: rect?.height || 40 };
}

/** 组装当前尺寸与视口参数;底部预留取实际 tabbar 高度(含安全区)+ 8px */
function layout(): FabLayout {
  const { w, h } = size();
  const tabbar = document.querySelector('.tabbar');
  const tabH = tabbar instanceof HTMLElement ? tabbar.getBoundingClientRect().height : 0;
  return {
    width: w,
    height: h,
    viewportWidth: window.innerWidth,
    viewportHeight: window.innerHeight,
    bottomReserve: (tabH || DEFAULT_TABBAR_H) + 8
  };
}

function clampPos(x: number, y: number): FabPoint {
  return clampFabPosition({ x, y }, layout());
}

function loadPos(): { x: number; y: number } | null {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return null;
    const p = JSON.parse(raw) as { x?: unknown; y?: unknown };
    if (typeof p?.x === 'number' && typeof p?.y === 'number') return { x: p.x, y: p.y };
  } catch {
    /* localStorage 不可用(隐私模式)时忽略 */
  }
  return null;
}

function savePos(): void {
  if (!pos.value) return;
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(pos.value));
  } catch {
    /* 忽略写入失败 */
  }
}

function onDown(e: PointerEvent): void {
  if (e.button > 0) return;
  const rect = el.value?.getBoundingClientRect();
  if (!rect) return;
  originX = pos.value?.x ?? rect.left;
  originY = pos.value?.y ?? rect.top;
  startX = e.clientX;
  startY = e.clientY;
  moved = false;
  dragging.value = true;
  pointerId = e.pointerId;
  el.value?.setPointerCapture(e.pointerId);
}

function onMove(e: PointerEvent): void {
  if (!dragging.value || e.pointerId !== pointerId) return;
  const dx = e.clientX - startX;
  const dy = e.clientY - startY;
  if (!moved && Math.hypot(dx, dy) > 6) moved = true;
  if (!moved) return;
  pos.value = clampPos(originX + dx, originY + dy);
}

function onUp(e: PointerEvent): void {
  if (e.pointerId !== pointerId) return;
  dragging.value = false;
  pointerId = null;
  try {
    el.value?.releasePointerCapture(e.pointerId);
  } catch {
    /* 已释放 */
  }
  if (!moved || !pos.value) return;
  // 吸附到最近的左/右边缘,避免停在内容中央继续挡住操作
  const l = layout();
  const side = nearestFabSide(pos.value.x, l.width, l.viewportWidth);
  pos.value = clampFabPosition({ x: snappedFabX(l, side), y: pos.value.y }, l);
  savePos();
}

function onClick(): void {
  if (moved) return; // 拖动产生的 click 不触发新增
  emit('click');
}

function onResize(): void {
  if (pos.value) pos.value = clampPos(pos.value.x, pos.value.y);
}

onMounted(() => {
  const saved = loadPos();
  if (saved) {
    pos.value = clampPos(saved.x, saved.y);
  }
  // 没有保存位置时保持 pos=null,直接用 CSS 的右下角定位 —— 首次渲染不再做一次
  // 「JS 夹取值 vs CSS 值」的换算(两者边距/底部预留口径本就差几像素,会让按钮跳一下)。
  // 首次拖拽时 onDown 从 getBoundingClientRect 取起点,无需预先转换。
  window.addEventListener('pointermove', onMove);
  window.addEventListener('pointerup', onUp);
  window.addEventListener('pointercancel', onUp);
  window.addEventListener('resize', onResize);
});

onBeforeUnmount(() => {
  window.removeEventListener('pointermove', onMove);
  window.removeEventListener('pointerup', onUp);
  window.removeEventListener('pointercancel', onUp);
  window.removeEventListener('resize', onResize);
});
</script>
