<template>
  <div class="op-body">
    <div class="op-side">
      <div
        v-for="c in menu"
        :key="c.categoryId"
        class="op-cat"
        :class="{ on: activeCat === c.categoryId }"
        @click="jumpTo(c.categoryId)"
      >
        {{ c.categoryName }}
      </div>
    </div>

    <div
      :ref="setListRef"
      class="op-list"
      :class="{ lifted: showTabbar }"
      @scroll="handleScroll"
    >
      <template
        v-for="c in menu"
        :key="c.categoryId"
      >
        <div
          :ref="el => setGroupRef(c.categoryId, el)"
          class="op-group-t"
        >
          {{ c.categoryName }}
        </div>
        <div
          v-if="!c.dishes || !c.dishes.length"
          class="empty-group"
        >
          暂无菜品
        </div>

        <div
          v-for="d in c.dishes || []"
          :key="d.dishId"
          class="op-dish"
        >
          <div class="op-img">
            <img
              v-if="d.dishImage && !imgErr(d.dishId)"
              :src="d.dishImage || ''"
              :alt="d.dishName || ''"
              loading="lazy"
              @error="onImgErr(d.dishId)"
            />
            <div
              v-else
              class="img-fallback"
            >
              <svg
                viewBox="0 0 24 24"
                width="26"
                height="26"
                fill="#dcd4cc"
              >
                <path
                  d="M4 3h16a1 1 0 0 1 1 1v16a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1zm1 2v9l3.5-3.5L12 14l3-3 5 5V5H5zm10 1.5A1.5 1.5 0 1 1 13.5 8 1.5 1.5 0 0 1 15 6.5z"
                />
              </svg>
              <span>暂无图片</span>
            </div>
          </div>

          <div class="op-info">
            <div class="op-name">
              {{ d.dishName }}
              <!-- 加菜时标注已下单份数,否则顾客只看到空加号,容易重复点 -->
              <span
                v-if="orderedQty(d.dishId)"
                class="op-ordered"
                >已点 {{ orderedQty(d.dishId) }}</span
              >
            </div>
            <div
              v-if="d.description"
              class="op-desc"
            >
              {{ d.description }}
            </div>

            <div
              v-if="d.specs && d.specs.length > 1"
              class="op-specs"
            >
              <span
                v-for="s in d.specs || []"
                :key="s.specId"
                class="op-spec"
                :class="{ on: specOn(d, s) }"
                @click="selectSpec(d, s)"
                >{{ s.specName }}</span
              >
            </div>

            <div class="op-bottom">
              <div class="op-price">
                ¥{{ currentSpec(d).price }}
                <small v-if="d.specs && d.specs.length === 1">/{{ singleSpecName(d) }}</small>
              </div>
              <div
                v-if="dishQty(d.dishId) > 0"
                class="op-stepper"
              >
                <span
                  class="m"
                  @click="changeQty(d, -1)"
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
                <span class="n">{{ dishQty(d.dishId) }}</span>
                <span
                  class="p"
                  @click="changeQty(d, 1)"
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
              <span
                v-else
                class="op-add"
                @click="changeQty(d, 1)"
                ><svg
                  viewBox="0 0 24 24"
                  width="16"
                  height="16"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2.5"
                  stroke-linecap="round"
                >
                  <path d="M12 5v14M5 12h14" /></svg
              ></span>
            </div>
          </div>
        </div>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { Dish, DishSpec, MenuCategory } from '../../types/entities';

const props = defineProps<{
  menu: MenuCategory[];
  selectedSpec: Record<number, number>;
  imgErrs: Set<number>;
  showTabbar: boolean;
  activeCat: number | null;
  orderedQty: (dishId: number | undefined) => number;
  currentSpec: (d: Dish) => DishSpec;
  dishQty: (dishId: number | undefined) => number;
  selectSpec: (d: Dish, s: DishSpec) => void;
  changeQty: (d: Dish, delta: number) => void;
  onImgErr: (id: number | undefined) => void;
  setListRef: (el: unknown) => void;
  setGroupRef: (categoryId: number | undefined, el: unknown) => void;
  jumpTo: (categoryId: number | undefined) => void;
  handleScroll: () => void;
}>();

function specOn(d: Dish, s: DishSpec): boolean {
  return props.selectedSpec[d.dishId as number] === s.specId;
}

function imgErr(id: number | undefined): boolean {
  return id !== undefined && props.imgErrs.has(id);
}

function singleSpecName(d: Dish): string {
  const specs = d.specs || [];
  return specs.length === 1 ? specs[0]?.specName || '' : '';
}
</script>

<style scoped>
/* ---------- 菜单主体:左分类栏 + 右菜品列表 ---------- */
.op-body {
  flex: 1;
  margin-top: -16px;
  background: #fff;
  border-radius: 18px 18px 0 0;
  overflow: hidden;
  display: flex;
  min-height: 0;

  /* hero 是定位元素,内容块是 static,默认 hero 会盖住这块上移的 16px:
     导致点分类后该组标题被橙色横幅切掉一半。加定位+z-index 让白色内容层压在 hero 之上 */
  position: relative;
  z-index: 1;
}

/* 左侧分类栏:竖排,选中项白底 + 品牌色竖条,与右侧列表滚动联动 */
.op-side {
  width: 86px;
  flex-shrink: 0;
  background: #f7f7f7;
  overflow-y: auto;

  /* 底部留白:结算栏与 tabbar 会盖住这一段 */
  padding: 6px 0 120px;
  scrollbar-width: none;
}

.op-side::-webkit-scrollbar {
  display: none;
}

.op-cat {
  position: relative;
  padding: 13px 8px;
  font-size: 12.5px;
  line-height: 1.35;
  color: var(--ink-3);
  text-align: center;
  cursor: pointer;
  transition: all 0.15s;
}

.op-cat.on {
  background: #fff;
  color: var(--brand-deep);
  font-weight: 700;
}

.op-cat.on::before {
  content: '';
  position: absolute;
  left: 0;
  top: 50%;
  transform: translateY(-50%);
  width: 3px;
  height: 16px;
  border-radius: 0 3px 3px 0;
  background: linear-gradient(180deg, var(--brand), var(--brand-deep));
}

.op-list {
  flex: 1;
  min-width: 0;
  overflow-y: auto;
  padding: 6px 12px 96px;
}

/* 存在底部 tabbar 时,额外留出一条导航的高度,否则最后一道菜会被挡住 */
.op-list.lifted {
  padding-bottom: 142px;
}

.op-group-t {
  font-size: 11px;
  font-weight: 700;
  color: var(--ink-4);
  padding: 10px 2px 6px;
}

.empty-group {
  padding: 30px 0;
  text-align: center;
  color: var(--ink-4);
  font-size: 13px;
}

.op-dish {
  display: flex;
  gap: 11px;
  padding: 11px 0;
  border-bottom: 1px solid #f5f5f5;
}

.op-dish:last-child {
  border-bottom: none;
}

.op-img {
  width: 84px;
  height: 84px;
  border-radius: var(--r-md);
  flex-shrink: 0;
  overflow: hidden;
  background: #f5f5f5;
}

.op-img img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.img-fallback {
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 4px;
  background: #f7f5f2;
}

.img-fallback span {
  font-size: 10px;
  color: var(--ink-4);
}

.op-info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.op-name {
  font-size: 14px;
  font-weight: 600;
  color: var(--ink);
}

/* 已下单份数标记:加菜时提示顾客这道菜已经点过,避免重复下单 */
.op-ordered {
  display: inline-block;
  margin-left: 6px;
  padding: 1px 6px;
  border-radius: var(--r-pill);
  background: var(--brand-soft);
  color: var(--brand-deep);
  font-size: 10px;
  font-weight: 600;
  vertical-align: 1px;
}

.op-desc {
  font-size: 10.5px;
  color: var(--ink-4);
  margin-top: 2px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.op-specs {
  display: flex;
  gap: 5px;
  margin-top: 5px;
  flex-wrap: wrap;
}

.op-spec {
  font-size: 10.5px;
  padding: 2px 8px;
  border-radius: var(--r-pill);
  border: 1px solid var(--line);
  color: var(--ink-3);
  background: #fafafa;
  cursor: pointer;
  transition: all 0.12s;
}

.op-spec.on {
  border-color: var(--brand);
  background: var(--brand-soft);
  color: var(--brand-deep);
  font-weight: 600;
}

.op-bottom {
  margin-top: auto;
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding-top: 6px;
}

.op-price {
  font-size: 16px;
  font-weight: 800;
  color: var(--brand-deep);
}

.op-price small {
  font-size: 10px;
  font-weight: 400;
  color: var(--ink-4);
  margin-left: 3px;
}

.op-add {
  width: 26px;
  height: 26px;
  border-radius: 50%;
  background: linear-gradient(135deg, var(--brand), var(--brand-deep));
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  box-shadow: 0 3px 8px rgb(240 72 31 / 30%);
  cursor: pointer;
  transition: transform 0.1s;
}

.op-add:active {
  transform: scale(0.88);
}

.op-stepper {
  display: flex;
  align-items: center;
  gap: 8px;
}

.op-stepper .m,
.op-stepper .p {
  width: 22px;
  height: 22px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
}

.op-stepper .m {
  border: 1.5px solid #ffd5c4;
  color: var(--brand);
  background: #fff;
}

.op-stepper .p {
  background: linear-gradient(135deg, var(--brand), var(--brand-deep));
  color: #fff;
}

.op-stepper .n {
  font-size: 12.5px;
  font-weight: 700;
  min-width: 14px;
  text-align: center;
}

@media (width <= 360px) {
  .op-img {
    width: 76px;
    height: 76px;
  }
}
</style>
