<!-- 访问明细下钻弹窗
     访问统计各图表(趋势/渠道/入口/设备/OS/浏览器)点击后共用的明细实现,
     只由调用方传入不同筛选参数(start/end 区间 + channel/entry/device/os/browser)。
     明细一行 = 一次访问;UV 是访客级去重口径,所以头部同时给「次数」与「访客数」。 -->
<template>
  <t-dialog
    :visible="visible"
    :header="title"
    :width="isMobile ? '94vw' : '980px'"
    :footer="false"
    top="8vh"
    @update:visible="(v: boolean) => emit('update:visible', v)"
  >
    <div class="drill-sub">
      共 <b>{{ total }}</b> 次访问 · <b>{{ visitors }}</b> 位访客<template v-if="hint"> · {{ hint }}</template>
      <span class="drill-caliber">明细按保留期清理，早于保留期的访问不可查；UV 为访客去重口径，列表是逐次访问</span>
    </div>

    <t-table
      :data="list"
      :columns="columns"
      row-key="time"
      :loading="loading"
      :pagination="pagination"
      @page-change="onPageChange"
    >
      <template #channel="{ row }">
        <t-tag
          size="small"
          :theme="channelTheme(row.channel)"
          variant="light"
        >
          {{ row.channelLabel || row.channel || '—' }}
        </t-tag>
      </template>
      <template #entry="{ row }">{{ row.entry ? row.entryLabel || row.entry : '—' }}</template>
      <template #device="{ row }">
        <span>{{ row.device || '未知' }}</span>
        <span
          v-if="row.model"
          class="model"
        >
          · {{ row.model }}</span
        >
      </template>
      <template #osBrowser="{ row }">
        <span>{{ row.os || '未知' }} / {{ row.browser || '未知' }}</span>
      </template>
      <template #time="{ row }">
        <!-- 悬停给完整时间(含秒),列内只占人类可读的宽度 -->
        <span
          :title="row.time"
          class="time"
          >{{ shortTime(row.time) }}</span
        >
      </template>
      <template #identity="{ row }">
        <t-tag
          v-if="row.isNew"
          size="small"
          theme="primary"
          variant="light"
        >
          新访客
        </t-tag>
        <t-tag
          v-if="row.isMember"
          size="small"
          theme="success"
          variant="light"
        >
          会员
        </t-tag>
        <span
          v-if="!row.isNew && !row.isMember"
          class="plain"
          >回头访客</span
        >
      </template>
    </t-table>
  </t-dialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue';
import type { PageInfo } from 'tdesign-vue-next';
import { getVisitLogs } from '@dining/api';
import type { VisitLogItem, PageQuery } from '@dining/types';
import { useIsMobile } from '../../utils/useMobile';
import { shortTime } from './reportShared';

const props = defineProps<{
  visible: boolean;
  title: string;
  /** 下钻筛选参数(start/end 区间 + channel/entry/device/os/browser),由触发方组装 */
  query: PageQuery;
  /** 区间等口径说明,拼在「共 N 次访问」后 */
  hint?: string;
}>();

const emit = defineEmits<{
  (e: 'update:visible', value: boolean): void;
}>();

const { isMobile } = useIsMobile();

const list = ref<VisitLogItem[]>([]);
const total = ref(0);
const visitors = ref(0);
const loading = ref(false);
const pagination = reactive({
  current: 1,
  pageSize: 10,
  total: 0,
  showJumper: true,
  showPageSize: true,
  pageSizeOptions: [10, 20, 50]
});

const columns = computed(() => [
  // 宽度按内容分配:时间用 shortTime 后『MM-DD HH:mm』约 80px;
  // IP 要放得下完整 15 位地址不做截断;长文本列(入口/设备/系统/城市)超宽时
  // 截断 + 悬停看全文,避免一张长桌名把整表撑出横向滚动。
  { colKey: 'time', title: '访问时间', width: 104 },
  { colKey: 'channel', title: '渠道', width: 84 },
  { colKey: 'entry', title: '入口', width: 132, ellipsis: true },
  { colKey: 'device', title: '设备', width: 130, ellipsis: true },
  { colKey: 'osBrowser', title: '系统 / 浏览器', width: 160, ellipsis: true },
  { colKey: 'city', title: '城市', width: 76, ellipsis: true },
  { colKey: 'ip', title: 'IP', width: 136 },
  { colKey: 'identity', title: '身份', width: 120 }
]);

// 渠道标签主题:与「终端/渠道构成」图的品牌色系一致即可,不必逐渠道对应
function channelTheme(channel: string | undefined): 'primary' | 'warning' | 'default' {
  if (channel === 'admin') return 'warning';
  if (channel === 'miniprogram') return 'primary';
  return 'default';
}

async function load(): Promise<void> {
  loading.value = true;
  try {
    const res = await getVisitLogs({
      ...props.query,
      pageNum: pagination.current,
      pageSize: pagination.pageSize
    });
    list.value = res.items || [];
    total.value = res.total || 0;
    visitors.value = res.visitors || 0;
    pagination.total = res.total || 0;
  } finally {
    loading.value = false;
  }
}

function onPageChange(info: PageInfo): void {
  pagination.current = info.current;
  pagination.pageSize = info.pageSize;
  load();
}

// 打开或筛选条件变化时回到第 1 页重查;仅翻页走 onPageChange 不重置页码。
watch(
  () => [props.visible, props.query] as const,
  ([visible]) => {
    if (!visible) return;
    pagination.current = 1;
    load();
  },
  { immediate: true }
);
</script>

<style scoped>
.drill-sub {
  font-size: 12px;
  color: var(--ink-4);
  margin-bottom: 10px;
  line-height: 1.6;
}

.drill-sub b {
  color: var(--brand-deep);
}

.drill-caliber {
  display: block;
  font-size: 11px;
  color: var(--ink-4);
}

.model {
  color: var(--ink-4);
}

.time {
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.plain {
  font-size: 12px;
  color: var(--ink-4);
}
</style>
