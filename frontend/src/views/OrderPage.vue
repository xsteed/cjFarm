<template>
  <div class="order-page">
    <!-- 加载中 -->
    <div v-if="view === 'loading'" class="state-box">
      <div class="spin"></div>
      <div class="state-text">正在进入点餐...</div>
    </div>

    <!-- 加载失败 -->
    <div v-else-if="view === 'error'" class="state-box">
      <div class="err-icon">!</div>
      <div class="state-text">{{ loadError }}</div>
      <button class="retry-btn" @click="load">重新加载</button>
    </div>

    <!-- 有 hero 的内容视图 -->
    <template v-else>
      <!-- 顶部大块 hero -->
      <div class="op-hero">
        <div class="op-shop">{{ shopName || '扫码点餐' }}</div>
        <div class="op-chip" v-if="table.tableNo">
          <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="8" r="5"/><path d="M4 21h16M12 13v8"/></svg>
          {{ table.tableName || (table.tableNo + '号桌') }}<template v-if="table.capacity"> · {{ table.capacity }}人桌</template>
        </div>
      </div>

      <!-- 菜单选择:左侧竖排分类栏 + 右侧菜品列表 -->
      <div v-if="view === 'menu'" class="op-body">
        <div class="op-side">
          <div
            v-for="c in menu"
            :key="c.categoryId"
            class="op-cat"
            :class="{ on: activeCat === c.categoryId }"
            @click="jumpTo(c.categoryId)"
          >{{ c.categoryName }}</div>
        </div>

        <div class="op-list" :class="{ lifted: showTabbar }" ref="dishListRef" @scroll="handleScroll">
          <template v-for="c in menu" :key="c.categoryId">
            <div class="op-group-t" :ref="(el) => setGroupRef(c.categoryId, el)">{{ c.categoryName }}</div>
            <div v-if="!c.dishes || !c.dishes.length" class="empty-group">暂无菜品</div>

            <div v-for="d in c.dishes" :key="d.dishId" class="op-dish">
              <div class="op-img">
                <img
                  v-if="d.dishImage && !imgErrs.has(d.dishId)"
                  :src="d.dishImage"
                  :alt="d.dishName"
                  loading="lazy"
                  @error="onImgErr(d.dishId)"
                >
                <div v-else class="img-fallback">
                  <svg viewBox="0 0 24 24" width="26" height="26" fill="#dcd4cc"><path d="M4 3h16a1 1 0 0 1 1 1v16a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1zm1 2v9l3.5-3.5L12 14l3-3 5 5V5H5zm10 1.5A1.5 1.5 0 1 1 13.5 8 1.5 1.5 0 0 1 15 6.5z"/></svg>
                  <span>暂无图片</span>
                </div>
              </div>

              <div class="op-info">
                <div class="op-name">
                  {{ d.dishName }}
                  <!-- 加菜时标注已下单份数,否则顾客只看到空加号,容易重复点 -->
                  <span v-if="orderedQty(d.dishId)" class="op-ordered">已点 {{ orderedQty(d.dishId) }}</span>
                </div>
                <div class="op-desc" v-if="d.description">{{ d.description }}</div>

                <div class="op-specs" v-if="d.specs && d.specs.length > 1">
                  <span
                    v-for="s in d.specs"
                    :key="s.specId"
                    class="op-spec"
                    :class="{ on: selectedSpec[d.dishId] === s.specId }"
                    @click="selectSpec(d, s)"
                  >{{ s.specName }}</span>
                </div>

                <div class="op-bottom">
                  <div class="op-price">
                    ¥{{ currentSpec(d).price }}
                    <small v-if="d.specs && d.specs.length === 1">/{{ d.specs[0].specName }}</small>
                  </div>
                  <div v-if="dishQty(d.dishId) > 0" class="op-stepper">
                    <span class="m" @click="changeQty(d, -1)"><svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><path d="M5 12h14"/></svg></span>
                    <span class="n">{{ dishQty(d.dishId) }}</span>
                    <span class="p" @click="changeQty(d, 1)"><svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><path d="M12 5v14M5 12h14"/></svg></span>
                  </div>
                  <span v-else class="op-add" @click="changeQty(d, 1)"><svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><path d="M12 5v14M5 12h14"/></svg></span>
                </div>
              </div>
            </div>
          </template>
        </div>
      </div>

      <!-- 订单状态 / 完成 / 取消 -->
      <div v-else-if="view === 'order' || view === 'done' || view === 'canceled'" class="track-body" :class="{ lifted: showTabbar }">
        <div class="st-steps">
          <div class="st-title">
            <template v-if="view === 'done'">
              <svg class="st-title-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M20 6L9 17l-5-5"/></svg>
              用餐愉快，订单已完成！
            </template>
            <template v-else-if="view === 'canceled'">
              <svg class="st-title-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 6L6 18M6 6l12 12"/></svg>
              订单已取消
            </template>
            <template v-else>
              {{ statusText }}
              <svg class="st-title-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/></svg>
            </template>
          </div>

          <!-- 四步进度条(仅进行中显示) -->
          <div v-if="view === 'order'" class="st-line">
            <div
              v-for="s in STEPS"
              :key="s.key"
              class="st-step"
              :class="{ done: currentStep > s.key || view === 'done', cur: currentStep === s.key && view !== 'done' }"
            >
              <div class="st-dot">
                <svg v-if="currentStep > s.key || view === 'done'" viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"><path d="M20 6L9 17l-5-5"/></svg>
                <span v-else>{{ s.key }}</span>
              </div>
              <div class="t">{{ s.label }}</div>
            </div>
          </div>
        </div>

        <div class="st-card">
          <div class="tt">
            菜品明细
            <span :title="'完整订单号 ' + currentOrder.orderNo">订单号 #{{ currentOrder.shortNo || currentOrder.orderNo }}</span>
          </div>
          <div v-for="it in currentOrder.items" :key="it.itemId" class="st-item">
            <span class="si-name">{{ it.dishName }}<em v-if="it.specName">（{{ it.specName }}）</em> ×{{ it.quantity }}</span>
            <span class="si-amount">¥{{ Number(it.amount).toFixed(2) }}</span>
          </div>
          <div v-for="(it, idx) in remarkItems" :key="'rk' + idx" class="st-item remark">
            <span class="si-remark">{{ it.dishName }}：{{ it.itemRemark }}</span>
          </div>
          <div class="st-item" v-if="Number(currentOrder.seatFee) > 0">
            <span class="si-name sub">餐位费（{{ currentOrder.personCount }}人）</span>
            <span class="si-amount">¥{{ Number(currentOrder.seatFee).toFixed(2) }}</span>
          </div>
          <div class="st-item discount" v-if="Number(currentOrder.discountAmount) > 0">
            <span class="si-name sub">满减优惠</span>
            <span class="si-amount">-¥{{ Number(currentOrder.discountAmount).toFixed(2) }}</span>
          </div>
          <div class="st-amt"><span>合计</span><span class="v">¥{{ Number(currentOrder.totalAmount).toFixed(2) }}</span></div>

          <!-- 整单备注:顾客提交的整体要求(如不要辣),必须回显否则顾客无法确认是否已传达 -->
          <div v-if="currentOrder.orderRemark" class="st-remark-line">
            <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M16 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"/><path d="M16 3v6h6"/><path d="M8 13h8M8 17h5"/></svg>
            <span>整单备注：{{ currentOrder.orderRemark }}</span>
          </div>

          <div v-if="view === 'canceled' && currentOrder.cancelReason" class="cancel-reason">取消原因：{{ currentOrder.cancelReason }}</div>

          <!-- 进行中的订单:可加菜、可催菜 -->
          <div class="st-actions" v-if="view === 'order' && isActive">
            <div class="st-append" v-if="canAppend" @click="startAppend">
              <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><path d="M12 5v14M5 12h14"/></svg>
              加菜
            </div>
            <div class="st-urge" :class="{ disabled: urgeCooldown > 0 || urging }" @click="urge">
              <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/></svg>
              {{ urgeCooldown > 0 ? urgeCooldown + 's 后可再催' : (urging ? '催菜中…' : '催菜') }}
            </div>
          </div>

          <div class="st-paybtn" v-if="view === 'order' && currentOrder.payStatus === 0" @click="view = 'pay'">
            去支付 ¥{{ Number(currentOrder.totalAmount).toFixed(2) }}
          </div>
          <div class="paid-badge" v-else-if="view === 'order' && currentOrder.payStatus === 1">已支付，等待商家确认用餐结束</div>
        </div>
      </div>

      <!-- 支付视图 -->
      <div v-else-if="view === 'pay'" class="pay-wrap">
        <div class="pay-label">应付金额</div>
        <div class="pay-amt">¥{{ Number(currentOrder.totalAmount).toFixed(2) }}</div>
        <div class="pay-tabs">
          <div class="pay-tab" :class="{ on: payTab === 'wx' }" @click="switchPayTab('wx')">微信支付</div>
          <div class="pay-tab" :class="{ on: payTab === 'ali' }" @click="switchPayTab('ali')">支付宝</div>
        </div>

        <!-- 在线支付(渠道已启用) -->
        <template v-if="onlineEnabled">
          <div v-if="!onlineQr" class="online-pay-btn" :class="{ disabled: paying }" @click="startOnlinePay">
            {{ paying ? '正在生成支付码…' : `在线支付 ¥${Number(currentOrder.totalAmount).toFixed(2)}` }}
          </div>
          <div v-else class="pay-block">
            <div class="pay-qr">
              <img :src="onlineQr" alt="支付二维码">
            </div>
            <div class="pay-tip">请用{{ payTab === 'wx' ? '微信' : '支付宝' }}扫码完成支付</div>
            <div class="pay-tip sub">支付成功后本页面自动更新，请勿关闭</div>
          </div>
          <div v-if="currentPayQr" class="pay-divider">—— 或使用线下收款码 ——</div>
        </template>

        <!-- 线下码牌收款(始终保留) -->
        <div v-if="currentPayQr" class="pay-block">
          <div class="pay-qr">
            <img :src="currentPayQr" alt="收款码">
          </div>
          <div class="pay-tip">长按识别二维码 或 用{{ payTab === 'wx' ? '微信' : '支付宝' }}扫码付款</div>
          <div class="pay-tip sub">支付完成后收银台确认即可，本页面会自动更新状态</div>
        </div>
        <div v-if="!onlineEnabled && !currentPayQr" class="qr-empty">商家未配置{{ payTab === 'wx' ? '微信' : '支付宝' }}收款码<br>请联系收银员付款</div>

        <button class="back-btn" @click="backFromPay">返回订单</button>
      </div>
    </template>

    <!-- 底部悬浮结算栏 -->
    <div class="op-bar" :class="{ lifted: showTabbar }" v-if="view === 'menu'">
      <div class="op-cartbtn" :class="{ has: totalCount > 0 }" @click="totalCount > 0 && (cartVisible = true)">
        <svg viewBox="0 0 24 24" width="22" height="22" fill="#fff"><path d="M7 18a2 2 0 1 0 0 4 2 2 0 0 0 0-4zm10 0a2 2 0 1 0 0 4 2 2 0 0 0 0-4zM3 2h2.6l.9 2H21a1 1 0 0 1 .94 1.34l-2.6 7.3A2 2 0 0 1 17.46 14H8.4l-.4 2H19v2H6.72a1 1 0 0 1-.98-1.2L6.6 12 4.4 4H3a1 1 0 0 1 0-2z"/></svg>
        <span class="b" v-if="totalCount > 0">{{ totalCount }}</span>
      </div>
      <div class="op-total">
        <div class="l">{{ canAppend ? '本次加菜' : '合计' }}</div>
        <div class="v"><small>¥</small>{{ totalPrice }}</div>
      </div>
      <div class="op-checkout" :class="{ disabled: !totalCount }" @click="totalCount > 0 && (cartVisible = true)">去结算</div>
    </div>

    <!-- 底部导航:点餐 / 订单。已有订单时常驻,加菜时顾客可随时切回核对已下单菜品 -->
    <div class="op-tabbar" v-if="showTabbar">
      <div class="tb-item" :class="{ on: view === 'menu' }" @click="switchView('menu')">
        <span class="tb-icon">
          <svg viewBox="0 0 24 24" width="21" height="21" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><path d="M4 6h16M4 12h16M4 18h10"/></svg>
        </span>
        <span class="tb-text">点餐</span>
      </div>
      <div class="tb-item" :class="{ on: view === 'order' }" @click="switchView('order')">
        <span class="tb-icon">
          <svg viewBox="0 0 24 24" width="21" height="21" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><path d="M9 5H7a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V7a2 2 0 0 0-2-2h-2"/><rect x="9" y="3" width="6" height="4" rx="1"/><path d="M9 13h6M9 17h4"/></svg>
          <span class="tb-badge" v-if="orderedCount > 0">{{ orderedCount }}</span>
        </span>
        <span class="tb-text">订单</span>
      </div>
    </div>

    <!-- 购物车抽屉 -->
    <t-drawer v-model:visible="cartVisible" placement="bottom" size="86%" :show-header="false" :footer="false">
      <div class="cart-sheet">
        <div class="sheet-head">
          <div class="sheet-title">已选菜品 <span v-if="totalCount">({{ totalCount }}件)</span></div>
          <button class="clear-btn" v-if="cart.length" @click="clearCart">清空</button>
        </div>

        <div class="cart-list">
          <div v-for="item in cart" :key="item.key" class="cart-item">
            <div class="ci-info">
              <div class="ci-name">{{ item.dishName }}<em v-if="item.specName">（{{ item.specName }}）</em></div>
              <div class="ci-remark" :class="{ add: !item.remark }" @click="openRemark(item)">
                <svg v-if="item.remark" viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M16 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"/><path d="M16 3v6h6"/><path d="M8 13h8M8 17h5"/></svg>
                <svg v-else viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><path d="M12 5v14M5 12h14"/></svg>
                <span>{{ item.remark || '添加备注' }}</span>
              </div>
            </div>
            <div class="ci-right">
              <div class="ci-price">¥{{ (item.price * item.quantity).toFixed(2) }}</div>
              <div class="qty-ctl">
                <button class="qty-btn minus" @click="changeCartQty(item, -1)"><svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><path d="M5 12h14"/></svg></button>
                <span class="qty-num">{{ item.quantity }}</span>
                <button class="qty-btn plus" @click="changeCartQty(item, 1)"><svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><path d="M12 5v14M5 12h14"/></svg></button>
              </div>
            </div>
          </div>
          <div v-if="!cart.length" class="cart-empty">购物车空空如也</div>
        </div>

        <template v-if="!canAppend">
          <div class="sheet-divider">用餐信息</div>
          <div class="dine-form">
            <div class="form-row">
              <span class="fr-label">用餐人数</span>
              <div class="qty-ctl">
                <button class="qty-btn minus" @click="personCount > 1 && personCount--"><svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><path d="M5 12h14"/></svg></button>
                <span class="qty-num big">{{ personCount }}</span>
                <button class="qty-btn plus" @click="personCount < 20 && personCount++"><svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><path d="M12 5v14M5 12h14"/></svg></button>
              </div>
            </div>
            <div class="form-row col">
              <span class="fr-label">整单备注</span>
              <input class="remark-input" v-model="orderRemark" placeholder="如：不要辣、尽快上菜" maxlength="100" />
            </div>
          </div>

          <div class="fee-lines">
            <div class="fee-line"><span>菜品金额</span><span>¥{{ dishTotal.toFixed(2) }}</span></div>
            <div class="fee-line" v-if="seatFee > 0"><span>餐位费（{{ personCount }}人）</span><span>¥{{ seatFee.toFixed(2) }}</span></div>
            <div class="fee-line off" v-if="discount > 0"><span>满减优惠</span><span>-¥{{ discount.toFixed(2) }}</span></div>
          </div>
        </template>
        <div v-else class="append-hint">本次加菜将累加到订单 #{{ currentOrder.orderNo }}，最终金额以订单为准</div>

        <div class="sheet-submit">
          <div class="ss-total">{{ canAppend ? '本次加菜' : '合计' }} <span class="money"><i>¥</i>{{ canAppend ? dishTotal.toFixed(2) : totalPrice }}</span></div>
          <button class="submit-btn" :disabled="submitting || !cart.length" @click="submit">
            <span v-if="submitting" class="spin small light"></span>
            {{ canAppend ? '确认加菜' : '确认下单' }}
          </button>
        </div>
      </div>
    </t-drawer>

    <!-- 菜品单项备注弹窗 -->
    <div v-if="remarkVisible" class="remark-mask" @click.self="saveRemark">
      <div class="remark-sheet">
        <div class="remark-head">
          <span>菜品备注</span>
          <span class="remark-target">{{ remarkTarget.item?.dishName }}</span>
        </div>
        <div class="remark-options">
          <span
            v-for="r in remarks"
            :key="r.remarkId"
            class="remark-chip"
            :class="{ active: remarkTarget.selected.includes(r.optionName) }"
            @click="toggleRemark(r.optionName)"
          >{{ r.optionName }}</span>
        </div>
        <input class="remark-custom" v-model="remarkTarget.custom" placeholder="其他备注（选填）" maxlength="50" />
        <button class="remark-confirm" @click="saveRemark">确定</button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onUnmounted } from 'vue'
import { useRoute } from 'vue-router'
import { MessagePlugin } from 'tdesign-vue-next'
import { getTable, getMenu, getRemarks, createOrder, appendOrder, getOrderByNo, getPublicConfig, createPay, queryPay, urgeOrder } from '../api'
import QRCode from 'qrcode'

const route = useRoute()
// 路由参数既可能是「桌台稳定码」(如 K7M3PQ9X),也可能是老的数字桌台 ID(如 1)。
// 先用它换取桌台详情,拿到数字 tableId 后再用于下单等接口。
const tableRef = String(route.params.tableId || '')
const tableId = ref(0)

const view = ref('loading') // loading | error | menu | order | done | canceled | pay
const loadError = ref('')
const table = ref({})
const menu = ref([])
const remarks = ref([])
const config = ref({})
const shopName = ref('扫码点餐')
const activeCat = ref(null)
const cart = ref([])
const cartVisible = ref(false)
const personCount = ref(1)
const orderRemark = ref('')
const submitting = ref(false)
const currentOrder = ref(null)
const payTab = ref('wx')
const onlineQr = ref('') // 在线支付二维码(dataURL)
const paying = ref(false)
const imgErrs = ref(new Set())

// 每道菜当前选中的规格(默认第一个)
const selectedSpec = reactive({})
const groupRefs = {}
const dishListRef = ref(null)

const ORDER_STATUS_TEXT = {
  1: '已下单，后厨正在接单',
  2: '正在制作中，请稍候',
  3: '餐品已上齐，请慢用',
  4: '订单已完成',
  5: '订单已取消'
}
const STEPS = [
  { key: 1, label: '已下单' },
  { key: 2, label: '制作中' },
  { key: 3, label: '已上齐' },
  { key: 4, label: '已完成' }
]

const seatFeeEnabled = computed(() => config.value.seat_fee_enabled === '1' && Number(config.value.seat_fee) > 0)
const seatFeePer = computed(() => Number(config.value.seat_fee) || 0)

const totalCount = computed(() => cart.value.reduce((s, i) => s + i.quantity, 0))
const dishTotal = computed(() => round2(cart.value.reduce((s, i) => s + i.price * i.quantity, 0)))
const seatFee = computed(() => (seatFeeEnabled.value ? round2(personCount.value * seatFeePer.value) : 0))
const discount = computed(() => {
  const enabled = config.value.promotion_enabled === '1'
  const t = Number(config.value.promotion_threshold)
  const d = Number(config.value.promotion_discount)
  if (!enabled || !(t > 0) || !(d > 0) || dishTotal.value < t) return 0
  return round2(Math.min(Math.floor(dishTotal.value / t) * d, dishTotal.value))
})
// 未选菜时不展示餐位费(合计为 0),选菜后才计入餐位费与优惠
const totalPrice = computed(() => (dishTotal.value > 0 ? round2(dishTotal.value + seatFee.value - discount.value) : 0))

const statusText = computed(() => ORDER_STATUS_TEXT[currentOrder.value?.orderStatus] || '处理中')
const currentStep = computed(() => {
  const s = Number(currentOrder.value?.orderStatus) || 1
  return Math.max(1, Math.min(4, s))
})
const currentPayQr = computed(() => (payTab.value === 'wx' ? config.value.pay_qr_wx : config.value.pay_qr_ali))
// 该渠道是否已启用在线支付(未申请 key 前为 false,前端自动降级为码牌收款)
const onlineEnabled = computed(() => (payTab.value === 'wx' ? config.value.wxpay_enabled === '1' : config.value.alipay_enabled === '1'))
const remarkItems = computed(() => (currentOrder.value?.items || []).filter((i) => i.itemRemark))

// 是否已存在当前订单。底部导航与结算栏的定位都依赖它。
const hasOrder = computed(() => !!currentOrder.value)
// 底部 tabbar 显示条件:已有订单且停留在点餐/订单视图。
// 首次扫码尚未下单时不显示(只有一个 tab 没有意义);支付页需专注,也不显示。
const showTabbar = computed(() => hasOrder.value && (view.value === 'menu' || view.value === 'order'))
// 已下单份数(按 dishId 聚合,同菜多规格累加)。tabbar 角标与菜单「已点」标记共用同一份数据。
const orderedMap = computed(() => {
  const m = {}
  for (const it of currentOrder.value?.items || []) {
    m[it.dishId] = (m[it.dishId] || 0) + Number(it.quantity || 0)
  }
  return m
})
const orderedCount = computed(() => Object.values(orderedMap.value).reduce((s, n) => s + n, 0))
const orderedQty = (dishId) => orderedMap.value[dishId] || 0

function round2(v) {
  return Math.round(v * 100) / 100
}

function currentSpec(d) {
  const specs = d.specs || []
  return specs.find((s) => s.specId === selectedSpec[d.dishId]) || specs[0] || { specId: 0, specName: '', price: 0 }
}

function dishQty(dishId) {
  const f = cart.value.find((i) => i.dishId === dishId)
  return f ? f.quantity : 0
}

function onImgErr(id) {
  const s = new Set(imgErrs.value)
  s.add(id)
  imgErrs.value = s
}

function selectSpec(d, s) {
  const prev = selectedSpec[d.dishId]
  selectedSpec[d.dishId] = s.specId
  if (prev === s.specId) return
  // 若购物车已有该菜,同步更新规格与价格
  const f = cart.value.find((i) => i.dishId === d.dishId)
  if (f) {
    f.key = `${d.dishId}-${s.specId || 0}`
    f.specId = s.specId
    f.specName = s.specName
    f.price = Number(s.price)
  }
}

function changeQty(d, delta) {
  const spec = currentSpec(d)
  const key = `${d.dishId}-${spec.specId || 0}`
  const f = cart.value.find((i) => i.key === key)
  if (f) {
    f.quantity += delta
    if (f.quantity <= 0) cart.value = cart.value.filter((i) => i.key !== key)
  } else if (delta > 0) {
    cart.value.push({
      key,
      dishId: d.dishId,
      dishName: d.dishName,
      specId: spec.specId,
      specName: spec.specName,
      price: Number(spec.price) || 0,
      quantity: 1,
      remark: ''
    })
  }
}

function changeCartQty(item, delta) {
  item.quantity += delta
  if (item.quantity <= 0) cart.value = cart.value.filter((i) => i.key !== item.key)
}

function clearCart() {
  cart.value = []
}

// ---- 单项备注 ----
const remarkVisible = ref(false)
const remarkTarget = reactive({ item: null, selected: [], custom: '' })

function openRemark(item) {
  remarkTarget.item = item
  const parts = (item.remark || '').split('、').filter(Boolean)
  remarkTarget.selected = parts.filter((p) => remarks.value.some((r) => r.optionName === p))
  remarkTarget.custom = parts.filter((p) => !remarks.value.some((r) => r.optionName === p)).join('、')
  remarkVisible.value = true
}

function toggleRemark(name) {
  const i = remarkTarget.selected.indexOf(name)
  i > -1 ? remarkTarget.selected.splice(i, 1) : remarkTarget.selected.push(name)
}

function saveRemark() {
  const parts = [...remarkTarget.selected]
  if (remarkTarget.custom.trim()) parts.push(remarkTarget.custom.trim())
  if (remarkTarget.item) remarkTarget.item.remark = parts.join('、')
  remarkVisible.value = false
}

// 订单是否仍在进行中(1已下单/2制作中/3已上齐)。
// 加菜还需未支付;催菜只要求进行中——已付款但迟迟未上齐的订单同样可以催。
const isActive = computed(() => {
  const o = currentOrder.value
  return !!o && [1, 2, 3].includes(Number(o.orderStatus))
})

// ---- 下单 / 加菜 ----
// 存在进行中且未支付的订单时走加菜(追加到同一订单),否则首次下单。
const canAppend = computed(() => isActive.value && Number(currentOrder.value?.payStatus) === 0)

function startAppend() {
  // 加菜期间保持轮询:后厨状态与已上齐菜品会持续刷新,菜单里的「已点」标记才不会过期
  cart.value = []
  orderRemark.value = ''
  view.value = 'menu'
}

// 底部 tabbar 切页。加菜时随时切回订单页核对已下单菜品,购物车内容保留不丢。
function switchView(v) {
  if (view.value === v || !currentOrder.value) return
  if (v === 'order') {
    view.value = 'order'
    startPolling()
  } else {
    view.value = 'menu'
  }
}

async function submit() {
  if (!cart.value.length) {
    MessagePlugin.warning('请先选择菜品')
    return
  }
  submitting.value = true
  try {
    const items = cart.value.map((i) => ({ dishId: i.dishId, specId: i.specId, quantity: i.quantity, itemRemark: i.remark }))
    if (canAppend.value) {
      const res = await appendOrder({ orderNo: currentOrder.value.orderNo, items })
      MessagePlugin.success('加菜成功，已通知后厨')
      currentOrder.value = await getOrderByNo(res.orderNo)
    } else {
      const res = await createOrder({
        tableId: tableId.value,
        personCount: personCount.value,
        orderRemark: orderRemark.value,
        items
      })
      MessagePlugin.success('下单成功，已通知后厨')
      currentOrder.value = await getOrderByNo(res.orderNo)
    }
    cart.value = []
    cartVisible.value = false
    view.value = 'order'
    startPolling()
  } finally {
    submitting.value = false
  }
}

let pollTimer = null
let pollCount = 0

function startPolling() {
  stopPolling()
  pollTimer = setInterval(async () => {
    if (!currentOrder.value) return
    try {
      // 在线支付每 5 轮(约 15s)主动向渠道查一次单:
      // 异步通知丢失或延迟时,由前端触发补单,避免顾客付款后页面一直停在待支付。
      pollCount++
      if (view.value === 'pay' && pollCount % 5 === 0) {
        const r = await queryPay(currentOrder.value.orderNo)
        if (r?.payStatus === 1) {
          const o = await getOrderByNo(currentOrder.value.orderNo)
          currentOrder.value = o
          onlineQr.value = ''
          view.value = 'order'
          return
        }
      }
      const o = await getOrderByNo(currentOrder.value.orderNo)
      currentOrder.value = o
      // 在线支付成功:支付视图自动回到订单状态页(显示已支付)
      if (view.value === 'pay' && o.payStatus === 1) {
        onlineQr.value = ''
        view.value = 'order'
      }
      if (o.orderStatus === 5) {
        view.value = 'canceled'
        clearUrgeTimer()
        stopPolling()
      } else if (o.orderStatus === 4) {
        view.value = 'done'
        clearUrgeTimer()
        stopPolling()
      }
    } catch (e) {
      /* 轮询失败忽略,下轮重试 */
    }
  }, 3000)
}

function stopPolling() {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

// ---- 在线支付 ----
function switchPayTab(tab) {
  payTab.value = tab
  onlineQr.value = '' // 切换渠道时重置在线支付二维码
}

async function startOnlinePay() {
  if (!currentOrder.value || paying.value) return
  paying.value = true
  try {
    const channel = payTab.value === 'wx' ? 'wxpay' : 'alipay'
    const res = await createPay({ orderNo: currentOrder.value.orderNo, channel })
    // 用 qrcode 库把支付串渲染为二维码图片(360px/M级容错)
    onlineQr.value = await QRCode.toDataURL(res.codeUrl, { width: 360, margin: 2, errorCorrectionLevel: 'M' })
    startPolling() // 轮询支付结果
  } catch (e) {
    /* 错误已由拦截器提示 */
  } finally {
    paying.value = false
  }
}

function backFromPay() {
  onlineQr.value = ''
  view.value = 'order'
}

// ---- 催菜 ----
// 顾客要求后厨加急。后端对同一订单有冷却限制(默认 180s),
// 前端同步展示倒计时并禁用按钮,避免连点后反复弹提示。
const urging = ref(false)
const urgeCooldown = ref(0) // 剩余冷却秒数
let urgeTimer = null

async function urge() {
  if (!currentOrder.value || urging.value || urgeCooldown.value > 0) return
  urging.value = true
  try {
    const res = await urgeOrder(currentOrder.value.orderNo)
    MessagePlugin.success('已通知后厨加急，请稍候')
    startUrgeCooldown(Number(res?.cooldown) || 180)
  } catch (e) {
    // 处于冷却期时后端返回剩余秒数,解析出来同步倒计时
    const m = /(\d+)\s*秒/.exec(e?.message || '')
    if (m) startUrgeCooldown(Number(m[1]))
  } finally {
    urging.value = false
  }
}

function startUrgeCooldown(sec) {
  clearUrgeTimer()
  urgeCooldown.value = Math.max(0, Math.floor(sec) || 0)
  if (urgeCooldown.value <= 0) return
  urgeTimer = setInterval(() => {
    urgeCooldown.value -= 1
    if (urgeCooldown.value <= 0) clearUrgeTimer()
  }, 1000)
}

function clearUrgeTimer() {
  if (urgeTimer) {
    clearInterval(urgeTimer)
    urgeTimer = null
  }
}

// ---- 分类滚动定位 ----
function setGroupRef(categoryId, el) {
  if (el) groupRefs[categoryId] = el
}

// 分组标题相对"滚动内容顶部"的偏移。用 rect 差值算而不是 offsetTop,
// 这样布局怎么改(如分类栏从横排改竖排)都不会算错。
function groupOffsetInList(el, listEl) {
  return el.getBoundingClientRect().top - listEl.getBoundingClientRect().top + listEl.scrollTop
}

function jumpTo(categoryId) {
  activeCat.value = categoryId
  const el = groupRefs[categoryId]
  const listEl = dishListRef.value
  if (el && listEl) {
    listEl.scrollTo({ top: groupOffsetInList(el, listEl), behavior: 'smooth' })
  }
}

function handleScroll() {
  const listEl = dishListRef.value
  if (!listEl) return
  // 已滚到底:末尾分类内容不足一屏时,它的标题永远无法顶到列表顶部,
  // 若不特判,点最后一个分类会一直高亮在上一类。
  if (listEl.scrollTop + listEl.clientHeight >= listEl.scrollHeight - 2) {
    activeCat.value = menu.value[menu.value.length - 1]?.categoryId
    return
  }
  let cur = menu.value[0]?.categoryId
  for (const c of menu.value) {
    const el = groupRefs[c.categoryId]
    if (!el) continue
    // 分组标题已滚到列表顶部 20px 以内,即视为当前分类
    if (groupOffsetInList(el, listEl) <= listEl.scrollTop + 20) cur = c.categoryId
  }
  activeCat.value = cur
}

async function load() {
  view.value = 'loading'
  loadError.value = ''
  try {
    const [cfg, tb, m, r] = await Promise.all([getPublicConfig(), getTable(tableRef), getMenu(), getRemarks()])
    config.value = cfg
    shopName.value = cfg.shop_name || '扫码点餐'
    table.value = tb
    tableId.value = tb.tableId
    menu.value = m
    remarks.value = r
    // 默认选中每道菜第一个规格
    m.forEach((c) => c.dishes.forEach((d) => {
      if (d.specs && d.specs.length) selectedSpec[d.dishId] = d.specs[0].specId
    }))
    if (m.length) activeCat.value = m[0].categoryId
    // 桌台已有进行中订单 → 直接进入订单状态
    if (tb && tb.currentOrder) {
      currentOrder.value = tb.currentOrder
      view.value = 'order'
      startPolling()
    } else {
      view.value = 'menu'
    }
  } catch (e) {
    view.value = 'error'
    loadError.value = friendlyLoadError(e)
  }
}

// 把加载失败转成顾客能看懂、且知道下一步该做什么的中文提示。
// 拦截器已把 HTTP 错误统一成中文(msg 优先),这里再做一层场景化改写。
function friendlyLoadError(e) {
  const msg = e?.friendlyMessage || e?.message || ''
  if (/桌台|桌号/.test(msg)) return '桌号不存在，请重新扫描餐桌上的二维码'
  if (/Network Error|网络连接失败/i.test(msg)) return '网络连接失败，请检查手机网络后重试'
  if (/超时/.test(msg)) return '网络响应较慢，请稍后重试'
  return msg || '加载失败，请检查网络后重试'
}

onMounted(load)
onUnmounted(() => {
  stopPolling()
  clearUrgeTimer()
})
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

/* ---------- 顶部大块 hero ---------- */
.op-hero {
  background: linear-gradient(135deg, #FF8A50 0%, #FF6B35 55%, #F0481F 100%);
  /* 顶部预留刘海/灵动岛安全区(iPhone X 及以后),避免店名被状态栏裁切 */
  padding: calc(38px + env(safe-area-inset-top, 0px)) 18px 30px;
  position: relative;
  overflow: hidden;
  flex-shrink: 0;
}
.op-hero::after {
  content: '';
  position: absolute;
  right: -28px;
  top: -34px;
  width: 120px;
  height: 120px;
  border-radius: 50%;
  background: rgba(255, 255, 255, 0.12);
}
.op-shop {
  position: relative;
  font-size: 17px;
  font-weight: 800;
  color: #fff;
  letter-spacing: 0.3px;
}
.op-chip {
  position: relative;
  display: inline-flex;
  align-items: center;
  gap: 5px;
  margin-top: 10px;
  padding: 4px 11px;
  border-radius: var(--r-pill);
  background: rgba(255, 255, 255, 0.22);
  color: #fff;
  font-size: 11.5px;
  font-weight: 500;
}

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
  background: #F7F7F7;
  overflow-y: auto;
  /* 底部留白:结算栏与 tabbar 会盖住这一段 */
  padding: 6px 0 120px;
  scrollbar-width: none;
}
.op-side::-webkit-scrollbar { display: none; }
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
.op-list.lifted { padding-bottom: 142px; }
.op-group-t {
  font-size: 11px;
  font-weight: 700;
  color: var(--ink-4);
  padding: 10px 2px 6px;
}
.empty-group { padding: 30px 0; text-align: center; color: var(--ink-4); font-size: 13px; }

.op-dish {
  display: flex;
  gap: 11px;
  padding: 11px 0;
  border-bottom: 1px solid #F5F5F5;
}
.op-dish:last-child { border-bottom: none; }
.op-img {
  width: 84px;
  height: 84px;
  border-radius: var(--r-md);
  flex-shrink: 0;
  overflow: hidden;
  background: #F5F5F5;
}
.op-img img { width: 100%; height: 100%; object-fit: cover; display: block; }
.img-fallback {
  width: 100%; height: 100%; display: flex; flex-direction: column;
  align-items: center; justify-content: center; gap: 4px; background: #F7F5F2;
}
.img-fallback span { font-size: 10px; color: var(--ink-4); }
.op-info { flex: 1; min-width: 0; display: flex; flex-direction: column; }
.op-name { font-size: 14px; font-weight: 600; color: var(--ink); }
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
  font-size: 10.5px; color: var(--ink-4); margin-top: 2px;
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.op-specs { display: flex; gap: 5px; margin-top: 5px; flex-wrap: wrap; }
.op-spec {
  font-size: 10.5px; padding: 2px 8px; border-radius: var(--r-pill);
  border: 1px solid var(--line); color: var(--ink-3); background: #FAFAFA; cursor: pointer; transition: all 0.12s;
}
.op-spec.on {
  border-color: var(--brand); background: var(--brand-soft); color: var(--brand-deep); font-weight: 600;
}
.op-bottom {
  margin-top: auto; display: flex; justify-content: space-between;
  align-items: center; padding-top: 6px;
}
.op-price { font-size: 16px; font-weight: 800; color: var(--brand-deep); }
.op-price small { font-size: 10px; font-weight: 400; color: var(--ink-4); margin-left: 3px; }
.op-add {
  width: 26px; height: 26px; border-radius: 50%;
  background: linear-gradient(135deg, var(--brand), var(--brand-deep));
  color: #fff; display: flex; align-items: center; justify-content: center;
  box-shadow: 0 3px 8px rgba(240, 72, 31, 0.3); cursor: pointer; transition: transform 0.1s;
}
.op-add:active { transform: scale(0.88); }
.op-stepper { display: flex; align-items: center; gap: 8px; }
.op-stepper .m, .op-stepper .p {
  width: 22px; height: 22px; border-radius: 50%;
  display: flex; align-items: center; justify-content: center; cursor: pointer;
}
.op-stepper .m { border: 1.5px solid #FFD5C4; color: var(--brand); background: #fff; }
.op-stepper .p { background: linear-gradient(135deg, var(--brand), var(--brand-deep)); color: #fff; }
.op-stepper .n { font-size: 12.5px; font-weight: 700; min-width: 14px; text-align: center; }

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
/* 底部有 tabbar 时结算栏整体上移,避免压住导航 */
.op-bar.lifted { bottom: calc(70px + env(safe-area-inset-bottom)); }
.op-cartbtn {
  position: relative; width: 42px; height: 42px; border-radius: 50%;
  background: #E5E5E5; display: flex; align-items: center; justify-content: center;
  color: #fff; flex-shrink: 0; cursor: pointer; transition: background 0.2s;
}
.op-cartbtn.has { background: linear-gradient(135deg, var(--brand), var(--brand-deep)); }
.op-cartbtn .b {
  position: absolute; top: -3px; right: -3px; min-width: 17px; height: 17px;
  padding: 0 4px; border-radius: var(--r-pill); background: var(--ink);
  border: 2px solid #fff; color: #fff; font-size: 9px; line-height: 13px;
  font-weight: 700; text-align: center;
}
.op-total { flex: 1; }
.op-total .l { font-size: 9.5px; color: var(--ink-4); }
.op-total .v { font-size: 18px; font-weight: 800; color: var(--ink); line-height: 1.1; }
.op-total .v small { font-size: 11px; }
.op-checkout {
  height: 42px; padding: 0 22px; border-radius: var(--r-pill);
  background: linear-gradient(135deg, var(--brand), var(--brand-deep));
  color: #fff; font-size: 13px; font-weight: 600; flex-shrink: 0;
  display: flex; align-items: center; cursor: pointer; transition: opacity 0.2s;
}
.op-checkout.disabled { background: #D8D8D8; cursor: not-allowed; }

/* ---------- 底部导航(点餐 / 订单) ---------- */
.op-tabbar {
  position: fixed;
  left: 50%;
  bottom: 0;
  transform: translateX(-50%);
  width: 100%;
  max-width: 480px;
  min-height: 58px;
  padding-bottom: env(safe-area-inset-bottom);
  background: #fff;
  border-top: 1px solid var(--line);
  box-shadow: 0 -2px 12px rgba(0, 0, 0, 0.05);
  display: flex;
  z-index: 88;
}
.tb-item {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 3px;
  padding: 8px 0;
  color: var(--ink-4);
  font-size: 10.5px;
  cursor: pointer;
  transition: color 0.15s;
}
.tb-item.on { color: var(--brand-deep); font-weight: 700; }
.tb-icon { position: relative; display: flex; align-items: center; justify-content: center; height: 21px; }
.tb-badge {
  position: absolute;
  top: -5px;
  right: -10px;
  min-width: 16px;
  height: 16px;
  padding: 0 4px;
  border-radius: var(--r-pill);
  background: var(--brand);
  border: 1.5px solid #fff;
  color: #fff;
  font-size: 9px;
  font-weight: 700;
  line-height: 13px;
  text-align: center;
}

/* ---------- 订单状态 / 完成 ---------- */
/* 订单状态 / 完成:同样上移压住 hero 底部,需要定位层否则卡片上沿被橙色横幅裁掉 */
.track-body { flex: 1; padding: 8px 14px 24px; overflow-y: auto; position: relative; z-index: 1; }
/* 底部 tabbar 会遮住卡片底部,加菜/催菜按钮需要额外让位 */
.track-body.lifted { padding-bottom: 86px; }
.st-steps {
  background: #fff; border-radius: var(--r-lg); padding: 18px 16px;
  /* 原为 -22px(想压住 hero 底边),但 track-body 是滚动容器,超出上边界的部分会被裁掉,
     结果只露出被切平的直角边。改成 -8px 正好抵掉容器 8px 上内边距,卡片贴住横幅且圆角完整 */
  margin-top: -8px; box-shadow: var(--shadow-1);
}
.st-title {
  font-size: 15px; font-weight: 700; text-align: center; margin-bottom: 14px;
  display: flex; align-items: center; justify-content: center; gap: 5px;
}
.st-title-svg { color: var(--brand); width: 16px; height: 16px; }
.st-line {
  display: flex; align-items: flex-start; justify-content: space-between;
  position: relative;
}
.st-line::before {
  content: ''; position: absolute; top: 11px; left: 14px; right: 14px;
  height: 2px; background: var(--line);
}
.st-step {
  position: relative; z-index: 1; display: flex; flex-direction: column;
  align-items: center; gap: 5px; width: 56px;
}
.st-dot {
  width: 22px; height: 22px; border-radius: 50%; background: #fff;
  border: 2px solid var(--line); display: flex; align-items: center; justify-content: center;
  font-size: 10px; color: var(--ink-4);
}
.st-step.done .st-dot { background: var(--success); border-color: var(--success); color: #fff; }
.st-step.cur .st-dot {
  background: var(--brand); border-color: var(--brand); color: #fff;
  box-shadow: 0 0 0 4px var(--brand-soft);
}
.st-step .t { font-size: 10px; color: var(--ink-3); text-align: center; }
.st-step.done .t, .st-step.cur .t { color: var(--ink); font-weight: 600; }

.st-card {
  background: #fff; border-radius: var(--r-lg); padding: 16px;
  margin-top: 12px; box-shadow: var(--shadow-1);
}
.st-card .tt { font-size: 13px; font-weight: 700; margin-bottom: 8px; }
.st-card .tt span { font-weight: 400; color: var(--ink-4); font-size: 11px; }
.st-item {
  display: flex; justify-content: space-between; align-items: flex-start;
  gap: 10px; padding: 7px 0; border-bottom: 1px solid #F5F5F5; font-size: 12px;
}
.st-item:last-of-type { border-bottom: none; }
.si-name { color: var(--ink); }
.si-name em { font-style: normal; font-size: 11px; color: var(--ink-3); }
.si-name.sub { color: var(--ink-3); }
.si-amount { flex-shrink: 0; color: var(--ink); }
.st-item.discount .si-amount { color: var(--brand-deep); }
.st-item.remark .si-remark { font-size: 11px; color: var(--ink-3); }
.st-amt {
  display: flex; justify-content: space-between; padding-top: 10px;
  font-size: 12.5px; font-weight: 700;
}
.st-amt .v { color: var(--brand-deep); font-size: 16px; }
.st-paybtn {
  width: 100%; height: 44px; border-radius: var(--r-pill);
  background: linear-gradient(135deg, var(--brand), var(--brand-deep));
  color: #fff; font-size: 14px; font-weight: 700;
  display: flex; align-items: center; justify-content: center;
  margin-top: 12px; box-shadow: var(--shadow-3); cursor: pointer;
}
.paid-badge {
  display: flex; justify-content: center; margin-top: 12px;
  padding: 10px 22px; border-radius: var(--r-pill);
  background: var(--success-soft); color: var(--success); font-size: 13.5px; font-weight: 600;
}
/* 整单备注回显:顾客提交的整体要求(不要辣等),下单后必须能看到 */
.st-remark-line {
  display: flex; align-items: flex-start; gap: 6px;
  margin-top: 10px; padding: 9px 12px; border-radius: var(--r-md);
  background: var(--brand-ghost); color: var(--ink-2);
  font-size: 12px; line-height: 1.6;
}
.st-remark-line svg { color: var(--brand); flex-shrink: 0; margin-top: 1px; }
/* 订单卡片操作区:加菜 / 催菜并排 */
.st-actions { display: flex; gap: 10px; margin-top: 12px; }
.st-append, .st-urge {
  flex: 1; height: 42px; border-radius: var(--r-pill);
  font-size: 14px; font-weight: 600;
  display: flex; align-items: center; justify-content: center; gap: 5px;
  cursor: pointer;
}
.st-append { border: 1.5px solid #FFD5C4; background: #fff; color: var(--brand-deep); }
.st-append:active { background: var(--brand-soft); }
.st-urge { border: 1.5px solid #FFD5C4; background: var(--brand-soft); color: var(--brand-deep); }
.st-urge:active { background: #FFE3D4; }
.st-urge.disabled {
  border-color: var(--line); background: #F5F5F5; color: var(--ink-4); cursor: not-allowed;
}
.st-urge.disabled:active { background: #F5F5F5; }
.append-hint {
  margin-top: 14px; padding: 10px 14px; border-radius: var(--r-md);
  background: var(--brand-soft); color: var(--brand-deep); font-size: 12px; line-height: 1.6;
}
.cancel-reason {
  margin-top: 10px; padding: 8px 12px; border-radius: var(--r-md);
  background: #FFF3F2; color: var(--danger); font-size: 12px;
}

/* ---------- 支付视图 ---------- */
.pay-wrap {
  flex: 1; display: flex; flex-direction: column; align-items: center;
  padding: 26px 16px; overflow-y: auto;
}
.pay-label { font-size: 13px; color: var(--ink-3); }
.pay-amt { font-size: 34px; font-weight: 800; color: var(--ink); margin-top: 8px; }
.pay-tabs { display: flex; gap: 8px; margin: 16px 0; }
.pay-tab {
  padding: 7px 22px; border-radius: var(--r-pill); font-size: 12.5px;
  background: #F5F5F5; color: var(--ink-3); font-weight: 500; cursor: pointer; transition: all 0.15s;
}
.pay-tab.on {
  background: linear-gradient(135deg, var(--brand), var(--brand-deep)); color: #fff; font-weight: 600;
}
/* 收款码区块:二维码与说明文字纵向排列。
   二维码容器必须保持固定方形、且只放图片;提示文字放在容器外层。
   否则 .pay-qr 的 flex 会把文字当兄弟节点横向挤压,导致竖排溢出裁切。 */
.pay-block {
  width: 100%; display: flex; flex-direction: column; align-items: center;
}
.pay-qr {
  width: 200px; height: 200px; flex-shrink: 0; border-radius: var(--r-lg); border: 1px solid var(--line);
  background: #fff; display: flex; align-items: center; justify-content: center;
  box-shadow: var(--shadow-1); overflow: hidden;
}
.pay-qr img { width: 100%; height: 100%; object-fit: contain; display: block; }
.qr-empty { text-align: center; color: var(--ink-3); font-size: 12px; line-height: 1.8; padding: 20px; }
.pay-tip { font-size: 12px; color: var(--ink-3); margin-top: 14px; text-align: center; line-height: 1.6; }
.pay-tip.sub { font-size: 11px; color: var(--ink-4); margin-top: 4px; }
.online-pay-btn {
  width: 100%; height: 44px; border-radius: var(--r-pill);
  background: linear-gradient(135deg, var(--brand), var(--brand-deep));
  color: #fff; font-size: 14px; font-weight: 700;
  display: flex; align-items: center; justify-content: center;
  margin-top: 6px; box-shadow: var(--shadow-3); cursor: pointer;
}
.online-pay-btn.disabled { opacity: 0.6; cursor: default; }
.pay-divider { margin-top: 18px; font-size: 11px; color: var(--ink-4); }
.back-btn {
  margin-top: 20px; padding: 10px 36px; border-radius: var(--r-pill);
  border: 1.5px solid #FFD5C4; background: #fff; color: var(--brand-deep);
  font-size: 14px; font-weight: 600; cursor: pointer;
}

/* ---------- 加载 / 错误 ---------- */
.state-box {
  flex: 1; min-height: 55vh; display: flex; flex-direction: column;
  align-items: center; justify-content: center; gap: 14px;
}
.state-text { font-size: 14px; color: var(--ink-3); }
.spin {
  width: 34px; height: 34px; border: 3px solid #FFE3D4; border-top-color: var(--brand);
  border-radius: 50%; animation: spin 0.8s linear infinite;
}
.spin.small { width: 14px; height: 14px; border-width: 2px; display: inline-block; vertical-align: -2px; margin-right: 6px; }
.spin.light { border-color: rgba(255, 255, 255, 0.4); border-top-color: #fff; }
@keyframes spin { to { transform: rotate(360deg); } }
.err-icon {
  width: 52px; height: 52px; border-radius: 50%; background: var(--danger-soft);
  color: var(--danger); font-size: 30px; font-weight: 700;
  display: flex; align-items: center; justify-content: center;
}
.retry-btn {
  margin-top: 4px; padding: 9px 28px; border: none; border-radius: var(--r-pill);
  background: linear-gradient(135deg, var(--brand), var(--brand-deep)); color: #fff; font-size: 14px; cursor: pointer;
}

/* ---------- 购物车抽屉 ---------- */
.cart-sheet { padding: 20px 18px calc(16px + env(safe-area-inset-bottom)); display: flex; flex-direction: column; }
.sheet-head { display: flex; justify-content: space-between; align-items: center; }
.sheet-title { font-size: 16px; font-weight: 700; color: var(--ink); }
.sheet-title span { font-size: 12px; font-weight: 400; color: var(--ink-3); margin-left: 2px; }
.clear-btn { border: none; background: none; color: var(--ink-3); font-size: 12.5px; cursor: pointer; padding: 4px; }
.cart-list { max-height: 240px; overflow-y: auto; margin-top: 8px; }
.cart-item { display: flex; justify-content: space-between; align-items: center; padding: 11px 0; border-bottom: 1px solid #F7F5F2; }
.ci-info { min-width: 0; }
.ci-name { font-size: 14.5px; font-weight: 500; color: var(--ink); }
.ci-name em { font-style: normal; font-size: 12px; color: var(--ink-3); }
.ci-remark { display: flex; align-items: center; gap: 4px; font-size: 12px; color: var(--ink-3); margin-top: 4px; cursor: pointer; }
.ci-remark svg { flex-shrink: 0; }
.ci-remark.add { color: var(--brand); }
.ci-right { display: flex; align-items: center; gap: 12px; flex-shrink: 0; }
.ci-price { font-size: 14px; font-weight: 700; color: var(--brand-deep); }
.qty-ctl { display: flex; align-items: center; gap: 10px; }
.qty-btn {
  width: 24px; height: 24px; border-radius: 50%; border: none; cursor: pointer;
  display: flex; align-items: center; justify-content: center; font-size: 16px; line-height: 1;
}
.qty-btn.minus { background: #fff; color: var(--brand); box-shadow: inset 0 0 0 1.5px #FFD5C4; }
.qty-btn.plus { background: linear-gradient(135deg, var(--brand), var(--brand-deep)); color: #fff; }
.qty-num { min-width: 18px; text-align: center; font-size: 13px; font-weight: 600; color: var(--ink); }
.qty-num.big { font-size: 16px; }
.cart-empty { padding: 40px 0; text-align: center; color: var(--ink-4); font-size: 13px; }

.sheet-divider {
  display: flex; align-items: center; gap: 12px; margin: 18px 0 14px;
  color: var(--ink-3); font-size: 12px; white-space: nowrap;
}
.sheet-divider::before, .sheet-divider::after { content: ''; flex: 1; height: 1px; background: var(--line); }
.dine-form { display: flex; flex-direction: column; gap: 14px; }
.form-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.form-row.col { flex-direction: column; align-items: stretch; gap: 8px; }
.fr-label { font-size: 13.5px; color: var(--ink-2); flex-shrink: 0; }
.remark-input {
  width: 100%; box-sizing: border-box; padding: 10px 14px; border: 1.5px solid var(--line);
  border-radius: var(--r-md); font-size: 13.5px; color: var(--ink); outline: none; background: #FAFAFA;
  transition: border-color 0.15s;
}
.remark-input:focus { border-color: var(--brand); }

.fee-lines {
  margin-top: 16px; padding: 12px 14px; background: #FAFAFA; border-radius: var(--r-md);
  display: flex; flex-direction: column; gap: 7px;
}
.fee-line { display: flex; justify-content: space-between; font-size: 12.5px; color: var(--ink-3); }
.fee-line.off span:last-child { color: var(--brand-deep); font-weight: 600; }
.sheet-submit { display: flex; align-items: center; justify-content: space-between; margin-top: 16px; }
.ss-total { font-size: 13px; color: var(--ink-3); }
.ss-total .money { font-size: 22px; font-weight: 800; color: var(--ink); margin-left: 4px; }
.ss-total .money i { font-style: normal; font-size: 14px; }
.submit-btn {
  min-width: 148px; height: 46px; border: none; border-radius: var(--r-pill);
  background: linear-gradient(135deg, var(--brand), var(--brand-deep)); color: #fff; font-size: 15.5px; font-weight: 600; cursor: pointer;
}
.submit-btn:disabled { opacity: 0.55; cursor: not-allowed; }

/* ---------- 单项备注弹窗 ---------- */
.remark-mask {
  position: fixed; inset: 0; background: rgba(0, 0, 0, 0.5); z-index: 200;
  display: flex; align-items: flex-end; justify-content: center;
}
.remark-sheet {
  width: 100%; max-width: 480px; background: #fff; border-radius: var(--r-lg) var(--r-lg) 0 0;
  padding: 20px 18px calc(20px + env(safe-area-inset-bottom));
}
.remark-head { display: flex; justify-content: space-between; align-items: center; font-size: 15px; font-weight: 700; color: var(--ink); }
.remark-target { font-size: 12.5px; font-weight: 400; color: var(--ink-3); }
.remark-options { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 16px; }
.remark-chip {
  padding: 7px 18px; border-radius: var(--r-pill); font-size: 13px; cursor: pointer; transition: all 0.12s;
  border: 1.5px solid var(--line); background: #FAFAFA; color: var(--ink-2);
}
.remark-chip.active { border-color: var(--brand); background: var(--brand-soft); color: var(--brand-deep); font-weight: 600; }
.remark-custom {
  width: 100%; box-sizing: border-box; margin-top: 14px; padding: 10px 14px;
  border: 1.5px solid var(--line); border-radius: var(--r-md); font-size: 13.5px; color: var(--ink); outline: none;
}
.remark-custom:focus { border-color: var(--brand); }
.remark-confirm {
  width: 100%; height: 46px; margin-top: 16px; border: none; border-radius: var(--r-pill);
  background: linear-gradient(135deg, var(--brand), var(--brand-deep)); color: #fff; font-size: 15.5px; font-weight: 600; cursor: pointer;
}

@media (max-width: 360px) {
  .op-img { width: 76px; height: 76px; }
  .op-checkout { padding: 0 18px; }
}
</style>
