# 门店本地打印代理（print-agent）

> 跑在门店内网的一台常开机设备上：**出站** HTTPS 拉取云端打印任务，
> 再向门店内网 `打印机IP:9100` 直发 ESC/POS 字节流。
> 门店不需要公网 IP、不需要端口映射、不需要装打印机驱动。

完整部署手册（排障表、协议细节、通道选型）：**[`../docs/print-agent.md`](../docs/print-agent.md)**。
本目录是代理程序的**独立 Go module**（纯标准库、零依赖），与后端解耦，可单独构建发版。

## 目录结构

```
print-agent/
├── main.go              # 入口与主循环:取单 → 打印 → 回执
├── config.go            # 命令行/环境变量解析、HTTP 客户端、agent.env 加载
├── api.go               # 云端接口:pull/ack/ping 与请求/响应结构
├── print.go             # 打印机 TCP 会话、DLE EOT 状态、拨号熔断(含写入超时冷却)
├── cups.go              # CUPS 打印通道(--print-via cups/auto):绕开 macOS 15+ 的「本地网络」隐私限制
├── cups_setup.go        # --setup-cups:一条命令建好 CUPS 队列并打开通道(macOS)
├── probe.go             # --probe 打印机连通性自检:拨 IP:9100 + 状态回读 + 可选自检页
├── doctor.go            # --doctor 一条命令体检:配置/自启动/通道/防睡眠/云端,逐项给处置命令
├── state.go             # 本地幂等去重状态(落盘)
├── watchdog.go          # 假死看门狗(卡死自杀退出,交由守护拉起)
├── log.go               # 日志与致命错误输出(stdout + 日志文件双写)
├── addr.go              # 打印机地址安全校验
├── install.go           # 一条命令安装/卸载与三平台自启动注册
├── utf8_windows.go / utf8_other.go  # Windows 控制台 UTF-8(其余平台空实现)
├── go.mod               # module cjfarm/print-agent(纯标准库、零第三方依赖)
├── Makefile             # 本目录独立构建:make build / build-all / package
├── agent.env.example    # 门店配置模板 → 复制为 agent.env
├── 门店安装说明.md       # 随 make package 发给门店的一页式安装说明(面向店员)
├── op/                  # 门店运维脚本(按操作系统分文件夹,各含 install/start/stop/p/upgrade/uninstall 六种)
│   ├── mac/             #   macOS: install.sh / start.sh / stop.sh / p.sh / upgrade.sh / uninstall.sh
│   ├── win/             #   Windows: install.bat / start.bat / stop.bat / p.bat / upgrade.bat / uninstall.bat(双击即可)
│   ├── linux/           #   Linux: install.sh / start.sh / stop.sh / p.sh / upgrade.sh / uninstall.sh
│   ├── cloud/           #   云端侧: check-online.sh(管理端账号查代理在线状态,不随包发门店)
│   └── README.md        #   用法与各 OS 对应关系
├── deploy/
│   ├── print-agent.service               # Linux systemd 自启动单元(--install 通过 go:embed 复用)
│   ├── print-agent.plist                 # macOS launchd 自启动模板(--install 通过 go:embed 复用)
│   ├── PrintAgent-Info.plist             # macOS .app bundle 的 Info.plist 模板(--install 部署 App 时复用)
│   ├── PrintAgent.icns                   # App 图标(10 个尺寸;--install 写进 App 的 Resources)
│   └── print-agent-windows-task.xml      # Windows 任务计划(开机自启+失败重启;--install 通过 go:embed 复用)
├── assets/
│   └── PrintAgent-1024.png               # 图标母版(不参与编译内嵌;改图标从这里重出 .icns)
├── bin/                 # 编译产物(五平台二进制;gitignore 排除)
└── dist/                # make package 组装的各平台部署包(压缩包带版本号+sha256;gitignore 排除)
```

## 构建与打包（五平台交叉编译 + 门店部署包）

两种方式等价，产物统一落在本目录 `bin/`：

```bash
cd backend && make build-agent     # 从后端目录统一发版
# 或在本目录:
make build-all                     # 五平台交叉编译 → bin/print-agent-<os>-<arch>[.exe]
```

| 产物(bin/) | 用在哪 |
|---|---|
| `print-agent-darwin-amd64` / `-darwin-arm64` | Intel / Apple Silicon Mac |
| `print-agent-windows-amd64.exe` | 收银电脑（Windows） |
| `print-agent-linux-amd64` / `-linux-arm64` | 小主机 / 树莓派 |

**打包门店部署包**：把「产物 + 对应平台运维脚本 + 门店安装说明」组装成可直接发给门店的
压缩包（带版本号与 sha256），门店解压后照包内 `门店安装说明.md` 双击/一条命令即可完成安装：

```bash
make package        # → dist/{darwin,windows,linux}/ 目录 + dist/print-agent-v<版本>-<os>.tar.gz/.zip(.sha256)
```

常用命令：

```bash
make build         # 本机编译 → bin/print-agent
make build-all     # 五平台交叉编译
make package       # 组装三平台门店部署包(压缩包带版本号+sha256)
make test / vet    # 测试与静态检查
make clean         # 清理 bin/ 与 dist/
```

本地运行调试（`go run` 时程序在临时目录执行，必须显式 `--env` 指向项目内的 `agent.env`）：

```bash
make env                      # 首次：从模板生成 ./agent.env，填入 PRINT_AGENT_SERVER / PRINT_AGENT_TOKEN
make run                      # go run 运行（读取 ./agent.env；状态文件落在 ./bin/print-agent.state）
make run ARGS="--once"        # 只跑一轮自检排障（可透传任意参数）
```

`agent.env` 与 `print-agent.state` 已在仓库根 `.gitignore` 中排除，不会误入库。

或直接 `CGO_ENABLED=0 go build -trimpath -o ./bin/print-agent-<os>-<arch> .`
（不要直接 `go build .`，会把二进制留在源码目录）。

## 版本与更新

产物内置软件版本号（Makefile `LDFLAGS -X main.version` 注入），发版时用
`make VERSION=x.y.z build-agent` 覆盖默认的 `1.0.0`，`./<产物> --version`（或短参数
`-v`）可查 `version` / `buildTime` / `gitCommit` 三行。

云端在「系统配置 → 本地打印代理 → 代理最新版本号」配置最新版本后，代理每次启动自检
会上报 `buildVersion`，版本落后时代理日志出现「[更新] …」提示、管理端代理列表带
「可升级」tag。

**门店自助升级**：服务器的部署目录里已放 `print-agent/bin/` 的产物（`release.sh`
发布包自带，可用 `AGENT_BIN_DIR` 环境变量改目录），且云端已配置「代理最新版本号」时，
门店一条命令完成升级（下载 → sha256 校验 → 替换自身 → 提示/触发重启，令牌不用重填）：

```bash
./<产物> --upgrade        # macOS / Linux
print-agent-windows-amd64.exe --upgrade   # Windows(自动触发任务计划重启)
```

完整流程（含整包替换的备选方式）见 [`../docs/print-agent.md`](../docs/print-agent.md)
的「发版与门店更新」章节。

## 快速部署（复制即用）

把变量换成门店的值后，下面命令**原样复制**执行即可（以 macOS Apple Silicon 为例）：

```bash
SERVER=https://你的管理后台域名      # 不带 /prod-api、不带结尾斜杠
TOKEN=你的代理令牌
AGENT=print-agent-darwin-arm64        # Intel Mac 用 darwin-amd64;Windows 用 windows-amd64.exe;Linux 用 linux-amd64

./$AGENT --install --server $SERVER --token $TOKEN   # 安装:写 agent.env + 注册开机自启
./$AGENT --once                                       # 自检(应打印「云端自检通过」)
./$AGENT --upgrade                                    # 自助升级(云端配好最新版本号后一条命令)
./$AGENT --probe 192.168.1.100                        # 连通性自检:到打印机这段通不通(不用装 nc)
./$AGENT --doctor                                     # 体检:配置/自启动/通道/防睡眠/云端一次查完(加 --probe IP 连打印机一起查)
./$AGENT --setup-cups 192.168.1.100                   # macOS:一条命令建好系统打印队列并打开 CUPS 通道(不需要管理员密码)
tail -f print-agent.log                               # 看日志(未装成 App 时在程序同目录)
./$AGENT --uninstall                                  # 卸载自启(内置命令,保留 agent.env)
./uninstall.sh                                        # 卸载脚本版:停进程 + 移除自启 + 清运行文件(--all 连产物一起删)
```

> **macOS 15+ 必读**：`--install` 会把程序部署成 `~/Applications/PrintAgent.app`，配置与
> 日志落在 `~/Library/Application Support/PrintAgent/`。这是因为 macOS 15 起连接局域网需要
> 「本地网络」授权，而该授权按 App 识别、且 launchd **agent** 不在 daemon 的豁免范围内。
> 若日志反复出现 `无法连接打印机 … no route to host`、而手工 `--probe` 却能通，就属于这种
> 情况 —— 三条处置办法（推荐改用 CUPS 通道 `PRINT_AGENT_PRINT_VIA=auto`）见
> [`../docs/print-agent.md`](../docs/print-agent.md) 的「macOS 15+ 的「本地网络」权限」一节。

> **macOS 出问题先跑 `--doctor`**：一次查完「配置 / 自启动 / 打印通道与 CUPS 队列 /
> 防睡眠与合盖 / 云端连通」，每一项都直接给出该敲的命令；有 `[问题]` 项时退出码为 1。
> 打不出纸且报 `no route to host` 时，`./print-agent --setup-cups <打印机IP>` 一条命令
> 就能配好系统打印通道（建队列 + 开通道 + 重启 + 核对，可重复执行、不需要管理员密码）。

> **macOS 合盖继续运行必读**：合盖能不能继续打单取决于**两件独立的事** ——
> ① `caffeinate` 顶住空闲睡眠（`--install` 时选「防睡眠」）；
> ② 关掉系统级的合盖睡眠开关（`sudo pmset -a disablesleep 1`，需管理员密码，
> `op/mac/install.sh` 会提示并代做）。
> **`caffeinate` 挡不住合盖睡眠**（那是 Clamshell Sleep，电源管理层的独立触发器），
> 只做 ① 的话盖子一合系统照样睡。用 `./p.sh` 的「合盖/睡眠」一节核对，
> 详见 [`../docs/print-agent.md`](../docs/print-agent.md) 的「macOS 合盖后继续运行」一节。

- **发整包给门店（推荐）**：开发机 `make package`，把 `dist/<os>/` 目录或对应压缩包发给门店，
  门店按包内 `门店安装说明.md` 操作即可（包内就是「产物 + op 脚本 + 说明」三件套，装的是
  `--install` 一条命令这套流程）；
- 不想在命令行明文出现令牌：直接 `./$AGENT --install`，程序会**交互询问**地址与令牌；
- 装好之后的日常运维用仓库 `print-agent/op/` 的运维脚本（**按操作系统分文件夹**，
  各含 `install` / `start` / `stop` / `p` / `upgrade` / `uninstall` 六种：mac 与 linux 为 `.sh`、Windows 为双击即跑的 `.bat`），
  把对应平台文件夹的脚本与产物放同一目录即可（安装也可走 `install.sh`）；完整命令与原理见
  [`../docs/print-agent.md`](../docs/print-agent.md) 顶部「**日常操作速查**」章节；
- `--install` 自动注册三平台自启动（Windows 任务计划 `DiningPrintAgent` / macOS launchd / Linux systemd），
  进程崩溃/被杀由守护自动拉起（`KeepAlive` / `RestartOnFailure` / `Restart=always`），
  假死由内置看门狗自杀退出后拉起；
- 排障利器：
  - `--probe 192.168.1.100[,192.168.1.101:9100]`：**只查「本机 → 打印机」这一段**，
    直接拨 `IP:9100` 并回读状态，一台一行结论（可达/不可达 + 缺纸/盖板开）。
    纯本地、不连云端（断网也能查）、不入队不回执（不消耗云端重试次数）；
    全部可达退出码 0，有不可达为 1。在同一条命令上加 `--probe-print`
    （即 `--probe IP --probe-print`）会真的吐一张 ASCII 自检页；注意它不能单独使用。
    可选 `--probe-timeout`（1–30 秒，默认 5）。
  - `--once`：只跑一轮并打印详细日志（含 v2 的打印机状态回读原文）。
    注意它验证的是**云端**连通；是否拨打印机取决于队列里有没有任务，
    想顺带看出纸效果，先在管理后台点一次「测试打印」再跑。
