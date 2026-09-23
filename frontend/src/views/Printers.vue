<template>
  <div class="page-card">
    <div class="toolbar">
      <span class="tip">
        三条通道任选：<b>网络直连</b>要求后端与打印机在同一局域网（后端拼 ESC/POS 走 IP:9100，无需收银电脑开机）；
        <b>飞鹅云</b>由打印机自己联网取单；<b>本地代理</b>由门店内网的代理程序取单后直发打印机，
        后端部署在云服务器时想继续用已有的 9100 网络机，选它（<b>不需要装任何打印机驱动</b>）。
      </span>
      <t-space v-if="canEdit">
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

    <t-alert
      v-if="!feie.configured && hasFeiePrinter"
      theme="warning"
      class="mb"
    >
      <template #message>
        飞鹅云打印尚未配置账号 —— 通道选「飞鹅云」的打印机无法出纸。请到 <b>系统配置 → 小票打印</b> 填写飞鹅账号与
        UKEY。
      </template>
    </t-alert>

    <t-alert
      v-if="hasAgentPrinter && !agent.configured"
      theme="warning"
      class="mb"
    >
      <template #message>
        本地打印代理尚未配置令牌 —— 通道选「本地代理」的打印机只会把票据排队，没有人来取单。 请到
        <b>系统配置 → 小票打印</b> 填写代理令牌，并在门店内网运行 print-agent（见 docs/print-agent.md）。
      </template>
    </t-alert>
    <t-alert
      v-else-if="hasAgentPrinter && !agent.online"
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
      v-else-if="hasAgentPrinter && agent.dead"
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
      v-if="hasAgentPrinter && (agent.pending > 20 || agent.oldestPendingSec > 300)"
      theme="warning"
      class="mb"
    >
      <template #message>
        本地打印代理积压 <b>{{ agent.pending }}</b> 单<template v-if="agent.oldestPendingSec > 0"
          >，最老单据已等 <b>{{ agentWaitMinutes }}</b> 分钟</template
        >。请检查门店代理与打印机是否正常。
      </template>
    </t-alert>

    <!-- 常驻状态总览:只要配置了代理令牌或存在 agent 通道打印机就始终可见,
         在线/离线一眼即明(30s 自动刷新,与后端 agentOnlineWindow 90s 同口径)。 -->
    <div
      v-if="agent.configured || hasAgentPrinter"
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
          <template v-if="canEdit">
            <t-button
              theme="primary"
              variant="text"
              size="small"
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
          <template v-if="canEdit">
            <t-button
              theme="primary"
              variant="text"
              size="small"
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

    <t-dialog
      v-model:visible="dialogVisible"
      :header="form.printerId ? '编辑打印机' : '新增打印机'"
      :width="isMobile ? '94vw' : '560px'"
      :confirm-btn="{ content: '保存', theme: 'primary' }"
      @confirm="save"
    >
      <t-form
        :data="form"
        :label-width="isMobile ? '90px' : '110px'"
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
          label="接入方式"
          name="provider"
        >
          <t-radio-group v-model="form.provider">
            <t-radio value="tcp"> 网络直连（同局域网） </t-radio>
            <t-radio value="feie"> 飞鹅云打印 </t-radio>
            <t-radio value="agent"> 本地打印代理（云端部署） </t-radio>
          </t-radio-group>
          <div class="tip mt">
            后端部署在云服务器时选<b>本地代理</b>：后端只把票据排队，门店内网的 print-agent 取单后直发下面这个
            IP:9100。仍然<b>不需要装打印机驱动</b>。
          </div>
        </t-form-item>
        <t-form-item
          label="打印机类型"
          name="printerType"
        >
          <t-radio-group v-model="form.printerType">
            <t-radio :value="1"> 厨房单 </t-radio>
            <t-radio :value="2"> 食客小票 </t-radio>
          </t-radio-group>
        </t-form-item>

        <template v-if="form.provider !== 'feie'">
          <t-form-item
            label="IP 地址"
            name="ip"
          >
            <t-input
              v-model="form.ip"
              placeholder="打印机网络 IP，如 192.168.1.8"
            />
            <div
              v-if="form.provider === 'agent'"
              class="tip mt"
            >
              填<b>打印机在门店内网的地址</b>：云后端不会去连它，由门店那台跑着 print-agent 的
              电脑去连，所以这里必须是代理能访问到的地址（不要填 127.0.0.1，除非代理和打印机在同一台机器上）。
            </div>
          </t-form-item>
          <t-form-item
            label="端口"
            name="port"
          >
            <t-input-number
              v-model="form.port"
              :min="1"
              :max="65535"
            />
            <span class="tip ml">热敏打印机默认 9100</span>
          </t-form-item>
        </template>
        <template v-else>
          <t-form-item
            label="打印机编号"
            name="feieSn"
          >
            <t-input
              v-model="form.feieSn"
              placeholder="飞鹅打印机机身标签上的 SN"
            />
          </t-form-item>
          <t-form-item label=" ">
            <span class="tip">若该 SN 还没绑定到账号，请先用右上角「绑定飞鹅打印机」录入 SN 与识别码。</span>
          </t-form-item>
        </template>

        <t-form-item
          label="纸宽"
          name="paperWidth"
        >
          <t-radio-group v-model="form.paperWidth">
            <t-radio :value="32"> 58mm </t-radio>
            <t-radio :value="48"> 80mm </t-radio>
          </t-radio-group>
        </t-form-item>
        <t-form-item
          label="打印份数"
          name="copies"
        >
          <t-input-number
            v-model="form.copies"
            :min="1"
            :max="5"
          />
          <span class="tip ml">同一张单据连续出几份（1-5）</span>
        </t-form-item>
        <t-form-item
          v-if="form.printerType === 1"
          label="负责分类"
          name="categoryIdList"
        >
          <t-select
            v-model="form.categoryIdList"
            :options="categoryOptions"
            multiple
            clearable
            placeholder="不选 = 本机打印全部菜品"
          />
          <div class="tip mt">
            用于多台厨房机分工：例如「凉菜机」只勾凉菜素菜、「热菜机」只勾荤菜与特色农家菜。
            本单没有它负责的菜品时不会出空白单（食客小票始终含全部菜品）。
          </div>
        </t-form-item>
        <t-form-item
          label="状态"
          name="status"
        >
          <t-switch
            v-model="form.status"
            :custom-value="[1, 0]"
          />
          <span class="ml tip">{{ form.status === 1 ? '启用（下单/结账会自动出纸）' : '停用' }}</span>
        </t-form-item>
      </t-form>
    </t-dialog>

    <t-dialog
      v-model:visible="bindVisible"
      header="绑定飞鹅打印机到账号"
      :width="isMobile ? '94vw' : '520px'"
      :confirm-btn="{ content: '绑定', theme: 'primary' }"
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
  PRINTER_PROVIDER
} from '../api';
import { useIsMobile } from '../utils/useMobile';
import { hasPerm } from '../utils/perm';
import type { MenuCategory, PrintAgent, Printer, PrinterPayload } from '../types/entities';

// 列表行在模板/操作函数里要把 printerId 传给各打印机接口(参数为 number | string),
// 并拿 provider 当字典下标,Printer 字段全可选,这里收紧为必填。
type PrinterRow = Printer & { printerId: number; provider: string };

const list = ref<PrinterRow[]>([]);
const loading = ref(false);
const dialogVisible = ref(false);
const bindVisible = ref(false);
const categories = ref<MenuCategory[]>([]);

interface FeieState {
  configured: boolean;
  user: string;
}

const feie = reactive<FeieState>({ configured: false, user: '' });
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

const categoryOptions = computed(() => categories.value.map(c => ({ label: c.categoryName, value: c.categoryId })));

// 通道中文名统一从 api 的枚举取,避免这里再硬编码一遍导致两处漂移。
const providerLabel = (p?: string): string => PRINTER_PROVIDER[p || '']?.label || '网络直连';

// 有没有用到某条通道的打印机 —— 决定要不要显示对应的「未配置/离线」告警。
// 只在真正用得上时提示,否则没用飞鹅的门店会一直看到一条无关的黄色告警。
const hasFeiePrinter = computed(() => list.value.some(r => r.provider === 'feie'));
const hasAgentPrinter = computed(() => list.value.some(r => r.provider === 'agent'));

// 最老待打印单已等分钟数(向上取整);老后端没有 oldestPendingSec 时为 0。
const agentWaitMinutes = computed(() => Math.ceil((agent.oldestPendingSec || 0) / 60));

// 与后端 agentOnlineWindow(90s)一致的在线判定:最近心跳距今 < 90 秒。
// lastSeen 为「YYYY-MM-DD HH:mm:ss」本地时间,补 T 后由浏览器按本地时区解析。
function isAgentOnline(row: PrintAgent): boolean {
  const seen = row.lastSeen || '';
  if (!seen) return false;
  const t = new Date(seen.replace(' ', 'T')).getTime();
  return !Number.isNaN(t) && Date.now() - t < 90_000;
}

// 清空队列:仅飞鹅云与本地代理有「云端/队列」概念,直连是即发即走。
const canClearQueue = (row: PrinterRow): boolean => row.provider === 'feie' || row.provider === 'agent';

// 无 printer:edit 时只保留「状态」查询(GET printer/status/:id → printer:view),
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
  // 只读时操作列只剩「状态」,宽度收窄,免得右侧留一大片空白。
  cols.push({ colKey: 'op', title: '操作', width: canEdit.value ? 280 : 90 });
  return cols;
});

async function load(): Promise<void> {
  loading.value = true;
  try {
    const res = await listPrinters();
    list.value = res.items as PrinterRow[];
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
    agent.configured = false;
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
  // 食客小票必须含全部菜品，后端也会强制清空分类，这里保持一致避免误导。
  const payload: PrinterPayload = { ...form, printerId: form.printerId ?? undefined };
  if (payload.printerType === 2) payload.categoryIdList = [];
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
  }
}

async function onTest(row: PrinterRow): Promise<void> {
  try {
    const res = await testPrinter(row.printerId);
    MessagePlugin.success(res?.msg || '测试指令已发送');
  } catch {
    // 失败原因（含飞鹅返回码释义）由请求拦截器统一弹出
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
  try {
    await bindFeiePrinter({ ...bindForm });
    MessagePlugin.success('绑定成功');
    bindVisible.value = false;
  } catch {
    // 拦截器已提示
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
</style>
