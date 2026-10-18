<template>
  <div class="page-card">
    <div class="toolbar">
      <span class="tip">
        会员由顾客在小程序「我的」页自助注册；停用后该会员立即无法查看会员资料与历史订单，可随时重新启用。
      </span>
    </div>

    <div
      v-if="!isMobile"
      class="filters"
    >
      <t-input
        v-model="query.keyword"
        placeholder="搜索昵称 / 手机号"
        clearable
        style="width: 180px"
        @enter="reload"
        @clear="reload"
      >
        <template #prefix-icon>
          <search-icon />
        </template>
      </t-input>
      <t-select
        v-model="query.status"
        :options="statusOptions"
        clearable
        placeholder="全部状态"
        style="width: 132px"
        @change="reload"
      />
      <t-button
        theme="primary"
        @click="reload"
      >
        查询
      </t-button>
      <t-button
        variant="outline"
        @click="resetQuery"
      >
        重置
      </t-button>
    </div>

    <div
      v-if="isMobile"
      class="m-bar"
    >
      <t-input
        v-model="query.keyword"
        placeholder="搜索昵称 / 手机号"
        clearable
        @enter="reload"
        @clear="reload"
      >
        <template #prefix-icon>
          <search-icon />
        </template>
      </t-input>
    </div>

    <!-- 移动端：卡片流（表格在窄屏只能横向滚动，关键信息全被挤出屏幕） -->
    <div
      v-if="isMobile"
      class="m-list"
    >
      <div
        v-if="!list.length"
        class="m-empty"
      >
        {{ loading ? '加载中…' : '暂无会员' }}
      </div>
      <div
        v-for="row in list"
        :key="row.customerId"
        class="mcard"
      >
        <div class="mcard-hd">
          <div style="min-width: 0">
            <div class="mcard-no">{{ row.nickname || '未填写昵称' }}</div>
            <div class="mcard-sub">
              ID {{ row.customerId }}<template v-if="row.phone"> · {{ row.phone }}</template> ·
              {{ providerText(row.providers) || '无登录方式' }}
            </div>
          </div>
          <t-tag
            :theme="row.status === 1 ? 'success' : 'default'"
            variant="light"
          >
            {{ row.status === 1 ? '正常' : '已停用' }}
          </t-tag>
        </div>
        <div class="mcard-grid">
          <div class="mcard-cell">
            <div class="k">累计订单</div>
            <div class="v">{{ row.orderCount }}</div>
          </div>
          <div class="mcard-cell">
            <div class="k">储值余额</div>
            <div class="v">¥{{ Number(row.balance || 0).toFixed(2) }}</div>
          </div>
          <div class="mcard-cell">
            <div class="k">最近下单</div>
            <div class="v">{{ row.lastOrderTime || '—' }}</div>
          </div>
          <div class="mcard-cell">
            <div class="k">入会时间</div>
            <div class="v">{{ row.createTime || '—' }}</div>
          </div>
          <div class="mcard-cell">
            <div class="k">最近登录</div>
            <div class="v">{{ row.lastLoginTime || '—' }}</div>
          </div>
        </div>
        <t-button
          v-if="canAdjust"
          theme="primary"
          variant="outline"
          size="small"
          block
          class="m-op"
          @click="onAdjust(row)"
        >
          储值调账
        </t-button>
        <t-button
          v-if="canEdit"
          theme="default"
          variant="outline"
          size="small"
          block
          class="m-op"
          @click="onToggle(row)"
        >
          {{ row.status === 1 ? '停用该会员' : '启用该会员' }}
        </t-button>
      </div>
      <div
        v-if="total > query.pageSize"
        class="m-pager"
      >
        <t-pagination
          v-model="query.pageNum"
          :total="total"
          :page-size="query.pageSize"
          size="small"
          @change="load"
        />
      </div>
    </div>

    <template v-else>
      <t-table
        :data="list"
        :columns="columns"
        row-key="customerId"
        :loading="loading"
      >
        <template #nickname="{ row }">
          <div class="strong">
            {{ row.nickname || '未填写昵称' }}
          </div>
          <div class="tip">
            ID {{ row.customerId }}<template v-if="row.phone"> · {{ row.phone }}</template>
          </div>
        </template>
        <template #providers="{ row }">
          {{ providerText(row.providers) || '—' }}
        </template>
        <template #balance="{ row }">
          <span class="mono">¥{{ Number(row.balance || 0).toFixed(2) }}</span>
        </template>
        <template #time="{ row }">
          <div class="tip">入会 {{ row.createTime || '—' }}</div>
          <div class="tip">最近登录 {{ row.lastLoginTime || '—' }}</div>
        </template>
        <template #status="{ row }">
          <t-tag
            :theme="row.status === 1 ? 'success' : 'default'"
            variant="light"
          >
            {{ row.status === 1 ? '正常' : '已停用' }}
          </t-tag>
        </template>
        <template #op="{ row }">
          <t-button
            v-if="canAdjust"
            theme="primary"
            variant="text"
            size="small"
            @click="onAdjust(row)"
          >
            调账
          </t-button>
          <t-button
            v-if="canEdit"
            theme="primary"
            variant="text"
            size="small"
            @click="onToggle(row)"
          >
            {{ row.status === 1 ? '停用' : '启用' }}
          </t-button>
          <span
            v-if="!canEdit && !canAdjust"
            class="tip"
            >—</span
          >
        </template>
      </t-table>

      <!-- 储值调账:正数赠送 / 负数扣减,原因必填(流水与审计都要能对上人) -->
      <t-dialog
        v-model:visible="adjustVisible"
        header="储值调账"
        width="560px"
        :confirm-btn="{ content: '确认调账', theme: 'primary' }"
        @confirm="onAdjustConfirm"
      >
        <div
          v-if="adjustRow"
          class="adjust"
        >
          <div class="adjust-cur">
            当前余额：<b>¥{{ Number(adjustRow.balance || 0).toFixed(2) }}</b>
          </div>
          <t-input
            v-model="adjustAmount"
            label="金额(元)"
            placeholder="正数赠送 / 负数扣减"
            style="margin-bottom: 8px"
          />
          <t-input
            v-model="adjustReason"
            label="原因"
            placeholder="必填，如 开业赠送 / 冲正纠错"
          />
          <div class="txn-title">最近流水</div>
          <div
            v-if="!adjustTxns.length"
            class="tip"
          >
            暂无流水
          </div>
          <div
            v-for="t in adjustTxns"
            :key="t.txnId"
            class="txn-row"
          >
            <span class="tip">{{ WALLET_TXN_LABELS[t.bizType] || t.bizType }}</span>
            <span :class="t.amount >= 0 ? 'up' : 'down'">
              {{ t.amount >= 0 ? '+' : '' }}{{ Number(t.amount).toFixed(2) }}
            </span>
            <span class="tip">{{ t.createTime }}</span>
          </div>
        </div>
      </t-dialog>

      <div class="pager">
        <t-pagination
          v-model="query.pageNum"
          v-model:page-size="query.pageSize"
          :total="total"
          :page-size-options="[10, 20, 50]"
          @change="load"
          @page-size-change="reload"
        />
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
// 顾客会员管理：只读列表 + 启停用。
//
// 刻意没有「新建 / 编辑 / 删除」：会员档案由顾客在小程序自助注册（必须带一条登录方式），
// 后台代建会留下谁也登不进去的孤儿档案；删除是顾客自己的权利（「我的」页有注销入口）。
import { computed, onMounted, reactive, ref } from 'vue';
import { DialogPlugin, MessagePlugin } from 'tdesign-vue-next';
import { SearchIcon } from 'tdesign-icons-vue-next';
import { hasPerm } from '@dining/auth';
import {
  adjustMemberWallet,
  getMemberWallet,
  listMembers,
  providerText,
  setMemberStatus,
  WALLET_TXN_LABELS
} from '@dining/api';
import type { MemberRow, WalletTxnRow } from '@dining/api';
import { useIsMobile } from '../utils/useMobile';

const { isMobile } = useIsMobile();

const list = ref<MemberRow[]>([]);
const total = ref<number>(0);
const loading = ref(false);

// 有 member:view 才能进页面，启停用按钮还要 member:edit —— 收银员只读。
const canEdit = computed(() => hasPerm('member:edit'));
// 储值调账动钱,与会员启停用分开收口(wallet:edit)。
const canAdjust = computed(() => hasPerm('wallet:edit'));

const query = reactive<{
  keyword: string;
  status: number | '';
  pageNum: number;
  pageSize: number;
}>({
  keyword: '',
  status: '',
  pageNum: 1,
  pageSize: 10
});

const statusOptions = [
  { label: '正常', value: 1 },
  { label: '已停用', value: 0 }
];

const columns = [
  { colKey: 'nickname', title: '会员', minWidth: 170 },
  { colKey: 'providers', title: '登录方式', width: 160 },
  { colKey: 'orderCount', title: '累计订单', width: 96 },
  { colKey: 'balance', title: '储值余额', width: 108 },
  { colKey: 'lastOrderTime', title: '最近下单', width: 165 },
  { colKey: 'time', title: '入会 / 最近登录', width: 205 },
  { colKey: 'status', title: '状态', width: 86 },
  { colKey: 'op', title: '操作', width: 140 }
];

// ---- 储值调账 ----
const adjustVisible = ref(false);
const adjustRow = ref<MemberRow | null>(null);
const adjustAmount = ref('');
const adjustReason = ref('');
const adjustTxns = ref<WalletTxnRow[]>([]);

/** 打开调账弹窗并拉最近流水(余额以列表行数据为准,不重复请求)。 */
async function onAdjust(row: MemberRow): Promise<void> {
  adjustRow.value = row;
  adjustAmount.value = '';
  adjustReason.value = '';
  adjustTxns.value = [];
  adjustVisible.value = true;
  try {
    const res = await getMemberWallet(row.customerId, 1, 10);
    adjustTxns.value = res.items || [];
  } catch {
    /* 流水拉不到不挡调账,弹窗里已有当前余额 */
  }
}

async function onAdjustConfirm(): Promise<void> {
  const row = adjustRow.value;
  if (!row) return;
  const amount = Number(adjustAmount.value);
  if (!Number.isFinite(amount) || amount === 0) {
    MessagePlugin.warning('请输入非 0 的金额（正数赠送 / 负数扣减）');
    return;
  }
  if (!adjustReason.value.trim()) {
    MessagePlugin.warning('请填写变动原因');
    return;
  }
  try {
    await adjustMemberWallet({ customerId: row.customerId, amount, reason: adjustReason.value.trim() });
    MessagePlugin.success('调账成功');
    adjustVisible.value = false;
    await load();
  } catch {
    /* 失败已由拦截器统一 toast */
  }
}

async function load(): Promise<void> {
  loading.value = true;
  try {
    // status 用 0/'' 区分：0 是「只看停用」这一有效筛选，不能当成「不传」。
    const params = {
      keyword: query.keyword || undefined,
      status: query.status === '' ? undefined : Number(query.status),
      pageNum: query.pageNum,
      pageSize: query.pageSize
    };
    const res = await listMembers(params);
    list.value = res.items || [];
    total.value = res.total || 0;
  } catch {
    /* 失败已由拦截器统一 toast；这里必须 catch，否则 onMounted(load) 会抛未处理的 Promise 拒绝 */
  } finally {
    loading.value = false;
  }
}

function reload(): void {
  query.pageNum = 1;
  void load();
}

function resetQuery(): void {
  query.keyword = '';
  query.status = '';
  query.pageNum = 1;
  void load();
}

async function onToggle(row: MemberRow): Promise<void> {
  const next = row.status === 1 ? 0 : 1;
  const name = row.nickname || '该会员';
  const dlg = DialogPlugin.confirm({
    header: next === 0 ? '停用会员' : '启用会员',
    body:
      next === 0
        ? `停用后「${name}」将立即无法查看会员资料与历史订单（已下的单与营业额不受影响），可随时重新启用。`
        : `确认重新启用「${name}」？启用后该顾客可继续使用会员功能。`,
    confirmBtn: { theme: next === 0 ? 'danger' : 'primary', content: next === 0 ? '确认停用' : '确认启用' },
    onConfirm: async () => {
      try {
        await setMemberStatus(row.customerId, next);
        MessagePlugin.success(next === 0 ? '已停用该会员' : '已启用该会员');
        dlg.hide();
        await load();
      } catch {
        // 失败提示已由拦截器统一弹出，这里只需关掉确认框。
        dlg.hide();
      }
    }
  });
}

onMounted(load);
</script>

<style scoped>
.toolbar {
  margin-bottom: 12px;
}

.tip {
  color: #999;
  font-size: 12px;
}

.mono {
  font-family: 'Courier New', monospace;
  font-size: 12px;
}

.adjust-cur {
  margin-bottom: 12px;
  font-size: 14px;
}

.txn-title {
  margin: 14px 0 6px;
  font-size: 12px;
  color: #999;
}

.txn-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 4px 0;
  font-size: 12px;
}

.up {
  color: #2ba471;
}

.down {
  color: #d54941;
}

.filters {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 12px;
}

.strong {
  font-weight: 600;
  color: #333;
}

.pager {
  display: flex;
  justify-content: flex-end;
  margin-top: 14px;
}

.m-bar {
  margin-bottom: 12px;
}

.m-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.m-empty {
  padding: 24px;
  text-align: center;
  color: #999;
  font-size: 13px;
}

.mcard {
  border: 1px solid #eee;
  border-radius: 8px;
  padding: 10px 12px;
}

.mcard-hd {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.mcard-no {
  font-weight: 600;
  color: #333;
}

.mcard-sub {
  color: #999;
  font-size: 12px;
}

.mcard-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 6px 12px;
  margin-top: 8px;
}

.mcard-cell .k {
  color: #999;
  font-size: 12px;
}

.mcard-cell .v {
  font-size: 13px;
}

.m-op {
  margin-top: 10px;
}

.m-pager {
  display: flex;
  justify-content: center;
  margin-top: 12px;
}
</style>
