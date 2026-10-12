<template>
  <div class="op-wrap">
    <div class="op-body">
      <!-- 搜索态藏起左栏:搜出来的结果本就不按分类浏览,分类名反而占地方 -->
      <div
        v-if="!searching"
        class="op-side"
      >
        <div
          v-for="c in menu"
          :key="c.categoryId"
          class="op-cat"
          :class="{ on: activeCat === c.categoryId }"
          @click="jumpTo(c.categoryId)"
        >
          {{ c.categoryName }}
          <!-- 分类内已点份数角标:0 时不渲染 -->
          <span
            v-if="catQty(c.categoryId)"
            class="cat-badge"
            >{{ catQty(c.categoryId) }}</span
          >
        </div>
      </div>

      <div
        :ref="bindList"
        class="op-list"
        :class="{ lifted: showTabbar }"
        @scroll="handleScroll"
      >
        <!-- 搜了没命中 / 菜单整体为空:给一句明确的「怎么办」 -->
        <div
          v-if="searching && !filteredMenu.length"
          class="op-empty"
        >
          没有找到「{{ keyword.trim() }}」相关的菜品，换个词试试
        </div>
        <div
          v-else-if="!filteredMenu.length"
          class="op-empty"
        >
          菜单暂时为空，请联系店员
        </div>
        <template
          v-for="c in filteredMenu"
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

          <!-- 整行可点 = 打开规格/备注弹层(与小程序一致);行内的 chip 与加减号各自 .stop 拦掉冒泡 -->
          <div
            v-for="d in c.dishes || []"
            :key="d.dishId"
            class="op-dish"
            @click="openSheet(d)"
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
              <!-- 招牌角标:图片左上角,帮顾客快速锁定镇店菜(与售罄蒙版互不干扰) -->
              <span
                v-if="Number(d.signature) === 1"
                class="op-signature"
              >招牌</span>
              <!-- 售罄蒙版:盖住图片(或占位图),与菜名后的「售罄」标签互相印证,与小程序端一致 -->
              <div
                v-if="isSoldOut(d)"
                class="op-soldout-mask"
              >
                售罄
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
                <span
                  v-if="isSoldOut(d)"
                  class="op-soldout-tag"
                  >售罄</span
                >
              </div>
              <div
                v-if="d.description"
                class="op-desc"
              >
                {{ d.description }}
              </div>

              <div
                v-if="inlineSpecs(d)"
                class="op-specs"
              >
                <span
                  v-for="s in d.specs || []"
                  :key="s.specId"
                  class="op-spec"
                  :class="{ on: specOn(d, s), off: isSpecSoldOut(s) }"
                  @click.stop="selectSpec(d, s)"
                  >{{ s.specName }}<template v-if="isSpecSoldOut(s)"> 售罄</template></span
                >
              </div>

              <div class="op-bottom">
                <div class="op-price">
                  ¥{{ priceText(d) }}
                  <small v-if="d.specs && d.specs.length === 1">/{{ singleSpecName(d) }}</small>
                  <!-- 规格多到行内放不下:先给最低价,具体选哪个进弹层(与小程序同口径) -->
                  <small v-else-if="isManySpecs(d)">起</small>
                </div>
                <!-- 已在购物车里的菜：即使随后沽清也要显示步进器 ——
                     「−」在购物车层面本来就是允许的（见 useCart.changeQty，减号不受沽清影响），
                     把加减号整体换成「售罄」会让顾客在卡片上减不掉，只能进购物车抽屉删。
                     此时「+」置灰且不可点（不能再加），价格仍照常展示。 -->
                <div
                  v-if="dishQty(d.dishId, d) > 0"
                  class="op-stepper"
                >
                  <span
                    class="m"
                    @click.stop="changeQty(d, -1)"
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
                  <span class="n"
                    >{{ dishQty(d.dishId, d) }}</span
                  >
                  <span
                    class="p"
                    :class="{ off: !canAddMore(d) }"
                    @click.stop="onAdd(d, $event)"
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
                  v-else-if="isSoldOut(d)"
                  class="op-soldout"
                  >售罄</span
                >
                <span
                  v-else-if="!isOrderable(d)"
                  class="op-soldout"
                  >暂不可点</span
                >
                <span
                  v-else
                  class="op-add"
                  @click.stop="onAdd(d, $event)"
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
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, shallowRef, watch } from 'vue';
import { INLINE_SPEC_MAX, filterMenu, isManySpecs, isOrderable, isSearching, isSoldOut } from '@dining/customer-core';
import type { CartItem } from '@dining/customer-core';
import { money } from '@dining/shared/money';
import type { Dish, DishSpec, MenuCategory } from '@dining/types';

// cart 可选:分类角标只是锦上添花,调用方(含单测)不传也不该渲染失败
const props = defineProps<{
  menu: MenuCategory[];
  /** 购物车条目:用于计算各分类的已点份数角标 */
  cart?: CartItem[];
  /** 搜索关键词：由页面层持有（搜索框在白卡顶部），这里只负责按它过滤 */
  keyword: string;
  selectedSpec: Record<number, number>;
  imgErrs: Set<number>;
  showTabbar: boolean;
  activeCat: number | null;
  orderedQty: (dishId: number | undefined) => number;
  currentSpec: (d: Dish) => DishSpec;
  /** 第二参传 dish 时按「当前生效规格」计数,与 +/- 改动的条目保持一致(见 useCart.dishQty) */
  dishQty: (dishId: number | undefined, dish?: Dish) => number;
  selectSpec: (d: Dish, s: DishSpec) => void;
  changeQty: (d: Dish, delta: number) => void;
  onImgErr: (id: number | undefined) => void;
  setListRef: (el: unknown) => void;
  setGroupRef: (categoryId: number | undefined, el: unknown) => void;
  jumpTo: (categoryId: number | undefined) => void;
  handleScroll: () => void;
  /** 打开规格/备注弹层（多规格菜点「+」或点菜名时） */
  openSheet: (d: Dish) => void;
}>();

const emit = defineEmits<{
  /** 直接加菜成功:把「+」的点击坐标抛给页面层,做小球飞入购物车动画 */
  fly: [x: number, y: number];
}>();

// ---------- 搜索 ----------
// 只有真的输入了内容才算搜索态:由此决定左栏是否收起(见模板的 op-side)
const searching = computed(() => isSearching(props.keyword));
const filteredMenu = computed(() => filterMenu(props.menu, props.keyword));

// 列表容器:scroll-spy 也要(setListRef),这里再留一份引用给「改关键词后置顶」
const listBox = shallowRef<HTMLElement | null>(null);
function bindList(el: unknown): void {
  listBox.value = (el as HTMLElement) || null;
  props.setListRef(el);
}

// 改关键词后把列表拉回顶部:结果的第一条必须从最上面开始看,
// 否则顾客会以为搜出来就这几道。
watch(
  () => props.keyword,
  () => {
    void nextTick(() => {
      if (listBox.value) listBox.value.scrollTop = 0;
    });
  }
);

function specOn(d: Dish, s: DishSpec): boolean {
  return props.selectedSpec[d.dishId as number] === s.specId;
}

// ---------- 分类角标 ----------
// 购物车条目只带 dishId,先建 dishId→categoryId 的映射再按分类累计份数;
// 同一菜出现在多个分类时以首次出现为准,数量不会重复计入
const catQtyMap = computed(() => {
  const dishCat = new Map<number, number>();
  for (const c of props.menu) {
    const cid = c.categoryId;
    if (cid === undefined) continue;
    for (const d of c.dishes || []) {
      if (d.dishId !== undefined && !dishCat.has(d.dishId)) dishCat.set(d.dishId, cid);
    }
  }
  const m = new Map<number, number>();
  // 已下单份数也要计入:加菜时顾客看角标就能知道该分类点了多少,与菜品行的「已点 N」同口径
  for (const c of props.menu) {
    for (const d of c.dishes || []) {
      if (d.dishId === undefined) continue;
      const cid = dishCat.get(d.dishId);
      const q = props.orderedQty(d.dishId);
      if (cid !== undefined && q) m.set(cid, (m.get(cid) || 0) + q);
    }
  }
  for (const it of props.cart || []) {
    const cid = dishCat.get(it.dishId);
    if (cid !== undefined) m.set(cid, (m.get(cid) || 0) + it.quantity);
  }
  return m;
});

function catQty(categoryId: number | undefined): number {
  if (categoryId === undefined) return 0;
  return catQtyMap.value.get(categoryId) || 0;
}

function imgErr(id: number | undefined): boolean {
  return id !== undefined && props.imgErrs.has(id);
}

function singleSpecName(d: Dish): string {
  const specs = d.specs || [];
  return specs.length === 1 ? specs[0]?.specName || '' : '';
}

function isSpecSoldOut(s: DishSpec): boolean {
  return Number(s.stock) === 0;
}

/** 行内放得下的规格才铺成 chip,再多交给弹层(与小程序 INLINE_SPEC_MAX 同值)。 */
function inlineSpecs(d: Dish): boolean {
  const n = (d.specs || []).length;
  return n > 1 && n <= INLINE_SPEC_MAX;
}

/** 多规格先给最低价(配「起」),具体选哪个进弹层;否则是「当前选中规格」的价。 */
function priceText(d: Dish): string {
  const specs = d.specs || [];
  if (isManySpecs(d)) return money(Math.min(...specs.map(s => Number(s.price) || 0)));
  return money(props.currentSpec(d).price);
}

/** 还能不能再加一份:沽清、暂不可点(无规格/下架)时为否。
 *  已在购物车里的菜会保留步进器,但「+」要置灰,避免点了没反应的假死感。 */
function canAddMore(d: Dish): boolean {
  return isOrderable(d) && !isSoldOut(d);
}

/** 「+」:规格多到行内放不下时进弹层,否则按当前选中规格直接加一份(与小程序 onAdd 同规则)。
 *  直接加菜时把点击坐标抛给页面层,驱动「小球抛物线飞入购物车」动画(美团外卖同款)。 */
function onAdd(d: Dish, e?: MouseEvent): void {
  if (!canAddMore(d)) return; // 置灰的「+」不响应,与样式保持一致
  if (isManySpecs(d)) {
    props.openSheet(d);
    return;
  }
  props.changeQty(d, 1);
  if (e) emit('fly', e.clientX, e.clientY);
}
</script>

<style scoped>
/* ---------- 菜单主体:左分类栏 + 右菜品列表 ---------- */

/* 外层白色圆角卡片(含与 hero 的重叠)由 OrderPage 的 .menu-sheet 提供,这里只做内部两栏布局。 */
.op-body {
  flex: 1;
  display: flex;
  min-height: 0;
  background: #fff;
}

/* 左侧分类栏:竖排,选中项白底 + 品牌色竖条,与右侧列表滚动联动 */
.op-side {
  width: 86px;
  flex-shrink: 0;
  background: #f7f7f7;
  overflow-y: auto;

  /* 顶部不留白:第一个分类贴住栏顶(与小程序一致),否则选中项上方会悬出一条灰缝;
     底部留白:结算栏与 tabbar 会盖住这一段 */
  padding: 0 0 120px;
  scrollbar-width: none;
}

.op-side::-webkit-scrollbar {
  display: none;
}

.op-cat {
  position: relative;
  padding: 13px 8px;
  font-size: 13px;
  line-height: 1.35;
  color: var(--ink-2);
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

/* 分类已点份数角标:固定在分类项右上角,单/双位数都居中 */
.cat-badge {
  position: absolute;
  top: 3px;
  right: 5px;
  min-width: 15px;
  height: 15px;
  padding: 0 4px;
  box-sizing: border-box;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 999px;
  background: linear-gradient(135deg, var(--brand), var(--brand-deep));
  color: #fff;
  font-size: 10px;
  font-weight: 700;
  line-height: 1;
}

.op-list {
  flex: 1;
  min-width: 0;
  overflow-y: auto;

  /* 顶部不留白:首个分组标题自带 10px 上内边距,与左栏齐平起步 */
  padding: 0 12px 96px;
}

/* 存在底部 tabbar 时,额外留出一条导航的高度,否则最后一道菜会被挡住 */
.op-list.lifted {
  padding-bottom: 142px;
}

/* ---------- 搜索 ---------- */

/* 搜索框 + 两栏主体的容器:占满白卡剩余高度,滚动交给列表自己 */
.op-wrap {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

/* 搜索框已抽到 MenuSearch.vue:页面层放在白卡最顶部,与小程序位置一致 */

/* 搜索无结果 / 菜单整体为空 */
.op-empty {
  padding: 40px 20px;
  color: var(--ink-4);
  font-size: 13px;
  line-height: 1.6;
  text-align: center;
}

/* 右栏分类分组标题:与小程序 .sec 一致——品牌色竖条 + 深灰加粗,取代原来的浅灰小字 */
.op-group-t {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 10px 2px 6px;
  color: var(--ink-2);
  font-size: 13px;
  font-weight: 700;
}

.op-group-t::before {
  flex-shrink: 0;
  width: 4px;
  height: 13px;
  border-radius: 2px;
  background: var(--grad-brand-soft);
  content: '';
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
  cursor: pointer;
}

.op-dish:last-child {
  border-bottom: none;
}

.op-img {
  position: relative;
  width: 84px;
  height: 84px;
  border-radius: var(--r-md);
  flex-shrink: 0;
  overflow: hidden;
  background: #f5f5f5;
}

/* 招牌角标:贴图片左上角,品牌色渐变底 + 白字,售罄蒙版(soldout-mask)层级更高会盖住它 */
.op-signature {
  position: absolute;
  top: 0;
  left: 0;
  z-index: 1;
  padding: 2px 7px;
  border-radius: 0 0 var(--r-md) 0;
  background: linear-gradient(135deg, var(--brand), var(--brand-deep));
  color: #fff;
  font-size: 10px;
  font-weight: 700;
  line-height: 1.3;
  letter-spacing: 1px;
}

/* 售罄蒙版:半透明盖住图片,居中白字「售罄」。没图/坏图的占位图同样会被盖住 */
.op-soldout-mask {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgb(26 26 26 / 55%);
  color: #fff;
  font-size: 13px;
  font-weight: 700;
  letter-spacing: 1px;
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

/* 售罄：跟在菜名后的小标签（上架中但今天没了） */
.op-soldout-tag {
  display: inline-block;
  margin-left: 6px;
  padding: 1px 6px;
  border-radius: var(--r-pill);
  background: #ececec;
  color: var(--ink-3);
  font-size: 10px;
  vertical-align: 1px;
}

/* 售罄占位：与「加号/步进器」同高度，行内节奏不被破坏 */
.op-soldout {
  padding: 5px 10px;
  border-radius: var(--r-pill);
  background: #ececec;
  color: var(--ink-3);
  font-size: 11px;
  white-space: nowrap;
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

/* 售罄规格：灰显，点不动（useCart.selectSpec 再拦一道） */
.op-spec.off {
  border-color: #eee;
  background: #f6f6f6;
  color: var(--ink-4);
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

/* 沽清后「+」置灰:顾客能继续减,但不能再加 */
.op-stepper .p.off {
  background: #e5e6eb;
  color: #b8bcc4;
  cursor: not-allowed;
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
