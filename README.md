# 扫码点餐管理系统

完整复刻自目标站点 `http://221.226.11.83:58085` 的「扫码点餐管理系统」。

- **前端**：Vue 3 + TDesign + Vue Router + Axios + ECharts（Vite 构建）
- **后端**：Golang + Gin + SQLite / MySQL 双后端（默认 SQLite，纯 Go 驱动 `modernc.org/sqlite`，无需 CGO，天然支持交叉编译；**改环境变量即可切 MySQL**，见 [`docs/mysql-migration.md`](docs/mysql-migration.md)）
- **接口前缀**：`/prod-api`（与目标站保持一致，RuoYi 风格响应 `{code, msg, data}` / `{total, rows, code, msg}`）
- **鉴权**：管理端接口需登录（HMAC 签名的 Token，24h 有效），并按 **角色 → 权限点** 做接口级鉴权（多员工 + 4 个内置角色，见下文「员工与权限」）；顾客点餐端、打印页为公开访问

## 目录结构

```
dining-system/
├── backend/                # Go + Gin 后端（按职责分层到 internal/ 子包）
│   ├── main.go             # 入口：装配 + 路由 + 静态托管 + CORS + 优雅退出
│   ├── config.example.yaml # 部署配置模板（复制为 config.yaml，启动时自动读取，推荐）
│   ├── .env.example        # 环境变量模板（复制为 .env，同样自动读取，二选一）
│   ├── internal/
│   │   ├── model/          # 数据模型 + 金额换算（纯函数，最底层无依赖）
│   │   ├── store/          # 数据库访问：连接/方言适配/建表/种子/各表查询
│   │   ├── service/        # 业务逻辑：价格重算/订单明细校验/订单号
│   │   ├── print/          # ESC/POS 网络打印（异步，失败不影响下单）
│   │   └── handler/        # HTTP 层：响应封装/鉴权/各资源处理器/上传
│   ├── cmd/gensql/         # SQL 脚本生成器（改表结构后重新生成两库脚本）
│   ├── cmd/print-agent/    # 门店本地打印代理（跑在门店内网，出站拉单后直发 9100）
│   ├── scripts/            # 运维脚本（sqlite2mysql.py：SQLite→MySQL 数据搬运）
│   ├── migrations/         # 数据库迁移脚本（SQLite / MySQL 各一套）
│   │   ├── README.md       #   迁移说明、命名规则与表/索引清单
│   │   ├── full/           #   全量：{sqlite,mysql}/schema.sql + seed.sql
│   │   └── incr/           #   增量：{sqlite,mysql}/日期时间_表名_动作.sql
│   └── Makefile            # 构建/打包/交叉编译脚本（输出到 bin/）
├── frontend/               # Vue 3 + TDesign 前端
│   ├── vite.config.js      # 构建配置（assetsDir 对齐后端 /static）
│   └── src/
│       ├── api/index.js    # 全部接口封装 + 登录 + 鉴权拦截
│       ├── router/index.js # 路由 + 登录守卫 + 权限守卫（按 meta.perm 过滤菜单）
│       ├── utils/perm.js   # 登录态与权限点缓存（setAuth/hasPerm/refreshProfile）
│       ├── layout/AdminLayout.vue
│       └── views/          # 登录页 + 13 个管理页（含员工、角色）+ 顾客点餐页 + 打印页
├── deploy/                 # 部署物料
│   ├── deploy.sh           # 服务器端部署/更新（自动识别首次安装或覆盖更新）
│   ├── setup-nginx.sh      # Nginx 配置一键生成（server_name/HTTPS/校验/回滚/重载）
│   ├── nginx-production.conf   # Nginx 配置模板（唯一来源，由 setup-nginx.sh 生成）
│   ├── check-health.sh     # 健康检查（也可用于 cron 探测）
│   ├── dining-backend.service  # 服务端 systemd 服务单元
│   └── print-agent.service # 门店本地打印代理 systemd 服务单元
├── docs/                   # 文档
│   ├── mysql-migration.md  #   切换到 MySQL 操作手册
│   └── print-agent.md      #   云部署 + 门店 9100 打印机的本地代理方案
├── start.bat / stop.bat    # Windows 一键启动 / 停止后端（双击运行）
└── README.md              
```

## 快速启动

### 1. 后端

```bash
cd backend
# 国内环境建议配置 Go 代理
export GOPROXY=https://goproxy.cn,direct
go mod tidy
go run .          # 或 make run
```

默认监听 `http://localhost:8080`。部署配置有 **三种写法，任选其一，也可以混用**：

| 方式 | 文件 / 位置 | 适用场景 |
|---|---|---|
| **config.yaml**（推荐） | `backend/config.yaml` | 自建服务器、传统运维；分层可读、能写注释 |
| `.env` | `backend/.env` | 扁平 KEY=VALUE 形式 |
| 真实环境变量 | systemd / docker `-e` / 命令行 | 容器与云原生；临时覆盖 |

**优先级：真实环境变量 > `config.yaml` > `.env` > 代码默认值。**
不配任何一项也能启动（默认 `sqlite` + `8080` + `admin`/`admin123`）。

```bash
cp config.example.yaml config.yaml   # 模板含全部可配项与逐项注释
cp .env.example .env                 # 两者可只用一个
```

常用可配项（完整对照表见 [`docs/mysql-migration.md`](docs/mysql-migration.md) 第三节）：

| config.yaml | 等价环境变量 | 说明 | 默认值 |
|---|---|---|---|
| `server.port` | `PORT` | 服务端口 | `8080` |
| `server.upload_dir` | `UPLOAD_DIR` | 上传文件目录 | `./uploads` |
| `server.static_dir` | `STATIC_DIR` | 前端构建产物目录（后端直接托管时） | `../frontend/dist` |
| `server.trusted_proxies` | `TRUSTED_PROXIES` | 可信代理 IP/CIDR（Nginx 反代时必须配置） | 空 |
| `admin.user` / `admin.pass` | `ADMIN_USER` / `ADMIN_PASS` | 管理端账号（仅首次建库生效） | `admin` / `admin123` |
| `security.token_ttl_hours` | `TOKEN_TTL_HOURS` | 登录令牌有效期（小时） | `24` |
| `database.driver` | `DB_DRIVER` | 数据库后端：`sqlite` / `mysql` | `sqlite` |
| `database.sqlite.path` | `DB_PATH` | SQLite 数据库路径 | `./dining.db` |
| `database.mysql.host` 等 | `DB_HOST` / `DB_PORT` / `DB_USER` / `DB_PASSWORD` / `DB_NAME` | MySQL 连接信息 | `127.0.0.1` / `3306` / `root` / 空 / `dining` |
| `database.mysql.dsn` | `DB_DSN` | MySQL 完整连接串（非空时优先于上面 5 项） | 空 |
| `security.master_key` | `CONFIG_MASTER_KEY` | 敏感配置加密主密钥（优先级高于 `data/master.key`） | 空 |

两个文件都读**当前工作目录**（即从 `backend/` 启动），路径可用 `CONFIG_FILE` /
`ENV_FILE` 覆盖。`config.yaml` 里出现模板之外的键名会**直接报错并提示行号**，
以免把键名拼错后静默失效。

> 说明：后端已移除启动期的管理端口令安全校验，不设置任何环境变量即可直接启动（默认账号 `admin` / `admin123`）。`ADMIN_USER` / `ADMIN_PASS` 现在**只在首次启动建号时读取一次**（写入 `tb_user` 表），之后账号与口令一律以「员工管理」页面为准；改环境变量不会再改动已有账号。首个超管每次启动由 `EnsureAdminUser()` 兜底保证存在，且 `admin` 角色的权限会被强制恢复为全量，避免误改锁死。**若部署到公网生产环境，请务必自行设置强口令**，本项目不再做启动期拦截。

### 切换到 MySQL

无需改代码，改配置即可：

```bash
cd backend
cp config.example.yaml config.yaml   # 编辑 database.driver=mysql 及下面的连接信息
# 或：cp .env.example .env            # 编辑 DB_DRIVER=mysql / DB_HOST / DB_USER / DB_PASSWORD
```

- 建库建账号、建表、**把现有 SQLite 数据搬过去**、回滚、常见问题，
  全部写在 **[`docs/mysql-migration.md`](docs/mysql-migration.md)**；
- 数据搬运脚本：`python scripts/sqlite2mysql.py --sqlite dining.db --out data.sql`；
- 迁移脚本目录（SQLite / MySQL 两套）：`backend/migrations/`，说明见其中的 `README.md`。
- ⚠️ 搬迁时**务必把 `backend/data/master.key` 一起带走**，否则已加密的支付密钥无法解密。

首次启动自动建表并写入种子数据（6 大分类 21 道菜品、8 张桌台、店铺配置、备注、2 台打印机）。菜品图片与微信/支付宝收款码已从目标站真实下载并打包进 `backend/uploads/`，首次启动即通过 `/uploads/` 路径展示。

### 2. 前端（开发模式）

```bash
cd frontend
npm install
npm run dev       # http://localhost:5173，已配置 /prod-api 代理到 8080
```

### 3. 前端（生产模式，由后端直接托管）

```bash
cd frontend
npm run build     # 产物输出到 frontend/dist/static（assetsDir 已对齐后端 /static）
# 后端会自动检测 frontend/dist 并静态托管，访问 http://localhost:8080 即可
```

## 后端打包（Makefile）

`backend/Makefile` 提供完整构建/打包能力，二进制统一输出到 `backend/bin/`：

| 命令 | 说明 |
|---|---|
| `make build` | 本机编译，输出 `bin/dining-backend(.exe)` |
| `make build-linux` | 交叉编译 Linux amd64 + arm64 |
| `make build-linux-amd64` | 交叉编译 `bin/dining-backend-linux-amd64` |
| `make build-linux-arm64` | 交叉编译 `bin/dining-backend-linux-arm64` |
| `make run` / `make test` / `make vet` / `make fmt` / `make tidy` | 运行 / 测试 / 静态检查 / 格式化 / 整理依赖 |
| `make clean` | 清理 `bin/` |
| `make help` | 查看所有目标 |

> 因 SQLite 使用纯 Go 驱动（无 CGO），交叉编译无需额外工具链：`make build-linux` 即产出可直接在 Linux 上运行的 ELF 二进制（`CGO_ENABLED=0`）。
>
> 支持注入版本信息：`make build VERSION=1.2.0`（通过 `-ldflags` 写入 `main.version/buildTime/gitCommit`，启动日志会打印版本号）。

## 生产部署

### 方案 A：一键脚本（推荐）

在目标 Linux 服务器（需预装 `go`/`node`/`nginx`/`systemd`）上：

```bash
# 完整部署（构建前端 + 构建后端 + 安装 systemd 服务 + 配置 Nginx）
sudo bash deploy/deploy.sh

# 或跳过构建，仅安装已有产物
sudo bash deploy/deploy.sh --skip-build

# 可通过环境变量覆盖默认配置
INSTALL_DIR=/opt/dining-system BACKEND_PORT=9000 sudo bash deploy/deploy.sh

# 已知服务器公网 IP / 域名时，直接安装生产 Nginx 配置（server_name 不再是通配的 "_"）
SERVER_NAME=111.230.154.50 sudo bash deploy/deploy.sh --skip-build

# 已有证书：自动启用 HTTPS（443 服务块 + 80→443 跳转）
SERVER_NAME=dining.example.com SSL_CERT=/etc/nginx/ssl/dining.crt \
  SSL_KEY=/etc/nginx/ssl/dining.key sudo bash deploy/deploy.sh --skip-build
```

脚本会：
1. `make build-linux-amd64` 交叉编译后端 → `$INSTALL_DIR/bin/dining-backend`
2. `npm ci && npm run build` 构建前端 → `$INSTALL_DIR/frontend/dist`
3. 生成 `dining-backend.service`（systemd，崩溃自动重启）并 `enable --now`
4. 由 `deploy/setup-nginx.sh` 按 `deploy/nginx-production.conf`（唯一配置来源）生成 Nginx 配置并 `nginx -t && nginx -s reload`：
   - 未设置 `SERVER_NAME`：用通配 `server_name _`（与过去的基础配置等价）；
   - 设置了 `SERVER_NAME`：写入指定域名/IP，带证书时自动启用 HTTPS；
   - 全程自动备份旧配置、`nginx -t` 失败自动回滚，并做前端与反代自检。

部署后后端日志有两个去处：

- **落盘文件（推荐排障）**：`$INSTALL_DIR/logs/app.log`，JSON 行格式，按 50MB / 30 天 / 7 份自动轮转并压缩。systemd 服务单元已显式指定该绝对路径，不受工作目录变化影响；
- **journald**：进程控制台输出由 systemd 接管，`journalctl -u dining-backend -f` 可看。

需要改路径或级别：编辑 `/etc/dining-backend.env`（`LOG_PATH` / `LOG_LEVEL` / `LOG_MAX_*`）后 `systemctl restart dining-backend`。
本地直接运行（`make run` 或 `start.bat`）时默认写到 `backend/logs/app.log`，无需任何配置；设 `LOG_PATH=off` 可只输出控制台。

### Nginx 生产配置（也可单独执行）

只调整 Nginx（不动程序产物）时，直接跑配置脚本，可反复执行、幂等：

```bash
# 纯 IP 模式（默认 server_name=111.230.154.50，只监听 80）
sudo bash deploy/setup-nginx.sh

# 指定公网 IP
sudo bash deploy/setup-nginx.sh --ip 111.230.154.50

# 域名 + HTTPS（自动取消 443 段注释、打开 80→443 跳转、替换证书路径）
sudo bash deploy/setup-nginx.sh --domain dining.example.com \
  --cert /etc/nginx/ssl/dining.crt --key /etc/nginx/ssl/dining.key

# 彩排：只生成并做语法校验，不落盘、不重载
sudo bash deploy/setup-nginx.sh --dry-run
```

流程：生成配置 → 备份旧配置（保留最近 5 份）→ `nginx -t` 校验（失败自动回滚）→ reload → 前端/后端自检；
同时会自动处理 SELinux（`httpd_can_network_connect`，否则反代 502）、可选的防火墙放行（`--open-firewall`）与发行版默认站点（`--disable-default`）。
完整参数见 `sudo bash deploy/setup-nginx.sh --help`。

### 方案 B：手动部署

```bash
# 1. 编译后端（在任意机器上交叉编译均可）
cd backend && make build-linux-amd64

# 2. 构建前端
cd ../frontend && npm ci && npm run build

# 3. 拷贝产物到服务器
#    bin/dining-backend-linux-amd64  -> /opt/dining-system/bin/dining-backend
#    frontend/dist/                 -> /opt/dining-system/frontend/dist/

# 4. 安装 systemd 服务
sudo cp deploy/dining-backend.service /etc/systemd/system/dining-backend.service
sudo systemctl daemon-reload && sudo systemctl enable --now dining-backend

# 5. 安装 Nginx 生产配置（写入 server_name / 安装路径 / 后端端口，并自动校验重载）
sudo bash deploy/setup-nginx.sh --ip 服务器公网IP

# 已有域名与证书时（自动启用 HTTPS）
sudo bash deploy/setup-nginx.sh --domain 你的域名 \
  --cert /etc/nginx/ssl/dining.crt --key /etc/nginx/ssl/dining.key
```

部署完成后访问 `http://服务器IP/` 即进入前端，管理后台 `/dining/dashboard`。

> Nginx 与 Go 后端的分工：Nginx 托管前端静态资源并做 SPA 回退，将 `/prod-api`、`/uploads` 反向代理到 Go 后端（`127.0.0.1:8080`）。

### 云部署后，门店的打印机怎么出纸？

后端在云服务器上**够不到门店内网的打印机**（`192.168.x.x` 没有路由，直连通道只会超时）。
三条通道任选一条，在「打印机管理 → 接入方式」里逐台选择：

| 通道 | 适用 | 门店侧要做什么 |
|---|---|---|
| **网络直连** `tcp` | 后端与打印机同一局域网（单机/内网部署） | 无 |
| **飞鹅云** `feie` | 任意网络，需用飞鹅云打印机 | 无（打印机自己联网取单，按台付费） |
| **本地打印代理** `agent` | 云部署 + **复用门店已有的 9100 网络热敏机** | 门店内网放一台常开机设备跑 `print-agent` |

第三条通道**不需要安装任何打印机驱动**（9100 是 RAW 端口，打印机直接收 ESC/POS 字节流）；
代理程序只**出站**连云端，门店不需要公网 IP、不需要端口映射、不需要 VPN。
云端只把票据排队（`tb_print_job`），打印机离线也不丢单，代理恢复后依次补打。

```bash
cd backend && make build-agent    # 产出 Linux amd64/arm64 + Windows amd64 三个代理程序
```

完整步骤（生成代理令牌 → 打印机选通道 → 门店部署 → 开机自启 → 排障表）见
**[`docs/print-agent.md`](docs/print-agent.md)**。

## 访问入口

| 入口 | 地址 | 说明 |
|---|---|---|
| 管理登录 | `/login` | 默认账号 `admin` / `admin123` |
| 管理后台 | `/dining/dashboard` | 仪表盘、桌台/分类/菜品/订单/打印机/备注/配置/报表 |
| 顾客点餐页 | `/order/:tableId` | `/order/1`（兼容旧数字 ID）或 `/order/K7M3PQ9X`（桌台稳定码），扫码点餐（无需登录） |
| 打印票据 | `/printTicket?orderNo=Dxxx` | 订单小票打印（无需登录） |

## 接口清单（与目标站一致）

- 登录 `/prod-api/auth/login`；当前登录者 `/prod-api/dining/auth/profile`、自助改密 `/prod-api/dining/auth/password`
- 管理端 `/prod-api/dining/*`（需携带 `Authorization: Bearer <token>`，并校验对应**权限点**）
  - 桌台 `table/list|save|update|delete/:id`
  - 分类 `category/list|save|update|delete/:id`
  - 菜品 `dish/list|get/:id|save|update|delete/:id`
  - 备注 `remark/list|save|update|delete/:id`
  - 打印机 `printer/list|save|update|delete/:id|test/:id|probe/:id|status/:id|clear/:id|bind|feie/info|agent/info`（字段对齐目标站：`printerType` 1厨房单/2食客小票、`provider` tcp 直连/feie 飞鹅云/agent 本地代理、`paperWidth` 32/48、`status` 启停；测试打印真实走对应通道）
  - 打印代理（门店侧程序调用，代理令牌鉴权，不属于管理端）：`agent/print/ping`、`agent/print/pull`、`agent/print/ack`（见 [`docs/print-agent.md`](docs/print-agent.md)）
  - 打印日志 `print/log/list`、`print/log/reprint`、`print/log/clear`（飞鹅云清空队列）
  - 配置 `config/list|save`
  - 订单 `order/list|get/:id|board|status|pay|settle|settle/cancel|credit/settle|finish|cancel|edit`
  - 报表 `report/summary|dailyTrend|monthlyTrend|dishRank`
  - 员工 `user/list|save|update|resetPassword/:id|status/:id|delete/:id`（改密/启停/删除）
  - 角色 `role/list|save|update|delete/:id`、权限点目录 `perm/catalog`
- 顾客端 `/prod-api/api/dining/*`（公开）：`table/:id`（`id` 同时接受桌台稳定码或数字 ID）、`menu`、`remarks`、`config`、`order`、`order/no/:orderNo`、`pay/qr`
- 上传 `/prod-api/common/upload`（需登录，仅限图片）

> 鉴权失败状态码：**401** = 未登录/令牌失效（前端清登录态并跳登录页）；**403** = 已登录但无该操作权限（前端仅弹提示，不退出）。业务错误统一 **400**。

## 桌台二维码（稳定码 / 中心 Logo / 批量导出打印）

- **码值固定**：每张桌台在创建时生成 8 位随机「点餐码」（`tb_table.table_code`，字母表剔除 0/O/1/I/L 等易混字符），二维码内容为 `H5访问地址 + /order/<点餐码>`。点餐码一经生成**永不变更**，桌牌印一次长期有效；同时避免自增 ID 被枚举遍历（老二维码 `/order/1` 仍然兼容）。
- **中心 Logo**：在「系统配置 → 店铺 Logo」上传（建议正方形透明底 PNG），二维码即用纠错等级 **H（30% 冗余）**并在中心叠加白底圆角 Logo；未配置 Logo 时可选在中心显示桌号。
- **单张**：桌台管理 → 行操作「二维码」→ 可切换中心 Logo / 中心桌号 / 清晰度（360–1200px），支持下载 PNG、打印、复制链接。
- **批量导出 / 打印**：桌台管理右上角「批量导出 / 打印」
  - 范围：全部桌台 / 当前查询结果
  - 排版：A4 网格（每行 2/3/4 张，按纸张宽度自动适配间距与行数，带裁剪虚线）或每页一张（标签机 / 已裁切桌牌，`@page` 按卡片尺寸出纸）
  - 卡片尺寸：60×90 / 80×80 / 100×70 / 90×130 / 50×50 mm
  - 内容开关：店铺名称、桌台名称、桌号、底部提示语、裁剪虚线、二维码中心 Logo/桌号
  - 输出：浏览器打印（可直接另存为 PDF）或打包下载 PNG（ZIP，零依赖实现）

> 打印时请在浏览器打印对话框中关闭「页眉和页脚」，并按所选尺寸设置纸张。

## 订单状态枚举

`1 已下单 → 2 制作中 → 3 已上齐 → 4 已完成 / 5 已取消`；支付状态 `0 未支付 / 1 已支付`。状态机为严格线性流转（1→2→3→4，进行中可取消），禁止跳级。

## 结算方式（收款 / 免单 / 挂账）

`order/settle` 的 `settleType` 支持三种值，结算后订单统一进入「已完成(4)」并释放桌台：

| settleType | 含义 | 实收(paidAmount) | 必填项 | 后续动作 |
|---|---|---|---|---|
| `normal` | 正常收款 | = 应收金额 | 支付方式 | 无 |
| `free` | 免单（让利） | 0 | **免单原因**（≤60字） | 可用 `order/settle/cancel` 撤销 |
| `credit` | 挂账（赊账） | 0 | **挂账单位/事由**（≤60字） | 需 `order/credit/settle` 核销收款 |

- **挂账核销**：`order/credit/settle` 对挂账中订单补收欠款，写入 `creditStatus=2`、实收金额与收款方式；已核销的挂账不可再核销、不可撤销结算。
- **撤销结算**：`order/settle/cancel` 仅对「已结算但未核销」的订单生效，把订单回退为未支付(3 已上齐)，桌台不重新占用（`normal` 收款订单同样可撤销，用于收银误操作纠错）。
- **营收口径**：营业额只计 `paid_amount`（实收），因此**免单不计入营收**、**挂账在核销前不计入营收**，核销后才计入。报表另给出 `todayFreeAmount`（今日免单让利）、`todayCreditAmount`（今日新增挂账）、`creditPendingAmount/Count`（挂账待收总额与笔数）、`todayCreditSettledAmount/Count`（今日挂账回款）。
- **加菜限制**：挂账/免单/已结算订单不可再通过顾客端加菜。
- **小票**：免单/挂账的结账单会打印结算方式标识（如「免单」「挂账」）与实收金额，便于给客人留底。

## 员工与权限（多账号 / 角色 / 接口级鉴权）

管理端不再只有一个固定账号，支持**多员工 + 角色 + 权限点**，粒度细到单个接口。

- **权限点**：共 28 个、分 13 个模块（`table`/`category`/`dish`/`remark`/`printer`/`order`/
  `credit`/`refund`/`report`/`config`/`user`/`role`/`log`），命名 `模块:动作`。
- **隐含规则**（保存角色与每次启动时归一化，管理员勾不掉）：
  ① 任一非 `view` 权限自动隐含同模块的 `view`（勾了「编辑」自动带上「查看」）；
  ② 跨模块数据依赖：`credit:view` → `order:view`（挂账列表其实就是订单数据）、
  `user:view` → `role:view`（员工页要读角色下拉）。少了②会出现「菜单能进、一开页就 403」——
  内置角色恰好都同时拥有所以碰不到，**只在自定义角色上暴露**。
- **4 个内置角色**（不可删除，权限可微调，`admin` 除外）：

  | 角色 | `role_key` | 权限数 | 定位 |
  |---|---|---|---|
  | 超级管理员 | `admin` | 28（全量） | 全部功能；**每次启动强制恢复为全量**，防止误改锁死 |
  | 店长 | `manager` | 22 | 日常最高权限：可改配置、看报表、查操作日志，但不能管理员工/角色，也不能发起退款 |
  | 收银员 | `cashier` | 13 | 前厅收银：订单全流程 + 挂账核销 + 基础资料查看（**不含**配置/报表/退款） |
  | 员工 | `staff` | 6 | 后厨/服务员通用：只读基础资料，订单可流转（接单/上菜/完成/处理催菜） |

- **两个高危动作只给超级管理员**：退款发起 `refund:operate`、操作日志清理 `log:manage`
  （会真的删数据）。店长可以**查看**退款记录与操作日志，但不能发起这两个动作。
- **登录落地页按权限自动选择**：登录后不写死跳仪表盘，而是取该角色可访问的第一个菜单（无 `report:view` 的账号不会撞上仪表盘的报表接口）。
- **三层鉴权**：前端路由守卫（体验：决定菜单与落地页）→ `AdminAuth`（验签 + 按 uid 查员工与角色，失败 **401**）→ `RequirePerm`（集中式「路由 → 权限点」表，**未登记的路由一律 403**，fail-closed）。
- **不缓存权限**：每请求实时查一次，换取「改角色 / 停用 / 改密立刻生效」；改密使 `token_version` 自增，旧令牌立即失效。
- **防锁死三件套**：① `admin` 角色权限每次启动强制全量；② 9 条写入规则（不能改自己的角色、不能停用/删除自己、不能动最后一个启用超管、内置角色不可删、被员工引用的角色不可删…）；③ `EnsureAdminUser()` 保证始终有可用超管。
- **员工管理**：后台可新增账号（登录名/姓名/角色/状态）、重置口令、启停、删除（软删除，同名可复用）；员工可自助改密，**不做强制首登改密**。

> 权限矩阵全表与鉴权链路细节见 [`docs/user-permission-design.md`](docs/user-permission-design.md)。

## 安全与生产要点

- 顾客下单金额由**后端以数据库价格为准重新计算**，不信任前端传入的价格，防止篡改。
- 订单创建/改单/菜品规格等涉及多表写入的操作均使用**数据库事务**，保证数据一致性。
- 食客下单成功后**自动触发网络打印机打印**：向启用的厨房单打印机打印厨房单、向食客小票打印机打印食客小票（ESC/POS，GBK 编码，异步执行，打印失败仅记录日志不影响下单）。
- 上传接口限制**仅图片、单文件 ≤ 5MB**，并对文件内容做**图片魔数嗅探**（防止伪造扩展名）；管理端写接口均有登录鉴权**与权限点校验**。
- 员工口令以**加盐 SHA-256**（16 字节随机盐，存 `salt$hash`）保存，不存明文；账号不存在时也走一次等价耗时的校验（`WastePasswordVerify`），**防止用户名枚举**。
- 登录接口内置**防暴力破解限流**（同 IP 连续 5 次失败锁定 15 分钟）；登录令牌为 HMAC-SHA256 签名（含过期时间），服务重启后旧令牌自动失效。
- 前端图标改为**本地打包**（不依赖腾讯 CDN），内网离线环境可正常显示。
- 服务支持**优雅退出**（收到 SIGINT/SIGTERM 后完成在途请求再关闭）。

## 说明

本复刻仅基于目标站公开可访问的前端资源与只读接口探测实现，未进行任何写操作或越权利用。种子数据沿用目标站店铺「长健农场 柴火农家土菜」的菜单、桌台、备注与打印机，菜品图片及收款码已从目标站公开资源下载并随项目打包（`backend/uploads/`）。
