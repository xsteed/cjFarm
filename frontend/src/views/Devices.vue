<template>
  <div>
    <div class="page-card">
      <div class="dev-tip">
        以下设备开启了「记住我(免登录)」,在有效期内打开系统无需输入密码。若发现不认识的设备,请立即吊销。
      </div>

      <div class="toolbar">
        <div class="toolbar-left">
          <span class="dev-count">共 {{ list.length }} 台设备在免登录有效期内</span>
        </div>
        <t-space>
          <t-button
            v-if="list.length"
            theme="danger"
            variant="outline"
            :disabled="loading"
            @click="askRevokeAll"
          >
            全部吊销
          </t-button>
          <t-button
            theme="primary"
            @click="load"
          >
            <template #icon>
              <refresh-icon />
            </template>
            刷新
          </t-button>
        </t-space>
      </div>

      <!-- 桌面:表格 -->
      <t-table
        v-if="!isMobile"
        :data="list"
        :columns="cols"
        row-key="tokenId"
        :loading="loading"
      >
        <template #device="{ row }">
          <span class="dev-name">{{ row.ua || '未知环境' }}</span>
          <t-tag
            v-if="row.tokenPrefix === myPrefix"
            theme="primary"
            variant="light"
            size="small"
            class="dev-cur"
          >
            本机
          </t-tag>
        </template>
        <template #createTime="{ row }">
          {{ row.createTime || '-' }}
        </template>
        <template #lastUsedTime="{ row }">
          {{ row.lastUsedTime || '从未使用' }}
        </template>
        <template #valid="{ row }"> {{ planText(row) }} · 剩余 {{ remainingText(row) }} </template>
        <template #op="{ row }">
          <t-button
            theme="danger"
            variant="text"
            size="small"
            @click="askRevoke(row)"
          >
            吊销
          </t-button>
        </template>
        <template #empty>
          <div class="empty">暂无开启「记住我」的设备 —— 登录时勾选「记住我(免登录)」即可添加。</div>
        </template>
      </t-table>

      <!-- 窄屏:卡片流 -->
      <div
        v-else
        class="m-list"
      >
        <div
          v-if="!list.length"
          class="m-empty"
        >
          {{ loading ? '加载中…' : '暂无开启「记住我」的设备' }}
        </div>
        <div
          v-for="row in list"
          :key="row.tokenId"
          class="mcard"
        >
          <div class="mcard-hd">
            <div style="min-width: 0">
              <span class="mcard-no">{{ row.ua || '未知环境' }}</span>
              <div class="mcard-sub">
                签发 {{ row.createTime || '-' }} · {{ planText(row) }} 剩余 {{ remainingText(row) }}
              </div>
            </div>
            <t-tag
              v-if="row.tokenPrefix === myPrefix"
              theme="primary"
              variant="light"
            >
              本机
            </t-tag>
          </div>
          <div class="mcard-ft">
            <span class="mcard-sub">最后使用 {{ row.lastUsedTime || '从未' }}</span>
            <t-button
              theme="danger"
              variant="text"
              size="small"
              @click="askRevoke(row)"
            >
              吊销
            </t-button>
          </div>
        </div>
      </div>
    </div>

    <!-- 吊销确认 -->
    <t-dialog
      v-model:visible="confirmVisible"
      :header="confirmAll ? '吊销全部设备？' : '吊销该设备？'"
      width="420px"
      :confirm-btn="{ content: '确认吊销', theme: 'danger', loading: revoking }"
      @confirm="doRevoke"
    >
      <div
        v-if="confirmAll"
        class="dev-confirm"
      >
        将吊销全部 {{ list.length }} 台设备的免登录,包括本机 —— 吊销后所有设备下次打开都需要重新输入密码。
      </div>
      <div
        v-else-if="pending && pending.tokenPrefix === myPrefix"
        class="dev-confirm"
      >
        即将吊销<b>本机</b>的免登录({{ pending.ua || '未知环境' }}),吊销后本机下次打开需要重新输入密码。
      </div>
      <div
        v-else
        class="dev-confirm"
      >
        即将吊销「{{ pending && pending.ua }}」的免登录,该设备下次打开需要重新输入密码,本机不受影响。
      </div>
    </t-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { MessagePlugin } from 'tdesign-vue-next';
import { listRememberSessions, revokeRememberSession } from '../api';
import type { RememberSession } from '../types/entities';
import { getRemember, clearRemember } from '../utils/remember';
import { useIsMobile } from '../utils/useMobile';

const { isMobile } = useIsMobile();
const loading = ref(false);
const revoking = ref(false);
const list = ref<RememberSession[]>([]);
// 本机识别:本地令牌与服务端回传的前 8 位比对(完整令牌不出服务端)。
const myPrefix = (getRemember() || '').slice(0, 8);

const cols = [
  { colKey: 'device', title: '设备', width: 190 },
  { colKey: 'createTime', title: '签发时间', width: 170 },
  { colKey: 'lastUsedTime', title: '最后使用', width: 170 },
  { colKey: 'valid', title: '有效期', width: 150 },
  { colKey: 'op', title: '操作', width: 80 }
];

function parseTime(s: unknown): number {
  // Safari 不认 'YYYY-MM-DD HH:mm:ss',斜杠化统一处理。
  return new Date(String(s || '').replace(/-/g, '/')).getTime();
}

// 档位:由「到期 - 签发」反推,即登录时勾选的 7 天 / 30 天。
function planText(row: RememberSession): string {
  const days = Math.round((parseTime(row.expireTime) - parseTime(row.createTime)) / 86400000);
  return `${days} 天`;
}

function remainingText(row: RememberSession): string {
  const ms = parseTime(row.expireTime) - Date.now();
  if (ms <= 0) return '已过期';
  const hours = Math.floor(ms / 3600000);
  if (hours >= 48) return `${Math.floor(hours / 24)} 天`;
  if (hours >= 1) return `${hours} 小时`;
  return `${Math.max(1, Math.floor(ms / 60000))} 分钟`;
}

async function load(): Promise<void> {
  loading.value = true;
  try {
    const res = await listRememberSessions();
    list.value = (res && res.sessions) || [];
  } finally {
    loading.value = false;
  }
}

// ---- 吊销(单台 / 全部) ----
const confirmVisible = ref(false);
const confirmAll = ref(false);
const pending = ref<RememberSession | null>(null);

function askRevoke(row: RememberSession): void {
  pending.value = row;
  confirmAll.value = false;
  confirmVisible.value = true;
}

function askRevokeAll(): void {
  confirmAll.value = true;
  confirmVisible.value = true;
}

async function doRevoke(): Promise<void> {
  if (revoking.value) return;
  revoking.value = true;
  try {
    if (confirmAll.value) {
      // 后端仅提供单条吊销;设备数是个位数,逐台吊销即可。
      for (const row of list.value) {
        await revokeRememberSession({ tokenId: row.tokenId });
      }
      clearRemember();
      MessagePlugin.success('已吊销全部设备的免登录');
    } else {
      const row = pending.value;
      if (!row) return;
      await revokeRememberSession({ tokenId: row.tokenId });
      // 吊销的是本机:同步清掉本地令牌,避免残留一条已死的令牌下次白跑一次 401。
      if (row.tokenPrefix === myPrefix) clearRemember();
      MessagePlugin.success('已吊销该设备');
    }
    confirmVisible.value = false;
    await load();
  } catch {
    // 拦截器已弹过提示;留在当前状态可重试。
  } finally {
    revoking.value = false;
  }
}

onMounted(load);
</script>

<style scoped>
.dev-tip {
  font-size: 12.5px;
  color: var(--ink-3);
  background: var(--brand-soft);
  border-radius: 10px;
  padding: 10px 14px;
  margin-bottom: 14px;
  line-height: 1.7;
}
.dev-count {
  font-size: 13px;
  color: var(--ink-2);
}
.dev-name {
  font-weight: 600;
  color: var(--ink);
}
.dev-cur {
  margin-left: 8px;
}
.dev-confirm {
  font-size: 13.5px;
  line-height: 1.8;
}
.dev-confirm b {
  color: var(--brand-deep);
}
.empty {
  padding: 32px 0;
  color: var(--ink-3);
  font-size: 13px;
}
.m-empty {
  padding: 32px 0;
  color: var(--ink-3);
  font-size: 13px;
  text-align: center;
}
</style>
