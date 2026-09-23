# 本地打印代理 v2 设计方案

> 状态：设计稿（2026-09-22）
> 范围：`backend/internal/print/agent.go`、`backend/internal/store/printjob.go`、
> `backend/cmd/print-agent/`、`backend/internal/handler/agent.go` 及配套构建/部署。
> v1 部署手册见 `docs/print-agent.md`（升级后仍适用，本文档只写差异与新增）。

---

## 一、背景：v1 做对了什么，差在哪

v1 的可靠性骨架是健全的，v2 全部保留、不动摇：

| v1 已做对 | 位置 |
|---|---|
| at-least-once 投递 + `deliveryId` 幂等号去重（先 fsync 落盘再回执） | `cmd/print-agent/main.go` jobState |
| 60 秒取单租约，代理被杀/断电任务自动回队 | `store/printjob.go` ClaimPrintJobs |
| 队列（`tb_print_job`，待办）与台账（`tb_print_log`，长期）分表 | `store/printjob.go` 顶部注释 |
| 条件 UPDATE + 受影响行数实现多代理原子抢单 | 同上 |
| 心跳双轨：内存即时 + 15s 限流落库 | `print/agent.go` MarkAgentSeen |
| 代理鉴权失败不写审计日志（防轮询刷爆审计表） | `handler/agent.go` agentAuth |
| 「傻管道」：ESC/POS 编码在云端取单时现场做，代理不懂协议 | `print/agent.go` PullAgentJobs |

四个真实短板（v2 要解决的）：

1. **打印结果盲发**：`ok=true` 只代表「字节写进 socket」。9100 端口是全双工 TCP，
   ESC/POS 有 `DLE EOT` 实时状态指令，v1 写完立刻关连接一个字节都不读。
   后厨厨房单卡纸没打出来、日志却显示「已送出」——**厨房单丢单 = 菜没做**。
2. **陈旧任务不过期**：`pending` 无限期积压。代理离线一周后恢复，上周的厨房单
   按序全部吐出，**后厨会照着过期单做菜**。
3. **多代理语义不闭环**：心跳是全局单值，两台代理死一台看不出来；各代理幂等
   state 文件独立，「一台打印机只应由一台代理负责」只写在文档里，系统零约束。
4. **协议无版本协商**：哪天要加能力（如状态回读），门店代理升级不受控，没有
   协商机制就只能赌。

另有三个次级问题顺手修：离线打印机拖慢全店出票（串行 + 每条 5s 超时占坑）、
GBK 编码失败静默回退 UTF-8（菜名带 emoji 打乱码但记成功）、`RetryPrintJob`
先 SELECT 再 UPDATE 非原子。

---

## 二、设计目标与非目标

### 目标

1. 打印结果可确认：代理向打印机回读状态，云端如实记录；
2. 陈旧任务自动作废：按单据类型设 TTL，过期厨房单绝不出纸；
3. 协议 v2：版本协商 + 能力声明，为所有后续演进留门，v1 代理零感知共存；
4. 离线打印机熔断：一台机器关机不拖慢其它打印机出票；
5. 多代理身份体系：per-agent 令牌与心跳，可吊销、可观测；
6. 出纸延迟可选降到亚秒（长轮询，不引入 WebSocket）；
7. **代理覆盖 macOS + Windows + Linux**：一套 Go 源码、五平台交叉编译产物。

### 非目标

- 不引入 WebSocket/SSE/MQTT 等长连接推送（轮询 + 长轮询已够餐饮场景，
  保持「出站 HTTP」的全部穿透优势）；
- 不做打印机端事务回执（「写入成功 → 打印头真的出纸」的物理确认，应用层无法达成）；
- 不改 `payload` 的语义（仍是 base64 ESC/POS 字节流，协议演进只加 JSON 字段）；
- 不做按订单维度的打印优先级（队列按 `job_id` FIFO，业务上单据都是即时的）。

---

## 三、v2 总体链路

```
┌───────────────────── 云后端(多实例) ─────────────────────┐
│                                                            │
│  下单/加菜/结账/补打/测试                                   │
│        │ print.go 编排层(不变)                             │
│        ▼                                                   │
│  enqueueAgentTicket ──> tb_print_job(入队时算好过期时刻)   │
│        │                    │                             │
│        │ 记「排队中」日志    │ StartAgentCleanup 周期任务   │
│        ▼                   │   ├─ 结案任务按保留期清理(不变)│
│   tb_print_log             │   └─ pending/claimed 超过 TTL │
│        ▲                   │        → 置 dead + 日志回写   │
│        │ 回写结果           │        「已过期自动作废」      │
│        │                   ▼                             │
│  AckAgentJob <──── pull(带 version=2, wait=长轮询)         │
│        ▲              │ 只取「未过期」的候选               │
│        │              │ 按 tb_print_agent 校验 per-agent  │
│        │              │   令牌(未命中回落全局 legacy 令牌)  │
└────────┼──────────────┼─────────────────────────────────────┘
         │              │ HTTPS 出站(443)
         │ ack(ok + printerStatus + 代理版本)│ pull(base64 ESC/POS + protoVersion)
         │              ▼
         │      ┌── 门店内网:print-agent v2(一套源码,五平台编译) ──┐
         │      │  ① 按 addr 熔断:30s 内连不上的 IP 挂起、不回执    │
         │      │  ② TCP 打印机IP:9100:写 payload × copies        │
         │      │  ③ DLE EOT 2/3/4 回读状态(不应答机型自动负面缓存)│
         │      │  ④ 幂等 state 文件先 fsync 再 ack(v1 机制,不变) │
         │      └────────────────────────────────────────────────┘
         │                    │
         └── 缺纸/卡纸/盖开 → 打印日志 detail 如实记录 + 管理端可见
```

链路不变量（v1 → v2 均成立）：

- 门店零入站端口、零公网 IP、零 VPN；
- 代理对打印协议的认知仍限于「写字节 + 读 4 字节应答」，排版与编码始终在云端；
- 任何一条失败路径最终都会落到 `tb_print_log`，商家端可查、可补打。

---

## 四、协议 v2 规范

### 4.1 版本协商

**代理侧**：`print-agent` 启动即知自身版本（`ldflags -X main.version` 注入，
`--version` 可查）。三个接口的请求头统一带：

```
X-Agent-Version: 2          # v1 代理不带此头 → 服务端按 v1 对待
X-Agent-Name: 门店收银电脑   # 与 body 的 agentId 一致,便于网关日志对账
```

**服务端**：三个接口的响应 `data` 统一新增：

```json
{ "protoVersion": 2, "serverCapabilities": ["printer-status", "long-pull"] }
```

### 4.2 兼容性原则（写死，防未来走样）

| 规则 | 说明 |
|---|---|
| 只增字段，不改语义 | v1 代理是 Go json 解析，忽略未知字段 → 服务端先升级即安全 |
| 新指令永远不进 `payload` | `payload` 恒为 base64 ESC/POS；新交互一律走新 JSON 字段 |
| 新行为由代理能力声明驱动 | 服务端对未声明能力的代理，行为与 v1 完全一致 |
| v1 令牌长期保留 | 全局 `agent_token` 是 legacy 兼容层，不删（见 §7 多代理） |

### 4.3 接口变更总表

| 接口 | 变更 |
|---|---|
| `ping` | 请求体新增 `version int`、`capabilities []string`；响应新增 `protoVersion`、`serverCapabilities`、`oldestPendingSec`（最老 pending 任务已等待秒数，可观测性）。代理启动自检打印版本与云端能力 |
| `pull` | 请求体新增 `version`、`capabilities`、`wait int`（0–30 秒长轮询，0 = v1 行为）；响应新增 `protoVersion`、`serverCapabilities`；`jobs[]` 字段不变 |
| `ack` | `results[]` 每条新增可选 `printerStatus`；请求头 `X-Agent-Version` 标记来源版本 |

`printerStatus` 结构（缺省 = 代理未查或机型不应答，不影响 `ok` 判定）：

```json
{ "ok": true,
  "printerStatus": {
    "queried":   true,      // 是否成功读到 DLE EOT 应答
    "raw":       "0200/0400",  // 各查询的原样 hex,机型差异大,存原文备查
    "paperOut":  false,     // 纸尽
    "paperNearEnd": false,  // 纸将尽
    "coverOpen": false,     // 盖板开
    "paused":    false,     // 被按键暂停
    "error":     false      // 不可恢复错误
  } }
```

---

## 五、能力一：打印结果确认（DLE EOT 状态回读）

### 5.1 指令与应答

打印数据写完后、关连接前，在**最后一份**的连接上补发三条查询：

| 查询 | 字节 | 能发现 |
|---|---|---|
| `DLE EOT 2` 脱机原因 | `1D 04 02` | 盖板开、按键暂停、缺纸 |
| `DLE EOT 3` 错误原因 | `1D 04 03` | 切纸错误、不可恢复错误 |
| `DLE EOT 4` 纸传感器 | `1D 04 04` | 纸将尽、纸尽 |

应答均为 `1D 04 n s` 共 4 字节。bit 语义按 ESC/POS 标准（如 n=2 的 bit2=盖开、
bit5=缺纸；n=4 的 bit2/bit5=纸将尽、bit3/bit6=纸尽），**机型实现参差，
解读仅作辅助展示，原始字节 `raw` 一并回传云端存档**。

### 5.2 行为细节

- **时机**：写完数据 `sleep 300ms`（让走纸进行）再查询，`SetReadDeadline(500ms)`
  逐条读应答；
- **不应答机型**：某 `IP:port` 首次查询全部超时/乱码 → 记入进程内负面缓存
  （30 分钟 TTL），期间对该打印机跳过查询、`queried=false`——**不为不支持的老
  机型付出每单 1.5s 的白等**；
- **copies > 1**：只查最后一份所在的连接（同一物理打印机，中间份的状态由最后
  一份代表）；
- **`ok` 判定不变**：`ok` 仍表示「字节成功写入」。`printerStatus` 是附加观测
  数据——读到「纸尽/盖开」时云端在打印日志 `detail` 里写
  「已送出(打印机报告缺纸)」，商家一眼看出是纸的问题而不是网络问题；
- **ProbePrinter 联动**：管理端「测试连接」时，云端无法探测门店打印机，但
  `AgentInfo` 响应里汇总最近一次 ack 回报的各打印机状态（见 §9 可观测性）。

### 5.3 云端落地

`AckAgentJob` 收到 `printerStatus.paperOut/coverOpen/...` 为 true 时，
日志 `detail` 拼接中文摘要（如「已送出，但打印机报告:缺纸」）；`raw` 追加在
括号里。无该字段（v1 代理）→ 行为与现在完全一致。

---

## 六、能力二：任务 TTL 过期作废

### 6.1 语义

**过期 = 作废，不是「尽快打出来」**。陈旧厨房单突然出纸是事故，宁可不打
（日志有记录、可人工补打）。

| 单据类型 | TTL 默认 | 环境变量 | 理由 |
|---|---|---|---|
| kitchen（厨房单/加菜单） | 2 小时 | `AGENT_JOB_TTL_KITCHEN_MIN` | 过了饭口的厨房单没有出纸价值 |
| guest（食客小票） | 24 小时 | `AGENT_JOB_TTL_GUEST_MIN` | 隔夜的小票仅剩对账价值，保留久一点 |
| test / 其它 | 30 分钟 | `AGENT_JOB_TTL_TEST_MIN` | 测试页过期即废 |

- **零表结构变更**：以 `create_time` 推算过期，不新增列、不迁移；
- **取单侧过滤**：`ClaimPrintJobs` 候选 SQL 按 `doc_type` 分别加
  `create_time > cutoff` 条件（三类 OR 组合），过期任务不再下发；
- **清理侧作废**：`StartAgentCleanup`（每 10 分钟）新增一步：把
  `status IN (pending, claimed)` 且已过 TTL 的任务置 `dead`，
  `last_error='任务过期自动作废(TTL)'`，并
  `UpdatePrintLogResult(failed, '任务未在有效期内送出,已自动作废,可人工补打')`；
- **误伤面**：`claimed` 且过期的作废存在误伤（代理正拿着、马上要打完）——
  概率极低（claim 距过期至少已 TTL 时长），且日志注明原因可补打，业务可接受；
- **补打不受影响**：`Reprint` 走实时路径，不入队。

---

## 七、能力三：多代理身份体系（per-agent 令牌与心跳）

### 7.1 新表 `tb_print_agent`（唯一的表结构变更）

```sql
CREATE TABLE IF NOT EXISTS tb_print_agent (
    agent_id    INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    agent_name  VARCHAR(64)  DEFAULT '',   -- 展示名,如「收银电脑」「后厨树莓派」
    token_hash  VARCHAR(64)  DEFAULT '',   -- SHA-256(令牌) hex,不存明文
    token_hint  VARCHAR(8)   DEFAULT '',   -- 令牌前 4 位,列表页辨认用
    printer_ids VARCHAR(255) DEFAULT '',   -- 授权打印机 id 逗号串;空 = 全部
    status      INT          DEFAULT 1,    -- 1=启用 0=吊销
    last_seen   VARCHAR(32),               -- 本代理最近心跳
    last_report VARCHAR(64)  DEFAULT '',   -- 最近上报的 agentId/主机名/版本
    create_time VARCHAR(32),
    update_time VARCHAR(32)
);
```

（SQLite 版同构，走 `migrations/incr/{mysql,sqlite}/` 双版本 + full schema 更新。）

### 7.2 鉴权与兼容（双轨过渡）

```
agentAuth(tok):
  1. SELECT * FROM tb_print_agent WHERE status=1
     → 逐行 SHA-256(tok) 与 token_hash 恒定时间比较
     → 命中:绑定该 agent_id;printer_ids 非空时,取单只下发授权打印机
  2. 未命中 → 回落全局 cfg「agent_token」(legacy,与 v1 完全一致)
  3. 都不中 → 401
```

- 老代理继续用全局令牌，**不升级也能跑**；
- 新代理用管理端逐台签发的令牌：单台泄漏可单独吊销，不用全门店轮换；
- `printer_ids` 授权域从根上抑制「拿到令牌 = 拉走全部队列含金额」的爆炸半径。

### 7.3 心跳与在线判定

- `tb_print_agent.last_seen` 按 agent 更新（落库限流 15s 不变）；
- 全局 `agent_last_seen`（cfg）继续维护 = legacy 代理的心跳；
- `AgentOnline()` = 「任一启用代理或 legacy 在 90s 窗口内有心跳」；
- **state 文件共享约束保持**：M4 仍不解决多代理各自 state 导致的重复打印——
  一台打印机仍只应由一台代理负责，`printer_ids` 授权域正是让这一约束可执行的
  手段（按打印机划分代理辖区）。

### 7.4 管理端

- `GET /printer/agent/info` v2 响应：`agents: [{name, lastSeen, report, scope}]` +
  全局 `online/pending/dead`（老字段保留，前端平滑升级）；
- 系统配置页新增「打印代理」区块：列表 / 新增（生成随机令牌，只回显一次）/
  吊销 / 查看最近心跳与版本；
- 前端 `Config.vue`、`Printers.vue` 对应扩展。

---

## 八、能力四：离线打印机熔断 + 长轮询

### 8.1 熔断（纯代理侧，零协议变更）

- 进程内 `map[addr]cooldownUntil`：对某 `IP:port` 拨号失败 → 该地址冷却 **30** 秒；
- 冷却期内的任务**不拨号、也不回执**（挂起），任务保持 claimed 直到 60 秒租约到期
  自动回到队列 —— 之所以不能照原设计「直接 ack 失败」：云端在 **claim 取单时**就
  `attempts+1`（上限 3），冷却期内回执失败会让任务 10 秒后就被重新取走，
  3 次额度全被「根本没拨过打印机的空转」吃光，打印机只是短暂不可达也会被判成
  「已放弃」而丢单。冷却 30s < 租约 60s，保证租约到期时冷却已结束、下次是真实拨号；
- 效果：一轮 10 条任务里 3 条属于离线打印机时，等待从 `3×5s` 变成 `1×5s`，
  其它打印机的单据不再被拖着；
- 不持久化，代理重启清零（可接受：重启本身就是一次重置）。

### 8.2 长轮询（`wait` 参数）

- 请求 `pull` 带 `wait: 25`（0–30，默认 0 = v1 短轮询行为）；
- 服务端：`wait>0` 时 hold 最多 N 秒——进程内「有新任务」notify channel，
  外加每 2 秒 DB 兜底扫描（多实例部署时他实例入队本实例收不到 notify）；
  取到任务或超时立即返回。已确认主服务 `http.Server` 未设 WriteTimeout，
  无服务端约束；**唯一部署注意**：nginx `proxy_read_timeout` ≥ 40s（默认 60s，
  已满足，`deploy/setup-nginx.sh` 加一行检查）；
- 代理侧：默认 `wait = interval`（即拉满），HTTP client 超时提到 35s；
  网络差的环境可 `PRINT_AGENT_WAIT=0` 退回纯轮询；
- 效果：出纸延迟从「0–3s 轮询 + 拉单」降到亚秒，且空闲时请求频率反而降低
  （一次 hold 顶若干次轮询）。

---

## 九、可观测性补强（小改动合集）

| 项 | 做法 |
|---|---|
| 出纸延迟可见 | `tb_print_log` 已有 `cost_ms`（agent 通道当前无意义）；v2 在 ack 时回写 `cost_ms = done_time - create_time`，日志页展示「排队 → 送出」耗时 |
| 队列积压告警 | `AgentInfo` 响应新增 `oldestPendingSec`；前端打印机管理页 pending > 20 或最老单据 > 5 分钟时黄条提示 |
| 代理版本可见 | ping 上报 `version`，`tb_print_agent.last_report` 存「名称/版本」，管理端能看出哪家门店还没升级 v2 |
| 代理本地日志 | 代理侧已有 stdout 日志；`--once` 模式下额外输出每条任务的 DLE EOT 应答原文，便于现场排障机型兼容性 |

---

## 十、附带修复（不占里程碑，随 M1 顺手改）

| 问题 | 修法 |
|---|---|
| `escpos.go gbk()` 编码失败静默回退 UTF-8，菜名带 emoji 打乱码却记成功 | 逐 rune 重编码，无法映射的字符替换为 `?`，并 `logger.Warnf` 记录原始行；tcp/agent 通道同时受益 |
| `RetryPrintJob` 先 SELECT attempts 再 UPDATE，两代理并发 ack 同一 job 有竞态 | 改为单条 `UPDATE ... SET status=CASE WHEN attempts>=? THEN dead ELSE pending END ... WHERE job_id=? AND status=claimed` 原子完成 |
| 代理二进制无版本号 | `main.go` 增加 `version/buildTime/gitCommit` 变量（Makefile 的 LDFLAGS 已在注入，目前是空注入）+ `--version` |

---

## 十一、跨平台与构建（macOS + Windows + Linux，一套源码）

### 11.1 现状结论

`cmd/print-agent/main.go` 仅用标准库（net/os/signal/filepath/flag），`CGO_ENABLED=0`
静态编译，**源码层面已经是跨平台的**，缺的只是构建目标与平台细节。

### 11.2 构建矩阵（Makefile `build-agent` 扩展）

| 产物 | GOOS/GOARCH | 用途 |
|---|---|---|
| `print-agent-darwin-amd64` | darwin/amd64 | Intel Mac |
| `print-agent-darwin-arm64` | darwin/arm64 | Apple Silicon Mac |
| `print-agent-windows-amd64.exe` | windows/amd64 | 收银电脑（已有） |
| `print-agent-linux-amd64` | linux/amd64 | 小主机（已有） |
| `print-agent-linux-arm64` | linux/arm64 | 树莓派（已有） |

新增 `build-agent-darwin-amd64` / `build-agent-darwin-arm64` 两个目标，
`build-agent` 汇总五平台；`release.sh` 打包清单同步。

### 11.3 Windows 细节

| 事项 | 处理 |
|---|---|
| 控制台中文乱码 | cmd 默认 GBK 代码页，Go stdout 写 UTF-8 会花屏。启动时检测 stdout 为控制台则 `SetConsoleOutputCP(65001)`（`syscall.NewLazyDLL("kernel32.dll")`，零新增依赖）；重定向到文件/journalctl 时本就是 UTF-8 不受影响 |
| 信号 | 现有 `signal.Notify(SIGINT, SIGTERM)` 在 Windows 的 Go 运行时下可编译可用（Ctrl+C 与任务终止均有响应），无需改 |
| 自启动 | v1 文档的 `schtasks /sc onlogon` 要求用户登录；v2 文档改为优先推荐**任务计划程序「开机时触发 + 不管用户是否登录 + 失败重启」**图形配置（导出 XML 供门店直接导入），`shell:startup` 作为简化备选。不引入 Windows 服务框架，保持单文件零依赖 |
| SmartScreen 告警 | 未签名 exe 首次运行会弹「已保护你的电脑」，文档注明「更多信息 → 仍要运行」 |

### 11.4 macOS 细节

| 事项 | 处理 |
|---|---|
| Gatekeeper | 未签名二进制首次运行被拦：`xattr -d com.apple.quarantine print-agent-darwin-*` 或右键打开；部署文档写明（Apple Silicon 用户大概率遇到） |
| 自启动 | `launchd` plist：`~/Library/LaunchAgents/com.cjfarm.print-agent.plist`，`RunAtLoad + KeepAlive`，`ProgramArguments` 直接传 `--server/--token` 或依赖同目录 `agent.env`；提供 plist 模板进 `deploy/` |
| 架构选择 | 文档给一行判别命令（`uname -m`：`arm64` 用 arm64 版，`x86_64` 用 amd64 版） |

### 11.5 `agent.env` 与状态文件

两者默认取「可执行文件同目录」，路径拼接已用 `filepath.Join`，跨平台成立；
macOS 的 LaunchAgent 场景下同目录约定同样有效。

---

## 十二、灰度与升级路径

升级顺序有硬约束：**云端先上，代理后上**（反向则新代理对老云端发
`X-Agent-Version` 被忽略、`wait` 被当未知字段丢弃，虽无害但失去意义）。

| 阶段 | 云端 | 门店代理 | 行为 |
|---|---|---|---|
| 0（现状） | v1 | v1 | 现状 |
| 1 | v2 | v1 | TTL 作废/GBK 修复/RetryPrintJob 原子化已生效；pull/ack 对 v1 代理行为不变（新字段被忽略） |
| 2 | v2 | v2（按门店逐台换） | 换一台享受一台：状态回读 + 熔断 + 长轮询；管理端可看到各代理版本，据此推进 |
| 3 | v2 | 全 v2 | 可开始用多代理身份体系签发 per-agent 令牌；全局 legacy 令牌继续可用，直到主动清空 |

验证口径：每阶段回归「模拟回执丢失重发不重复出纸」「代理离线任务积压不丢」
（v1 已有测试），v2 新增「DLE EOT 负面缓存机型 100 单压测无 1.5s/单退化」
「TTL 过期任务不再出纸且日志可补打」「熔断生效时同轮异 IP 任务零额外等待」。

---

## 十三、变更清单汇总

| 层 | 文件 | 变更 | 里程碑 |
|---|---|---|---|
| 云端 | `store/printjob.go` | TTL 过滤 + 过期作废 + `RetryPrintJob` 原子化 | M1 |
| 云端 | `print/escpos.go` | `gbk()` 失败替换 `?` + 告警 | M1 |
| 云端 | `handler/agent.go` | 协议 v2 字段（version/capabilities/wait/printerStatus）+ per-agent 鉴权 + 长轮询 | M2/M4 |
| 云端 | `print/agent.go` | `AckAgentJob` 记录 printerStatus 摘要；心跳 per-agent；`AgentInfo` v2 | M2/M4 |
| 云端 | `migrations/incr/{mysql,sqlite}` + full schema | 新表 `tb_print_agent` | M4 |
| 云端 | `main.go` 路由 | 新增代理管理接口（列表/新增/吊销） | M4 |
| 代理 | `cmd/print-agent/main.go` | 版本变量与上报、DLE EOT 回读 + 负面缓存、熔断、长轮询 wait、Windows 控制台 UTF-8 | M2 |
| 代理 | `cmd/print-agent/main.go` | `--version` | M2 |
| 构建 | `backend/Makefile` | darwin amd64/arm64 目标 | M3 |
| 构建 | `release.sh`、`deploy/` | 五平台打包、launchd plist 模板、Windows 任务计划 XML、nginx timeout 检查 | M3 |
| 前端 | `Config.vue`、`Printers.vue`、`PrintLogs.vue` | 代理管理区块、版本/状态展示、积压黄条 | M4 |
| 文档 | `docs/print-agent.md` | 补 macOS/Windows 部署章节与新状态说明 | M3 |

环境变量与配置新增汇总：

| 名 | 默认 | 作用 |
|---|---|---|
| `AGENT_JOB_TTL_KITCHEN_MIN` | 120 | 厨房单 TTL（分钟，≤0 视为不过期） |
| `AGENT_JOB_TTL_GUEST_MIN` | 1440 | 食客小票 TTL |
| `AGENT_JOB_TTL_TEST_MIN` | 30 | 测试页 TTL |
| `PRINT_AGENT_WAIT`（代理侧） | 25 | pull 长轮询秒数，0 = 退回纯轮询 |
| cfg `agent_token`（已有） | — | legacy 全局令牌，长期保留 |

---

## 十四、里程碑

| 里程碑 | 内容 | 特点 |
|---|---|---|
| **M1 云端止血**（约 1 天） | TTL 过期作废、GBK 兜底、RetryPrintJob 原子化 | 零迁移、零代理升级，当天可发 |
| **M2 代理 v2.0.0**（约 2 天） | 协议 v2 协商、DLE EOT 状态回读 + 负面缓存、熔断、版本上报 | 云端 handler 同步改；代理与云端一起发版 |
| **M3 跨平台与部署**（约 1 天） | Makefile 五平台、macOS/Windows 自启动物料、长轮询（含 nginx 检查）、`docs/print-agent.md` 更新 | 纯构建与文档；与 M2 合并出一个 v2.0.0 代理发布 |
| **M4 多代理身份**（约 2 天） | `tb_print_agent`、鉴权双轨、心跳 per-agent、管理端页面 | 唯一表结构变更，独立排期 |

---

## 十五、风险与开放问题

| 风险 | 评估 | 对策 |
|---|---|---|
| DLE EOT 机型实现差异大，bit 语义不准 | 中 | 负面缓存兜底「不应答」场景；raw 原文存档；解读只作展示不作判定 |
| TTL 作废误伤正打着的 claimed 任务 | 低 | 窗口极小且日志注明、可补打；如出现再引入「作废前重查 claim_time」 |
| 长轮询被中间层（自建网关/CDN）idle timeout 掐断 | 低 | wait 可配 0 退回纯轮询；响应头加 `X-Accel-Buffering: no` 提示 nginx 不缓冲 |
| 多实例下长轮询 notify 失效 | 低 | 2s DB 兜底扫描已在设计内 |
| v1/v2 长期共存导致行为矩阵复杂 | 低 | 管理端可见各代理版本，升级完成度一目了然；legacy 令牌不设淘汰期限，由商家自觉迁移 |
| 未签名二进制被 macOS Gatekeeper / Windows SmartScreen 拦截 | 确定会发生 | 部署文档置顶写清绕行操作；后续如需正式签名再立项 |
