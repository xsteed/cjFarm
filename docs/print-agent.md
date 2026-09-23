# 本地打印代理（print-agent）部署手册

> 适用场景：**后端部署在云服务器，门店想继续用已有的 9100 网络热敏打印机**。
> 不适用：后端与打印机在同一局域网（直接用「网络直连」通道更简单），
> 或门店愿意买飞鹅云打印机（直接用「飞鹅云」通道，零门店侧软件）。

## v2 变化摘要

本文档已更新到 v2 协议与代理 v2。相对 v1 新增：

- **打印结果可确认**：打印后回读 `DLE EOT` 实时状态（缺纸 / 盖板开 / 暂停 / 纸将尽 / 不可恢复错误），并在打印日志如实记录；
- **任务 TTL 过期作废**：过期厨房单绝不再吐纸（语义是「过期=作废」，可人工补打）；
- **拨号熔断**：某台打印机连不上时冷却 30 秒，期间该打印机的任务**挂起不回执**（不消耗云端仅有的 3 次重试额度），不拖慢全店出票；
- **长轮询**：取单 `wait`（0–30 秒，默认 25），有任务立即返回，出纸更快；
- **多代理令牌**：管理端逐台签发/吊销、可限定授权打印机范围；
- **五平台构建**：新增 macOS Intel / Apple Silicon 产物。

**v1 代理继续可用**：协议只增字段、不改语义，老代理零感知共存。升级顺序请遵循
「**云端先升级，代理后升级**」（反向则新字段被忽略、失去意义）。

---

## 部署清单（从零到验收）

> 从上往下照着做即可。每步都写了**期望输出**，对不上就停在那一步，跑体检命令
> `--doctor` 让它告诉你缺什么。后面各章节是原理与细节，第一次部署不必读。
>
> **命令里的路径怎么写**：下文出现的 `./print-agent` 是「产物可执行文件」的通称，压缩包里
> 实际叫 `print-agent-darwin-arm64`（Apple 芯片）/ `print-agent-darwin-amd64`（Intel）、
> `print-agent-linux-amd64`、`print-agent-windows-amd64.exe`。而 macOS 装好之后，
> **真正在跑的那份**在 `~/Applications/PrintAgent.app/Contents/MacOS/print-agent` ——
> 体检、排障、`--setup-cups` 都用它（解压目录那份读的是另一个位置的配置，结论会误导）。

### 第 0 步：云端准备（管理后台，3 件事）

1. **系统配置 → 小票打印 → 本地打印代理**：签发**代理令牌**；
2. **打印机管理 → 新增**：通道选「**本地代理**」，把门店打印机的**内网 IP** 填对
   （形如 `192.168.1.133`，端口默认 9100）；
3. 记下这三样，门店那边只用它们：后台地址（**不带** `/prod-api`、**不带**结尾斜杠）、代理令牌、打印机 IP。

### 第 1 步：出包（开发机，一条命令）

```bash
cd print-agent
make VERSION=1.0.0 package
```

产物：`dist/{darwin,windows,linux}/` 三个目录 + 三个压缩包（文件名带版本号，旁边有 `.sha256`）。

> 发版时把 `VERSION` 换成新版本号，并**同步更新管理后台的「代理最新版本号」** ——
> 否则门店执行 `--upgrade` 会被换回服务器上的旧包。门店自助升级还需要服务器提供产物：
> 把 `print-agent/bin/` 放进后端部署目录（`release.sh` 自带，可用 `AGENT_BIN_DIR` 改目录）。

### 第 2 步：门店安装（把对应的压缩包发过去，解压后在该目录执行）

**macOS：一条命令**（`install.sh` 依次做完四件事 —— 安装 → 打印通道 → 合盖继续运行 → 体检）：

```bash
./install.sh https://你的后台域名 你的令牌 192.168.1.133
#                                        ^^^^^^^^^^^^^^ 打印机 IP,可省略
```

- **省略打印机 IP** 时会用「代理见过的地址」自动配置。本机还没见过任何打印机时，
  脚本会直接给出补做命令：到管理后台点一次「测试打印」，再执行
  `<App 内可执行文件> --setup-cups auto` 即可（代理收到任务就会记下地址）。
  多台打印机用逗号分隔：`192.168.1.133,192.168.1.134`。
- **合盖继续运行**由脚本一并处理：它把 `caffeinate -i -s` 写进自启动（顶住空闲睡眠），
  并把系统的合盖睡眠开关关掉（`pmset disablesleep`，需要一次管理员密码）。
  这两件缺一不可 —— 合盖触发的是 Clamshell Sleep，`caffeinate` 挡不住它，详见 §3.5.2。
- 想只要其中一部分：

  ```bash
  ./install.sh --minimal                       # 只安装(等价 --no-cups --no-lid)
  ./install.sh --no-lid https://后台 令牌       # 只不要「合盖继续运行」
  ./install.sh --no-cups https://后台 令牌      # 只不要打印通道(保持直连)
  ```

  也可以双击同目录的 `双击安装.command`，它会交互式问齐三样并停住让你看结果。

**Windows / Linux：一条命令**

```bash
./install.sh https://你的后台域名 你的令牌     # Windows 双击 install.bat;Linux 用 sudo ./install.sh
```

> 验证：`<App 内可执行文件> --doctor` 一次查完全部（macOS 上脚本已自动跑过一遍）。
> 想让纸真的出来：`--probe 192.168.1.133 --probe-print`。

> `--install` 会把程序部署成 `~/Applications/PrintAgent.app`（自带图标），
> 配置与日志落在 `~/Library/Application Support/PrintAgent/`。

### 第 3 步：验收（门店机器上，两条命令 + 后台点一次）

```bash
AGENT="$HOME/Applications/PrintAgent.app/Contents/MacOS/print-agent"
"$AGENT" --doctor                                 # 期望:「结论: … 无阻塞性问题」
"$AGENT" --probe 192.168.1.133 --probe-print      # 期望:「已额外经 CUPS 队列[…] 送出自检页」
```

> **为什么用这个长路径而不是解压目录里的那个**：macOS 上真正在跑的是 App 里那份，配置也在
> 它的数据目录。站在解压目录里跑 `print-agent-darwin-arm64 --doctor`，读到的是**另一个位置**
> 的配置，会显示「未配置云端地址 / 通道 tcp」，把人引向完全错误的方向（体检会额外提示这一点）。

再到管理后台点一次**测试打印**，日志应出现：

```
[成功] #13 测试页 → 打印机[后厨打印机](192.168.1.133:9100) 已送出 ×1
```

### 第 4 步：日常与升级

| 操作 | 命令 |
|---|---|
| 看状态 | `./p.sh`（进程 + 最近日志 + 合盖/睡眠 + 出问题该跑什么） |
| 体检 | `./print-agent --doctor`（加 `--probe <IP>` 连打印机一起查） |
| 升级 | `./upgrade.sh`（不用重填令牌；升级后建议再跑一次 `--doctor`） |
| 停止 / 启动 | `./stop.sh` / `./start.sh`（自启动保留） |
| 卸载 | `./uninstall.sh`（保留配置）／`./uninstall.sh --all`（连产物与配置一起删） |
| 云端停用 | 管理后台「打印机管理 → 代理列表 → 吊销」 |

---

## 日常操作速查（复制即用）

> 门店最常用的启动 / 自检 / 停止 / 卸载全在这里。安装只有一条命令；装好之后，
> 启动 / 停止 / 状态打印 / 卸载统一用仓库 `print-agent/op/` 里的**运维脚本**（按操作系统分文件夹，
> 每个文件夹六种：`install` / `start` / `stop` / `p` / `upgrade` / `uninstall`）。详细原理见后文对应章节。

### 1. 安装（把对应平台文件夹里的脚本与产物放同目录，然后一条命令）

```bash
# 把 print-agent/op/ 下对应平台文件夹的脚本 + 对应产物拷到门店设备同一目录,然后:

./install.sh https://你的管理后台域名 你的令牌   # 安装:写配置 + 注册开机自启
                                                # (省略参数则交互式询问,令牌不落命令行历史)
./print-agent --once                             # 自检一轮(应打印「云端自检通过」)

# ---- 以下两条主要针对 macOS ----
./print-agent --setup-cups 192.168.1.133         # 把打印机交给系统打印服务(换成门店打印机 IP)
                                                 # 一条命令、不需要管理员密码;详见 §3.5.1
./print-agent --doctor                           # 体检:一次查完配置/自启动/打印通道/防睡眠与合盖/云端
                                                 # 加 --probe 192.168.1.133 可把打印机那段一起查
```

> 也可以直接用产物安装（与脚本等价）：`./print-agent-darwin-arm64 --install --server https://... --token ...`；
> Windows 双击 `install.bat`；Linux 用 `sudo ./install.sh ...`。产物名见 §三 产物表。
> macOS 上 `install` 会把程序部署成 `~/Applications/PrintAgent.app`（自带图标），
> 配置文件与日志在 `~/Library/Application Support/PrintAgent/`（数据刻意放在 App 之外，
> 往 App 里写文件会破坏它的代码签名）。

### 2. 日常运维（同一套脚本，复制即用）

```bash
./start.sh     # 启动(已安装自启动时;未安装会给出 install 引导)
./stop.sh      # 停止(自启动保留,重启后自动恢复)
./p.sh         # 打印状态:进程状态 + 最近 15 行日志 + 云端自检提示
./upgrade.sh   # 自助升级(服务端已部署新产物且云端配置了最新版本号时)
./uninstall.sh # 卸载:停进程 + 移除开机自启 + 清理日志/状态/升级残留(--all 连产物与配置一起删)
```

| 平台 | 文件夹 | 脚本 |
|---|---|---|
| macOS | `op/mac/` | `install.sh` / `start.sh` / `stop.sh` / `p.sh` / `upgrade.sh` / `uninstall.sh` |
| Windows | `op/win/` | `install.bat` / `start.bat` / `stop.bat` / `p.bat` / `upgrade.bat` / `uninstall.bat`（双击即可） |
| Linux | `op/linux/` | `install.sh` / `start.sh` / `stop.sh` / `p.sh` / `upgrade.sh` / `uninstall.sh` |

其它操作：产物级命令 `./print-agent --uninstall` 也只移除自启动（保留配置，重装不用重填令牌）；
**连通性自检** `./print-agent --probe 192.168.1.100`（多个用逗号分隔，端口缺省 9100）：
只查「本机 → 打印机」这一段，不用在门店电脑上装 `nc`；实时日志 `tail -f print-agent.log`；
想连产物一起删：脚本后加 `--all`。

**出问题先跑这两条，能省掉大半来回**：

| 命令 | 用途 |
|---|---|
| `./print-agent --doctor` | **体检**：配置、自启动、打印通道与 CUPS 队列、防睡眠与合盖、云端连通，一次查完并直接给出处置命令。不要求 `--server`/`--token`（配置坏了也要能查）。有「[问题]」项时退出码为 1 |
| `./print-agent --setup-cups <打印机IP>` | **一条命令配好 macOS 的系统打印通道**：建好 CUPS 队列 + 打开 `auto` 通道 + 重启代理 + 核对结果。可重复执行，不需要管理员密码 |

各平台原生命令（了解原理用）：

| 操作 | macOS | Windows | Linux |
|---|---|---|---|
| 停止 | `launchctl stop com.cjfarm.print-agent` | `schtasks /end /tn "DiningPrintAgent"` | `sudo systemctl stop print-agent` |
| 再启动 | `launchctl start com.cjfarm.print-agent` | `schtasks /run /tn "DiningPrintAgent"` | `sudo systemctl start print-agent` |
| 看日志 | `tail -f print-agent.log` | `Get-Content print-agent.log -Wait` | `tail -f print-agent.log` |

### 3. 云端侧停用（不用碰门店设备）

管理后台「打印机管理」页的代理列表点**吊销**：该代理立即失效、取不到单；点**恢复**即重新启用。
适合远程临时停用多门店场景。

---

## 一、先回答三个最常见的问题

### 1. 需要安装打印机驱动吗？

**不需要。** 9100 是打印机的 RAW / JetDirect 端口，打印机在 TCP 层直接接收
ESC/POS 字节流，**不经过操作系统的打印队列**，因此没有驱动这一层。

需要驱动的是另一条通道：浏览器打印（A4 单据、`/printTicket` 页面）走的是收银电脑的
操作系统打印系统，那种场景才必须装厂商驱动。

### 2. 为什么云服务器不能直接连门店打印机？

后端「网络直连」通道做的是 `net.Dial("tcp", 打印机IP:9100)`。门店打印机是
`192.168.x.x` 这类**私有地址**，云服务器上没有到达门店内网的路由，只会连接超时：

```1:7:backend/internal/print/escpos.go
// 适用面:后端与打印机在同一个局域网。一旦后端部署到云服务器,这条路径
// 就够不到门店打印机了 —— 那种场景请把打印机配成飞鹅云打印(见 feie.go)。
```

### 3. 那代理是怎么绕过去的？

代理程序跑在**门店内网**，它主动向云端发请求（出站），因此：

- 门店**不需要公网 IP**、不需要端口映射、不需要 VPN；
- 门店的防火墙/NAT 不需要放行任何入站端口；
- 门店断电/断网恢复后自动续上，不用人工干预。

```
        ┌──────────────────────── 门店内网 ────────────────────────┐
        │                                                          │
云后端 ─┼─① 出站 HTTPS 拉单 ──> print-agent ──② TCP 打印机IP:9100──> 打印机
        │        (443)              (常开机设备)      (局域网内, 原始字节)
        └──────────────────────────────────────────────────────────┘
   ③ 出站 HTTPS 回执(打成功/失败原因)
```

---

## 二、云端配置（3 步，都在管理后台完成）

### 1. 签发代理令牌

「系统配置 → 打印代理 → 代理令牌」→ 点**签发新令牌**。

- 令牌由**后端生成并直接落库**：点一次即生效，不必再点页面底部的「保存配置」。
  （早前是前端生成、随整份配置保存才入库，漏点保存就会出现「代理填了令牌、云端却没存过它」，
  现象是代理一直报「令牌不正确」而界面上看不出任何差别。）
- 令牌是**敏感项**：落库前 AES-256-GCM 加密，接口永不回显；
- 令牌等同于「一台打印机的操作权限」（拿到它就能把待打印队列整个拉走，**含订单金额**），
  所以用「签发新令牌」由后端随机生成，不要手填 `123456`；
- 签发后弹窗里的明文**只显示这一次**，关窗后再也看不到，忘了只能重新签发
  （重新签发后旧令牌立即失效，所有用它的代理都要同步更新）。
- 输入框仍可手填（例如从旧系统迁移过来的令牌），手填的值**仍需点「保存配置」**才入库。

### 2. 新增/编辑打印机，通道选「本地代理」

「打印机管理 → 新增打印机」：

| 字段 | 填什么 |
|---|---|
| 打印机名称 | 如 `后厨厨房单打印机` |
| 接入方式 | **本地打印代理（云端部署）** |
| 打印机类型 | 厨房单 / 食客小票 |
| IP 地址 | **打印机在门店内网的地址**，如 `192.168.1.8` |
| 端口 | `9100`（绝大多数热敏机默认值） |
| 纸宽 / 份数 / 负责分类 | 与直连通道完全一致 |

> IP 是给**门店那台代理设备**用的，云后端不会去连它。因此要保证：代理设备与打印机
> 在同一网段、能互相 ping 通。不要填 `127.0.0.1`（除非代理与打印机在同一台机器上）。

### 3. 先看到「等待代理取单」

保存后到「打印机管理」点**状态**，应看到类似：

```
打印代理已离线(最近心跳 2026-09-22 10:31:02);本机当前积压 0 单
```

这说明云端已就绪，只等门店把代理跑起来。此时下单会正常排队（不会报错、也不会丢单），
但纸不会出来。

---

## 三、门店侧部署

### 1. 拿一个代理程序

在**开发机**上交叉编译，然后把产物拷到门店那台设备：

```bash
cd backend
make build-agent            # 一次产出 Windows / Linux / macOS 共五个平台版本
ls -l ../print-agent/print-agent/bin/print-agent-*
```

| 产物 | 用在什么设备上 |
|---|---|
| `print-agent/bin/print-agent-windows-amd64.exe` | 收银电脑（Windows，最常见） |
| `print-agent/bin/print-agent-linux-amd64` | 小主机 / 软路由 / 旧笔记本 / 云电脑 |
| `print-agent/bin/print-agent-linux-arm64` | 树莓派等 ARM 设备 |
| `print-agent/bin/print-agent-darwin-amd64` | Intel Mac |
| `print-agent/bin/print-agent-darwin-arm64` | Apple Silicon Mac（M1/M2/M3 等） |

源码在顶层 `print-agent/` 独立 Go module（`module cjfarm/print-agent`，纯标准库），
`make build-agent` 会切到该目录交叉编译，产物统一输出到 `print-agent/bin/`。

**给门店发整包（推荐）**：在 `print-agent/` 目录执行 `make package`，会把
「对应平台产物 + 运维脚本 + 门店安装说明」组装成 `print-agent/dist/<os>/` 目录
并打成带版本号与 sha256 的压缩包（`print-agent-v<版本>-<os>.zip` / `.tar.gz`），
门店解压后照包内 `门店安装说明.md` 双击/一条命令即可完成安装，无需另发说明文档。

程序是**静态编译、零依赖**的单文件（`CGO_ENABLED=0`），拷过去就能跑，不需要装 Go 或运行时。

### 2. 一条命令安装

把上一步对应平台的产物拷到门店那台设备（Linux 下记得 `chmod +x` 给执行权限），
然后在**产物同目录**执行一条命令完成配置与自启动：

```bash
# 把 <产物> 换成实际文件名(见上表),--server 换成管理后台地址,--token 换成签发的令牌
./<产物> --install --server https://dining.example.com --token <令牌>
```

`--install` 会自动写 `agent.env` 并注册开机自启（macOS launchctl /
Windows 任务计划 `DiningPrintAgent` / Linux systemd）。不想在命令行里明文出现令牌的话，
直接运行 `<产物> --install`，程序会**交互询问** `--server` 与 `--token`，效果相同。

配置文件落在哪（`--install` 完成后日志里会打印实际路径）：

| 平台 | 程序位置 | `agent.env` / 状态 / 日志 |
|---|---|---|
| Windows / Linux | 你解压的目录 | 同目录（`agent.env`、`print-agent.state`、`print-agent.log`） |
| macOS（`--install` 安装） | `~/Applications/PrintAgent.app`（App 形态，见 §3.5.1） | `~/Library/Application Support/PrintAgent/` |
| macOS（直接解压运行） | 你解压的目录 | 同目录 |

macOS 的数据刻意放在 App 之外：App 里写任何文件都会破坏代码签名，而系统正是靠签名识别
这个 App（「本地网络」授权按 App 记录）。路径可用 `PRINT_AGENT_DATA_DIR` 覆盖。

重复安装时的取值规则（`agent.env` 已存在的情况）：

| 这次怎么跑 | SERVER / TOKEN 的结果 |
|---|---|
| 带了 `--server` / `--token`（或 shell 里 export，或交互当场输入） | **按本次输入改写**旧值，日志会打印「已按本次输入更新 …」 |
| 什么都没带，直接 `--install` | 沿用文件里的旧值，一个字不改（重装不必重填令牌） |

换地址、重发令牌时带上参数重装即可；`--uninstall` 从不删 `agent.env`，所以「先卸载再装」
并不会自动清掉旧令牌，仍然要用带参数的 `--install` 覆盖。

它写出的 `agent.env` 等价于（其余可选项见 `print-agent/agent.env.example`，默认值一般不用改）：

```ini
PRINT_AGENT_SERVER=https://dining.example.com
PRINT_AGENT_TOKEN=<你签发的令牌>
# 可选:轮询间隔 / 代理标识 / 自签证书开关等,见 agent.env.example
```

安装完成后跑一次自检：

```bash
# Linux / macOS
./<产物> --once

# Windows（cmd 或双击 exe）
print-agent-windows-amd64.exe --once
```

`--once` 只跑一轮就退出，适合排障。正常输出：

```
2026-09-22 10:35:01 已连接云端 https://dining.example.com (代理标识 门店收银电脑, 轮询间隔 3s)
2026-09-22 10:35:01 云端自检通过: 店铺="长健农场 柴火农家土菜" 服务器时间=2026-09-22 10:35:01 待送任务=0 条
```

然后到管理后台点打印机的**测试打印/状态**，应立刻出纸，状态变成：

```
本地打印代理在线(最近心跳 10:36:12);本机积压 0 单
```

### 2.5 代理日志长什么样（排障第一现场）

每轮取单只要云端有任务下发，就先落一行入口日志，再逐条打印（订单号/桌号都在里面，
可直接按单检索，不必拿 jobId 回云端反查）：

```
2026-09-23 10:35:01 调试日志已开启,空轮次与回执统计也会记录
2026-09-23 10:35:02 [取单] 本轮收到 2 条任务(pull 耗时 12ms): #42 厨房单 订单NO20260922042/桌T1 → 打印机[后厨打印机](192.168.1.100:9100); #43 食客小票 订单NO20260922042/桌T1 → 打印机[前台](192.168.1.101:9100)
2026-09-23 10:35:02 [成功] #42 厨房单 订单NO20260922042/桌T1 → 打印机[后厨打印机](192.168.1.100:9100) 已送出 ×2
2026-09-23 10:35:02 [失败] #43 食客小票 订单NO20260922042/桌T1 → 打印机[前台](192.168.1.101:9100): 无法连接打印机 192.168.1.101:9100: dial tcp ...: i/o timeout
2026-09-23 10:35:02 [汇总] 本轮回执 2 条(成功 1 / 失败 1)
```

最后这行汇总用**如实口径**：成功/失败分开写（不会写成「已处理 2 条」——失败也算处理过，
那样会让人误以为纸出来了）。全部成功时是 `[汇总] 本轮回执 N 条,全部成功`；
有熔断挂起的任务时另起一段 `未尝试(冷却等待)N`（它既没成功也没失败，不消耗重试次数）。

其它会出现的行：`[去重] …已在本机打印过(deliveryId …),跳过并回执`、`[等待] …打印机连接熔断冷却中(约 N 秒后重试),本轮不回执`
（挂起等租约到期，不消耗云端重试次数）、`[警告] 本轮取单失败(稍后自动重试)`、
`[状态] … DLE EOT raw=…`（`--once` 时）、`[调试] …`（仅 `--verbose`）。

- **判据**：「代理在线但没出纸」时，先看有没有 `[取单]` —— 没有说明云端没下发到这台代理
  （队列空 / 令牌对应的打印机不对），有则看后续是 `[成功]` 还是 `[失败]`。
- **`--verbose`**（或 `PRINT_AGENT_VERBOSE=1`）：额外打印空轮次心跳（每几秒一行，常驻模式
  默认关闭避免刷屏）、上报前的回执明细（`[调试] 准备回执上报: 共 N 条(成功 x / 失败 y)`，
  与汇总对照可看出回执有没有丢）以及冷却挂起说明。`--once` 自动开启。

### 3. 自启动（--install 已自动注册）

第 2 步的 `--install` 已经把开机自启一起配好，无需再做任何手工操作：

| 平台 | 注册方式（--install 自动完成） |
|---|---|
| Windows | 任务计划程序，任务名 `DiningPrintAgent`（开机即启、无需用户登录、失败自动重启） |
| macOS | 先把程序部署成 `~/Applications/PrintAgent.app`，再注册 launchd `~/Library/LaunchAgents/com.cjfarm.print-agent.plist`（plist 里带 `AssociatedBundleIdentifiers`，与 App 的 `CFBundleIdentifier` 一致） |
| Linux | systemd unit `print-agent.service`（`systemctl enable --now`） |

- macOS 之所以要多这一步 App 部署，是为了「本地网络」权限能授予出去 —— 见 §3.5.1；
- 重复执行 `--install` 会**覆盖注册**（以最后一次为准），但**保留已有的 `agent.env`**
  配置，不会冲掉你手改的地址/令牌（旧位置在同目录的 `agent.env` 会被自动沿用过来）；
- 想移除自启动：在门店设备上运行 `<产物> --uninstall`。它会移除自启动并删除
  `~/Applications/PrintAgent.app`，但**不删除** `agent.env`，下次 `--install` 直接复用；
- 想一次做完「停进程 + 移除自启动 + 清掉日志/状态/升级残留」：用运维脚本
  `./uninstall.sh`（Windows 双击 `uninstall.bat`，Linux 用 `sudo`）；加 `--all`
  还会删掉产物与 `agent.env`。

想了解底层到底注册了什么、或要在没有 `--install` 的老版本上部署，见下一节的手动注册步骤。

### 4. 手动注册（备选，了解原理用）

老版本代理、或想手动控制自启动时按平台操作（`--install` 内部正是复用
`print-agent/deploy/` 下这三个模板）：

**Windows（任务计划程序）**

推荐直接导入仓库自带的模板 `print-agent/deploy/print-agent-windows-task.xml`：开机即启动、
**无需用户登录**、失败自动重启。先用文本编辑器打开模板，把
`<Command>C:\dining\print-agent.exe</Command>` 改成实际放 exe 的路径
（例如 `C:\dining\print-agent-windows-amd64.exe`），再以管理员身份导入：

```cmd
schtasks /create /tn "DiningPrintAgent" /xml print-agent\deploy\print-agent-windows-task.xml
```

控制台中文乱码已由程序自己处理（启动时把代码页切到 UTF-8），**不需要**再 `chcp 65001`。
首次运行未签名的 exe 会弹 SmartScreen「Windows 已保护你的电脑」——点「更多信息 → 仍要运行」。

仍想用旧方式也行（登录后自动启动、不弹黑窗口）：

```cmd
schtasks /create /tn "DiningPrintAgent" /sc onlogon /rl highest ^
  /tr "C:\dining\print-agent-windows-amd64.exe"
```

或把 exe 和 `agent.env` 放到一个文件夹，为 exe 创建快捷方式并放入
`shell:startup`（`Win+R` 输入即可打开启动目录）。

**Linux（systemd）**

仓库自带单元文件 `print-agent/deploy/print-agent.service`：

```bash
sudo mkdir -p /opt/dining-print-agent
sudo cp print-agent/bin/print-agent-linux-amd64 /opt/dining-print-agent/print-agent
sudo cp print-agent/deploy/print-agent.service /etc/systemd/system/
# 编辑 agent.env 里的地址与令牌
sudo tee /opt/dining-print-agent/agent.env >/dev/null <<'EOF'
PRINT_AGENT_SERVER=https://dining.example.com
PRINT_AGENT_TOKEN=换成你的代理令牌
EOF
sudo systemctl daemon-reload
sudo systemctl enable --now print-agent
journalctl -u print-agent -f          # 看日志
```

**macOS（launchd）**

1. 判架构，挑对应产物：
   ```bash
   uname -m
   # arm64   → print-agent/bin/print-agent-darwin-arm64（Apple Silicon）
   # x86_64  → print-agent/bin/print-agent-darwin-amd64（Intel）
   ```
2. 把程序放到固定位置并给执行权限：
   ```bash
   sudo cp print-agent/bin/print-agent-darwin-arm64 /usr/local/print-agent/bin/print-agent   # 按上一步结果选产物
   sudo chmod +x /usr/local/print-agent/bin/print-agent
   ```
3. 首次运行若被 Gatekeeper 拦（提示「无法打开，因为无法验证开发者」），二选一：
   ```bash
   sudo xattr -d com.apple.quarantine /usr/local/print-agent/bin/print-agent
   ```
   或在 Finder 里右键该文件 → 打开 → 再点一次「打开」。
4. 改仓库自带模板 `print-agent/deploy/print-agent.plist`：把 `/usr/local/print-agent/bin/print-agent` 改成实际路径、
   `https://CHANGEME.example.com` 与 `CHANGEME_TOKEN` 改成实际地址与令牌。
   （也可以删掉 `--server`/`--token` 两组参数，改用与 print-agent 同目录的 `agent.env`。）
   **另外必须补上 `AssociatedBundleIdentifiers`**（值为承载该程序的 App 的 bundle id）：

   ```xml
   <key>AssociatedBundleIdentifiers</key>
   <array>
       <string>com.cjfarm.print-agent</string>
   </array>
   ```

   macOS 15+ 上这是「本地网络」权限能被授予的前提：launchd **agent** 不在 daemon 的豁免
   范围内，不用 `SMAppService` 安装的 agent 必须靠它让系统定位 responsible code；缺了它
   系统找不到可授权的主体，连接被直接拒绝。完整规则与三条处置办法见 §3.5.1 —— 建议直接
   用 `<产物> --install`，它会自动部署 `~/Applications/PrintAgent.app` 并写好这一项。
5. 安装并加载：
   ```bash
   mkdir -p ~/Library/LaunchAgents
   cp print-agent/deploy/print-agent.plist ~/Library/LaunchAgents/com.cjfarm.print-agent.plist
   launchctl load ~/Library/LaunchAgents/com.cjfarm.print-agent.plist
   tail -f /tmp/print-agent.log   # launchd 采集的日志在 /tmp/print-agent.log
   ```
   手动注册方式下程序日志落在 `agent.env` 里 `PRINT_AGENT_LOG` 指定的文件（未配置则仅由
   launchd 采集到上述路径）；用 `--install` 安装时会自动写一份 `print-agent.log`。
   `PRINT_AGENT_LOG` 填相对路径（如默认的 `print-agent.log`）时按**运行数据目录**解析
   （普通部署即程序所在目录；macOS 上 `--install` 装成 App 后为
   `~/Library/Application Support/PrintAgent`，可用 `PRINT_AGENT_DATA_DIR` 覆盖）：
   launchd/systemd/任务计划的工作目录是 `/`、`System32` 等只读目录，不锚定的话日志根本打不开。
   填绝对路径则按绝对路径原样使用。

### 5. 网络放行要求

| 方向 | 目标 | 端口 | 说明 |
|---|---|---|---|
| 出站 | 云端域名/IP | 443（或 80） | HTTPS 轮询，必须放行 |
| 出站 | 打印机 IP | 9100 | 门店内网，通常本来就通 |

**不需要**任何入站规则。

### 5.1 macOS 15+ 的「本地网络」权限（重要，打不出纸时先看这里）

macOS 15（Sequoia）起引入 **本地网络隐私（Local Network Privacy）**：任何连接局域网地址
的出站操作都需要用户授权，而授权是**按代码签名识别程序身份**记录的。

Apple TN3179 对各形态的规定（macOS 部分）：

| 进程形态 | 是否受该权限约束 |
|---|---|
| 由 launchd 启动的 **daemon** | **自动放行**，无需授权 |
| 以 **root** 运行的程序 | **自动放行** |
| 从**终端或 SSH** 运行的工具及其子进程 | **自动放行** |
| 由 launchd 启动的 **agent**（本代理的形态） | **受约束** —— daemon 的豁免**不适用于 agent** |
| GUI App / App 扩展等 | 受约束 |

被拦下时，BSD socket 层返回 `EHOSTUNREACH`，Go 里显示为：

```
[失败] #8 测试页 → 打印机[后厨打印机](192.168.1.133:9100): 无法连接打印机 192.168.1.133:9100: dial tcp 192.168.1.133:9100: connect: no route to host
```

**这与「打印机没开机 / 不在同一网段」的报错一字不差**，是这条链路最难排查的坑。

#### 怎么确认是它

两个判据，任一命中基本可确认：

1. **在终端里手工执行 `--probe` 能通，而开机自启的代理打不出来** —— 终端及其子进程不受
   该权限限制，这条差异最能说明问题：

   ```bash
   ~/Applications/PrintAgent.app/Contents/MacOS/print-agent --probe 192.168.1.133
   # 期望输出: [自检] 192.168.1.133:9100 可达(5ms) ...
   ```

2. **耗时极短。** 真实的网络不可达要么等 ARP 超时、要么收到 ICMP，都会花掉明显时间；
   被本机策略拦下是**瞬间**返回。`--probe` 会把它直接在提示里点出来：

   ```
   [自检] 192.168.1.133:9100 不可达(0s): ... connect: no route to host
   [自检]   排查:① 打印机是否开机/休眠 ② IP 是否变过 ... ③ 本机与打印机是否同一网段
   [自检]   ④ 连接是被本机**瞬间**拒绝的(不是网络不可达):macOS 15 及以上最常见的原因是
             「本地网络」隐私权限拦下了自启动的代理。
   ```

代理运行时也会在日志里给出**一次性**提示（`[提示] 连接被瞬间拒绝(no route to host)…`），
不会每条任务都刷。

#### 处置一（推荐）：改用系统打印服务（CUPS 通道）

让打印由系统打印服务发起，本程序只把字节交给本机 `cupsd`：

```
代理 ──(127.0.0.1:631,回环,不需要权限)──> cupsd(root launchd daemon,自动放行) ──> 192.168.x.x:9100
```

回环地址按 TN3179 对「本地网络」的定义不属于本地网络（本地网络 = 与支持广播的接口关联的
IP 网络），所以整条链路与本地网络权限无关，**升级、换机器都不用再授权**。

这条链路的两个前提都可以在门店机器上自行核实：

```bash
launchctl print system/org.cups.cupsd | grep -E "type|program|domain"
#   type = LaunchDaemon
#   program = /usr/sbin/cupsd
#   domain = system
```

`cupsd` 是 `system` 域的 root LaunchDaemon，正落在 TN3179「自动放行」那一类里。
（它是按需启动的，平时打印任务为空时 `state = not running` 属正常现象，提交作业时由
launchd 拉起。）

一条命令（推荐，不需要管理员密码）：

```bash
./print-agent --setup-cups 192.168.1.133
```

它做四件事：建 CUPS 队列 → 打开 `auto` 通道 → 重启代理 → 核对结果。可重复执行，
已有队列时直接复用。输出形如：

```
===== 为 192.168.1.133:9100 配置 CUPS 打印通道 =====
[完成] 已创建打印队列 Cjfarm-192.168.1.133 → socket://192.168.1.133:9100
[完成] 已把打印通道设为 auto(仍优先直连,直连失败再改投 CUPS);配置:~/Library/Application Support/PrintAgent/agent.env
[完成] 已重启自启动的代理,新配置即刻生效
[完成] 核对通过:192.168.1.133:9100 → 队列 Cjfarm-192.168.1.133;代理现在可以经系统打印服务出纸
```

想手工做（或要换成自己的队列名，比如中文名）时，等价的三步是：

```bash
# 1) 把打印机加进系统打印服务(IP 方式,协议选 Socket / HP JetDirect / LPD)
#    图形界面:「系统设置 → 打印机与扫描仪 → 添加打印机 → IP」
lpadmin -p Kitchen -E -v socket://192.168.1.133:9100
# 2) 确认队列存在(设备 URI 是 socket:// 的才会被自动识别)
lpstat -v
# 3) 打开 CUPS 通道
#    编辑 ~/Library/Application Support/PrintAgent/agent.env,加一行 PRINT_AGENT_PRINT_VIA=auto
./start.sh
```

> **别给 `lpadmin` 加 `-m`**（实测结论）：`-m raw` 在 macOS 26 上被直接拒绝
> （「macOS不再支持原始队列」）；`-m everywhere` 要求打印机支持 IPP Everywhere，
> 而 ESC/POS 网口机通常只开 9100（实测 631 连接被拒），也用不了。
> **不带 `-m` 反而成功**；内容靠 `lp -o raw` 原样透传，所以队列没有驱动不影响出纸。

- `auto`（推荐）：仍**优先直连** `IP:9100`，只在直连失败且本机确有对应队列时改投 CUPS；
- `cups`：一律交给 CUPS；CUPS 不可用时**不会**退回直连（避免又撞上权限，报错更直白）；
- 队列自动发现（两个来源并用，谁先认出来以谁为准）：
  1. `lpstat -v` 的 `socket://` 设备 URI —— 快，但 **macOS 的 lpstat 输出随系统语言变化
     且无视 `LC_ALL`/`LANG`**（实测中文系统上设了也没用）。中文下的实际输出是
     `用于CjfarmKitchen的设备：socket://...`，**标签词和队列名之间没有空格**，
     解析不能按空格分词（实测踩过这个坑，已修）。
  2. `system_profiler -json SPPrintersDataType` —— JSON 键名是固定英文标识符
     （`uri` / `_name`），任何系统语言下都成立，实测耗时约 0.2 秒。
     但它对「用 `lpadmin` 建的无驱动队列」只报 `no_info_found`（实测），所以只是补充来源。

  有了自动发现，门店只要在系统设置里加过打印机，这一步什么都不用配。只认 `socket://`：
  `ipp://`、`lpd://` 走的是别的协议，`lp -o raw` 投过去要么被拒要么打出乱码，与其
  「看起来配好了却出不了纸」，不如不认并在日志里明确报「找不到队列」。
- 需要手填时：`PRINT_AGENT_CUPS_QUEUE=192.168.1.133=厨房打印机`（多个用逗号分隔；
  只写队列名如 `=厨房打印机` 则作为所有打印机的默认队列）。队列名是纯中文、或系统语言
  罕见导致两条发现路都没认出时，**显式配置永远有效**。
- **代价**：CUPS 通道回读不到 `DLE EOT` 实时状态（链路在 `cupsd` 手里，我们只有一个作业
  提交口），因此缺纸/盖板开只能靠出纸结果判断，回执里 `printerStatus` 为空。日志会写
  `[通道] … 经 CUPS 队列[Kitchen] 送出 ×N`。直连通道不受影响。

#### 验证（不必等真实订单）

```bash
APP=~/Applications/PrintAgent.app/Contents/MacOS/print-agent
"$APP" --probe 192.168.1.133                # 列出发现到的队列 + 每个目标会走哪条通道(不出纸)
"$APP" --probe 192.168.1.133 --probe-print  # 真的出纸:直连那张多半会被拦,由 CUPS 送出一张
```

`--probe` 的输出形如：

```
[自检] 打印通道=auto, CUPS 队列自动发现到 1 个:
[自检]   192.168.1.133:9100 → 队列[Kitchen]
[自检]   192.168.1.133:9100 会先直连,直连失败再改投队列[Kitchen]
[自检] 192.168.1.133:9100 可达(6ms) 状态未回读(该机型不支持 DLE EOT 查询,不影响出纸)
```

「找不到队列」时日志会直接给出该去哪添加打印机、以及 `PRINT_AGENT_CUPS_QUEUE` 怎么写 ——
这一步在 3.5.1 这套处置里最容易卡住，所以特意做成一条命令可查。

> 没有打印机时 `lpstat -v` 只报一句本地化错误（中文系统上「未添加目的位置。」），
> 这不影响代理运行，也不会被当成故障。

#### 处置二：给 App 授予「本地网络」权限

`--install` 已经把程序部署成 `~/Applications/PrintAgent.app`，并在 launchd plist 里写了
`AssociatedBundleIdentifiers` —— 没有这两样，系统**找不到可授权的责任主体**：既不弹授权
窗口，也不在设置里留下条目，连接被直接拒绝。

```bash
# 打开权限面板,找到「长健农场打印代理」并打开开关
open "x-apple.systempreferences:com.apple.preference.security?Privacy_LocalNetwork"
```

> **实测结论（macOS 26.6.2，2026-09）：装成 App 只是拿到「可以被授权」的资格，不等于
> 已经授权。** 在未授权的机器上按上面这套装完之后，用**同样的 LaunchAgent 形态**（plist 带
> `AssociatedBundleIdentifiers`、指向 App 内的可执行文件）实测仍然是：
>
> ```
> [自检] 192.168.1.133:9100 不可达(0s): ... connect: no route to host
> ```
>
> 瞬间拒绝（0s）说明系统连尝试都没允许。所以「重新 `--install` 装成 App 就好了」这个
> 预期**是错的** —— 装完必须真的去设置面板把它打开；如果面板列表里根本没有这一项，
> 说明系统还没为它生成过授权条目，那就走处置一（CUPS），别在这里耗。
>
> 想让 App 出现在列表里，可以试一次「用图形方式启动它去碰一下局域网」，逼系统弹授权窗口：
>
> ```bash
> open -a ~/Applications/PrintAgent.app --args --probe 192.168.1.133
> ```
>
> 用 `--probe` 而不是直接启动：后者会再拉起一个轮询实例，与开机自启的那个抢同一批任务。

两个已知限制，配置前先知道：

- **权限按代码身份记录，而本程序未购买 Apple 开发者证书**（ad-hoc 签名 + 每次构建的
  UUID 都会变）。因此**每次升级后可能需要重新授权一次**；这也是把处置一列为推荐的原因。
- macOS **无法**把某程序的本地网络权限重置回「未决」状态（FB14944392）。若列表里的开关
  状态异常，只能换一个用户账户验证，或在虚拟机里还原快照。

#### 处置三：按网段整体放行（需 sudo，需重启）

macOS 15.5 起可按网段声明「这些地址不算本地网络」，对所有程序生效：

```bash
# 有线走 AllowedEthernetLocalNetworkAddresses,Wi-Fi 走 AllowedWiFiLocalNetworkAddresses
sudo defaults write com.apple.network.local-network AllowedWiFiLocalNetworkAddresses \
  -array "192.168.0.0/16"
# 必须重启系统才生效
```

适合「打印机网段固定、由技术人员一次性配置」的门店。注意它影响全系统（所有 App 都能访问
该网段的任意地址），安全边界比只授权单个 App 宽。

### 5.2 macOS 合盖后继续运行（收银机合上盖子照常打单）

**一句话**：合盖能不能继续打单，取决于两件**互相独立**的事，少做一件都不成立：

| # | 要做的事 | 谁负责 | 怎么确认 |
|---|---|---|---|
| ① | 用 `caffeinate` 顶住**空闲睡眠** | `--install` 时选「防睡眠」（或 `PRINT_AGENT_NO_SLEEP=1`） | `./p.sh` 的「合盖/睡眠」一节里能看到 caffeinate 进程 |
| ② | 关掉系统级的**合盖睡眠**开关 | 需 root；`op/mac/install.sh` 会提示并代为执行 | `ioreg -r -c IOPMrootDomain -d 1` 的输出里有 `"SleepDisabled" = Yes` |

#### 为什么 ② 不能省（也是最容易被漏掉的一步）

合盖触发的是 **Clamshell Sleep**，属于电源管理层的独立触发器，`caffeinate` 用的
IOPMAssertion **抑制不了它**。这不是推理，电源日志里能直接看到接了电源也照样睡：

```
Entering Sleep state due to 'Clamshell Sleep':TCPKeepAlive=active Using AC (Charge:100%)
```

也就是说「装了 caffeinate 就等于合盖不睡」是错的判断。真正关掉它的是：

```bash
sudo pmset -a disablesleep 1     # 关闭合盖睡眠(需管理员密码,一次性)
sudo pmset -a disablesleep 0     # 恢复「合盖即睡」
```

（`disablesleep` 是 pmset 的合法设置但没写进 man 手册，可以用「关键字写错会报
`Usage: pmset <options>`、写对则报 `must be run as root`」来验证本机支持它。
读取不需要 root：`SleepDisabled` 是 IOPMrootDomain 的属性。）

#### 为什么 ① 也不能省

合盖后**没有任何输入**，系统的**空闲睡眠计时器**会照常到点。`caffeinate` 负责挡住
这一路。这也是为什么 plist 里用的是 `caffeinate -i -s` 而不是只有 `-s`：
`man caffeinate` 写明 `-s` 的断言「**仅在交流电源下有效**」，只给 `-s` 时电池供电
仍然会空闲睡眠，而 `-i` 不受电源限制。

#### 必须接交流电源

合盖运行会持续耗电并发热。门店机器请插电、留出散热空间 —— 常年合盖满载运行的
MacBook 机身温度不低，这是这套方案的固有代价。

#### 怎么判断有没有生效

```bash
./p.sh
```

```
== 合盖/睡眠(合盖后能否继续打单) ==
合盖睡眠开关: 已关闭(合盖不睡)
供电:         Now drawing from 'AC Power'
屏幕盖状态:   Yes(Yes=已合盖)
caffeinate:
  13705 /usr/bin/caffeinate -i -s /Users/x/Applications/PrintAgent.app/Contents/MacOS/print-agent
  → 已启用防睡眠,空闲睡眠被顶住
最近的合盖睡眠记录(有输出=确实因合盖睡过):
  (无)
```

「最近的合盖睡眠记录」最直观：配置生效后合上盖子，这里不应再新增记录；
若仍在新增，说明第 ② 步没落到实处。

`--install` 每次都会核对一次并在缺 ② 时明确告警（`reportDarwinLidClosedReadiness`），
不会让门店停留在「以为配好了」的状态。

---

## 四、它到底做了什么（实现要点）

| 环节 | 谁做 | 说明 |
|---|---|---|
| 排版（店名、菜品、合计、免单/挂账标注） | 云后端 `print/ticket.go` | 与直连/飞鹅云**共用同一份渲染结果**，三种通道打出来长得一样 |
| ESC/POS 编码（GBK + 切纸） | 云后端 `print/escpos.go` | 在**代理取单时**现场编码，base64 下发 |
| 字节搬运 → 打印机 | 代理程序 | base64 解码 → `Dial(IP:9100)` → 写入 → 关闭；份数=重复发送（`--print-via tcp`，默认） |
| 字节搬运（CUPS 通道） | 代理程序 + 系统打印服务 | `--print-via cups/auto` 时改由 `lp -d <队列> -o raw` 提交，真正连打印机的是 `cupsd`（root launchd daemon，不受本地网络权限约束）；队列由 `lpstat -v` 的 `socket://` URI 自动发现或 `PRINT_AGENT_CUPS_QUEUE` 手填，详见 §3.5.1 |
| DLE EOT 状态回读 | 代理程序 | 写完后、关连接前补发 `DLE EOT 2/3/4` 三条查询，回读缺纸/盖板开/暂停/纸将尽/不可恢复错误；不应答机型记入负面缓存 30 分钟，期间跳过查询、不阻塞出票。**仅直连通道有此能力**，CUPS 通道回执里 `printerStatus` 为空 |
| 拨号熔断冷却 | 代理程序 | 直连失败后 30 秒内不再重拨该 `IP:port`（避免每单白等 5 秒拖慢同批次其它打印机）。`auto` 通道下只有**直连与 CUPS 都失败**才记冷却 —— 否则下一条任务会被冷却挡住，永远走不到明明能成功的 CUPS |
| 离线打印机熔断 | 代理程序 | 某 IP:9100 拨号失败后冷却 30 秒；冷却期内的任务**挂起不回执**（任务保持「打印中」，60 秒租约到期后自动回到队列重试），不拖慢其它打印机 |
| 长轮询 | 代理程序 + 云后端 | pull 带 `wait`（0–30 秒，默认 25），有任务立即返回；`wait=0` 退回纯轮询 |
| 队列与重试 | 云后端 `tb_print_job` | 见下 |
| 安装与自启动 | 代理程序 | `--install` 写同目录 `agent.env` 并注册自启动（macOS launchctl / Windows 任务计划 / Linux systemd）；`--uninstall` 移除自启动（保留 `agent.env`） |

**为什么把编码放在后端而不是代理里**：协议一旦要改（换纸宽、加二维码、改切纸方式），
只改后端即可，不必逐家门店更新代理程序。代理因此只有 ~400 行、不懂任何打印协议。

**任务 TTL 过期作废**

入队任务按单据类型设滞留上限，**过期 = 作废，不是「尽快打出来」**——陈旧厨房单突然出纸，
后厨会照着过期单做菜，宁可不打（日志有记录、可人工补打）：

| 单据类型 | 默认滞留上限 | 环境变量 |
|---|---|---|
| kitchen（厨房单/加菜单） | 120 分钟 | `AGENT_JOB_TTL_KITCHEN_MIN` |
| guest（食客小票） | 1440 分钟 | `AGENT_JOB_TTL_GUEST_MIN` |
| test / 其它 | 30 分钟 | `AGENT_JOB_TTL_TEST_MIN` |

环境变量 ≤0 表示关闭对应类型的 TTL。过期任务不再下发给代理；后台每 10 分钟把
`pending/claimed` 里的过期任务置为失败，打印日志注明「任务未在有效期内送出,已自动作废,可人工补打」。

**可靠性与重试**

- 下单 → 票据入队（`tb_print_job`），**立刻**在「打印日志」里记一条 `排队中`；
- 代理取单时把任务标记为「打印中」并带 **60 秒租约**：代理进程被杀/断电，超时后任务
  会被重新放回队列，不会永远卡住；
- 打印失败 → 退避 10 秒重试，**最多 3 次**（这 3 次都是**真实尝试**：取单时预扣、代理上报
  `skipped`「未尝试」时归还，所以打印机只是短暂不可达不会被提前判死，详见 §八 ack 协议）；
- 3 次都失败 → 该任务标记为「放弃」，日志变`失败`并写明原因，商家可在「打印日志」里补打；
- 代理恢复上线后，积压的任务会**按时间顺序依次补打**出来 → 这就是「打印机离线不丢单」。

**幂等投递（不会重复出票）**

「回执丢失 → 云端重发」本是打印系统里最顽固的重复出票来源。这里用**幂等号 + 本地落盘**把它关掉：

1. 每条任务入队时生成一个永不变更的 `deliveryId`；
2. 代理打印成功后，**先把 `deliveryId` 写进本地状态文件并 fsync，再回执**；
3. 若回执因断网丢失，任务租约超时后重发时仍带**同一个** `deliveryId`；
4. 代理取到后一查本地状态：这号已打过 → **跳过真正打印，直接回执成功**。

状态文件默认在代理程序同目录的 `print-agent.state`（可用 `--state` / `PRINT_AGENT_STATE` 改），
超过 1 万条会自动截断最老的一半。残余窗口只剩「打印成功 → 落盘」之间的毫秒级断电——
这在应用层已无法再压缩（再往下需要打印机端事务回执，不现实）。实测：模拟「回执丢失、
任务重发」后，打印机只收到一次连接，没有重复出纸。

**日志状态速查（打印日志页）**

| 状态 | 含义 | 该做什么 |
|---|---|---|
| `排队中` | 已入队，等代理取单 | 看代理是否在线；离线就去看门店那台设备 |
| `已送出` | 代理回报写入成功 | 正常 |
| `失败` | 重试 3 次仍未成功 | 看说明列（多为「无法连接打印机 xxx.xxx.xxx.xxx:9100」） |

**`ok` 到底表示什么**

agent 通道的 `ok=true` 始终表示「字节成功写进打印机 socket」，不代表「纸一定出来了」。
缺纸/盖板开等是 v2 通过 DLE EOT 回读的**附加观测**：有异常时打印日志 `detail` 会带
「打印机报告:缺纸」等字样与 raw 原文（如
`本地打印代理已送出(192.168.1.8:9100)，但打印机报告:缺纸(raw 0220/--/--)`），
方便区分「纸的问题」还是「网络的问题」。v2 起 agent 通道的 `cost_ms` 记录「入队 → 送出」的排队耗时。

---

## 五、排障表

| 现象 | 原因与处理 |
|---|---|
| **不知从哪查起 / 想一次看全** | 先跑 `./print-agent --doctor`（**体检**）：配置、自启动、打印通道与 CUPS 队列、防睡眠与合盖、云端连通一次查完，每项都直接给出处置命令；有「[问题]」项时退出码为 1。想连打印机那段一起查就加 `--probe 192.168.1.133` |
| **macOS 打不出纸，且不想逐步排查** | `./print-agent --setup-cups <打印机IP>`：建队列 + 打开 CUPS 通道 + 重启 + 核对，一条命令（见 §3.5.1，不需要管理员密码） |
| 前端告警「本地打印代理尚未配置令牌」 | 云端没配代理令牌。去「系统配置 → 打印代理」点「签发新令牌」 |
| 代理启动即退出，提示「代理令牌不正确」 | 令牌与云端不一致（改过令牌但没同步）。重新复制云端令牌到 `agent.env` |
| 代理启动即退出，提示「服务端未启用本地打印代理」 | 云端 `agent_token` 为空。去「系统配置 → 打印代理」签发新令牌 |
| 状态显示「代理已离线」 | 门店那台设备上的 print-agent 没在跑 / 已关机 / 出站 443 被拦。登录该设备确认进程与网络 |
| 任务一直是「排队中」，但代理显示在线 | 代理取到了但卡在打印。看代理日志里是否有 `无法连接打印机`；确认打印机 IP 填的是门店内网地址、打印机已开机 |
| 想确认「代理 → 打印机」通不通（不靠出单） | 在门店设备上跑 `./print-agent --probe 192.168.1.100`（多个 IP 逗号分隔，端口缺省 9100）：直接拨 `IP:9100` 并回读状态，一台一行结论。`--print-via cups/auto` 时还会一并列出**自动发现到的 CUPS 队列**以及每个目标**实际会走哪条通道**；加 `--probe-print` 会吐一张 ASCII 自检页（CUPS 通道下额外再从 CUPS 送一张，把整条链路验完）。纯本地、不连云端、不入队，不消耗重试次数；全部可达退出码 0，有不可达为 1 |
| `--probe` 报「目标未通过地址安全校验」 | 填的不是门店内网 IP：不支持域名，回环/0.0.0.0/169.254.* 一律不放行（与打印任务同一条防线，防止代理被当扫描器） |
| 日志显示「无法连接打印机 192.168.x.x:9100」 | 代理设备与打印机不在同一网段 / 打印机休眠或关机 / IP 变了（建议在打印机上设静态 IP 或做 DHCP 保留） |
| 出纸是乱码或只出半张 | 纸宽设错（58mm 应选 32、80mm 选 48），或该机型不是 ESC/POS 指令集（少数标签机/针式机不支持） |
| 同一张票打了两遍 | 正常情况**不会发生**（幂等投递已兜底）。若仍出现，通常是误用：换机器/换 `--state` 路径导致去重状态丢失、或多台代理共用一台打印机且各自维护独立 state —— 一台打印机只应由一台代理负责 |
| 队列大量积压想清掉 | 「打印机管理 → 清空队列」：丢弃所有未送出的任务，已打印的不受影响 |
| 打印日志带「打印机报告:缺纸 / 盖板开」 | 打印机物理缺纸或盖板没合上，不是网络问题。换纸/合盖后下一单自动恢复（状态回读是附加观测，不影响出票结果） |
| 打印日志出现「任务未在有效期内送出,已自动作废」 | TTL 机制按单据类型作废过期任务（厨房单 120 分钟 / 食客小票 1440 分钟 / 测试页 30 分钟），语义是「过期=作废」。如需这张单，到「打印日志」人工补打 |
| 代理日志出现「[等待] …熔断冷却中…」 | 该打印机刚拨号失败，进入 30 秒冷却，期间不再重拨以免拖慢其它打印机。后续按云端能力分两种：日志写「已上报「未尝试」」→ 任务立刻退回队列且**不消耗重试次数**，冷却结束即重试（云端 v2.1+）；日志写「本轮不回执」→ 任务保持「打印中」，60 秒租约到期后自动重试（老云端兼容路径）。两种都会在冷却结束后恢复 |
| macOS 首次运行被拦「无法打开，因为无法验证开发者」 | Gatekeeper 拦未签名二进制。执行 `xattr -d com.apple.quarantine <文件>`，或 Finder 右键 → 打开 |
| **macOS**：日志反复「无法连接打印机 … `no route to host`」，但手工跑 `--probe` 能通 | 系统的「本地网络」隐私权限拦下了开机自启的代理（终端及其子进程不受限制）。按 §3.5.1 处置：推荐加 `PRINT_AGENT_PRINT_VIA=auto` 改走系统打印服务；或到「系统设置 → 隐私与安全性 → 本地网络」允许本 App。注意 macOS **无法**把权限重置回未决状态，改动后可能需要在别的用户账户或虚拟机里验证 |
| **macOS**：合上盖子后代理就离线 / 不打单 | 合盖触发的是 Clamshell Sleep，`caffeinate` 挡不住它，必须另设 `sudo pmset -a disablesleep 1`；同时确认 `--install` 时选了「防睡眠」（否则空闲睡眠也没人挡）且接了电源。用 `./p.sh` 的「合盖/睡眠」一节核对，详见 §3.5.2 |
| **macOS**：日志里出现成段的「云端自检通过 / 已连接云端」，中间隔着十几分钟的空档 | 那些空档就是系统在睡眠（睡眠期间进程全停、网络断开）。看 `pmset -g log` 里的 `Entering Sleep state due to '...'`：若是 `Clamshell Sleep` 就按上一条处置；若是 `Maintenance Sleep` / `Idle Sleep` 说明顶住空闲睡眠的那一半没生效（`--install` 没选防睡眠，或 caffeinate 进程不在） |
| 代理日志出现「不应答状态查询,30分钟内跳过查询」 | 该机型不支持 DLE EOT 状态查询，属正常兜底：30 分钟内跳过查询、不阻塞出票，回执里 `queried=false` |
| 换了公网域名 / 上了 HTTPS | 改所有代理的 `PRINT_AGENT_SERVER` 后重启代理 |
| 想彻底卸载代理 | 运维脚本 `./uninstall.sh --all`（Windows `uninstall.bat --all`，Linux 加 `sudo`）：停进程 + 移除自启动 + 删产物与 `agent.env`；只移除自启动则用 `<产物> --uninstall`（**保留** `agent.env`，下次 `--install` 可直接复用） |
| 重复执行 `--install` | 会覆盖自启动注册（以最后一次为准），但保留已有 `agent.env` 配置，不会冲掉地址/令牌 |

排障利器（两台机器、两个方向，别混用）：

- `--probe 192.168.1.100`：只查**本机 → 打印机**（拨 `IP:9100` + 状态回读，纯本地、断网也能查）；
- `--once`：只跑一轮并打印详细日志，验证的是**本机 → 云端**（令牌/地址/拉取）；
  它是否拨打印机取决于队列里有没有任务 —— 想顺便看出纸，先在管理后台点一次「测试打印」再跑。

---

## 六、发版与门店更新

**为什么要有版本号**：代理跑在各家门店自己的设备上，升级不受云端控制——发了新版，
哪家门店还停在旧版、谁缺了哪些修复，没有版本号就完全不可见。把软件版本打进二进制、
随心跳上报，管理端才能一眼看出「谁落后了」，升级也有据可依。

### 1. 开发机发版

```bash
cd backend
make VERSION=x.y.z build-agent    # 一次产出五平台产物,版本号写进二进制
```

`VERSION` 默认 `1.0.0`，`make VERSION=x.y.z` 覆盖（Makefile 里
`LDFLAGS -X main.version=$(VERSION)` 注入）。发版后先核对版本号：

```bash
./print-agent/bin/print-agent-<os>-<arch>[.exe] --version   # 或短参数 -v
# version=x.y.z
# buildTime=2026-09-22T10:35:01Z
# gitCommit=abc1234
```

三行依次是软件版本、构建时间（UTC）、Git 提交短哈希；逐台下发前先确认这里的版本号正确。

### 2. 云端配置

发版后在管理后台「系统配置 → 本地打印代理 → 代理最新版本号」填 `x.y.z`（配置项
`agent_latest_version`）。代理每次启动自检（含 `--once`）时，云端会把这个值随 `ping`
响应下发；若与代理自身版本不一致，代理日志会输出：

```
[更新] 云端最新版本 vx.y.z,当前 vX.Y.Z,请下载新产物替换后重启(升级步骤见 docs/print-agent.md)
```

留空则不提示任何门店。管理端「打印机管理」页的代理列表会显示每台代理上报的版本，
落后于云端最新版本的会带「可升级」tag。

> **顺序要求**：先让服务器 `print-agent/bin/` 里的产物就位（`release.sh` + `deploy.sh`
> 自动部署；手动部署则先拷产物），**再**填这里的版本号。反过来的话门店 `--upgrade`
> 下载到的是旧产物、只有版本号是新的 —— 下载端点的 sha256 只保证传输完整，
> 不校验产物内嵌版本，两边一致性靠这个操作顺序保证。

### 3. 门店升级

**推荐：自助升级**（一条命令，无需人工拷文件）：

前提（云端侧，一次配好）：服务器的部署目录里已放 `print-agent/bin/` 的五个产物
（`release.sh` 发布包自带），且管理后台已配置「代理最新版本号」。

```bash
# 门店设备上,到代理所在目录执行:
./print-agent --upgrade          # macOS / Linux
print-agent-windows-amd64.exe --upgrade   # Windows(cmd 或双击后加参数)
```

代理会 ping 云端核对最新版本 → 下载对应平台产物 → sha256 校验 → 替换自身 →
提示重启（Windows 自动触发任务计划重启）。升级**不需要重新配置令牌**：
`agent.env` 原样保留，重启后自检上报新版本，管理端「可升级」tag 消失。

**备选：整包替换**（门店网络差、或服务端未部署产物时）：

1. 把对应平台的新产物拷到门店那台设备；
2. 覆盖旧文件：Windows 上先退出旧进程（任务计划里的 `DiningPrintAgent` 或正在运行的
   exe），或用 `<产物> --uninstall` / `<产物> --install` 往返一次；Linux/macOS 覆盖文件后
   重启进程（`systemctl restart print-agent` / launchd 重载）；
3. 重启后代理自检会上报新版本，管理端代理列表的版本随之更新、「可升级」tag 消失。

升级**不需要重新配置令牌**：`--uninstall` 不删 `agent.env`，重复 `--install` 也只覆盖
自启动注册、保留已有 `agent.env`，地址/令牌原样复用。

### 4. 版本规则建议

- 用**主版本.次版本.修订号**（如 `1.2.0`），有行为变化就升版本号，云端填同一个值即可；
- 云端与代理在 **v2 协议兼容期内**均可平滑升级：协议字段只增不改，新字段对老版本透明；
- **v1 代理继续可用**，只是不具备 v2 的状态回读、长轮询等能力——版本号让它们可被
  管理端识别出来，便于逐步淘汰；
- 注意区分**软件版本**（`buildVersion`，随请求体上报，展示/提示用）与**协议版本**
  （`version`，int=2，能力协商用），二者不是一回事。

### 5. 代码签名（可选，消除首次运行安全提示）

产物未签名时，门店首次运行会看到 Windows SmartScreen / macOS Gatekeeper 提示
（门店侧的绕过方法已写进 `门店安装说明.md`）。正式对外分发建议签名：

```bash
# macOS(需 Apple Developer ID 证书;公证后首次运行无任何提示)
codesign -s "Developer ID Application: 你的公司" <产物>
xcrun notarytool submit <产物>.zip --apple-id xxx --team-id xxx --password xxx --wait
xcrun stapler staple <产物>

# Windows(需 OV/EV 代码签名证书)
signtool sign /a /tr http://timestamp.digicert.com /td sha256 /fd sha256 <产物>.exe
```

签名与 `--upgrade` 自助升级不冲突：下载端点返回 sha256 保证传输完整，签名保证发布可信；
二者是不同层次（传输完整性 vs 来源可信）的保障，可以只启用其一。

---

## 七、与飞鹅云通道的选型对比

| | 本地打印代理 | 飞鹅云 |
|---|---|---|
| 门店已有 9100 网络机 | **可直接复用** | 需换机 |
| 需要装驱动 | 不需要 | 不需要 |
| 门店需要一台常开机设备 | **需要** | 不需要 |
| 打印机自带 4G 也能用 | 不适用（要能连内网） | 可以（机器自带流量卡） |
| 费用 | 一次性（设备） | 按台/按年付费 |
| 出纸延迟 | 3 秒轮询，基本无感 | 云→机器链路，通常更快 |
| 打印机离线不丢单 | 支持（云端排队） | 支持（厂商云端排队） |

> 两种通道可以**共存**：后厨用已有的网络机走代理，前台小票机用飞鹅云，
> 在打印机管理里逐台选通道即可。

---

## 八、接口参考（自研代理/换语言重写时用）

四个接口都在 `/api/agent/print/` 下，鉴权用请求头 `X-Agent-Token`
（也兼容 `Authorization: Bearer <token>`）。JSON 接口统一响应体 `{code, msg, data}`，
`code=200` 为成功；download 直接响应二进制。

**协议 v2（v1 代理零感知共存）**：三个 JSON 接口的请求体新增 `version`、`capabilities`
（pull 另加 `wait`），请求头新增 `X-Agent-Version`（v1 代理不带此头，服务端按 v1 对待）；
响应 `data` 统一新增 `protoVersion: 2` 与 `serverCapabilities`（当前为
`["printer-status","long-pull","ack-skipped"]`）。
全部是**只增字段、不改语义**——老代理按 JSON 忽略未知字段，云端先升级是安全的。

### `POST /api/agent/print/ping` — 心跳 / 启动自检

请求（v2 代理）：

```json
{ "agentId": "门店收银电脑", "version": 2, "capabilities": ["printer-status", "long-pull", "ack-skipped"] }
```

响应 `data`：

```json
{
  "shopName": "长健农场 柴火农家土菜", "serverTime": "2026-09-22 10:35:01",
  "pending": 0, "dead": 0,
  "protoVersion": 2, "serverCapabilities": ["printer-status", "long-pull", "ack-skipped"],
  "oldestPendingSec": 0
}
```

`oldestPendingSec` 是最老 pending 任务已等待的秒数（可观测性）。

### `GET /api/agent/print/download` — 自助升级下载

代理 `--upgrade` 时调用：按平台下载服务端部署的最新产物。鉴权与其它接口一致
（`X-Agent-Token` 请求头），产物来自服务器的 `AGENT_BIN_DIR`（默认发布包内
`print-agent/bin`，可按 `AGENT_BIN_DIR` 环境变量覆盖）。

```
GET /api/agent/print/download?goos=windows&goarch=amd64
```

响应（HTTP 200）：`application/octet-stream` 二进制产物；`goos`/`goarch` 不在发布矩阵
（linux/darwin/windows + amd64/arm64，windows 仅 amd64）时 400，服务端缺该产物时 404。

| 响应头 | 含义 |
|---|---|
| `X-Agent-Sha256` | 产物 sha256（hex），客户端强制校验，防传输损坏/篡改 |
| `X-Agent-Version` | 与 `agent_latest_version` 同源的版本号，客户端与 ping 结果核对 |
| `Content-Disposition` | `attachment; filename="print-agent-<os>-<arch>[.exe]"` |

### `POST /api/agent/print/pull` — 取单

请求（v2 代理）：

```json
{ "agentId": "门店收银电脑", "limit": 10, "version": 2,
  "capabilities": ["printer-status", "long-pull", "ack-skipped"], "wait": 25 }
```

`wait` 是长轮询等待秒数，0–30，**0 = v1 短轮询行为**；有任务立即返回，无任务最多 hold
`wait` 秒。响应 `data`：

```json
{
  "jobs": [
    {
      "jobId": 12, "printerId": 1, "printerName": "后厨厨房单打印机",
      "ip": "192.168.1.8", "port": 9100, "copies": 1,
      "docType": "kitchen", "orderNo": "D20260922...", "tableNo": "A1",
      "deliveryId": "20b76d7c67f008b349ce8dbc3bad72a4",
      "payload": "<base64 的 ESC/POS 字节流:初始化 + GBK 文本 + 切纸>"
    }
  ],
  "serverTime": "2026-09-22 10:35:01",
  "protoVersion": 2, "serverCapabilities": ["printer-status", "long-pull", "ack-skipped"]
}
```

`jobs[]` 字段与 v1 完全一致。处理方式不变：`base64 解码 → 对 ip:port 建 TCP 连接 →
原样写入 → 关闭`，并**重复 copies 次**（ESC/POS 没有联数指令）。

### `POST /api/agent/print/ack` — 回执

```json
{
  "results": [
    { "jobId": 12, "ok": true, "detail": "",
      "printerStatus": { "queried": true, "raw": "0220/--/--",
        "paperOut": true, "paperNearEnd": false, "coverOpen": false,
        "paused": false, "error": false } },
    { "jobId": 13, "ok": false, "detail": "无法连接打印机 192.168.1.9:9100" },
    { "jobId": 14, "ok": false, "skipped": true, "retryAfter": 25,
      "detail": "打印机连接熔断冷却中(约 25 秒后重试)" }
  ]
}
```

`printerStatus` 每条**可选**：`queried` 表示是否成功读到 DLE EOT 应答；`raw` 是三条查询
应答的原样 hex（查询字节+状态字节，逐条以 `/` 分隔，未应答记为 `--`）；`paperOut` 缺纸、
`paperNearEnd` 纸将尽、`coverOpen` 盖板开、`paused` 已暂停、`error` 不可恢复错误。
缺省（v1 代理或不查询）不影响 `ok` 判定。

`ok=false` 时 `detail` 会原样写进「打印日志」的说明列，请填人话（会直接展示给商家）。

`skipped=true` 表示**代理没有真正尝试打印**（该打印机正处于拨号熔断冷却，本机一次都没拨）。
它与 `ok=false` 的关键区别：云端把任务退回队列并**归还**本次取单消耗的尝试次数，
`retryAfter` 给出建议的重新下发秒数（通常是冷却剩余）。这样 `attempts` 严格等于真实尝试次数，
打印机只是短暂不可达（刚上电 / WiFi 未就绪）不会被误判成「已放弃」而丢单。
仅当云端在 `serverCapabilities` 里声明 `ack-skipped` 时代理才会发这两个字段；
老云端不声明，代理退回「挂起不回执」的兼容行为（任务保持「打印中」，60 秒租约到期后自动回到队列），效果等价、只是要等一个租约。

响应 `data`：`{ "accepted": 2, "done": 1, "rejected": 0, "skipped": 0, "protoVersion": 2, "serverCapabilities": [...] }`。
`serverCapabilities` 当前为 `["printer-status","long-pull","ack-skipped"]`。

**注意**：取单成功后任务即进入 60 秒租约期，若一直不回执，任务会被重新下发（可能重复打印）。
因此打印成功后请尽快回执，并像 `print-agent` 一样对回执本身做几次重试。

---

## 九、上线检查清单

> 每项验证方法以代码事实为准。2026-09-22 实施当日的验证结果如下；2026-09-23 补充「熔断冷却不丢单」修复项
> 并复跑全量（详见下表带 2026-09-23 标注的行）。正式上线前请以线上环境复跑一遍。

| 勾选 | 检查项 | 验证方法 | 实施当日结果 |
|---|---|---|---|
| [x] | 后端构建 / 静态检查 / 单测全绿；前端构建通过 | `backend/` 下 `go build ./...`、`go vet ./...`、`go test ./...` 退出码 0；`frontend/` 下 `npm run build` 通过 | 全绿（多次回归验证；期间偶发编译错误均为工作区进行中重构的中间态，稳定后复验通过）。2026-09-23 本次改动后复跑：`go build` / `go vet` / `go test ./...` 均退出码 0 |
| [x] | 代理独立 module 单测全绿、五平台交叉编译通过 | `cd print-agent && go test ./...`；`cd backend && make build-agent` 五个产物齐全且 `<产物> --version` 三行正常 | 全绿；实测 `make VERSION=2.0.0 build-agent` → `--version` 输出 `version=2.0.0/buildTime/gitCommit` 三行。2026-09-23 本次改动后 `go test ./...` 复跑全绿（新增 skipped / 熔断用例） |
| [x] | 迁移脚本两库可重复执行 | `incr/mysql` 与 `incr/sqlite` 下 `tb_print_job`/`tb_print_agent` 脚本重复执行无表结构报错 | SQLite 经测试基建建库验证；MySQL 版与 SQLite 版同构、`CREATE TABLE IF NOT EXISTS`，人工审查确认 |
| [x] | v1 代理与 v2 云端共存 | 旧报文调 `ping`/`pull`/`ack`，云端按 v1 对待、行为不变 | 协议「只增字段」原则 + 单测（AckAgentJob nil 状态、请求新字段均可选）验证。2026-09-23 补充：`skipped` 不传时按失败重试（`TestAgentAckWithoutSkippedStillConsumesAttempt` 锁定） |
| [x] | 幂等投递回归 | 模拟「回执丢失 → 任务重发」，同一 `deliveryId` 被跳过 | `TestRunCycleIdempotentDedup`（net.Pipe 注入）覆盖，打印机只收一次连接 |
| [x] | 授权域回归 | 代理 A 只授权打印机 1，取不到打印机 2 的任务 | `TestClaimPrintJobsScopesByPrinterIDs` 断言域外任务保持 pending 不被 claim |
| [x] | TTL 过期作废生效 | 过期厨房单不派发、日志记「已自动作废」可补打 | `TestClaimPrintJobsSkipsExpiredJobs` + `TestAgentJobTTL*` + 作废路径代码核验 |
| [x] | 代理地址校验生效 | 回环/非法地址拒绝连接并回执失败 | `addr_test.go` 断言精确拒绝文案，校验位于拨号前（代码核验） |
| [x] | 熔断冷却不消耗重试次数（兼容路径：挂起不回执） | 云端未声明 `ack-skipped` 时，冷却期内的任务**不拨号也不回执**，保持 claimed 等 60 秒租约到期自动回到队列 | 2026-09-23 实测：`TestRunCyclePrinterUnreachableThenCoolingDown` 断言第二轮 ack 数不增、拨号次数不增，冷却清除后恢复真实拨号并回执 |
| [x] | `skipped` 回执归还尝试次数 | 云端声明 `ack-skipped` 后，代理上报 `skipped=true`，任务退回 pending 且 `attempts` 回退 1（严格等于真实尝试次数），`next_try_time` 按 `retryAfter` 顺延 | 2026-09-23 实测：`TestRunCycleCoolingReportsSkippedWhenSupported`（代理侧字段契约） + `TestAgentAckSkippedReturnsAttempt`（云端侧 attempts 归零）；`TestAgentAckWithoutSkippedStillConsumesAttempt` 锁定老报文行为不变 |
| [x] | 多打印机互不拖累 | 一台离线机的任务被冷却拦下后，同批次其它打印机的任务照常打印；写入超时（半死不活的打印机）同样计入冷却 | 2026-09-23 实测：冷却按 `ip:port` 独立（`print.go`）；`printJob` 写失败补 `markDialCooldown`，避免每单各耗 5 秒拖慢同批次 |
| [x] | `--install` / `--uninstall` 往返实测 | 自启动注册/移除正确、`agent.env` 保留 | macOS 实测：plist 渲染正确、`launchctl list` 已加载 → 卸载后 plist 删除、agent.env 保留 |
| [x] | 版本提示生效 | 云端填「代理最新版本号」后代理日志出现 `[更新]` 提示 | 代码核验 + `TestPingBodyContainsBuildVersion`；提示文案与文档逐字一致 |
| [ ] | 灰度顺序确认 | 先云端后门店，不反向发布 | 待正式上线执行时确认（上线操作由人工执行）。本次 `skipped` 改动同理：云端先升、代理后升亦可，代理按 `serverCapabilities` 自动降级为「挂起不回执」 |
