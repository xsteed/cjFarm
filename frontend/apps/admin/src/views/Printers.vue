<template>
  <div class="page-card">
    <!-- 移动端下拉刷新 -->
    <PullRefresh
      v-if="isMobile"
      :distance="refreshDistance"
      :refreshing="refreshRefreshing"
      :pulling="refreshPulling"
    />

    <div class="toolbar">
      <!-- 三条通道拆成三行条目:原来是一整段长文案,三种通道混在一起,
           要读到第二行才知道有几个选项;分行 + 悬挂缩进后一眼能扫到各自的要求。 -->
      <div class="tip toolbar-tip">
        <div>三条通道任选其一：</div>
        <ul class="tip-list">
          <li><b>网络直连</b>：要求后端与打印机在同一局域网（后端拼 ESC/POS 走 IP:9100，无需收银电脑开机）</li>
          <li><b>飞鹅云</b>：由打印机自己联网取单</li>
          <li>
            <b>本地代理</b>：由门店内网的代理程序取单后直发打印机；后端部署在云服务器、想继续用已有的 9100
            网络机时选它（<b>不需要装任何打印机驱动</b>）
          </li>
        </ul>
      </div>
      <t-space v-if="canEdit && !isMobile">
        <t-button
          variant="outline"
          @click="openBind"
        >
          绑定飞鹅打印机
        </t-button>
        <t-button
          theme="primary"
          @click="openAdd"
        >
          <template #icon>
            <add-icon />
          </template>
          新增打印机
        </t-button>
      </t-space>
    </div>

    <!-- 移动端:绑定入口放搜索栏位置,新增走悬浮按钮 -->
    <div
      v-if="isMobile && canEdit"
      class="m-bar"
    >
      <t-button
        variant="outline"
        block
        @click="openBind"
      >
        绑定飞鹅打印机
      </t-button>
    </div>
    <MobileFab
      v-if="isMobile && canEdit"
      @click="openAdd"
    >
      <span class="m-fab-ic"><add-icon /></span>
      新增
    </MobileFab>

    <t-alert
      v-if="feieLoaded && hasFeiePrinter && !feie.configured"
      theme="warning"
      class="mb"
    >
      <template #message>
        飞鹅云打印尚未配置账号 —— 通道选「飞鹅云」的打印机无法出纸。请到 <b>系统配置 → 小票打印</b> 填写飞鹅账号与
        UKEY。
      </template>
    </t-alert>

    <t-alert
      v-if="agentStateReady && !agentConfigured"
      theme="warning"
      class="mb"
    >
      <template #message>
        本地打印代理尚未配置令牌 —— 通道选「本地代理」的打印机只会把票据排队，没有人来取单。 请到
        <b>系统配置 → 小票打印</b> 填写代理令牌，并在门店内网运行 print-agent（见 docs/print-agent.md）。
      </template>
    </t-alert>
    <t-alert
      v-else-if="agentStateReady && !agent.online"
      theme="warning"
      class="mb"
    >
      <template #message>
        本地打印代理已配置，但云端最近没有收到它的心跳 —— 票据会一直排队等它上线。 请确认门店内网的 print-agent
        正在运行（{{ agent.lastSeen ? '最近心跳 ' + agent.lastSeen : '尚未收到过心跳' }}）； 当前积压
        <b>{{ agent.pending }}</b> 单{{ agent.dead ? '，另有 ' + agent.dead + ' 单重试耗尽已放弃' : '' }}。
      </template>
    </t-alert>
    <t-alert
      v-else-if="agentStateReady && agent.dead"
      theme="error"
      class="mb"
    >
      <template #message>
        有 <b>{{ agent.dead }}</b> 单重试次数用尽已放弃（多为打印机未开机 / IP 填错），
        可在「打印日志」里按失败筛选并补打。
      </template>
    </t-alert>
    <!-- 代理在线也可能积压:队列堆积或最老单据等太久时单独提示,不并入上面的 v-else-if 链,
         否则「在线但积压」这种最需要提醒的场景反而被隐藏。 -->
    <t-alert
      v-if="agentStateReady && (agent.pending > 20 || agent.oldestPendingSec > 300)"
      theme="warning"
      class="mb"
    >
      <template #message>
        本地打印代理积压 <b>{{ agent.pending }}</b> 单<template v-if="agent.oldestPendingSec > 0"
          >，最老单据已等 <b>{{ agentWaitMinutes }}</b> 分钟</template
        >。请检查门店代理与打印机是否正常。
      </template>
    </t-alert>

    <!-- 代理状态总览:只在确实配置过代理时出现。
         没配过代理的门店(哪怕有打印机把通道选成了「本地代理」)看到一张恒「离线」
         的卡片只会困惑 —— 那种「通道选了代理但没配令牌」的场景由上方那条告警负责指引,
         状态卡片在这里没有可看的信号(心跳/队列都还是空的)。
         30s 自动刷新,与后端 agentOnlineWindow 90s 同口径。 -->
    <div
      v-if="agentLoaded && agentConfigured"
      class="agent-section mb"
    >
      <div class="agent-section-title">本地打印代理</div>
      <div class="agent-item-hd">
        <t-tag
          :theme="agent.online ? 'success' : 'warning'"
          variant="light"
        >
          {{ agent.online ? '在线' : '离线' }}
        </t-tag>
        <span class="agent-status-text">
          {{ agent.online ? '门店代理正在正常轮询取单' : '云端最近未收到心跳，票据将一直排队等它上线' }}
        </span>
      </div>
      <div class="agent-item-grid">
        <div class="agent-cell">
          <span class="k">最近心跳</span>
          <span class="v">{{ agent.lastSeen || '尚未收到过心跳' }}</span>
        </div>
        <div class="agent-cell">
          <span class="k">队列积压</span>
          <span class="v">{{ agent.pending }} 单{{ agent.dead ? '（另 ' + agent.dead + ' 单已放弃）' : '' }}</span>
        </div>
        <div
          v-if="agent.oldestPendingSec > 0"
          class="agent-cell"
        >
          <span class="k">最老待打印</span>
          <span class="v">已等 {{ agentWaitMinutes }} 分钟</span>
        </div>
      </div>
    </div>

    <!-- v2 打印代理身份列表:老后端没有 agents 字段时整块不显示 -->
    <div
      v-if="agent.agents.length"
      class="agent-section mb"
    >
      <div class="agent-section-title">打印代理</div>
      <div
        v-for="a in agent.agents"
        :key="a.agentId"
        class="agent-item"
      >
        <div class="agent-item-hd">
          <span class="agent-name">{{ a.agentName || '未命名代理' }}</span>
          <t-tag
            :theme="a.status === 1 ? 'success' : 'default'"
            variant="light"
          >
            {{ a.status === 1 ? '启用' : '吊销' }}
          </t-tag>
          <!-- 在线判定与后端 agentOnlineWindow(90s)一致:最近心跳距今 < 90 秒 -->
          <t-tag
            v-if="a.status === 1"
            :theme="isAgentOnline(a) ? 'success' : 'warning'"
            variant="light"
          >
            {{ isAgentOnline(a) ? '在线' : '离线' }}
          </t-tag>
          <!-- 代理版本与云端最新版本均有值且不一致时才提示可升级 -->
          <t-tag
            v-if="a.version && agent.latestAgentVersion && a.version !== agent.latestAgentVersion"
            theme="warning"
            variant="light"
          >
            可升级
          </t-tag>
        </div>
        <div class="agent-item-grid">
          <div class="agent-cell">
            <span class="k">最近上报</span>
            <span class="v">{{ a.lastReport || '—' }}</span>
          </div>
          <div class="agent-cell">
            <span class="k">最近心跳</span>
            <span class="v">{{ a.lastSeen || '—' }}</span>
          </div>
          <div class="agent-cell">
            <span class="k">令牌</span>
            <span class="v mono">{{ a.tokenHint || '—' }}</span>
          </div>
        </div>
      </div>
    </div>

    <!-- 窄屏:7 列 + 200px 操作列只能横向拖;改为卡片流 -->
    <div
      v-if="isMobile"
      class="m-list"
    >
      <div
        v-if="!list.length"
        class="m-empty"
      >
        {{ loading ? '加载中…' : '暂无打印机' }}
      </div>
      <div
        v-for="row in list"
        :key="row.printerId"
        class="mcard"
      >
        <div class="mcard-hd">
          <span
            class="mcard-no"
            style="min-width: 0"
            >{{ row.printerName }}</span
          >
          <t-tag
            :theme="row.printerType === 1 ? 'danger' : 'primary'"
            variant="light"
          >
            {{ row.printerType === 1 ? '厨房单' : '食客小票' }}
          </t-tag>
        </div>

        <div class="mcard-grid">
          <div class="mcard-cell">
            <div class="k">通道</div>
            <div class="v">
              {{ providerLabel(row.provider) }}
            </div>
          </div>
          <div class="mcard-cell">
            <div class="k">
              {{ row.provider === 'feie' ? '打印机编号' : 'IP 地址' }}
            </div>
            <div class="v">
              {{ row.provider === 'feie' ? row.feieSn || '未填写' : `${row.ip}:${row.port}` }}
            </div>
          </div>
          <div class="mcard-cell">
            <div class="k">纸宽 / 份数</div>
            <div class="v">{{ row.paperWidth === 32 ? '58mm' : '80mm' }} · {{ row.copies || 1 }} 份</div>
          </div>
          <div
            v-if="row.printerType === 1"
            class="mcard-cell"
          >
            <div class="k">负责分类</div>
            <div class="v">
              {{ row.categoryNames || '全部菜品' }}
            </div>
          </div>
          <div class="mcard-cell">
            <div class="k">状态</div>
            <div class="v">
              <t-tag
                :theme="row.status === 1 ? 'success' : 'default'"
                variant="light"
              >
                {{ row.status === 1 ? '启用' : '停用' }}
              </t-tag>
            </div>
          </div>
        </div>

        <div class="mcard-ft">
          <t-button
            theme="primary"
            variant="text"
            size="small"
            @click="onStatus(row)"
          >
            状态
          </t-button>
          <t-button
            theme="default"
            variant="text"
            size="small"
            @click="onPreview(row)"
          >
            票据预览
          </t-button>
          <template v-if="canEdit">
            <t-button
              theme="primary"
              variant="text"
              size="small"
              :disabled="testingIds.has(Number(row.printerId))"
              @click="onTest(row)"
            >
              测试打印
            </t-button>
            <t-button
              theme="primary"
              variant="text"
              size="small"
              @click="openEdit(row)"
            >
              编辑
            </t-button>
            <t-button
              v-if="canClearQueue(row)"
              theme="warning"
              variant="text"
              size="small"
              @click="onClear(row)"
            >
              清空队列
            </t-button>
            <t-button
              theme="danger"
              variant="text"
              size="small"
              @click="onDelete(row)"
            >
              删除
            </t-button>
          </template>
        </div>
      </div>
    </div>

    <t-table
      v-else
      :data="list"
      :columns="columns"
      row-key="printerId"
      :loading="loading"
    >
      <template #printerType="{ row }">
        <t-tag
          :theme="row.printerType === 1 ? 'danger' : 'primary'"
          variant="light"
        >
          {{ row.printerType === 1 ? '厨房单' : '食客小票' }}
        </t-tag>
      </template>
      <template #provider="{ row }">
        <t-tag
          :theme="PRINTER_PROVIDER[row.provider]?.theme || 'default'"
          variant="light"
        >
          {{ providerLabel(row.provider) }}
        </t-tag>
      </template>
      <template #target="{ row }">
        <span
          v-if="row.provider === 'feie'"
          class="mono"
          >SN {{ row.feieSn || '未填写' }}</span
        >
        <span
          v-else
          class="mono"
          >{{ row.ip }}:{{ row.port }}</span
        >
      </template>
      <template #paperWidth="{ row }">
        <span>{{ row.paperWidth === 32 ? '58mm' : '80mm' }} × {{ row.copies || 1 }}</span>
      </template>
      <template #categoryIds="{ row }">
        <span
          v-if="row.printerType === 2"
          class="tip"
          >全部菜品</span
        >
        <span v-else-if="row.categoryNames">{{ row.categoryNames }}</span>
        <span
          v-else
          class="tip"
          >全部菜品</span
        >
      </template>
      <template #status="{ row }">
        <t-tag
          :theme="row.status === 1 ? 'success' : 'default'"
          variant="light"
        >
          {{ row.status === 1 ? '启用' : '停用' }}
        </t-tag>
      </template>
      <template #op="{ row }">
        <t-space size="4px">
          <t-button
            theme="primary"
            variant="text"
            size="small"
            @click="onStatus(row)"
          >
            状态
          </t-button>
          <t-button
            theme="default"
            variant="text"
            size="small"
            @click="onPreview(row)"
          >
            票据预览
          </t-button>
          <template v-if="canEdit">
            <t-button
              theme="primary"
              variant="text"
              size="small"
              :disabled="testingIds.has(Number(row.printerId))"
              @click="onTest(row)"
            >
              测试打印
            </t-button>
            <t-button
              theme="primary"
              variant="text"
              size="small"
              @click="openEdit(row)"
            >
              编辑
            </t-button>
            <t-button
              v-if="canClearQueue(row)"
              theme="warning"
              variant="text"
              size="small"
              @click="onClear(row)"
            >
              清空队列
            </t-button>
            <t-button
              theme="danger"
              variant="text"
              size="small"
              @click="onDelete(row)"
            >
              删除
            </t-button>
          </template>
        </t-space>
      </template>
    </t-table>

    <!-- 测试页预览:不出纸先看样式,确认无误再点「测试打印」 -->
    <TicketPreviewDialog
      v-model:visible="previewVisible"
      :title="previewTitle"
      :loading="previewLoading"
      :chunks="previewData?.chunks"
      :line-width="previewData?.lineWidth ?? 48"
      :copies="previewRow?.copies"
    >
      <template #meta>
        测试页由后端「出纸同一份渲染代码」生成；纸宽
        {{ previewRow?.paperWidth === 32 ? '58mm' : '80mm' }}。确认无误后再点「测试打印」出纸。
      </template>
    </TicketPreviewDialog>

    <t-dialog
      v-model:visible="dialogVisible"
      :header="form.printerId ? '编辑打印机' : '新增打印机'"
      :width="isMobile ? '94vw' : '720px'"
      :confirm-btn="{ content: '保存', theme: 'primary', loading: saving }"
      @confirm="save"
    >
      <t-form
        class="printer-form"
        :data="form"
        :label-width="isMobile ? '84px' : '100px'"
      >
        <t-form-item
          label="打印机名称"
          name="printerName"
        >
          <t-input
            v-model="form.printerName"
            placeholder="如 后厨厨房单打印机"
          />
        </t-form-item>
        <t-form-item
          label="打印机类型"
          name="printerType"
        >
          <t-radio-group
            v-model="form.printerType"
            variant="default-filled"
          >
            <t-radio-button :value="1">厨房单</t-radio-button>
            <t-radio-button :value="2">食客小票</t-radio-button>
          </t-radio-group>
        </t-form-item>

        <t-form-item
          label="接入方式"
          name="provider"
        >
          <div class="field-block">
            <t-radio-group
              v-model="form.provider"
              class="provider-cards"
              :class="{ stacked: isMobile }"
            >
              <t-radio value="tcp">
                <span class="pc-title">网络直连</span>
                <span class="pc-desc">同网直发 9100</span>
              </t-radio>
              <t-radio value="feie">
                <span class="pc-title">飞鹅云打印</span>
                <span class="pc-desc">打印机联网取单</span>
              </t-radio>
              <t-radio value="agent">
                <span class="pc-title">本地打印代理</span>
                <span class="pc-desc">内网代理取单直发</span>
              </t-radio>
            </t-radio-group>
            <div class="provider-hint">{{ providerHint }}</div>
          </div>
        </t-form-item>

        <template v-if="form.provider !== 'feie'">
          <div
            class="conn-row"
            :class="{ stacked: isMobile }"
          >
            <t-form-item
              class="conn-ip"
              label="IP 地址"
              name="ip"
            >
              <t-input
                v-model="form.ip"
                placeholder="打印机网络 IP，如 192.168.1.8"
              />
            </t-form-item>
            <t-form-item
              class="conn-port"
              label="端口"
              label-width="56px"
              name="port"
            >
              <t-input-number
                v-model="form.port"
                :min="1"
                :max="65535"
                theme="normal"
                placeholder="默认 9100"
              />
            </t-form-item>
          </div>
        </template>
        <t-form-item
          v-else
          label="打印机编号"
          name="feieSn"
        >
          <t-input
            v-model="form.feieSn"
            placeholder="飞鹅打印机机身标签上的 SN"
          />
        </t-form-item>

        <t-form-item label="打印参数">
          <div class="print-opts">
            <div class="opt">
              <span class="opt-label">纸宽</span>
              <t-radio-group
                v-model="form.paperWidth"
                variant="default-filled"
              >
                <t-radio-button :value="32">58mm</t-radio-button>
                <t-radio-button :value="48">80mm</t-radio-button>
              </t-radio-group>
            </div>
            <div class="opt">
              <span class="opt-label">份数</span>
              <t-input-number
                v-model="form.copies"
                :min="1"
                :max="5"
                style="width: 96px"
              />
            </div>
            <div class="opt">
              <span class="opt-label">状态</span>
              <t-switch
                v-model="form.status"
                :custom-value="[1, 0]"
              />
              <span class="opt-value">{{ form.status === 1 ? '启用' : '停用' }}</span>
            </div>
          </div>
        </t-form-item>

        <t-form-item
          v-if="form.printerType === 1"
          label="负责分类"
          name="categoryIdList"
        >
          <div class="field-block">
            <t-select
              v-model="form.categoryIdList"
              :options="categoryOptions"
              multiple
              clearable
              placeholder="不选 = 本机打印全部菜品"
            />
            <div class="tip">
              用于多台厨房机分工：例如「凉菜机」只勾凉菜素菜、「热菜机」只勾荤菜与特色农家菜。
              本单没有它负责的菜品时不会出空白单。
            </div>
          </div>
        </t-form-item>
      </t-form>
    </t-dialog>

    <t-dialog
      v-model:visible="bindVisible"
      header="绑定飞鹅打印机到账号"
      :width="isMobile ? '94vw' : '520px'"
      :confirm-btn="{ content: '绑定', theme: 'primary', loading: saving }"
      @confirm="doBind"
    >
      <t-form
        :data="bindForm"
        :label-width="isMobile ? '90px' : '110px'"
      >
        <t-form-item label="打印机编号">
          <t-input
            v-model="bindForm.sn"
            placeholder="机身标签上的 SN，如 316500010"
          />
        </t-form-item>
        <t-form-item label="识别码 KEY">
          <t-input
            v-model="bindForm.key"
            placeholder="机身标签上的 KEY"
          />
        </t-form-item>
        <t-form-item label="备注名称">
          <t-input
            v-model="bindForm.name"
            placeholder="如 后厨打印机（选填）"
          />
        </t-form-item>
        <t-form-item label="流量卡号">
          <t-input
            v-model="bindForm.phone"
            placeholder="选填"
          />
        </t-form-item>
      </t-form>
      <div class="tip">绑定成功后，还需在上方「新增打印机」里把该 SN 录入进来，才能接单打印。</div>
    </t-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, onUnmounted } from 'vue';
import { MessagePlugin, DialogPlugin } from 'tdesign-vue-next';
import {
  listPrinters,
  savePrinter,
  updatePrinter,
  deletePrinter,
  testPrinter,
  printerStatus,
  bindFeiePrinter,
  clearPrinterQueue,
  getFeieInfo,
  getAgentInfo,
  listCategories,
  previewPrinterTest,
  PRINTER_PROVIDER
} from '@dining/api';
import { useIsMobile } from '../utils/useMobile';
import { usePullRefresh } from '../composables/usePullRefresh';
import { hasPerm } from '@dining/auth';
import PullRefresh from '../components/PullRefresh.vue';
import MobileFab from '../components/MobileFab.vue';
import TicketPreviewDialog from '../components/TicketPreviewDialog.vue';
import type { MenuCategory, PrintAgent, Printer, PrinterPayload, TicketPreview } from '@dining/types';

// 列表行在模板/操作函数里要把 printerId 传给各打印机接口(参数为 number | string),
// 并拿 provider 当字典下标,Printer 字段全可选,这里收紧为必填。
type PrinterRow = Printer & { printerId: number; provider: string };

const list = ref<PrinterRow[]>([]);
const loading = ref(false);
const dialogVisible = ref(false);
const bindVisible = ref(false);
// 保存 / 绑定在途标记:防连点重复提交
const saving = ref(false);
const categories = ref<MenuCategory[]>([]);

interface FeieState {
  configured: boolean;
  user: string;
}

const feie = reactive<FeieState>({ configured: false, user: '' });

// 飞鹅 / 代理的状态字段在首次拉取前都是初始值(false)。页面加载时「打印机列表」与
// 「状态接口」是两个并行的请求,一旦列表先回来,就会拿 configured=false 先渲染出一条
// 「尚未配置账号/令牌」的假告警,等状态接口回来才消失 —— 慢网下能停留好几秒。
// 这两个标记表示「首次拉取已结束」,用来把那段窗口遮住(成功失败都算结束,
// 失败时行为与改动前一致:按未配置呈现,不会把告警永久藏起来)。
const feieLoaded = ref(false);
const agentLoaded = ref(false);

// 本地打印代理:令牌是否已配、代理是否还在心跳、队列积压。
// 云后端连不到门店内网,「代理心跳」是判断打印链路是否活着的唯一信号。
interface AgentState {
  configured: boolean;
  online: boolean;
  lastSeen: string;
  pending: number;
  dead: number;
  oldestPendingSec: number;
  agents: PrintAgent[];
  latestAgentVersion: string;
}

const agent = reactive<AgentState>({
  configured: false,
  online: false,
  lastSeen: '',
  pending: 0,
  dead: 0,
  oldestPendingSec: 0,
  agents: [],
  latestAgentVersion: ''
});

interface PrinterForm {
  printerId: number | null;
  printerName: string;
  printerType: number;
  provider: string;
  ip: string;
  port: number;
  feieSn: string;
  paperWidth: number;
  copies: number;
  categoryIdList: number[];
  status: number;
}

const form = reactive<PrinterForm>({
  printerId: null,
  printerName: '',
  printerType: 1,
  provider: 'tcp',
  ip: '',
  port: 9100,
  feieSn: '',
  paperWidth: 48,
  copies: 1,
  categoryIdList: [],
  status: 1
});
const bindForm = reactive({ sn: '', key: '', name: '', phone: '' });
const { isMobile } = useIsMobile();
// 移动端下拉刷新打印机列表
const { distance: refreshDistance, refreshing: refreshRefreshing, pulling: refreshPulling } = usePullRefresh(load);

const categoryOptions = computed(() => categories.value.map(c => ({ label: c.categoryName, value: c.categoryId })));

// 通道中文名统一从 api 的枚举取,避免这里再硬编码一遍导致两处漂移。
const providerLabel = (p?: string): string => PRINTER_PROVIDER[p || '']?.label || '网络直连';

// 接入方式卡片的随选说明:选中哪条通道就解释哪条,避免一整段大杂烩文案。
const providerHint = computed((): string => {
  switch (form.provider) {
    case 'feie':
      return '打印机自己联网到飞鹅云取单，后端无需与其同网。若 SN 尚未绑定，请先用右上角「绑定飞鹅打印机」录入 SN 与识别码。';
    case 'agent':
      return '后端部署在云服务器时选它：IP 必须是代理能访问到的门店内网地址（不要填 127.0.0.1，除非代理和打印机在同一台机器上）。不需要装任何打印机驱动。';
    default:
      return '要求后端与打印机在同一局域网，下单/结账时后端直发指令到 IP:9100，收银电脑无需开机。';
  }
});

// 有没有用到某条通道的打印机 —— 决定要不要显示对应的「未配置/离线」告警。
// 只在真正用得上时提示,否则没用飞鹅的门店会一直看到一条无关的黄色告警。
const hasFeiePrinter = computed(() => list.value.some(r => r.provider === 'feie'));
const hasAgentPrinter = computed(() => list.value.some(r => r.provider === 'agent'));

// 「本地打印代理是否配置过」:legacy 全局令牌 或 任一 v2 per-agent 身份,满足其一即为已配置。
//
// 不能只看 agent.configured —— 后端 AgentConfigured() 验的是 legacy 的 agent_token,
// 只逐台签发 v2 身份、没配全局令牌的门店会被误判成「未配置」,状态卡片随之消失。
// 用途:决定要不要显示「代理状态总览」卡片(见模板);没有配置过时不展示,
// 避免「通道选了本地代理但没配令牌」的门店看到一张永远离线的空卡片。
const agentConfigured = computed(() => agent.configured || agent.agents.length > 0);

// 代理告警(未配置令牌 / 离线 / 有放弃单 / 积压)统一的前提:
// 门店确实有代理通道打印机,且状态接口已经答复过(见 agentLoaded 处说明,防假告警闪现)。
const agentStateReady = computed(() => agentLoaded.value && hasAgentPrinter.value);

// 最老待打印单已等分钟数(向上取整);老后端没有 oldestPendingSec 时为 0。
const agentWaitMinutes = computed(() => Math.ceil((agent.oldestPendingSec || 0) / 60));

// 在线判定直接用服务端下发的 online（后端按 90 秒窗口、且与写入同一时区判定）。
//
// 不要自己解析 lastSeen:它是服务端本地墙钟字符串「YYYY-MM-DD HH:mm:ss」,浏览器按自己的
// 时区解析 —— 容器多跑在 UTC、门店浏览器在东八区,相差 8 小时远超 90 秒窗口,代理明明在
// 正常心跳,页面却恒显示「离线」。
// 老后端不返回 online 字段时退回 false（显示离线），不再做有歧义的本地时间推算。
function isAgentOnline(row: PrintAgent): boolean {
  return row.online === true;
}

// 清空队列:仅飞鹅云与本地代理有「云端/队列」概念,直连是即发即走。
const canClearQueue = (row: PrinterRow): boolean => row.provider === 'feie' || row.provider === 'agent';

// 无 printer:edit 时只保留「状态」与「票据预览」(两者都是只读 GET),
// 其余按钮(测试打印/编辑/清空队列/删除/新增/绑定)都会改打印机状态或真的出纸。
const canEdit = computed(() => hasPerm('printer:edit'));

const columns = computed(() => {
  const cols = [
    { colKey: 'printerName', title: '打印机名称', minWidth: 140 },
    { colKey: 'printerType', title: '类型', width: 100 },
    { colKey: 'provider', title: '通道', width: 100 },
    { colKey: 'target', title: '地址', width: 170 },
    { colKey: 'paperWidth', title: '纸宽/份数', width: 110 },
    { colKey: 'categoryIds', title: '负责分类', width: 150 },
    { colKey: 'status', title: '状态', width: 90 }
  ];
  // 只读时操作列只剩「状态」与「票据预览」,宽度收窄,免得右侧留一大片空白。
  cols.push({ colKey: 'op', title: '操作', width: canEdit.value ? 400 : 180 });
  return cols;
});

async function load(): Promise<void> {
  loading.value = true;
  try {
    const res = await listPrinters();
    // 必须兜底:后端返回 { total: 0 } 而无 items 时,undefined 会让模板里的
    // !list.length 直接抛 TypeError 白屏(同仓库其它页面都写了 || [])。
    list.value = (res.items || []) as PrinterRow[];
  } catch {
    /* 失败已由拦截器统一 toast;必须 catch,否则 onMounted(load) 会抛未处理的 Promise 拒绝 */
  } finally {
    loading.value = false;
  }
}

async function loadFeie(): Promise<void> {
  try {
    const res = await getFeieInfo();
    feie.configured = !!res.configured;
    feie.user = res.user || '';
  } catch {
    feie.configured = false;
  } finally {
    // 成功失败都算「答复过了」:失败时按未配置呈现(与改动前一致),不能把告警永久藏住。
    feieLoaded.value = true;
  }
}

async function loadAgent(): Promise<void> {
  try {
    const res = await getAgentInfo();
    agent.configured = !!res.configured;
    agent.online = !!res.online;
    agent.lastSeen = res.lastSeen || '';
    agent.pending = res.pending || 0;
    agent.dead = res.dead || 0;
    // v2 多代理身份与最老待打印单等待秒数;老后端无这两个字段时按缺省容错。
    agent.oldestPendingSec = Number(res.oldestPendingSec) || 0;
    agent.agents = Array.isArray(res.agents) ? res.agents : [];
    // 云端配置的代理最新版本号;老后端无此字段时按缺省容错。
    agent.latestAgentVersion = res.latestAgentVersion || '';
  } catch {
    // 整体复位:只改 configured 会让 online/pending/dead/agents 停留在上一次的值,
    // 网络抖动时页面继续渲染几分钟前的旧状态(「在线」「积压 N 单」)且不会消失。
    agent.configured = false;
    agent.online = false;
    agent.lastSeen = '';
    agent.pending = 0;
    agent.dead = 0;
    agent.oldestPendingSec = 0;
    agent.agents = [];
    agent.latestAgentVersion = '';
  } finally {
    // 成功失败都算「答复过了」:失败时按未配置呈现(与改动前一致),不能把告警永久藏住。
    agentLoaded.value = true;
  }
}

async function loadCategories(): Promise<void> {
  try {
    const res = await listCategories();
    categories.value = (res && res.items) || [];
  } catch {
    categories.value = [];
  }
}

function resetForm(): void {
  Object.assign(form, {
    printerId: null,
    printerName: '',
    printerType: 1,
    provider: 'tcp',
    ip: '',
    port: 9100,
    feieSn: '',
    paperWidth: 48,
    copies: 1,
    categoryIdList: [],
    status: 1
  });
}

function openAdd(): void {
  resetForm();
  dialogVisible.value = true;
}

function openEdit(row: PrinterRow): void {
  Object.assign(form, {
    printerId: row.printerId ?? null,
    printerName: row.printerName ?? '',
    printerType: row.printerType ?? 1,
    // 归一化:库里出现未知通道时回落到直连,避免单选组拿到不存在的值而渲染空白。
    provider: row.provider && PRINTER_PROVIDER[row.provider] ? row.provider : 'tcp',
    ip: row.ip || '',
    port: row.port || 9100,
    feieSn: row.feieSn || '',
    paperWidth: row.paperWidth || 48,
    copies: row.copies || 1,
    categoryIdList: [...(row.categoryIdList || [])],
    status: Number(row.status) === 1 ? 1 : 0
  });
  dialogVisible.value = true;
}

async function save(): Promise<void> {
  if (!form.printerName) {
    MessagePlugin.warning('请填写打印机名称');
    return;
  }
  // tcp 与 agent 都要填 IP(agent 通道下它是打印机在门店内网的地址,由代理使用)。
  if (form.provider !== 'feie' && !form.ip) {
    MessagePlugin.warning(form.provider === 'agent' ? '请填写打印机在门店内网的 IP 地址' : '请填写打印机 IP 地址');
    return;
  }
  if (form.provider === 'feie' && !form.feieSn) {
    MessagePlugin.warning('请填写飞鹅打印机编号(SN)');
    return;
  }
  // 端口/份数必须在这里兜住:t-input-number 的 min/max 只约束输入控件,
  // 用户清空输入框后值会变成 undefined/null —— 原样提交会落库成 0 端口 / 0 份,
  // 之后测试打印与出纸全部失效,且报错信息与「端口填错」长得一样,很难排查。
  if (form.provider !== 'feie') {
    const port = Number(form.port);
    if (!Number.isInteger(port) || port < 1 || port > 65535) {
      MessagePlugin.warning('请填写合法的端口(1~65535)');
      return;
    }
    form.port = port;
  }
  const copies = Number(form.copies);
  // 后端契约与归一化均为 1-5(handler 会把超值收敛为 5),前端必须同口径拦截,
  // 否则填 6-10 会「校验通过却被静默改成 5」,与用户填写的值不一致。
  if (!Number.isInteger(copies) || copies < 1 || copies > 5) {
    MessagePlugin.warning('请填写合法的打印份数(1~5)');
    return;
  }
  form.copies = copies;
  // 食客小票必须含全部菜品，后端也会强制清空分类，这里保持一致避免误导。
  const payload: PrinterPayload = { ...form, printerId: form.printerId ?? undefined };
  if (payload.printerType === 2) payload.categoryIdList = [];
  if (saving.value) return;
  saving.value = true;
  try {
    if (form.printerId) {
      await updatePrinter(payload);
    } else {
      await savePrinter(payload);
    }
    MessagePlugin.success('保存成功');
    dialogVisible.value = false;
    load();
    // 新增/改通道可能改变「有没有 agent 打印机」,告警条的显隐要跟着变。
    loadAgent();
  } catch {
    /* 失败已由拦截器统一 toast,弹窗保留已填内容供修改重试 */
  } finally {
    saving.value = false;
  }
}

// 逐行在途标记:测试打印会真的吐纸,连点会连出多张;逐行去重避免误伤其它打印机。
const testingIds = ref<Set<number>>(new Set());
async function onTest(row: PrinterRow): Promise<void> {
  const id = Number(row.printerId);
  if (testingIds.value.has(id)) return;
  testingIds.value.add(id);
  try {
    const res = await testPrinter(row.printerId);
    MessagePlugin.success(res?.msg || '测试指令已发送');
  } catch {
    // 失败原因（含飞鹅返回码释义）由请求拦截器统一弹出
  } finally {
    testingIds.value.delete(id);
  }
}

// 票据预览:不出纸地看这台机器的测试页样式 —— 新装机器时先在这儿确认
// 纸宽/通道名/份数写对了,再决定要不要真吐一张纸。
const previewVisible = ref(false);
const previewLoading = ref(false);
const previewData = ref<TicketPreview | null>(null);
const previewRow = ref<PrinterRow | null>(null);

const previewTitle = computed(() =>
  previewRow.value ? `票据预览 · ${previewRow.value.printerName} · 测试页` : '票据预览'
);

async function onPreview(row: PrinterRow): Promise<void> {
  previewVisible.value = true;
  previewLoading.value = true;
  previewData.value = null;
  previewRow.value = row;
  try {
    previewData.value = await previewPrinterTest(row.printerId);
  } catch {
    // 失败原因由请求拦截器统一弹出
    previewVisible.value = false;
  } finally {
    previewLoading.value = false;
  }
}

// onStatus 查询实时状态:飞鹅返回在线/缺纸状态与当日打印统计,直连只能探端口。
async function onStatus(row: PrinterRow): Promise<void> {
  try {
    const res = await printerStatus(row.printerId);
    const extra =
      res.todayPrinted !== undefined ? `；今日已打印 ${res.todayPrinted} 单，待打印 ${res.todayWaiting} 单` : '';
    messageByTheme(res.online, `${row.printerName}：${res.status}${extra}`);
  } catch {
    // 拦截器已提示
  }
}

function messageByTheme(online: boolean | undefined, text: string): void {
  if (online) MessagePlugin.success(text);
  else MessagePlugin.warning(text);
}

function onClear(row: PrinterRow): void {
  // 两条通道的队列位置不同(飞鹅在厂商云、本地代理在后端 tb_print_job),
  // 但「丢弃还没送出的、已打印的不受影响」这个语义是一样的。
  const where = row.provider === 'agent' ? '后端待代理取单的' : '飞鹅云端排队中的';
  // DialogPlugin 不会自动关闭,必须自己持有句柄并在确认回调里 hide。
  const dlg = DialogPlugin.confirm({
    header: '清空待打印队列',
    body: `确认清空「${row.printerName}」${where}打印任务？已打印过的单据不受影响。`,
    onConfirm: async () => {
      try {
        const res = await clearPrinterQueue(row.printerId);
        MessagePlugin.success(res?.msg || '已清空');
        dlg.hide();
        loadAgent();
      } catch {
        dlg.hide();
      }
    }
  });
}

function openBind(): void {
  Object.assign(bindForm, { sn: '', key: '', name: '', phone: '' });
  bindVisible.value = true;
}

async function doBind(): Promise<void> {
  if (!bindForm.sn || !bindForm.key) {
    MessagePlugin.warning('请填写打印机编号与识别码');
    return;
  }
  if (saving.value) return;
  saving.value = true;
  try {
    const res = await bindFeiePrinter({ ...bindForm });
    MessagePlugin.success(res?.msg || '绑定成功');
    bindVisible.value = false;
  } catch {
    // 拦截器已提示
  } finally {
    saving.value = false;
  }
}

function onDelete(row: PrinterRow): void {
  const dlg = DialogPlugin.confirm({
    header: '确认删除',
    body: `确认删除打印机【${row.printerName}】？`,
    onConfirm: async () => {
      try {
        await deletePrinter(row.printerId);
        MessagePlugin.success('删除成功');
        dlg.hide();
        load();
      } catch {
        dlg.hide();
      }
    }
  });
}

// 代理状态常驻展示,30s 刷新一次保持「在线/离线」不过时;
// 页面切后台/锁屏时暂停轮询(省流量与后端 QPS),回到可见立即补一轮并恢复节奏。
const AGENT_POLL_MS = 30_000;
let agentTimer: ReturnType<typeof setInterval> | null = null;

function startAgentPolling(): void {
  stopAgentPolling();
  agentTimer = setInterval(loadAgent, AGENT_POLL_MS);
}

function stopAgentPolling(): void {
  if (agentTimer) {
    clearInterval(agentTimer);
    agentTimer = null;
  }
}

function onVisibilityChange(): void {
  if (document.hidden) {
    stopAgentPolling();
  } else {
    loadAgent();
    startAgentPolling();
  }
}

onMounted(() => {
  load();
  loadFeie();
  loadAgent();
  loadCategories();
  startAgentPolling();
  document.addEventListener('visibilitychange', onVisibilityChange);
});

onUnmounted(() => {
  stopAgentPolling();
  document.removeEventListener('visibilitychange', onVisibilityChange);
});
</script>

<style scoped>
.tip {
  color: #999;
  font-size: 12px;
}

/* 顶部工具栏:动作按钮（绑定飞鹅打印机 / 新增打印机）必须固定在右上角。
   全局 .toolbar 是 flex-wrap:wrap,换行判定按各子项的 max-content 宽来算 ——
   这里提示文案很长(max-content 远超容器),按钮会被当作「放不下」换到第二行,
   而 space-between 对「独占一行」的单个元素等于左对齐,于是按钮跑到左下角。
   这里:提示列 flex-basis 置 0(换行判定时不再占宽),按钮不参与收缩,并让按钮
   对齐工具栏顶部,保证它始终贴右上角。 */
.toolbar {
  flex-wrap: nowrap;
  align-items: flex-start;
}

.toolbar > .tip {
  flex: 1 1 0;
  min-width: 0;
}

.toolbar :deep(.t-space) {
  flex: 0 0 auto;
}

/* 左侧提示:条目之间的行距比正文略放开,长时间盯配置页不挤。
   (不设 max-width:每行宽度由内容决定,不会被拉成超宽行;窄屏时靠容器自然回行。) */
.toolbar-tip {
  line-height: 1.7;
}

/* 每条通道一行:前面用小圆点做标记,文字用悬挂缩进 —— 条目换行时左边缘
   与首行文字对齐,不会跑到圆点下面,视觉上仍是「一条」。 */
.tip-list {
  margin: 4px 0 0;
  padding: 0;
  list-style: none;
}

.tip-list li {
  position: relative;
  padding-left: 14px;
}

.tip-list li + li {
  margin-top: 2px;
}

.tip-list li::before {
  content: '';
  position: absolute;
  left: 3px;
  top: 0.62em;
  width: 4px;
  height: 4px;
  border-radius: 50%;
  background: #c8c8c8;
}

.ml {
  margin-left: 8px;
}

.mt {
  margin-top: 4px;
  line-height: 1.6;
}

.mb {
  margin-bottom: 12px;
}

.mono {
  font-family: 'Courier New', monospace;
  font-size: 12px;
}

.agent-section {
  border: 1px solid #e7e7e7;
  border-radius: 6px;
  padding: 12px;
  background: #fafafa;
}

.agent-section-title {
  font-size: 14px;
  font-weight: 600;
  color: #333;
  margin-bottom: 8px;
}

.agent-item + .agent-item {
  margin-top: 10px;
  padding-top: 10px;
  border-top: 1px dashed #e7e7e7;
}

.agent-item-hd {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
}

/* 常驻状态总览条里的描述文字 */
.agent-status-text {
  color: #666;
  font-size: 12px;
}

.agent-name {
  font-weight: 600;
  color: #333;
}

.agent-item-grid {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 24px;
}

.agent-cell {
  display: flex;
  gap: 6px;
  font-size: 12px;
}

.agent-cell .k {
  color: #999;
}

.agent-cell .v {
  color: #555;
}

/* ===== 新增/编辑打印机弹窗排版 ===== */

/* TDesign 给「最后一个表单项」打 .t-form__item--last 并 margin:0(收掉末尾多余的
   24px 行距)。但它只统计带 name 的表单项来判定谁是最后一项:
   「打印参数」没有 name(它只是容器,不参与校验),「IP 地址 / 端口」又包在普通
   div.conn-row 里、不算 Form 的直接子项 —— 于是选「食客小票」隐藏掉「负责分类」
   后,这顶「最后一项」的帽子落到了前面的「接入方式」(飞鹅时是「打印机编号」)上,
   它与下方字段之间整段行距被清零。
   这里只给「被误标」的项拿回行距:真正的最后一项必然是 :last-of-type,仍保持收口。 */
.printer-form :deep(.t-form__item--last:not(:last-of-type)) {
  margin-bottom: var(--td-comp-margin-xxl, 24px);
}

/* TDesign 的 .t-form__controls-content 是 nowrap 的 flex 行:控件之外再放说明文字,
   必须自己包一层竖排容器,否则文字会和控件挤在同一行被压窄(仓库 Config.vue 同做法)。 */
.field-block {
  display: flex;
  flex-direction: column;
  gap: 8px;
  width: 100%;
  min-width: 0;
}

/* IP + 端口并排:用 flex 而不是 t-row/t-col —— TDesign 的 gutter 会以列内边距实现,
   首尾列各内缩 8px,导致这一行的标签和输入框与上下控件错开 8px。
   这里左列自适应、右列定宽,两端正好与控件列对齐。
   margin-bottom 必须自己补:它是普通 div 而不是 t-form__item,不会自动带上
   表单项的 24px 行距,少了它下一行(打印参数)会紧贴在这一行下面。 */
.conn-row {
  display: flex;
  gap: 16px;
  width: 100%;
  margin-bottom: var(--td-comp-margin-xxl, 24px);
}

.conn-ip {
  flex: 1 1 0;
  min-width: 0;
}

.conn-port {
  flex: 0 0 200px;
}

.conn-row.stacked {
  flex-direction: column;
  gap: 16px;
}

/* 竖排时 flex-basis 作用在纵轴(高度)上:不复位会把「端口」项撑成 200px 高,
   而「IP 地址」项的 flex-basis:0 会让它塌陷,两者都要复位。 */
.conn-row.stacked .conn-ip,
.conn-row.stacked .conn-port {
  flex: 0 0 auto;
}

/* TDesign 默认给 .t-form__controls 里的表单项加了 24px 右边距(为内联排布准备) */
.conn-row :deep(.t-form__item) {
  margin-bottom: 0;
  margin-right: 0;
}

/* 接入方式:三张可选卡片(标题 + 说明),选中态描边高亮。
   注意:card 内容里必须 normal 掉 TDesign 默认的 nowrap,否则说明文字会撑出卡片。 */
.provider-cards {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  align-items: stretch;
  gap: 10px;
  width: 100%;
  max-width: 100%;
}

.provider-cards.stacked {
  grid-template-columns: minmax(0, 1fr);
}

.provider-cards :deep(.t-radio) {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  height: auto;
  min-width: 0;
  margin: 0;
  padding: 10px 12px;
  border: 1px solid var(--td-component-stroke, #e7e7e7);
  border-radius: 9px;
  background: var(--td-bg-color-container, #fff);
  transition:
    border-color 0.15s,
    background-color 0.15s,
    box-shadow 0.15s;
}

.provider-cards :deep(.t-radio:hover) {
  border-color: var(--td-brand-color, #0052d9);
}

.provider-cards :deep(.t-radio.t-is-checked) {
  border-color: var(--td-brand-color, #0052d9);
  background: var(--td-brand-color-light, #f2f7ff);
}

/* 圆点与两行文字视觉对齐(gap 已负责间距,取消默认 margin-left) */
.provider-cards :deep(.t-radio__input) {
  flex-shrink: 0;
  margin-top: 3px;
}

.provider-cards :deep(.t-radio__label) {
  display: flex;
  flex-direction: column;
  gap: 2px;
  flex: 1;
  min-width: 0;
  margin-left: 0;
  white-space: normal;
}

.pc-title {
  font: var(--td-font-body-medium);
  font-weight: 600;
  line-height: 1.4;
  color: var(--td-text-color-primary, #333);
}

.pc-desc {
  font-size: 12px;
  line-height: 1.5;
  color: var(--td-text-color-placeholder, #999);
}

/* 随选说明:与控件列对齐的浅底色块,随通道切换只保留当前通道的注意事项。
   box-sizing 必须写:width:100% + 左右内边距,content-box 下右侧会戳出容器 24px,
   底色块比上面的卡片和下面的输入框都宽出一截。 */
.provider-hint {
  box-sizing: border-box;
  width: 100%;
  padding: 8px 12px;
  border-radius: 6px;
  background: var(--td-bg-color-secondarycontainer, #f5f5f5);
  font-size: 12px;
  line-height: 1.7;
  color: var(--td-text-color-secondary, #666);
}

/* 打印参数:纸宽 / 份数 / 状态一行排布 */
.print-opts {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px 32px;
  width: 100%;
}

.opt {
  display: flex;
  align-items: center;
  gap: 8px;
}

.opt-label {
  font-size: 13px;
  color: var(--td-text-color-secondary, #666);
}

.opt-value {
  font-size: 13px;
  color: var(--td-text-color-primary, #333);
}
</style>
