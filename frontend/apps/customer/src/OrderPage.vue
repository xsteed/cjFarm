<template>
  <div class="order-page">
    <!-- 加载中:骨架屏按真实布局铺一遍(hero + 搜索 + 左栏 + 菜品行),
         比转圈更接近最终画面,加载完成时也不会整页「跳」一下(与小程序一致) -->
    <div
      v-if="view === 'loading'"
      class="sk-wrap"
    >
      <div class="sk-hero">
        <div class="sk-bar sk-shop"></div>
        <div class="sk-bar sk-table"></div>
      </div>
      <div class="sk-sheet">
        <div class="sk-body">
          <div class="sk-cats">
            <div
              v-for="n in 6"
              :key="n"
              class="sk-bar sk-cat"
            ></div>
          </div>
          <div class="sk-dishes">
            <div
              v-for="n in 4"
              :key="n"
              class="sk-dish"
            >
              <div class="sk-bar sk-img"></div>
              <div class="sk-main">
                <div class="sk-bar sk-line"></div>
                <div class="sk-bar sk-line sk-line-short"></div>
                <div class="sk-bar sk-price"></div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- 加载失败 -->
    <div
      v-else-if="view === 'error'"
      class="state-box"
    >
      <div class="err-icon">
        <svg
          class="err-svg"
          viewBox="0 0 24 24"
          fill="none"
          aria-hidden="true"
        >
          <path
            d="M12 6.5v7.2"
            stroke="currentColor"
            stroke-width="2.6"
            stroke-linecap="round"
          />
          <circle
            cx="12"
            cy="17.8"
            r="1.6"
            fill="currentColor"
          />
        </svg>
      </div>
      <div class="state-text">
        {{ loadError }}
      </div>
      <button
        class="retry-btn"
        @click="load"
      >
        重新加载
      </button>
    </div>

    <!-- 有 hero 的内容视图 -->
    <template v-else>
      <HeroHeader
        :shop-name="shopName"
        :table="table"
        :show-search="view === 'menu'"
        :show-back="orderViewActive"
        @search="searchOpen = true"
        @back="backFromOrder"
      />

      <!-- 白色圆角卡片:承载出餐提示条 + 菜单,整体上移盖住 hero 底部。
           搜索进行中整页切到独立搜索页,菜单卡让位 -->
      <div
        v-if="view === 'menu' && !searchOpen"
        class="menu-sheet"
      >
        <!-- 「不是本人的单」浏览模式:可以随便挑,下单在 submit 里统一拦截 -->
        <div
          v-if="foreign"
          class="serving-banner foreign-banner"
        >
          本桌上一笔订单还未结账，可以先挑好菜，服务员清台后即可下单。
          <button
            class="foreign-back"
            @click="view = 'resume'"
          >
            返回
          </button>
        </div>

        <!-- 本桌「已付款但出餐未完成」的订单:顾客可查看进度。
             文字与订单标签保持同一行,标签多了向右滑动,不撑高菜单区 -->
        <div
          v-if="servingOrders.length"
          class="serving-banner"
        >
          <span class="serving-label">本桌还有 {{ servingOrders.length }} 笔订单正在出餐</span>
          <div class="serving-scroll">
            <div class="serving-chips-inner">
              <button
                v-for="o in servingOrders"
                :key="o.orderId"
                class="serving-chip"
                @click="openServing(o)"
              >
                <span>#{{ o.shortNo || o.orderNo }} · {{ ORDER_STATUS_TEXT[Number(o.orderStatus)] || '处理中' }}</span>
                <span class="serving-chip-go">›</span>
              </button>
            </div>
          </div>
        </div>

        <MenuView
          :menu="menu"
          :cart="cart"
          :selected-spec="selectedSpec"
          :img-errs="imgErrs"
          :show-tabbar="showTabbar"
          :active-cat="activeCat"
          :ordered-qty="orderedQty"
          :current-spec="currentSpec"
          :dish-qty="dishQty"
          :select-spec="selectSpec"
          :change-qty="changeQty"
          :on-img-err="onImgErr"
          :set-list-ref="setListRef"
          :set-group-ref="setGroupRef"
          :jump-to="jumpTo"
          :handle-scroll="handleScroll"
          :keyword="keyword"
          :sig-cards="sigCards"
          :open-sheet="openSheet"
          @fly="onFly"
        />
      </div>

      <!-- 桌台被店员停用/预约:不允许新点餐,给出明确解释与出口(本桌已有订单的顾客不受影响) -->
      <div
        v-else-if="view === 'locked'"
        class="state-box"
      >
        <div class="err-icon">
        <svg
          class="err-svg"
          viewBox="0 0 24 24"
          fill="none"
          aria-hidden="true"
        >
          <path
            d="M12 6.5v7.2"
            stroke="currentColor"
            stroke-width="2.6"
            stroke-linecap="round"
          />
          <circle
            cx="12"
            cy="17.8"
            r="1.6"
            fill="currentColor"
          />
        </svg>
      </div>
        <div class="state-text">
          {{ lockText }}
        </div>
        <button
          class="retry-btn"
          @click="load"
        >
          刷新状态
        </button>
      </div>

      <!-- 本桌已有一笔未结账订单:先问清楚是不是眼前这位客人的单。
           店家忘了点「收款」时,不问就直接进订单页,等于把上一桌的账单摊给下一桌。
           「不是本人的单」分支复用居中的 state-box 布局,与错误/锁定页保持一致 -->
      <div
        v-else-if="view === 'resume'"
        :class="foreign ? 'state-box' : 'resume-box'"
      >
        <template v-if="!foreign">
          <div class="resume-title">本桌已有一笔未结账的订单</div>
          <div class="resume-card">
            <div class="resume-row">
              <span>下单时间</span>
              <b>{{ resumeTime }}</b>
            </div>
            <div class="resume-row">
              <span>菜品数量</span>
              <b>{{ resumeCount }} 道</b>
            </div>
            <div class="resume-row">
              <span>合计金额</span>
              <b>￥{{ resumeAmount }}</b>
            </div>
          </div>
          <p class="resume-hint">
            如果这是您这桌刚点的单，点「是我的」查看出餐进度或继续加菜；不是的话请点下面的按钮，我们会请服务员来处理。
          </p>
          <button
            class="resume-btn primary"
            @click="confirmMine"
          >
            是我的，继续
          </button>
          <button
            class="resume-btn"
            @click="notMine"
          >
            不是我的订单
          </button>
        </template>

        <!-- 不是本人的单:后端不允许该桌再开一单(一桌只留一笔未结账订单)。
             菜单可以先进去逛,购物车随便挑,清台后回这里刷新即可下单 -->
        <template v-else>
          <div class="state-emoji">
            <svg
              class="wait-svg"
              viewBox="0 0 64 64"
              fill="none"
              aria-hidden="true"
            >
              <!-- 餐盘:投影 + 盘沿 + 凹槽盘心 + 高光,层次齐全才像真盘子 -->
              <ellipse
                cx="32"
                cy="56"
                rx="20"
                ry="4"
                fill="#ffe6d3"
              />
              <circle
                cx="32"
                cy="34"
                r="20"
                fill="#fff"
                stroke="var(--brand, #ff6b35)"
                stroke-width="3.4"
              />
              <circle
                cx="32"
                cy="34"
                r="13.5"
                fill="#fff6ec"
                stroke="#ffd9c2"
                stroke-width="2"
              />
              <path
                d="M25 27a13.5 13.5 0 0 1 8-6"
                fill="none"
                stroke="#fff"
                stroke-width="3"
                stroke-linecap="round"
              />
              <!-- 擦盘子的小海绵:圆角海绵块 + 左上高光 + 散落小气孔,左右擦动 -->
              <g class="sponge">
                <rect
                  x="15.5"
                  y="24.5"
                  width="33"
                  height="17"
                  rx="8.5"
                  fill="#fff0bd"
                  stroke="#8a5a3b"
                  stroke-width="2.8"
                />
                <!-- 左上高光,体现海绵柔软蓬松的质感 -->
                <ellipse
                  cx="20.5"
                  cy="27.5"
                  rx="2.6"
                  ry="1.7"
                  fill="#fff"
                />
                <!-- 气孔:自然散布、大小不一,一看就是海绵 -->
                <circle
                  cx="26"
                  cy="29.5"
                  r="1.2"
                  fill="#8a5a3b"
                />
                <circle
                  cx="31"
                  cy="27"
                  r="1.3"
                  fill="#8a5a3b"
                />
                <circle
                  cx="36"
                  cy="29.5"
                  r="1.1"
                  fill="#8a5a3b"
                />
                <circle
                  cx="41"
                  cy="27"
                  r="1.3"
                  fill="#8a5a3b"
                />
                <circle
                  cx="27.5"
                  cy="34"
                  r="1.1"
                  fill="#8a5a3b"
                />
                <circle
                  cx="36.5"
                  cy="34.5"
                  r="1.2"
                  fill="#8a5a3b"
                />
                <circle
                  cx="43"
                  cy="32.5"
                  r="1.1"
                  fill="#8a5a3b"
                />
                <circle
                  cx="32"
                  cy="36.5"
                  r="1"
                  fill="#8a5a3b"
                />
              </g>
              <!-- 洗洁精泡泡:三颗错峰往上飘 -->
              <circle
                class="bubble b1"
                cx="11"
                cy="33"
                r="3.2"
                fill="#ffd9c2"
              />
              <circle
                class="bubble b2"
                cx="53"
                cy="24"
                r="2.6"
                fill="#ffd9c2"
              />
              <circle
                class="bubble b3"
                cx="54"
                cy="51"
                r="2.2"
                fill="#ffd9c2"
              />
              <!-- 星星点缀 -->
              <path
                class="spark"
                d="M54 8.5l1.6 3.9 3.9 1.6-3.9 1.6-1.6 3.9-1.6-3.9-3.9-1.6 3.9-1.6Z"
                fill="#ffb27d"
              />
            </svg>
          </div>
          <div class="state-title">本桌还未清台</div>
          <div class="state-text">
            <p>上一笔订单还未结账，服务员清台后即可点餐</p>
            <p class="sub">您可以先逛逛菜单，把想吃的挑进购物车 ～</p>
          </div>
          <div class="state-actions">
            <button
              class="retry-btn"
              @click="browseMenu"
            >
              先看看菜单
            </button>
            <button
              class="retry-btn plain"
              @click="load"
            >
              刷新状态
            </button>
          </div>
        </template>
      </div>

      <OrderTrackView
        v-else-if="currentOrder && (view === 'order' || view === 'settled' || view === 'done' || view === 'canceled')"
        :view="view"
        :current-order="currentOrder"
        :is-active="isActive"
        :can-append="canAppend"
        :urging="urging"
        :urge-cooldown="urgeCooldown"
        :show-tabbar="showTabbar"
        :pay-first="payFirst"
        :review-done="reviewStatus.reviewed"
        @append="startAppend"
        @urge="urge"
        @pay="view = 'pay'"
        @new-order="startNewOrder"
        @review="openReviewSheet"
      />

      <!-- 本桌「已付款出餐中」的订单列表:currentOrder 只含未结账单,
           出餐中的单在这里列出,点一条即可看进度(与菜单页顶部提示条同一数据源)。
           每张卡自带三件事:取餐号 + 出餐状态、点了什么(摘要)、多少钱 ——
           顾客在列表页就能确认「这是我的单、钱付过了」,不必逐条点进去核对 -->
      <div
        v-else-if="view === 'order' && servingOrders.length"
        class="serving-list"
      >
        <div class="sl-head">
          <div class="sl-title">本桌正在出餐</div>
          <span class="sl-badge">{{ servingOrders.length }} 笔</span>
        </div>
        <p class="sl-hint">已付款的订单无需再次支付，点开可查看出餐进度</p>

        <button
          v-for="o in servingOrders"
          :key="o.orderId"
          class="sl-item"
          @click="openServing(o)"
        >
          <div class="sl-main">
            <div class="sl-row">
              <span class="sl-no">#{{ o.shortNo || o.orderNo }}</span>
              <span
                class="sl-tag"
                :class="'sl-tag--' + (SERVING_STATUS_THEME[Number(o.orderStatus)] || 'primary')"
              >
                {{ SERVING_STATUS_TEXT[Number(o.orderStatus)] || '处理中' }}
              </span>
            </div>
            <div class="sl-dishes">{{ servingDishes(o) }}</div>
            <div
              v-if="servingMeta(o)"
              class="sl-meta"
            >
              {{ servingMeta(o) }}
            </div>
          </div>
          <div class="sl-side">
            <span class="sl-amt">¥{{ Number(o.totalAmount || 0).toFixed(2) }}</span>
            <span class="sl-go">
              查看进度
              <svg
                viewBox="0 0 24 24"
                width="12"
                height="12"
                fill="none"
                stroke="currentColor"
                stroke-width="2.4"
                stroke-linecap="round"
                stroke-linejoin="round"
                aria-hidden="true"
              >
                <path d="M9 5l7 7-7 7" />
              </svg>
            </span>
          </div>
        </button>

        <!-- 「去加菜」压到列表底部:本桌已有出餐中的单,回菜单继续点就是加菜
             (购物车保留、与底部 tab「点餐」同效果)。列表短时贴底、长时跟在流末 -->
        <button
          class="sl-add-more"
          @click="switchView('menu')"
        >
          <span
            class="sl-add-ic"
            aria-hidden="true"
          >＋</span>
          去加菜
        </button>
      </div>

      <!-- 还没下单:订单视图给空态而不是一片空白(导航常驻后进得来) -->
      <div
        v-else-if="view === 'order'"
        class="order-empty"
      >
        <!-- 空态插画:菜单卡 + 筷子 + 冒热气的茶杯(与全站线性 SVG 图标同一画风,品牌色绘制) -->
        <div
          class="empty-illo"
          aria-hidden="true"
        >
          <svg
            viewBox="0 0 120 96"
            fill="none"
          >
            <defs>
              <!-- 筷子渐变:前端(左)浅橙 → 末端(右)深橙,整根过渡无接缝 -->
              <linearGradient
                id="empty-chopstick"
                gradientUnits="userSpaceOnUse"
                x1="20"
                y1="80"
                x2="70"
                y2="76"
              >
                <stop
                  offset="0"
                  stop-color="#ff9c66"
                />
                <stop
                  offset="1"
                  stop-color="var(--brand-deep, #f0481f)"
                />
              </linearGradient>
            </defs>
            <!-- 桌面投影:让元素落地,不悬空 -->
            <ellipse
              class="illo-shadow"
              cx="60"
              cy="89"
              rx="46"
              ry="4.5"
              fill="#ffb27d"
              opacity="0.4"
            />
            <!-- 闪烁星点 -->
            <path
              class="illo-spark s1"
              d="M14 14l2.6 5.2 5.2 2.6-5.2 2.6L14 29.6l-2.6-5.2-5.2-2.6 5.2-2.6z"
              fill="#ffb27d"
            />
            <path
              class="illo-spark s2"
              d="M80 10l1.8 3.6 3.6 1.8-3.6 1.8-1.8 3.6-1.8-3.6-3.6-1.8 3.6-1.8z"
              fill="#ffb27d"
            />
            <!-- 单页菜单卡:品牌色标题栏 + 三行菜品占位 -->
            <rect
              x="24"
              y="22"
              width="56"
              height="48"
              rx="5"
              fill="#fff"
              stroke="var(--brand, #ff6b35)"
              stroke-width="4"
            />
            <path
              d="M29 22h46a5 5 0 0 1 5 5v7H24v-7a5 5 0 0 1 5-5z"
              fill="var(--brand, #ff6b35)"
            />
            <circle
              cx="32.5"
              cy="28"
              r="2.2"
              fill="#fff"
            />
            <path
              d="M39 28h28"
              stroke="#fff"
              stroke-width="3.5"
              stroke-linecap="round"
            />
            <path
              d="M33 43h22M33 53h16M33 63h20"
              stroke="var(--ink-4, #c8c9cc)"
              stroke-width="2.5"
              stroke-linecap="round"
            />
            <circle
              cx="35.5"
              cy="43"
              r="3"
              fill="#ffb27d"
            />
            <circle
              cx="35.5"
              cy="53"
              r="3"
              fill="#ffb27d"
            />
            <circle
              cx="35.5"
              cy="63"
              r="3"
              fill="#ffb27d"
            />
            <!-- 茶杯与热气:三缕波浪热气从杯沿袅袅升起 -->
            <path
              class="illo-steam st1"
              d="M95 44c2.5-3-2.5-5 0-8"
              stroke="#ffb27d"
              stroke-width="3"
              stroke-linecap="round"
            />
            <path
              class="illo-steam st2"
              d="M100 44c2.5-3-2.5-5 0-8c1.5-2-1.5-3 0-4"
              stroke="#ffb27d"
              stroke-width="3"
              stroke-linecap="round"
            />
            <path
              class="illo-steam st3"
              d="M105 44c2.5-3-2.5-5 0-8"
              stroke="#ffb27d"
              stroke-width="3"
              stroke-linecap="round"
            />
            <!-- 托碟:宽浅椭圆,先画、让杯子压在其上形成「坐落」关系 -->
            <ellipse
              cx="100.5"
              cy="64"
              rx="16"
              ry="4"
              fill="#fff"
              stroke="var(--brand, #ff6b35)"
              stroke-width="3"
            />
            <path
              d="M92 47v8c0 5 3.5 8 8 8h1c4.5 0 8-3 8-8v-8z"
              fill="#fff"
              stroke="var(--brand, #ff6b35)"
              stroke-width="3.5"
              stroke-linejoin="round"
            />
            <!-- 杯口茶水:一点浅茶色,交代杯内是有水的 -->
            <ellipse
              cx="100"
              cy="47.5"
              rx="6"
              ry="1.8"
              fill="#f7c9a8"
            />
            <!-- 筷子:整根单段直线 + 渐变着色,前端浅、末端深,无接缝 -->
            <path
              d="M20 80L70 76"
              stroke="url(#empty-chopstick)"
              stroke-width="3.4"
              stroke-linecap="round"
            />
            <path
              d="M22 85L72 81"
              stroke="url(#empty-chopstick)"
              stroke-width="3.4"
              stroke-linecap="round"
            />
          </svg>
        </div>
        <div class="empty-title">上桌啦，点起来吧</div>
        <div class="empty-sub">还没有进行中的订单，先去挑几道想吃的菜吧</div>
        <button
          class="empty-btn"
          @click="switchView('menu')"
        >
          去点餐
        </button>
      </div>

      <PayView
        v-else-if="currentOrder && view === 'pay'"
        :current-order="currentOrder"
        :pay-tab="payTab"
        :online-qr="onlineQr"
        :paying="paying"
        :online-enabled="onlineEnabled"
        :current-pay-qr="currentPayQr"
        :pay-first="payFirst"
        @switch-tab="switchPayTab"
        @online-pay="startOnlinePay"
        @back="backFromPay"
      />

      <!-- 独立搜索页:hero 搜索按钮进入,整页覆盖点餐视图;
           复用 MenuView 渲染结果(未输入时就是完整菜单,输入即过滤,左栏自动收起) -->
      <div
        v-if="view === 'menu' && searchOpen"
        class="search-layer"
      >
        <MenuSearch
          v-model:keyword="keyword"
          v-model:open="searchOpen"
        />
        <MenuView
          :menu="menu"
          :cart="cart"
          :selected-spec="selectedSpec"
          :img-errs="imgErrs"
          :show-tabbar="showTabbar"
          :active-cat="activeCat"
          :ordered-qty="orderedQty"
          :current-spec="currentSpec"
          :dish-qty="dishQty"
          :select-spec="selectSpec"
          :change-qty="changeQty"
          :on-img-err="onImgErr"
          :set-list-ref="setListRef"
          :set-group-ref="setGroupRef"
          :jump-to="jumpTo"
          :handle-scroll="handleScroll"
          :keyword="keyword"
          :sig-cards="sigCards"
          :open-sheet="openSheet"
          @fly="onFly"
        />
      </div>
    </template>

    <!-- 底部悬浮结算栏:仅本次会话从未点过菜时隐藏;一旦出现过(cartEverShown)就常驻 -->
    <CartBar
      v-if="view === 'menu' && (cartEverShown || totalCount > 0 || canAppend)"
      :show-tabbar="showTabbar"
      :total-count="totalCount"
      :total-price="totalPrice"
      :can-append="canAppend"
      @open="cartVisible = true"
    />

    <!-- 底部导航:点餐 / 订单。已有订单时常驻,加菜时顾客可随时切回核对已下单菜品 -->
    <BottomTabBar
      v-if="showTabbar"
      :view="view"
      @switch="switchView"
    />

    <!-- 购物车抽屉 -->
    <CartDrawer
      v-model:visible="cartVisible"
      v-model:person-count="personCount"
      v-model:order-remark="orderRemark"
      :cart="cart"
      :total-count="totalCount"
      :dish-total="dishTotal"
      :seat-fee="seatFee"
      :discount="discount"
      :total-price="totalPrice"
      :can-append="canAppend"
      :submitting="submitting"
      :pay-first="payFirst"
      :current-order="currentOrder"
      :open-remark="openRemark"
      :change-cart-qty="changeCartQty"
      @clear="onClearCart"
      @submit="submit"
    />

    <!-- 付款成功反馈:覆盖在页面之上,1.8s 后自动收起(点遮罩也可立即关掉) -->
    <PayDoneCard
      v-if="payDoneVisible && currentOrder"
      :amount="Number(currentOrder.totalAmount || 0)"
      :sub="payDoneSub"
      @close="payDoneVisible = false"
    />

    <!-- 订单评价弹层(仅商家可见):入口在订单页 settled/done 态,一单一评 -->
    <ReviewSheet
      v-if="reviewSheetOpen && currentOrder"
      :submitting="reviewSubmitting"
      @close="reviewSheetOpen = false"
      @submit="submitReview"
    />

    <!-- 菜品单项备注弹窗。层级高于购物车抽屉,详见 RemarkSheet 的 .remark-mask -->
    <RemarkSheet
      v-if="remarkVisible"
      :dish-name="remarkTarget.item?.dishName || ''"
      :selected="remarkTarget.selected"
      :custom="remarkTarget.custom"
      :remarks="remarks"
      :has-remark="!!remarkTarget.item?.remark"
      @toggle="toggleRemark"
      @update:custom="remarkTarget.custom = $event"
      @clear="clearRemark"
      @close="closeRemark"
      @save="onRemarkSave"
    />

    <!-- 规格 / 单项备注弹层:多规格菜点「+」或点菜名时打开(对齐小程序 spec-sheet) -->
    <SpecSheet
      v-if="sheetVisible"
      :dish-name="sheetDish?.dishName || ''"
      :specs="sheetDish?.specs || []"
      :remarks="remarks"
      :selected-spec-id="sheetSpecId"
      :selected="sheetRemark.selected"
      :custom="sheetRemark.custom"
      :in-cart-qty="sheetInCartQty"
      :qty="sheetQty"
      @pick-spec="pickSheetSpec"
      @toggle-remark="toggleSheetRemark"
      @update:custom="sheetRemark.custom = $event"
      @update:qty="sheetQty = $event"
      @close="closeSheet"
      @confirm="confirmSheet"
    />

    <!-- 入座选人数：半透明蒙层 + 贴底面板（茶位费门店）。菜单在蒙层下可见但点不了，
         顾客扫码后不会面对陌生页面；面板只做「选人数」一件事，茶位费照旧在购物车明细里 -->
    <div
      v-if="personGate"
      class="person-mask"
    >
      <div class="person-sheet">
        <div class="ps-title">本桌用餐几位？</div>
        <div class="ps-hint">选好人数，即刻点餐～</div>

        <div class="pg-grid">
          <button
            v-for="n in PERSON_CHIPS"
            :key="n"
            class="pg-cell"
            :class="{ on: personDraft === n }"
            @click="personDraft = n"
          >
            {{ n }}
          </button>
        </div>

        <div class="ps-row">
          <div class="pg-stepper">
            <button
              class="pg-step"
              :disabled="personDraft <= PERSON_MIN"
              @click="personDraft = Math.max(PERSON_MIN, personDraft - 1)"
            >
              −
            </button>
            <input
              v-model.number="personDraft"
              class="pg-num"
              type="number"
              inputmode="numeric"
              :min="PERSON_MIN"
              :max="PERSON_MAX"
              placeholder="—"
            />
            <button
              class="pg-step"
              :disabled="personDraft >= PERSON_MAX"
              @click="personDraft = Math.min(PERSON_MAX, personDraft + 1)"
            >
              +
            </button>
          </div>
          <button
            class="pg-confirm"
            :disabled="personDraft < PERSON_MIN || personDraft > PERSON_MAX"
            @click="confirmPerson"
          >
            {{ personDraft >= PERSON_MIN ? `开始点餐（${personDraft}人）` : '请先选择人数' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue';
import { useRoute } from 'vue-router';
import { DialogPlugin, MessagePlugin } from 'tdesign-vue-next';
import {
  appendOrder,
  createOrder,
  createPay,
  createReview,
  getMenu,
  getOrderByNo,
  getPublicConfig,
  getRemarks,
  getReviewStatus,
  getTable,
  reportVisit,
  setTablePerson
} from '@dining/api';
import type {
  CustomerReviewStatus,
  MenuCategory,
  OrderDetail,
  OrderSummary,
  PublicConfig,
  RemarkOption,
  TableInfo
} from '@dining/types';
import { isActiveStatus, safeSession, ui, visitorId } from '@dining/shared';
import { useCart, useOrderPolling, useUrge, signatureDishes, SIG_CAT_ID } from '@dining/customer-core';
import { PERSON_MAX, PERSON_MIN, isValidPerson } from '@dining/customer-core';
import type { ViewState } from '@dining/customer-core';
import { setCustomerTitle } from './title';
import { useCartPersist } from './composables/useCartPersist';
import { useScrollSpy } from './composables/useScrollSpy';
import HeroHeader from './order/HeroHeader.vue';
import MenuView from './order/MenuView.vue';
import OrderTrackView from './order/OrderTrackView.vue';
import PayView from './order/PayView.vue';
import PayDoneCard from './order/PayDoneCard.vue';
import CartBar from './order/CartBar.vue';
import BottomTabBar from './order/BottomTabBar.vue';
import CartDrawer from './order/CartDrawer.vue';
import SpecSheet from './order/SpecSheet.vue';
import MenuSearch from './order/MenuSearch.vue';
import RemarkSheet from './order/RemarkSheet.vue';
import ReviewSheet from './order/ReviewSheet.vue';

const route = useRoute();
// 路由参数既可能是「桌台稳定码」(如 K7M3PQ9X),也可能是老的数字桌台 ID(如 1)。
// 先用它换取桌台详情,拿到数字 tableId 后再用于下单等接口。
const tableRef = String(route.params.tableId || '');
const tableId = ref(0);

const view = ref<ViewState>('loading'); // loading | error | menu | order | done | canceled | pay
const loadError = ref('');
const table = ref<TableInfo>({});
const menu = ref<MenuCategory[]>([]);
const remarks = ref<RemarkOption[]>([]);
const config = ref<PublicConfig>({});
const shopName = ref('扫码点餐');
const cartVisible = ref(false);
const personCount = ref(1);
// 「入座选人数」蒙层（茶位费门店）：盖在菜单上方，菜单可见但点不了
const personGate = ref(false);
// 蒙层面板上的草稿值：0 = 还没选（不预选，人数必须被显式选出来）
const personDraft = ref(0);
/** 快捷人数格子（1~10，五列两行）；11~20 走步进器与直接输入。 */
const PERSON_CHIPS = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10];
const orderRemark = ref('');
const submitting = ref(false);
const currentOrder = ref<OrderDetail | null>(null);
const servingOrders = ref<OrderSummary[]>([]);
// 幂等键:同一次提交(含超时重试)复用同一 ID,后端据此去重。
// pendingFingerprint 记录该幂等键对应的「提交内容指纹」:后端命中幂等键时会直接返回
// 首次结果、完全不看本次 items,所以键必须与内容绑定 —— 否则弱网超时后顾客改了菜再提交,
// 会拿到第一次的 orderNo(新加的菜没进单),前端还会清空购物车。
let pendingRequestId = '';
let pendingFingerprint = '';
const payTab = ref<'wx' | 'ali'>('wx');
const onlineQr = ref(''); // 在线支付二维码(dataURL)
const paying = ref(false);
// 付款成功反馈:轮询发现「刚变成已支付」时弹一次,1.8s 后自动收起(也可点遮罩提前关)
const payDoneVisible = ref(false);
let payDoneTimer: ReturnType<typeof setTimeout> | null = null;

function showPayDone(): void {
  payDoneVisible.value = true;
  if (payDoneTimer) clearTimeout(payDoneTimer);
  payDoneTimer = setTimeout(() => {
    payDoneVisible.value = false;
    payDoneTimer = null;
  }, 1800);
}

// 成功卡副标题:带上支付渠道(渠道支付时 payType 为「微信 / 支付宝」,前台收款多为「现金」)
const payDoneSub = computed(() => {
  const t = currentOrder.value?.payType;
  return t ? `${t}支付 · 后厨已在做啦` : '后厨已在做啦';
});
const imgErrs = ref(new Set<number>());
// 菜单搜索关键词:放在页面层,搜索框与菜单列表共用(搜索框在白卡顶部,菜单里面只做过滤)
const keyword = ref('');
// 人数变化统一落 sessionStorage：购物车抽屉改人数、加菜同步、再点一单都覆盖，
// 刷新后即使不弹面板也能恢复到上次的值（与购物车持久化同一会话语义）
watch(personCount, v => {
  if (tableId.value <= 0) return;
  const n = Math.floor(Number(v) || 0);
  if (n >= PERSON_MIN && n <= PERSON_MAX) safeSession.set(personKey(), String(n));
});

// 搜索框展开态:hero 头部的搜索按钮控制展开,空词点 × 收起
const searchOpen = ref(false);
// 收起搜索页时清掉关键词,回到菜单页不残留搜索态
watch(searchOpen, v => {
  if (!v) keyword.value = '';
});

const ORDER_STATUS_TEXT: Record<number, string> = {
  1: '已下单，后厨正在接单',
  2: '正在制作中，请稍候',
  3: '餐品陆续上桌，请慢用',
  4: '订单已完成',
  5: '订单已取消'
};

// ---- 「本桌正在出餐」列表 ----
// 出餐中的单只可能停在 1/2/3;标签用短词 + 语义色,和订单详情页的状态卡同一套口径。
// 3 不写「已上齐」:商家未必逐单维护状态,顾客端只说「用餐中」这种不精确的阶段词。
const SERVING_STATUS_TEXT: Record<number, string> = { 1: '已下单', 2: '制作中', 3: '用餐中' };
const SERVING_STATUS_THEME: Record<number, string> = { 1: 'primary', 2: 'warning', 3: 'success' };

/** 菜品摘要:前两道菜名 + 「等 N 道」,让顾客一眼认出这是自己点的那单。 */
function servingDishes(o: OrderSummary): string {
  const items = o.items || [];
  const names = items.map(i => String(i.dishName || '').trim()).filter(Boolean);
  if (!names.length) return '菜品明细稍后显示';
  const head = names.slice(0, 2).join('、');
  return names.length > 2 ? `${head} 等 ${names.length} 道` : head;
}

/** 次行:下单时刻 · 人数 · 件数(缺什么就跳过什么,不留空分隔符)。 */
function servingMeta(o: OrderSummary): string {
  const t = String(o.createTime || '').replace('T', ' ');
  const time = t.length >= 16 ? t.slice(11, 16) : '';
  const qty = (o.items || []).reduce((s, it) => s + (Number(it.quantity) || 0), 0);
  return [
    time ? `${time} 下单` : '',
    o.personCount ? `${o.personCount} 人` : '',
    qty > 0 ? `共 ${qty} 件` : ''
  ]
    .filter(Boolean)
    .join(' · ');
}

const {
  cart,
  selectedSpec,
  remarkVisible,
  remarkTarget,
  totalCount,
  dishTotal,
  round2,
  currentSpec,
  dishQty,
  selectSpec,
  changeQty,
  changeCartQty,
  clearCart,
  openRemark,
  toggleRemark,
  closeRemark,
  clearRemark,
  saveRemark,
  sheetVisible,
  sheetDish,
  sheetSpecId,
  sheetRemark,
  sheetInCartQty,
  sheetQty,
  openSheet,
  closeSheet,
  pickSheetSpec,
  toggleSheetRemark,
  confirmSheet
} = useCart(remarks);

const { urging, urgeCooldown, urge, clearUrgeTimer } = useUrge(currentOrder, tableId);
const { startPolling, stopPolling } = useOrderPolling({
  view,
  currentOrder,
  onlineQr,
  clearUrgeTimer,
  onPaid: showPayDone
});
// 招牌菜卡片：与小程序端同一份提取算法（customer-core），左栏第一项 + 右栏置顶分组都靠它。
// 没有招牌菜时整项不出现（不留一个空组的入口）。
const sigCards = computed(() => signatureDishes(menu.value));
// 滚动联动用的分类列表：非搜索态且存在招牌菜时，把「本店招牌」合成分类插到最前
// （categoryId = SIG_CAT_ID = -1，不会与真实正整数分类撞车）。
const categories = computed<MenuCategory[]>(() => {
  const base = menu.value;
  return sigCards.value.length ? [{ categoryId: SIG_CAT_ID, categoryName: '本店招牌' }, ...base] : base;
});
const { activeCat, setListRef, setGroupRef, jumpTo, handleScroll } = useScrollSpy(categories);
// 刷新/重进不丢购物车:菜单到位后再按当前菜单复核恢复。
// key 带本桌订单号 —— 换单/清台后订单号变了,上一批客人没提交的车不会被带出来。
const { restore: restoreCart } = useCartPersist(
  cart,
  tableId,
  computed(() => String(currentOrder.value?.orderNo || ''))
);

const seatFeeEnabled = computed(() => config.value.seat_fee_enabled === '1' && Number(config.value.seat_fee) > 0);
const seatFeePer = computed(() => Number(config.value.seat_fee) || 0);

const seatFee = computed(() => (seatFeeEnabled.value ? round2(personCount.value * seatFeePer.value) : 0));
const discount = computed(() => {
  const enabled = config.value.promotion_enabled === '1';
  const t = Number(config.value.promotion_threshold);
  const d = Number(config.value.promotion_discount);
  if (!enabled || !(t > 0) || !(d > 0) || dishTotal.value < t) return 0;
  return round2(Math.min(Math.floor(dishTotal.value / t) * d, dishTotal.value));
});
// 未选菜时不展示茶位费(合计为 0),选菜后才计入茶位费与优惠。
// 加菜态例外:合计只含本次追加的菜品 —— 茶位费与满减由后端按整单重算一次
// (append 接口 RecalcAmount(effectivePerson, 全部明细)),把整桌茶位费摊进
// 「仅加菜」小计会让顾客以为被重复收费(先付模式下马上要付款,尤其伤信任)。
const totalPrice = computed(() => {
  if (dishTotal.value <= 0) return 0;
  const appending = isActive.value && Number(currentOrder.value?.payStatus) === 0 && !foreign.value;
  return appending ? dishTotal.value : round2(dishTotal.value + seatFee.value - discount.value);
});

// 结算栏显隐:仅「本次会话从未点过菜」时隐藏;购物车一旦出现过(含刷新后从持久化恢复)
// 就置位并永不复位——之后清空、支付、取消都不再隐藏,给顾客一个稳定的加菜入口。
const cartEverShown = ref(false);
watch(totalCount, n => {
  if (n > 0) cartEverShown.value = true;
});

const currentPayQr = computed(() => (payTab.value === 'wx' ? config.value.pay_qr_wx : config.value.pay_qr_ali));
// 该渠道是否已启用在线支付(未申请 key 前为 false,前端自动降级为码牌收款)
const onlineEnabled = computed(() =>
  payTab.value === 'wx' ? config.value.wxpay_enabled === '1' : config.value.alipay_enabled === '1'
);
// 先付模式:下单后需先支付,后厨才开始制作(由门店在管理端「营业设置」配置)。
// 顾客端据此把下单后的落点从「订单页」改成「支付页」,并在各处提示先付款。
const payFirst = computed(() => config.value.pay_first_enabled === '1');
// 是否已存在当前订单（订单视图能不能出内容、能不能催菜都看它）。
const hasOrder = computed(() => !!currentOrder.value);
// 底部 tabbar 常驻(点餐/订单两档):与小程序一致 —— 还没下单也能进订单页看空态,
// 不必先点菜才知道「有没有单」。支付页需要专注,不显示。
const showTabbar = computed(() => view.value === 'menu' || view.value === 'order');
// 已下单份数(按 dishId 聚合,同菜多规格累加)。菜单「已点」标记用这份数据。
const orderedMap = computed<Record<number, number>>(() => {
  const m: Record<number, number> = {};
  for (const it of currentOrder.value?.items || []) {
    const id = it.dishId as number;
    m[id] = (m[id] || 0) + Number(it.quantity || 0);
  }
  return m;
});
const orderedQty = (dishId: number | undefined) =>
  // 「不是本人的单」浏览模式下不显示上一桌的「已点」标记,避免泄露他人点了什么
  (foreign.value ? 0 : orderedMap.value[dishId as number]) || 0;

// ---- 入座选人数（茶位费门店） ----
// 人数是「这张桌」的属性：判定链 = 本机会话已选 > 桌级共享值 > 弹面板。

function personKey(): string {
  return `dining:person:${tableId.value}`;
}

/** 本会话为这张桌选过的人数；没选过返回 0。 */
function rememberedPerson(): number {
  const v = Math.floor(Number(safeSession.get(personKey())) || 0);
  return v >= PERSON_MIN && v <= PERSON_MAX ? v : 0;
}

/** 桌级共享值：同桌其他设备已选过时直接沿用（返回是否采纳）。 */
function adoptPendingPerson(tb: TableInfo): boolean {
  const n = Math.floor(Number(tb.pendingPersonCount) || 0);
  if (!isValidPerson(n)) return false;
  personCount.value = n;
  return true;
}

/**
 * 本桌还有「已付款但出餐未完成」的订单 = 这桌人正在吃，茶位费已随那些单收过：
 * 人数沿最近一笔，不再重复问。返回是否采纳。
 */
function adoptServingPerson(tb: TableInfo): boolean {
  const serving = tb.servingOrders || [];
  if (!serving.length) return false;
  // 后端按订单倒序返回，首笔即最近一笔
  const n = Math.floor(Number(serving[0]?.personCount) || 0);
  if (!isValidPerson(n)) return false;
  personCount.value = n;
  return true;
}

/** 茶位费是否开启（进入面板只看它）。 */
function seatFeeOn(cfg: PublicConfig): boolean {
  return cfg.seat_fee_enabled === '1' && Number(cfg.seat_fee) > 0;
}

function confirmPerson(): void {
  const n = Math.floor(Number(personDraft.value) || 0);
  if (!isValidPerson(n)) return;
  personCount.value = n;
  safeSession.set(personKey(), String(n));
  // view 本来就是 'menu'：面板一收，菜单已渲染好，顾客直接点菜
  personGate.value = false;
  // 全桌共享：上报给后端，同桌其他手机扫码进来直接沿用、不再重复选。
  // fire-and-forget：上报失败只影响「别人免选」这一体验，绝不打断自己的点餐。
  if (tableId.value > 0) {
    void setTablePerson({ tableId: tableId.value, personCount: n }).catch(() => {
      /* 静默：面板是体验优化，不是下单链路的一环 */
    });
  }
}

// 订单是否仍在进行中(1已下单/2制作中/3已上齐)。
// 加菜还需未支付;催菜只要求进行中——已付款但迟迟未上齐的订单同样可以催。
const isActive = computed(() => isActiveStatus(currentOrder.value?.orderStatus));

// ---- 下单 / 加菜 ----
// 存在进行中且未支付的订单时走加菜(追加到同一订单),否则首次下单。
const canAppend = computed(
  // foreign(不是本人的单)时绝不走加菜:currentOrder 是上一桌的账单,不能往里追加
  () => !foreign.value && isActive.value && Number(currentOrder.value?.payStatus) === 0
);

// 订单跟踪/支付视图的渲染已由模板上的 `currentOrder &&` 守卫收窄;
// CartDrawer 的 currentOrder prop 声明为可空并内部空值容错。

function onImgErr(id: number | undefined): void {
  const s = new Set(imgErrs.value);
  if (id !== undefined) s.add(id);
  imgErrs.value = s;
}

/** 「+」加菜的小球抛物线动画(美团外卖同款):
 *  从点击位置飞向底部购物车图标。外层水平位移用线性,内层竖直位移用
 *  缓入曲线,两者叠加即标准抛物线;落地后购物车图标弹一下做反馈。 */
function onFly(x: number, y: number): void {
  if (typeof document === 'undefined') return;
  const target = document.querySelector<HTMLElement>('.op-cartbtn');
  if (target) {
    startFly(x, y, target);
    return;
  }
  // 首次加菜:结算栏要等 totalCount>0 触发 v-if 挂载,本帧 DOM 还没更新,
  // 查不到 .op-cartbtn;等挂载并完成布局后再起飞,落点与弹跳反馈才准确。
  void nextTick(() => {
    requestAnimationFrame(() => {
      const el = document.querySelector<HTMLElement>('.op-cartbtn');
      if (el) startFly(x, y, el);
    });
  });
}

// 飞球动画的兜底定时器句柄:组件卸载时要一并清掉(见 onUnmounted)。
const flyBallTimers = new Set<number>();

function startFly(x: number, y: number, target: HTMLElement): void {
  const rect = target.getBoundingClientRect();

  const outer = document.createElement('div');
  outer.className = 'fly-ball-x';
  outer.style.left = `${x - 7}px`;
  outer.style.top = `${y - 7}px`;
  outer.style.setProperty('--fly-x', `${rect.left + rect.width / 2 - x}px`);
  const mid = document.createElement('div');
  mid.className = 'fly-ball-y';
  mid.style.setProperty('--fly-y', `${rect.top + rect.height / 2 - y}px`);
  const core = document.createElement('div');
  core.className = 'fly-ball-core';
  mid.appendChild(core);
  outer.appendChild(mid);
  document.body.appendChild(outer);

  // 兜底:动画事件丢失(如测试环境)也不残留节点。
  // 句柄登记进 flyBallTimers:动画正常结束时清掉它,组件卸载时也一并清 ——
  // 否则连续加菜会在 body 上堆积一串待执行的 remove 回调(切页时残留)。
  const fallback = window.setTimeout(() => outer.remove(), 900);
  flyBallTimers.add(fallback);

  const done = () => {
    window.clearTimeout(fallback);
    flyBallTimers.delete(fallback);
    outer.remove();
    // 落地反馈:购物车图标弹一下
    target.classList.remove('cart-bump');
    void target.offsetWidth; // 强制 reflow,连续加菜也能重启动画
    target.classList.add('cart-bump');
  };
  outer.addEventListener('animationend', done, { once: true });
}

// 清空前二次确认:抽屉里一键清空太容易误触(与小程序 showModal 同文案)
function onClearCart(): void {
  if (!cart.value.length) return;
  const dlg = DialogPlugin.confirm({
    header: '清空购物车',
    body: '将移除已选的全部菜品，确定吗？',
    confirmBtn: { content: '清空', theme: 'danger' },
    cancelBtn: '再想想',
    onConfirm: () => {
      clearCart();
      // 清空后抽屉自动收起,顾客直接回到菜单继续点菜
      cartVisible.value = false;
      dlg.destroy();
    }
  });
}

// 备注写回后给一句反馈:弹层直接消失的话,顾客不确定到底存没存上
function onRemarkSave(): void {
  saveRemark();
  ui.success('备注已保存');
}

function startAppend(): void {
  // 加菜期间保持轮询:后厨状态与已上齐菜品会持续刷新,菜单里的「已点」标记才不会过期
  cart.value = [];
  orderRemark.value = '';
  // 人数同步为订单当前值,加菜时可在抽屉里继续调整。
  personCount.value = currentOrder.value?.personCount || 1;
  view.value = 'menu';
}

// 顾客侧已结束(已结账/完成/取消)后的「再点一单」:清空当前订单与购物车,回菜单开新单。
function startNewOrder(): void {
  // 「再点一单」是同一批客人继续吃：人数沿用上一单，避免新单按 1 人收茶位费。
  // 旧单没有人数(理论上不会)时退回本会话记忆值，再退回 1。
  const prev = Number(currentOrder.value?.personCount) || rememberedPerson() || 1;
  currentOrder.value = null;
  cart.value = [];
  orderRemark.value = '';
  personCount.value = Math.max(PERSON_MIN, Math.min(PERSON_MAX, prev));
  view.value = 'menu';
  // 必须显式停轮询:pollOnce 在 currentOrder 为空时会直接 return 不发请求,
  // 但 3 秒的定时器会一直空转到组件卸载(「再点一单」后长时间停在菜单页)。
  stopPolling();
}

// 订单相关视图(进度/已结账/完成/取消)才显示左上角返回按钮。
const orderViewActive = computed(
  () =>
    (view.value === 'order' || view.value === 'settled' || view.value === 'done' || view.value === 'canceled') &&
    !!currentOrder.value
);

// 左上角返回：看的是从「出餐中订单」列表点进来的已付款订单 → 回列表；
// 自己的进行中订单 → 回点餐页（与底部 tab 的「点餐」同效果）。
function backFromOrder(): void {
  if (servingOrders.value.length && Number(currentOrder.value?.payStatus) === 1) {
    currentOrder.value = null;
    stopPolling();
    clearUrgeTimer();
    // 必须连 view 一起拉回 'order'：轮询一拍（3s）就会把已付款的单推到 settled，
    // 出餐完成/取消还会推到 done / canceled。只清 currentOrder 的话，OrderTrackView
    // 要求 currentOrder、列表与空态要求 view === 'order'，四个分支全不成立 ——
    // 页面只剩一个橙色头部（顾客看到的就是「点了返回，内容没了」）。
    view.value = 'order';
    return;
  }
  view.value = 'menu';
}

// 订单 Tab 里点「出餐中」列表的一条:回拉完整详情后进入订单进度视图。
// 回拉失败留在列表页(拦截器已提示),不误导顾客「点了没反应」。
async function openServing(o: OrderSummary): Promise<void> {
  const no = String(o.orderNo || '');
  if (!no) return;
  try {
    currentOrder.value = await getOrderByNo(no, tableId.value);
  } catch {
    return;
  }
  // 视图按刚拉到的那份数据直接定:出餐中的单都是已付款的,本来就应该进「已结账」态。
  // 如果硬写 'order' 等 3s 后的轮询来纠正,顾客会先看到一版少了「已结账」提示的订单卡,
  // 而且评价状态(只在 settled/done 里查)也跟着被拖到 3s 之后。
  view.value = Number(currentOrder.value?.payStatus) === 1 ? 'settled' : 'order';
  startPolling();
}

// 底部 tabbar 切页。加菜时随时切回订单页核对已下单菜品,购物车内容保留不丢。
function switchView(v: 'menu' | 'order'): void {
  if (view.value === v) return;
  // 浏览模式下订单页展示的是上一桌的账单明细,不让切过去(隐私与误操作双重考虑)
  if (v === 'order' && foreign.value) {
    foreignTip();
    return;
  }
  // 离开菜单页顺手收起搜索页,避免切回时残留
  searchOpen.value = false;
  if (v === 'order') {
    view.value = 'order';
    // 没单时展示空态即可,起轮询只会白拉接口(与小程序一致:订单页常驻可进)。
    // 立即补一拍:手上的订单可能是几分钟前的快照,不补就先给顾客看 3s 旧状态,
    // 状态若已翻成「已结账 / 已完成」,评价入口(挂在 settled/done 上)也要多等 3s 才出。
    if (hasOrder.value) startPolling({ immediate: true });
    else stopPolling();
  } else {
    view.value = 'menu';
  }
}

function genRequestId(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') return crypto.randomUUID();
  return 'req-' + Date.now().toString(36) + '-' + Math.random().toString(16).slice(2);
}

async function submit(): Promise<void> {
  // 「不是本人的单」:菜可以挑,单不能下 —— 一桌只允许一笔未结账订单,后端也会拒绝。
  if (foreign.value) {
    foreignTip();
    return;
  }
  if (!cart.value.length) {
    MessagePlugin.warning('先挑几道想吃的菜吧～');
    return;
  }
  submitting.value = true;
  try {
    const items = cart.value.map(i => ({
      dishId: i.dishId,
      specId: i.specId,
      quantity: i.quantity,
      itemRemark: i.remark
    }));
    // 幂等:只有「本次提交内容与上次完全一致」的重复提交才复用同一个 clientRequestId
    // (超时/未知错误后的原样重试)。内容变了(顾客改了菜、加了菜、改了备注)就换新键,
    // 否则后端命中幂等键直接返回首次结果,新提交的内容会被静默丢弃。
    const fingerprint = JSON.stringify({
      append: canAppend.value,
      orderNo: currentOrder.value?.orderNo || '',
      personCount: personCount.value,
      orderRemark: orderRemark.value,
      items
    });
    if (!pendingRequestId || pendingFingerprint !== fingerprint) {
      pendingRequestId = genRequestId();
      pendingFingerprint = fingerprint;
    }
    const clientRequestId = pendingRequestId;
    let orderNo: string;
    if (canAppend.value) {
      const res = await appendOrder({
        tableId: tableId.value,
        orderNo: currentOrder.value?.orderNo,
        items,
        personCount: personCount.value,
        clientRequestId
      });
      // 先付模式：加的菜要等整单付完款才进后厨，不能说“已进后厨，马上安排”
      MessagePlugin.success(
        payFirst.value ? '加菜成功～整单付款后后厨立刻开工' : '加菜成功～新菜已进后厨，马上安排'
      );
      orderNo = res.orderNo;
    } else {
      const res = await createOrder({
        tableId: tableId.value,
        personCount: personCount.value,
        orderRemark: orderRemark.value,
        items,
        clientRequestId
      });
      // 先付模式：还没收到钱，后厨此刻并不知道这颗单，不能说“已经收到、火力全开”
      MessagePlugin.success(
        payFirst.value ? '下单成功～还没付款哦，付完款后厨立刻开工' : '下单成功～大厨已收到，火力全开中'
      );
      orderNo = res.orderNo;
    }
    // 订单已在后端创建成功:先落定前端状态(清幂等键与购物车、切订单视图),再回拉完整详情。
    // 回拉失败绝不能连累这些状态 —— 否则会停留菜单、购物车仍在,顾客以为没下单而重复操作。
    // 这里用订单号占一个最小订单,让轮询能按单号继续拉取,下一拍(≤3s)即补全。
    pendingRequestId = '';
    pendingFingerprint = '';
    cart.value = [];
    cartVisible.value = false;
    try {
      currentOrder.value = await getOrderByNo(orderNo, tableId.value);
    } catch {
      currentOrder.value = { orderNo } as OrderDetail;
    }
    // 下单成功后一律落到订单明细页：顾客要在这里核对菜品、看到金额，
    // 再由页面上的「待付款」引导条带他去支付（先付模式下那条引导是必看的一步）。
    view.value = 'order';
    startPolling();
  } catch {
    /* 失败已由拦截器统一提示,停留当前视图供重试;pendingRequestId 保留以便重试去重 */
  } finally {
    submitting.value = false;
  }
}

// ---- 在线支付 ----
function switchPayTab(tab: 'wx' | 'ali'): void {
  payTab.value = tab;
  onlineQr.value = ''; // 切换渠道时重置在线支付二维码
}

async function startOnlinePay(): Promise<void> {
  if (!currentOrder.value || paying.value) return;
  paying.value = true;
  try {
    const channel = payTab.value === 'wx' ? 'wxpay' : 'alipay';
    const res = await createPay({ orderNo: currentOrder.value.orderNo, channel });
    // qrcode 约 24kB,只有走到「在线支付」才会用:按需加载,别让扫码点菜的首屏陪它。
    // 用 qrcode 库把支付串渲染为二维码图片(360px/M级容错)
    const { default: QRCode } = await import('qrcode');
    onlineQr.value = await QRCode.toDataURL(res.codeUrl, { width: 360, margin: 2, errorCorrectionLevel: 'M' });
    startPolling(); // 轮询支付结果
  } catch {
    /* 错误已由拦截器提示 */
  } finally {
    paying.value = false;
  }
}

function backFromPay(): void {
  onlineQr.value = '';
  view.value = 'order';
}

// 金额变化守卫:支付页已展示二维码后订单金额变了(同桌另一部手机加菜 / 店员改单),
// 旧二维码仍按旧金额收款 —— 顾客扫旧码会付成「金额不一致」,钱到账却不入账,
// 只能人工退款(后端防线拒绝入账并留痕,见 payment_service.RecordMismatchedPayment)。
// 检测到变化立即作废二维码:按钮上的金额与页头实时金额同源,重新生成即对齐。
// 首次从 0 变为实际金额(订单加载)不算变化;不在支付页或还没生成过二维码也不处理。
watch(
  () => Number(currentOrder.value?.totalAmount) || 0,
  (now, prev) => {
    if (prev === 0 || now === prev) return;
    if (view.value !== 'pay' || !onlineQr.value) return;
    onlineQr.value = '';
    MessagePlugin.warning('订单金额有变化，请重新生成支付码');
  }
);

// ---- 订单评价(仅商家可见) ----
// 一单一评:入口在订单页 settled/done 态(见 OrderTrackView 的 canReview)。
// 状态在进结束视图时查一次驱动「去评价 / 已评价」两种形态;弹层提交后即时更新。
const reviewStatus = ref<CustomerReviewStatus>({ reviewed: false });
const reviewSheetOpen = ref(false);
const reviewSubmitting = ref(false);

// 进结束视图(含轮询把状态推到已付+上齐)时查一次评价状态:
// silent 失败不打扰 —— 评价是锦上添花,查询失败只表现为入口仍显示「去评价」。
watch(
  [view, () => Number(currentOrder.value?.payStatus)],
  ([v]) => {
    const no = currentOrder.value?.orderNo;
    if (!no || (v !== 'settled' && v !== 'done')) return;
    // 匿名顾客带设备键:查的是「本机是否评过」;会员由后端按令牌派生,键不传。
    getReviewStatus(no, tableId.value, visitorId()).then(st => (reviewStatus.value = st)).catch(() => {});
  },
  { immediate: true }
);

function openReviewSheet(): void {
  if (reviewStatus.value.reviewed) return;
  reviewSheetOpen.value = true;
}

async function submitReview(payload: { rating: number; tags: string[]; content: string; images: string[] }): Promise<void> {
  if (!currentOrder.value || reviewSubmitting.value) return;
  reviewSubmitting.value = true;
  try {
    const st = await createReview({
      tableId: tableId.value,
      orderNo: currentOrder.value.orderNo,
      // 匿名键=设备 visitorId(持久化,同机恒定);会员后端按身份覆盖。
      reviewerKey: visitorId(),
      ...payload
    });
    reviewStatus.value = st;
    reviewSheetOpen.value = false;
    MessagePlugin.success('评价已提交，感谢你的反馈');
  } catch {
    /* 失败已由拦截器提示;弹层保持打开,内容不丢,顾客可直接重试 */
  } finally {
    reviewSubmitting.value = false;
  }
}

// 桌台人工状态:0 正常 / 1 停用 / 2 预约(店员在管理端「桌台管理」里设置)。
// 非 0 时本桌不让新开单 —— 避免顾客白点一车菜,给出联系店员的出口。
const lockStatus = computed(() => Number(table.value.lockStatus) || 0);
const lockText = computed(() =>
  lockStatus.value === 2 ? '该桌台已被预订，如需用餐请联系店员' : '该桌台暂不可用，如需用餐请联系店员'
);

// ---- 未结账订单的归属确认 ----
// 扫码进来时本桌已有一笔未结账订单:店家漏点「收款」的话,它很可能就是上一桌留下的。
// 直接进订单页等于把上一桌的账单摊给下一桌,所以先让顾客自己认领一次。
// 认领结果记在 sessionStorage —— 同一位顾客刷新/切页回来不再重复打扰,
// 换一部手机重新扫码(新会话)则必然重新问一次。
const foreign = ref(false);

function ownKey(): string {
  return `dining:mine:${tableId.value}:${String(currentOrder.value?.orderNo || '')}`;
}

function confirmMine(): void {
  safeSession.set(ownKey(), '1');
  // 落到订单明细页：顾客先核对菜品，再由页面上的待付款引导去支付
  view.value = 'order';
  startPolling();
}

function notMine(): void {
  safeSession.set(ownKey(), 'no');
  // 已经不是自己的单:本地残留的购物车一并丢掉,免得菜单里还挂着上一桌点的菜
  cart.value = [];
  foreign.value = true;
}

// 「不是本人的单」也允许先浏览菜单、把菜挑进购物车;
// 只是本桌还没清台,提交会卡在下面 submit 的拦截上。
function browseMenu(): void {
  view.value = 'menu';
}

// 浏览模式的统一提示:下单与订单页入口共用
function foreignTip(): boolean {
  if (!foreign.value) return false;
  MessagePlugin.warning('本桌上一笔订单还未结账，服务员清台后即可下单');
  return true;
}

// 确认页只给「下单时间 + 菜数 + 金额」三行:够本人认出自己的单,
// 又不至于把上一桌具体点了什么菜完整摊给陌生人看。
const resumeTime = computed(() => {
  // 统一时间口径:后端可能返回 "YYYY-MM-DD HH:MM:SS" 或 ISO "YYYY-MM-DDTHH:MM:SSZ",
  // 先把 T 归一为空格再截取,避免切出 "10-09T12:03" 这种带 T 的丑值(与 paidAt 展示一致)。
  const t = String(currentOrder.value?.createTime || '').replace('T', ' ');
  return t.length >= 16 ? t.slice(5, 16) : '--:--';
});
const resumeCount = computed(() =>
  (currentOrder.value?.items || []).reduce((s, i) => s + (Number(i.quantity) || 0), 0)
);
const resumeAmount = computed(() => Number(currentOrder.value?.totalAmount || 0).toFixed(2));

/** Promise 成败收敛成值:提前发出、稍后才 await 的请求不会先触发未捕获拒绝。 */
type Settled<T> = { ok: true; v: T } | { ok: false; e: unknown };
const settle = <T>(p: Promise<T>): Promise<Settled<T>> =>
  p.then(
    v => ({ ok: true as const, v }),
    e => ({ ok: false as const, e })
  );

// 菜单/备注缺失只影响点餐:订单视图已经画出来了,就别把顾客正在看的那张卡顶成错误页。
function failMenuLoad(e: unknown): void {
  if (view.value !== 'loading') return;
  view.value = 'error';
  loadError.value = friendlyLoadError(e);
}

async function load(): Promise<void> {
  view.value = 'loading';
  loadError.value = '';
  // 菜单 / 备注是两份大包,但订单卡用不到它们:先并行发出、稍后再 await。
  // 若像以前那样 Promise.all 一起等,订单卡(菜品明细)必须等整份菜单回来才画得出,
  // 顾客点进「订单」就会空等一个菜单往返 —— 桌台详情 + 基础配置本来早就到了。
  const menuTask = settle(getMenu());
  const remarkTask = settle(getRemarks());

  let tb: TableInfo;
  try {
    const [cfg, t] = await Promise.all([getPublicConfig(), getTable(tableRef)]);
    config.value = cfg;
    // 顶部展示「店铺名称 · 标语」(标语为系统配置的独立字段,留空则只显示名称)
    shopName.value = [cfg.shop_name, cfg.shop_slogan].filter(Boolean).join(' · ') || '扫码点餐';
    setCustomerTitle(cfg.shop_name);
    tb = t;
    table.value = tb;
    tableId.value = tb.tableId ?? 0;
    servingOrders.value = tb.servingOrders || [];
  } catch (e) {
    view.value = 'error';
    loadError.value = friendlyLoadError(e);
    return;
  }

  // 桌台详情一到就先定「订单相关」视图:已结账 / 订单明细 / 归属确认 / 锁定桌台
  // 都只依赖 table + config,不必为菜单陪跑。
  if (tb.currentOrder) {
    currentOrder.value = tb.currentOrder;
    const paid = Number(tb.currentOrder.payStatus) === 1;
    // 已付款的单只看出餐进度,不涉及归属;同一会话内认领过(1)的不再重复问,
    // 明确说过不是自己单的(no)直接停在「等服务员清台」。
    const mine = safeSession.get(ownKey()) || '';
    if (paid) view.value = 'settled';
    // 两位都落订单明细页：顾客先核对菜品与金额，再由页面上的「待付款」引导去支付。
    // 只有认领过(mine==='1')才直达订单页；没认领的先走归属确认，
    // 避免把上一桌的账单摊给下一桌。
    else if (mine === '1') view.value = 'order';
    else {
      view.value = 'resume';
      foreign.value = mine === 'no';
    }
    // 待确认期间不轮询:后厨一改状态就把确认页顶掉的话,顾客还没选就被跳走了。
    if (view.value !== 'resume') startPolling();
  } else if (lockStatus.value) {
    view.value = 'locked';
  }

  const menuRes = await menuTask;
  const remarkRes = await remarkTask;
  if (!menuRes.ok) {
    failMenuLoad(menuRes.e);
    return;
  }
  if (!remarkRes.ok) {
    failMenuLoad(remarkRes.e);
    return;
  }
  const m = menuRes.v;
  menu.value = m;
  remarks.value = remarkRes.v;
  // 默认选中每道菜的「当前可用规格」(与 currentSpec 的回退口径一致)。
  // 不能直接取 specs[0]:若第一个规格恰已售罄,选它会让 dishQty 按售罄规格的 key 计数,
  // 而 changeQty 实际加到 currentSpec 回退出的有货规格上 —— 卡片步进器恒显示 0、与购物车不一致。
  m.forEach(c =>
    c.dishes?.forEach(d => {
      if (d.specs && d.specs.length) selectedSpec[d.dishId as number] = currentSpec(d).specId as number;
    })
  );
  if (categories.value.length) activeCat.value = categories.value[0].categoryId ?? null;
  // 没有本桌订单、桌台也没锁定 = 正常进菜单(等菜单到齐再切,避免空菜单闪一下)
  if (view.value === 'loading') {
    // 菜单照常进：就算要选人数，顾客也应该先看到菜单（蒙层形式）
    const remembered = rememberedPerson();
    if (remembered) personCount.value = remembered;
    // 桌级共享：别的手机选过（pendingPersonCount）则沿用；本机记忆优先。
    // 本桌还有已付款出餐中的订单时，人数沿最近一笔（茶位费已收过，不重复问）。
    const adopted = !remembered && (adoptPendingPerson(tb) || adoptServingPerson(tb));
    view.value = 'menu';
    personGate.value = seatFeeOn(config.value) && !remembered && !adopted;
    if (personGate.value) personDraft.value = 0;
  }
  // 购物车要等订单归属定下来再恢复:它的 key 与恢复口径都依赖 currentOrder。
  // 浏览模式同样恢复 —— 顾客挑好的菜在刷新后不丢,清台后能直接提交
  restoreCart(m);
}

// 把加载失败转成顾客能看懂、且知道下一步该做什么的中文提示。
// 拦截器已把 HTTP 错误统一成中文(msg 优先),这里再做一层场景化改写。
function friendlyLoadError(e: unknown): string {
  const err = e as { friendlyMessage?: string; message?: string } | null | undefined;
  const msg = err?.friendlyMessage || err?.message || '';
  if (/桌台|桌号/.test(msg)) return '桌号不存在，请重新扫描餐桌上的二维码';
  if (/Network Error|网络连接失败/i.test(msg)) return '网络连接失败，请检查手机网络后重试';
  if (/超时/.test(msg)) return '网络响应较慢，请稍后重试';
  return msg || '加载失败，请检查网络后重试';
}

onMounted(load);

// 站点访问统计:每位客人扫码进入即一次 PV(fire-and-forget,失败静默)。
// entry 带桌台点餐码(老数字桌号也原样带上),报表里能看出「哪张桌的码被扫得最多」。
// 口径与隐私说明见 docs/design-docs/visit-analytics/spec.md。
try {
  void reportVisit({ visitorId: visitorId(), channel: 'h5', entry: tableRef }).catch(() => {
    /* 埋点失败绝不打扰点餐 */
  });
} catch {
  /* 埋点同步异常(接口被替换/禁用)同样静默:统计永远不能挡住点餐 */
}
onUnmounted(() => {
  stopPolling();
  clearUrgeTimer();
  if (payDoneTimer) clearTimeout(payDoneTimer);
  // 飞球兜底定时器:动画未结束时卸载也要清掉,否则回调仍会执行(且节点残留在 body)
  for (const t of flyBallTimers) window.clearTimeout(t);
  flyBallTimers.clear();
});
</script>

<style scoped>
.order-page {
  max-width: 480px;
  margin: 0 auto;
  height: 100vh;
  height: 100dvh;
  background: var(--bg);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

/* ---------- 加载骨架屏 ----------
   按真实布局铺一遍（渐变头部 + 搜索 + 左栏 + 菜品行），加载完成时不会整页跳。
   白色区域的灰条用横向流光；头部在渐变底上改用透明度呼吸，避免灰底压在品牌色上。
   尺寸与 HeroHeader / MenuView 的真实布局对齐（1rpx ≈ 0.5px 换算）。 */
.sk-wrap {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.sk-bar {
  background: linear-gradient(90deg, #eef0f2 25%, #f8f9fa 37%, #eef0f2 63%);
  background-size: 400% 100%;
  animation: sk-sweep 1.4s ease infinite;
}

@keyframes sk-sweep {
  from {
    background-position: 100% 50%;
  }

  to {
    background-position: 0 50%;
  }
}

.sk-hero {
  display: flex;
  flex-shrink: 0;
  align-items: center;
  justify-content: space-between;

  /* 与 .op-hero 同一套内边距:加载完成切到真实头部时不跳高 */
  padding: calc(18px + env(safe-area-inset-top, 0px)) 18px 30px;
  background: linear-gradient(135deg, #ff8a50 0%, #ff6b35 55%, #f0481f 100%);
}

.sk-shop,
.sk-table {
  height: 26px;
  border-radius: var(--r-md);
  background: rgb(255 255 255 / 28%);
  animation: sk-pulse 1.3s ease-in-out infinite;
}

.sk-shop {
  width: 130px;
}

.sk-table {
  width: 60px;
  border-radius: var(--r-pill);
}

@keyframes sk-pulse {
  0%,
  100% {
    opacity: 0.55;
  }

  50% {
    opacity: 1;
  }
}

.sk-sheet {
  display: flex;
  flex: 1;
  flex-direction: column;
  min-height: 0;
  margin-top: -16px;
  border-radius: 18px 18px 0 0;
  background: #fff;
  overflow: hidden;
}

.sk-body {
  display: flex;
  flex: 1;
  min-height: 0;
}

.sk-cats {
  flex-shrink: 0;
  width: 90px;
  padding: 10px 10px 0;
  box-sizing: border-box;
  background: #fafafa;
}

.sk-cat {
  height: 18px;
  margin-bottom: 24px;
  border-radius: var(--r-sm);
}

.sk-dishes {
  flex: 1;
  min-width: 0;
  padding: 10px 12px 0;
  box-sizing: border-box;
}

.sk-dish {
  display: flex;
  gap: 10px;
  padding: 12px 0;
}

.sk-img {
  flex-shrink: 0;
  width: 80px;
  height: 80px;
  border-radius: var(--r-md);
}

.sk-main {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: 8px;
  min-width: 0;
  padding-top: 4px;
}

.sk-line {
  height: 15px;
  border-radius: var(--r-sm);
}

.sk-line-short {
  width: 60%;
}

.sk-price {
  width: 70px;
  height: 18px;
  margin-top: auto;
  border-radius: var(--r-sm);
}

/* ---------- 加载 / 错误 ---------- */
.state-box {
  flex: 1;
  min-height: 55vh;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 14px;
}

.state-text {
  max-width: 300px;
  padding: 0 16px;
  font-size: 14px;
  line-height: 1.7;
  text-align: center;
  color: var(--ink-2);
}

/* 主文案下的补充说明:缩小一号、颜色更浅,形成信息层级 */
.state-text p {
  margin: 0;
}

.state-text .sub {
  margin-top: 4px;
  font-size: 13px;
  color: var(--ink-3);
}

.spin {
  width: 34px;
  height: 34px;
  border: 3px solid #ffe3d4;
  border-top-color: var(--brand);
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

.err-icon {
  width: 52px;
  height: 52px;
  border-radius: 50%;
  background: var(--danger-soft);
  color: var(--danger);
  display: flex;
  align-items: center;
  justify-content: center;
}

/* 「本桌还未清台」的可爱插画风图标:一块圆滚滚的小海绵在盘子上左右擦动,
   配泡泡上浮、星星闪烁,直观传达「正在清理、稍等片刻」 */
.state-emoji {
  width: 88px;
  height: 88px;
  border-radius: 50%;
  background: var(--brand-ghost, #fff3ec);
  display: flex;
  align-items: center;
  justify-content: center;
}

.state-emoji svg {
  display: block;
  width: 58px;
  height: 58px;
}

/* 海绵:以盘心为轴左右擦动,带一点摇头似的摇摆 */
.state-emoji .sponge {
  transform-box: view-box;
  transform-origin: 32px 33px;
  animation: sponge-wipe 1.8s ease-in-out infinite alternate;
}

@keyframes sponge-wipe {
  from {
    transform: translateX(-4px) rotate(-5deg);
  }

  to {
    transform: translateX(4px) rotate(5deg);
  }
}

/* 泡泡:三颗错峰往上飘、飘散 */
.state-emoji .bubble {
  animation: bubble-up 2.2s ease-in infinite;
}

.state-emoji .b2 {
  animation-delay: 0.6s;
}

.state-emoji .b3 {
  animation-delay: 1.2s;
}

@keyframes bubble-up {
  0% {
    opacity: 0;
    transform: translateY(5px) scale(0.6);
  }

  30% {
    opacity: 1;
  }

  100% {
    opacity: 0;
    transform: translateY(-9px) scale(1.1);
  }
}

/* 星星:一闪一闪 */
.state-emoji .spark {
  transform-box: view-box;
  transform-origin: 54px 14px;
  animation: spark-twinkle 1.6s ease-in-out infinite;
}

@keyframes spark-twinkle {
  0%,
  100% {
    opacity: 0.3;
    transform: scale(0.8);
  }

  50% {
    opacity: 1;
    transform: scale(1);
  }
}

/* SVG 感叹号:线条粗细、圆点大小、光学居中均可精确控制,替代字体「!」
   (字体基线导致的偏移在圆形底上会显得不协调) */
.err-icon svg {
  display: block;
  width: 30px;
  height: 30px;
}

.retry-btn {
  margin-top: 4px;
  padding: 9px 28px;
  border: none;
  border-radius: var(--r-pill);
  background: linear-gradient(135deg, var(--brand), var(--brand-deep));
  color: #fff;
  font-size: 14px;
  cursor: pointer;
}

/* 状态页的按钮组(如「先看看菜单」+「刷新状态」):并排等宽、同一高度,
   主按钮在前。box-sizing 统一为 border-box,避免「带边框的一颗比渐变的一颗高 2px」 */
.state-actions {
  display: flex;
  gap: 12px;
  width: 100%;
  max-width: 320px;
  margin-top: 6px;
}

.state-actions .retry-btn {
  flex: 1;
  height: 44px;
  margin: 0;
  padding: 0;
  box-sizing: border-box;
  font-size: 15px;
  font-weight: 600;
}

.retry-btn.plain {
  border: 1px solid var(--line);
  background: #fff;
  color: var(--ink-2);
  font-weight: 400;
}

.state-title {
  font-size: 17px;
  font-weight: 600;
  color: var(--ink-1);
}

/* 菜单白色卡片:整体上移盖住 hero 底部,圆角与裁切统一在这里提供 */
.menu-sheet {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  margin-top: -16px;
  background: #fff;
  border-radius: 18px 18px 0 0;
  overflow: hidden;
  position: relative;
  z-index: 1;
}

/* 独立搜索页:整页白底盖住 hero 与菜单卡,压在结算栏/底部导航之下
   (z-index 60 < tabbar 88 / cartbar 90),顾客搜索时仍能看车、切页签 */
.search-layer {
  position: fixed;
  inset: 0;
  z-index: 60;
  max-width: 480px;
  margin: 0 auto;
  background: #fff;
  display: flex;
  flex-direction: column;
}

/* 订单 Tab 的「出餐中订单」列表:currentOrder 只含未结账单,
   已付款出餐中的单在这里列出,点一条看进度。
   每张卡一行摘要(取餐号 + 状态)、一行菜品、一行时间人数,右侧金额与入口 ——
   先扫得出「这单是什么」,再决定点不点进去 */
.serving-list {
  display: flex;
  flex-direction: column;
  flex: 1;
  padding: 16px 14px 86px;
  overflow-y: auto;
}

.sl-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 0 2px;
}

.sl-title {
  font-size: 16px;
  font-weight: 800;
  letter-spacing: 0.2px;
  color: var(--ink-1);
}

/* 笔数小胶囊:一眼看出本桌还有几单在出餐 */
.sl-badge {
  flex-shrink: 0;
  padding: 2px 8px;
  border-radius: var(--r-pill);
  background: var(--brand-soft);
  color: var(--brand-deep);
  font-size: 11px;
  font-weight: 600;
  line-height: 1.5;
}

.sl-hint {
  margin: 6px 2px 12px;
  font-size: 11.5px;
  line-height: 1.5;
  color: var(--ink-3);
}

.sl-item {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
  padding: 13px 14px;
  margin-bottom: 10px;
  border: none;
  border-radius: var(--r-lg);
  background: #fff;
  box-shadow: var(--shadow-1);
  text-align: left;
  cursor: pointer;
  transition: transform 0.12s ease, box-shadow 0.12s ease;
}

/* 按下时轻微下沉:整卡可点,给一点物理反馈 */
.sl-item:active {
  transform: scale(0.985);
  box-shadow: var(--shadow-2);
}

.sl-main {
  flex: 1;
  min-width: 0;
}

.sl-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.sl-no {
  flex-shrink: 0;
  font-size: 15px;
  font-weight: 800;
  letter-spacing: 0.4px;
  color: var(--ink-1);
}

/* 状态标签:与订单详情页 .st-tag 同一套配色与尺寸 */
.sl-tag {
  display: inline-flex;
  align-items: center;
  height: 20px;
  padding: 0 8px;
  border-radius: var(--r-pill);
  font-size: 10.5px;
  line-height: 1;
}

.sl-tag--primary {
  background: var(--brand-soft);
  color: var(--brand-deep);
}

.sl-tag--warning {
  background: var(--warning-soft);
  color: #b7791f;
}

.sl-tag--success {
  background: var(--success-soft);
  color: var(--success);
}

/* 菜品摘要:单行省略,菜名再长也不把右侧金额挤出去 */
.sl-dishes {
  margin-top: 6px;
  font-size: 12.5px;
  line-height: 1.4;
  color: var(--ink-2);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sl-meta {
  margin-top: 4px;
  font-size: 11.5px;
  line-height: 1.4;
  color: var(--ink-3);
}

.sl-side {
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 8px;
}

/* 金额:已付款的实付合计,顾客核对账目用 */
.sl-amt {
  font-size: 14px;
  font-weight: 700;
  color: var(--brand-deep);
  letter-spacing: 0.2px;
}

/* 入口:箭头暗示「点得进去」,不用整卡加边框去强调 */
.sl-go {
  display: inline-flex;
  align-items: center;
  gap: 1px;
  font-size: 11.5px;
  color: var(--ink-3);
}

.sl-item:active .sl-go {
  color: var(--brand-deep);
}

/* 列表底部「去加菜」:ghost 描边款压底(与小程序 .sp-menu 同布局),
   比通用灰描边多一层品牌浅橙描边 + 深品牌字,和页内状态标签同一色系 */
.sl-add-more {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  width: 100%;
  height: 44px;
  margin-top: auto;
  padding: 0 20px;
  border: 1px solid var(--td-brand-color-2, #ffe3d4);
  border-radius: var(--r-pill);
  background: #fff;
  color: var(--brand-deep);
  font-family: inherit;
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
  transition: transform 0.12s ease, box-shadow 0.12s ease;
}

/* 按下轻微下沉,与出餐卡片(.sl-item)同一套物理反馈 */
.sl-add-more:active {
  transform: scale(0.98);
  box-shadow: 0 2px 8px rgb(240 72 31 / 15%);
}

.sl-add-ic {
  font-size: 16px;
  line-height: 1;
}

/* 订单空态:还没下单时进「订单」页看到的是它(底部导航常驻后进得来)。
   图标轻轻上下浮动 + 年轻化文案 + 一键去点餐,替代干巴巴的一行灰字 */
.order-empty {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 0 24px 80px;
  text-align: center;
}

.empty-illo {
  width: 104px;
  height: 84px;
  animation: empty-float 2.4s ease-in-out infinite;
  filter: drop-shadow(0 12px 20px rgb(240 72 31 / 14%));
}

.empty-illo svg {
  width: 100%;
  height: 100%;
}

/* 桌面投影:整图上下浮动时,影子同步收放,保持「落地」错觉 */
.illo-shadow {
  transform-box: fill-box;
  transform-origin: center;
  animation: shadow-breathe 2.4s ease-in-out infinite;
}

@keyframes shadow-breathe {
  0%,
  100% {
    opacity: 0.38;
    transform: scaleX(1);
  }

  50% {
    opacity: 0.2;
    transform: scaleX(0.88);
  }
}

/* 星点轮流闪烁,让插画「活」起来 */
.illo-spark {
  transform-box: fill-box;
  transform-origin: center;
  animation: spark-twinkle 1.8s ease-in-out infinite;
}

.illo-spark.s2 {
  animation-delay: 0.9s;
}

/* 热气袅袅:三缕错拍升腾 */
.illo-steam {
  animation: steam-rise 2.2s ease-in-out infinite;
}

.illo-steam.st2 {
  animation-delay: 0.7s;
}

.illo-steam.st3 {
  animation-delay: 1.4s;
}

@keyframes steam-rise {
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

@keyframes spark-twinkle {
  0%,
  100% {
    opacity: 0.35;
    transform: scale(0.8);
  }

  50% {
    opacity: 1;
    transform: scale(1.15);
  }
}

@media (prefers-reduced-motion: reduce) {
  .empty-illo,
  .illo-spark,
  .illo-steam,
  .illo-shadow {
    animation: none;
  }
}

@keyframes empty-float {
  0%,
  100% {
    transform: translateY(0);
  }

  50% {
    transform: translateY(-8px);
  }
}

.empty-title {
  margin-top: 16px;
  font-size: 18px;
  font-weight: 700;
  color: var(--ink-1);
}

.empty-sub {
  margin-top: 8px;
  max-width: 260px;
  font-size: 13px;
  line-height: 1.6;
  color: var(--ink-3);
}

.empty-btn {
  margin-top: 24px;
  padding: 11px 40px;
  border: none;
  border-radius: var(--r-pill);
  background: linear-gradient(135deg, var(--brand), var(--brand-deep));
  box-shadow: 0 8px 18px rgb(240 72 31 / 28%);
  color: #fff;
  font-size: 15px;
  font-weight: 600;
  cursor: pointer;
  transition: transform 0.12s ease, box-shadow 0.12s ease;
}

.empty-btn:active {
  transform: translateY(1px);
  box-shadow: 0 4px 10px rgb(240 72 31 / 28%);
}

/* 本桌「已付款但出餐未完成」订单提示条:置顶于白色卡片内。
   文字与订单标签同一行:标签区横向滚动,再多订单也不撑高菜单区。
   样式与小程序端 pages/index 的出餐提示条对齐(rpx ≈ 0.5px 换算) */
.serving-banner {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
  padding: 8px 12px 8px 16px;
  background: var(--brand-ghost);
  color: var(--ink-2);
  font-size: 12px;
  border-bottom: 1px solid var(--line);
}

.serving-label {
  flex-shrink: 0;
}

/* 标签横向滚动区:滚动条隐藏;overflow 裁纵向溢出,靠内层的上下留白给阴影让位 */
.serving-scroll {
  flex: 1;
  min-width: 0;
  overflow-x: auto;
  -webkit-overflow-scrolling: touch;
  scrollbar-width: none;
  white-space: nowrap;
}

.serving-scroll::-webkit-scrollbar {
  display: none;
}

/* 内层包一层:上下留白防止胶囊阴影被滚动区裁掉,右侧留出滑到底的呼吸空间 */
.serving-chips-inner {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 3px 8px 3px 0;
  vertical-align: middle;
}

.serving-chip {
  display: inline-flex;
  align-items: center;
  flex-shrink: 0;
  padding: 4px 9px;
  border: 1px solid var(--td-brand-color-2, #ffe3d4);
  border-radius: var(--r-pill);
  background: #fff;
  color: var(--brand-deep);
  font-family: inherit;
  font-size: 11px;
  line-height: 1;
  font-weight: 500;
  white-space: nowrap;
  cursor: pointer;
  box-shadow: 0 1px 4px rgb(240 72 31 / 12%);
}

/* 胶囊尾部的「可点进详情」箭头:与单号留隙、用品牌色弱化存在感 */
.serving-chip-go {
  margin-left: 3px;
  color: var(--brand);
  font-size: 12px;
  line-height: 1;
}

/* 「不是本人的单」浏览模式提示条:文案 + 返回按钮两端对齐 */
.foreign-banner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.foreign-back {
  flex-shrink: 0;
  padding: 4px 14px;
  border: 1px solid var(--brand, #ff6b35);
  border-radius: var(--r-pill);
  background: #fff;
  color: var(--brand-deep, #f0481f);
  font-size: 12px;
  cursor: pointer;
}

/* ---- 未结账订单归属确认 ---- */
.resume-box {
  flex: 1;
  padding: 24px 20px 32px;
  overflow-y: auto;
}

.resume-title {
  font-size: 17px;
  font-weight: 600;
  color: var(--ink-1);
  margin-bottom: 14px;
}

.resume-card {
  background: #fff;
  border: 1px solid var(--line);
  border-radius: var(--r-card, 12px);
  padding: 4px 14px;
  margin-bottom: 14px;
}

.resume-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 11px 0;
  font-size: 14px;
  color: var(--ink-3);
}

.resume-row + .resume-row {
  border-top: 1px solid var(--line);
}

.resume-row b {
  color: var(--ink-1);
  font-weight: 600;
}

.resume-hint {
  margin: 0 0 18px;
  font-size: 13px;
  line-height: 1.7;
  color: var(--ink-3);
}

.resume-btn {
  display: block;
  width: 100%;
  padding: 13px 0;
  border: 1px solid var(--line);
  border-radius: var(--r-pill);
  background: #fff;
  color: var(--ink-2);
  font-size: 15px;
  cursor: pointer;
}

.resume-btn.primary {
  margin-bottom: 12px;
  border-color: var(--brand, #ff6b35);
  background: var(--brand, #ff6b35);
  color: #fff;
  font-weight: 600;
}

.resume-btn:active {
  opacity: 0.85;
}

/* ---------- 入座选人数：半透明蒙层 + 贴底面板（茶位费门店） ----------
   菜单照常渲染，蒙层只是盖在上面：顾客扫码后立刻看得到菜品，不会因为
   「什么都没有」而心烦。z-index 高于结算栏(90)/底部导航(88)，低于抽屉(1500)。 */
.person-mask {
  position: fixed;
  inset: 0;
  z-index: 100;
  display: flex;
  align-items: flex-end;
  justify-content: center;
  background: rgb(15 15 20 / 45%);
  animation: pg-fade 0.2s ease-out;
}

@keyframes pg-fade {
  from {
    opacity: 0;
  }

  to {
    opacity: 1;
  }
}

.person-sheet {
  width: 100%;
  max-width: 480px;
  padding: 18px 18px calc(16px + env(safe-area-inset-bottom));
  border-radius: 18px 18px 0 0;
  background: #fff;
  box-shadow: 0 -16px 48px rgb(0 0 0 / 18%);
  animation: pg-up 0.24s cubic-bezier(0.22, 0.9, 0.36, 1);
}

@keyframes pg-up {
  from {
    transform: translateY(40px);
    opacity: 0.4;
  }

  to {
    transform: translateY(0);
    opacity: 1;
  }
}

.ps-title {
  color: var(--ink, #1a1a1a);
  font-size: 17px;
  font-weight: 700;
  letter-spacing: 1px;
  text-align: center;
}

/* 一行中性提示：只交代「选完就能点菜」这个下一步，不涉及金额 */
.ps-hint {
  margin-top: 4px;
  margin-bottom: 14px;
  color: var(--ink-3, #8c8c8c);
  font-size: 12px;
  text-align: center;
}

/* 快捷人数 1~10：五列两行等宽格子，点按没有瞄准压力 */
.pg-grid {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.pg-cell {
  width: calc((100% - 32px) / 5);
  height: 44px;
  border: 1.5px solid var(--line, #eee);
  border-radius: var(--r-md, 12px);
  background: #fafafa;
  box-sizing: border-box;
  color: var(--ink-2, #4a4a4a);
  font-size: 15px;
  cursor: pointer;
  transition: border-color 0.15s ease, background-color 0.15s ease, color 0.15s ease;
}

.pg-cell.on {
  border-color: var(--brand, #ff6b35);
  background: var(--brand-soft, #fff1ea);
  color: var(--brand-deep, #f0481f);
  font-weight: 700;
  box-shadow: 0 4px 16px rgb(240 72 31 / 16%);
}

/* 步进器（左）+ 确认按钮（右）：同一行，高度对齐 */
.ps-row {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 14px;
}

.pg-stepper {
  display: flex;
  flex-shrink: 0;
  align-items: center;
  gap: 8px;
  height: 46px;
  padding: 0 14px;
  border: 1.5px solid var(--line, #eee);
  border-radius: var(--r-pill, 999px);
  background: #fafafa;
  box-sizing: border-box;
}

.pg-step {
  width: 26px;
  height: 26px;
  border: none;
  border-radius: 50%;
  background: none;
  color: var(--brand-deep, #f0481f);
  font-size: 18px;
  line-height: 1;
  cursor: pointer;
}

.pg-step:disabled {
  color: var(--ink-4, #c7c7c7);
  cursor: not-allowed;
}

.pg-num {
  width: 40px;
  border: none;
  background: none;
  outline: none;
  color: var(--ink, #1a1a1a);
  font-size: 17px;
  font-weight: 700;
  text-align: center;
  -moz-appearance: textfield;
  appearance: textfield;
}

.pg-num::-webkit-outer-spin-button,
.pg-num::-webkit-inner-spin-button {
  -webkit-appearance: none;
  margin: 0;
}

.pg-confirm {
  flex: 1;
  height: 46px;
  border: none;
  border-radius: var(--r-pill, 999px);
  background: linear-gradient(135deg, var(--brand, #ff6b35), var(--brand-deep, #f0481f));
  box-shadow: 0 8px 18px rgb(240 72 31 / 28%);
  color: #fff;
  font-size: 15px;
  font-weight: 600;
  cursor: pointer;
}

.pg-confirm:disabled {
  background: #d9d9d9;
  box-shadow: none;
  cursor: not-allowed;
}
</style>

<!-- 飞球动画:由 JS 动态插入 body,不在组件树里,必须用全局样式 -->
<style>
/* 外层只做水平位移(线性) */
.fly-ball-x {
  position: fixed;
  z-index: 250; /* 高于结算栏(90)/搜索页(60),低于弹层抽屉(1500) */
  pointer-events: none;
  animation: fly-ball-x 0.4s linear forwards;
  will-change: transform;
}

/* 中层只做竖直位移(缓入),与外层线性水平位移叠加成抛物线 */
.fly-ball-y {
  animation: fly-ball-y 0.4s cubic-bezier(0.5, 0, 0.85, 0.42) forwards;
  will-change: transform;
}

/* 内层是「小球包裹」本体:品牌渐变球 + 高光,飞行途中略微缩小,
   落袋更有「收进去」的感觉(缩放独立分层,避免与位移的 transform 相互覆盖) */
.fly-ball-core {
  width: 14px;
  height: 14px;
  border-radius: 50%;
  background: radial-gradient(
    circle at 30% 28%,
    #ffb27d 0%,
    var(--brand, #ff6b35) 55%,
    var(--brand-deep, #f0481f) 100%
  );
  box-shadow: 0 2px 6px rgb(240 72 31 / 35%);
  animation: fly-ball-shrink 0.4s ease-in forwards;
}

@keyframes fly-ball-x {
  to {
    transform: translate3d(var(--fly-x, 0), 0, 0);
  }
}

@keyframes fly-ball-y {
  to {
    transform: translate3d(0, var(--fly-y, 0), 0);
  }
}

@keyframes fly-ball-shrink {
  0% {
    transform: scale(1.15);
  }

  70% {
    transform: scale(0.9);
  }

  100% {
    transform: scale(0.6);
    opacity: 0.6;
  }
}
</style>
