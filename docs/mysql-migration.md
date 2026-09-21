# 切换到 MySQL 操作手册

> 适用版本：扫码点餐管理系统（`dining-system`）
> 目标读者：负责把这套系统从「SQLite 单机」迁到「MySQL 服务端」的运维 / 开发人员

---

## 一、结论先行

**可以切，代码层面已经全部适配好，切换动作只是改几个环境变量。**

后端从设计上就做了数据库方言层（`backend/internal/store/dialect.go`），业务代码与 SQL 语句
两库通吃，**不需要改任何一行业务逻辑**。已适配的差异点：

| 差异点 | SQLite | MySQL | 适配位置 |
|---|---|---|---|
| 自增主键 | `INTEGER PRIMARY KEY AUTOINCREMENT` | `INT NOT NULL AUTO_INCREMENT PRIMARY KEY` | `autoIncPKFor` |
| 建表选项 | 无 | `ENGINE=InnoDB DEFAULT CHARSET=utf8mb4` | `tableOptionsFor` |
| 索引存在性 | `sqlite_master` | `information_schema.STATISTICS` | `indexExists` |
| 列存在性 | `PRAGMA table_info` | `information_schema.COLUMNS` | `columnExists` |
| 插入忽略 | `INSERT OR IGNORE` | `INSERT IGNORE` | `InsertIgnoreInto` |
| 插入替换 | `INSERT OR REPLACE` | `REPLACE INTO` | `InsertReplaceInto` |
| 部分索引 | 支持 | 不支持（降级为普通索引） | `NewUniqueTableCode` / `EnsureUsernameAvailable` / `EnsureRoleKeyAvailable` 应用层兜底 |
| 连接池 | 小池（16） | 大池（64）+ 生命周期控制 | `applyPool` |
| 连接串 | 文件路径 | `user:pass@tcp(host:port)/db?params` | `resolveDBConfig` |

> 已经在 MySQL 协议兼容服务端上做过实测：建表 15 张、18 个索引、
> 下单 / 加菜 / 催菜 / 改单 / 状态流转 / 结账 / 报表全链路跑通，
> 迁移脚本与数据搬运逐表核对一致。详见 `backend/migrations/README.md` 第八节。

---

## 二、动手前必读的三件事

### 1. 主密钥 `data/master.key` 必须一起带走 ⚠️

`tb_config` 里的敏感项（如微信支付 APIv3 密钥）是用 AES-256-GCM 加密存储的，
密文形如 `enc:v1:xxxx`，解密靠 `backend/data/master.key`。

**换个库但不带这个文件 → 密文永远解不开**（系统不会崩，但支付密钥会读不出来）。

- 默认位置：`backend/data/master.key`
- 也可用环境变量 `CONFIG_MASTER_KEY`（64 位十六进制或 base64）指定，此时不用带文件。

### 2. MySQL 的「库」必须提前建好

程序只能建表，**不能建库**。库不存在会报 `Unknown database 'dining'`。
建库语句已经写在 `migrations/full/mysql/schema.sql` 头部，照抄即可。

### 3. 字符集必须是 utf8mb4

库、表、连接三处都要 `utf8mb4`，否则中文菜品名会乱码或直接插入失败。
建库语句里已指定 `DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci`，
连接串里已默认带 `charset=utf8mb4`。

---

## 三、部署配置速查

部署参数有 **三种写法，任选其一**，也可以混用：

| 方式 | 文件 / 位置 | 适用场景 |
|---|---|---|
| **config.yaml**（推荐） | `backend/config.yaml` | 自建服务器、传统运维；分层可读、能写注释 |
| `.env` | `backend/.env` | 已有环境变量体系、想用扁平 KEY=VALUE |
| 真实环境变量 | systemd `Environment=` / docker `-e` / 命令行 | 容器与云原生；临时覆盖 |

**优先级：真实环境变量 > `config.yaml` > `.env` > 代码默认值。**

也就是说：三者可以共存，同键时按上面的顺序取高优先级；**不配任何一项也能启动**
（默认 SQLite + 8080 端口 + `admin`/`admin123`）。

```bash
cp config.example.yaml config.yaml   # 模板含全部可配项与注释
cp .env.example .env                 # 两者可只用一个
```

两者的路径都由环境变量可覆盖：`CONFIG_FILE`（默认 `./config.yaml`）、
`ENV_FILE`（默认 `./.env`），**都是相对当前工作目录**，即从 `backend/` 启动。
Windows 下写路径请用 `C:/path/config.yaml` 形式。

> `config.yaml` 出现模板之外的键名会**直接启动失败并报行号** —— 这是有意为之，
> 避免把 `upload_dir` 拼成 `uploadDir` 后静默失效、排查半天。

### 键名对照表

| config.yaml | 等价环境变量 | 默认值 | 说明 |
|---|---|---|---|
| `server.port` | `PORT` | `8080` | HTTP 端口 |
| `server.upload_dir` | `UPLOAD_DIR` | `./uploads` | 上传目录 |
| `server.static_dir` | `STATIC_DIR` | `../frontend/dist` | 前端产物目录 |
| `server.cors_origins[]` | `CORS_ORIGINS` | 空 | 跨域白名单（列表自动拼成逗号分隔） |
| `server.trusted_proxies[]` | `TRUSTED_PROXIES` | 空 | 可信代理，**经 Nginx 反代时必须配置** |
| `database.driver` | `DB_DRIVER` | `sqlite` | `sqlite` / `mysql`；不填时若设置了 DSN 则按 mysql 处理 |
| `database.sqlite.path` | `DB_PATH` | `./dining.db` | SQLite 数据文件（仅 sqlite 生效） |
| `database.mysql.dsn` | `DB_DSN` | 空 | 完整连接串，**非空时优先于下面所有分项** |
| `database.mysql.host` | `DB_HOST` | `127.0.0.1` | MySQL 主机 |
| `database.mysql.port` | `DB_PORT` | `3306` | MySQL 端口 |
| `database.mysql.user` | `DB_USER` | `root` | MySQL 账号 |
| `database.mysql.password` | `DB_PASSWORD` | 空 | MySQL 密码 |
| `database.mysql.name` | `DB_NAME` | `dining` | MySQL 库名 |
| `database.mysql.params` | `DB_PARAMS` | `charset=utf8mb4&parseTime=true&loc=Local` | 连接附加参数 |
| `security.master_key` | `CONFIG_MASTER_KEY` | 空 | 敏感配置加密主密钥（优先级高于 `data/master.key`） |
| `security.master_key_path` | `MASTER_KEY_PATH` | `./data/master.key` | 主密钥文件路径 |
| `security.token_ttl_hours` | `TOKEN_TTL_HOURS` | `24` | 登录令牌有效期（小时） |
| `security.harden_file_acl` | `HARDEN_FILE_ACL` | `false` | 是否收紧主密钥文件 ACL（仅 Windows） |
| `admin.user` | `ADMIN_USER` | `admin` | 管理端初始账号（仅首次建库生效） |
| `admin.pass` | `ADMIN_PASS` | `admin123` | 管理端初始口令（**仅首次启动建号生效**，之后在后台「员工管理」维护） |

> **留空 = 未配置**。`config.yaml` 里写 `password: ""` 或整行注释掉，都会继续
> 回退到 `.env` / 默认值，不会用空值把其它来源顶掉。

> 只设环境变量的例子：Windows 启动脚本 `set DB_DRIVER=mysql`；
> Linux systemd `Environment="DB_DRIVER=mysql"`。

---

## 四、场景 A：全新部署（无历史数据，直接用 MySQL）

```bash
# ---------- 1) 建库与账号（只需一次） ----------
mysql -u root -p
```
```sql
CREATE DATABASE IF NOT EXISTS dining
  DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci;

CREATE USER IF NOT EXISTS 'dining'@'%' IDENTIFIED BY '改成你的强密码';
GRANT ALL PRIVILEGES ON dining.* TO 'dining'@'%';
FLUSH PRIVILEGES;
```

```bash
# ---------- 2) 二选一：手工建表 或 让程序自动建 ----------

# 方式 ① 手工建表 + 灌种子数据（推荐：可控、可审计）
cd backend
mysql -u dining -p dining < migrations/full/mysql/schema.sql
mysql -u dining -p dining < migrations/full/mysql/seed.sql

# 方式 ② 什么都不做，启动时程序自动建表与初始化种子数据

# ---------- 3) 配置并启动 ----------
cp config.example.yaml config.yaml     # 方式 A：YAML（推荐）
cp .env.example .env                   # 方式 B：.env（两者选一即可）
```
编辑 `backend/config.yaml`（方式 A）：
```yaml
database:
  driver: mysql
  mysql:
    host: 127.0.0.1
    port: 3306
    user: dining
    password: 改成你的强密码
    name: dining
```
或编辑 `backend/.env`（方式 B）：
```ini
DB_DRIVER=mysql
DB_HOST=127.0.0.1
DB_PORT=3306
DB_USER=dining
DB_PASSWORD=改成你的强密码
DB_NAME=dining
```
```bash
./bin/dining-server.exe        # Windows
# ./bin/dining-server          # Linux
```

启动日志会明确打印「配置来源」与「连的是哪个后端」：
```
[config] 已从 config.yaml 载入 6 项配置(其中 0 项覆盖了 .env 同键)
[config] 已从 .env 载入 3 项配置(未覆盖更高优先级的来源)
[db] 已连接 mysql 后端: dining:***@tcp(127.0.0.1:3306)/dining?charset=utf8mb4&parseTime=true&loc=Local
种子数据已初始化
扫码点餐管理系统已启动 v1.0.0 (build ..., commit ...): http://localhost:8080 (管理端账号 admin)
```
（密码在日志里已脱敏为 `***`。）

浏览器打开 `http://localhost:8080`，用 `admin / admin123` 登录，进「系统配置」能看到店铺名即成功。

---

## 五、场景 B：从现有 SQLite 搬迁数据（推荐做法）

### 步骤 0 · 备份（别跳过）

```bash
cd backend
cp dining.db dining.db.bak-$(date +%Y%m%d)
cp -r data data.bak-$(date +%Y%m%d)      # 主密钥目录，务必一起备份
```

### 步骤 1 · 建库建账号

同场景 A 第 1 步。

### 步骤 2 · 建表（只建结构，先不灌种子）

```bash
mysql -u dining -p dining < migrations/full/mysql/schema.sql
```

> 不要执行 `seed.sql`：数据要从 SQLite 搬过去，灌种子会带来重复数据
> （虽然都是 `INSERT IGNORE`，但桌台码等字段会以种子值先占位）。

### 步骤 3 · 导出 SQLite 数据

```bash
cd backend
python scripts/sqlite2mysql.py --sqlite dining.db --out /tmp/tb_data.sql
```

输出示例：
```
已导出 15 张表 / NNN 行 -> /tmp/tb_data.sql

迁移后请核对行数：
  tb_config              30
  tb_role                 4
  tb_table                8
  tb_category             6
  tb_dish                21
  ...
  tb_user                 N   ← 员工账号(含引导超管),按实际库为准
```

> 脚本特点：金额（整数分）、时间（字符串）、AES 密文（`enc:v1:` 前缀）全部**原样搬运**，
> 不做任何类型推断，避免把密文改坏。默认用 `INSERT IGNORE`，重复导入不会报错。

### 步骤 4 · 导入 MySQL

```bash
mysql -u dining -p dining < /tmp/tb_data.sql
```

### 步骤 5 · 搬主密钥 + 切配置

```bash
# 主密钥目录整体复制到后端工作目录（如原本就在 backend/data 则无需动）
ls backend/data/master.key      # 确认存在

# 改配置
vi backend/.env                 # 见第四节第 3 步
```

### 步骤 6 · 启动并核对

见下一节验证清单。

---

## 六、验证清单

启动后逐项确认（也可直接跑接口）：

| # | 检查项 | 方法 | 期望 |
|---|---|---|---|
| 1 | 连的是 MySQL | 看启动日志 `[db] 已连接 mysql 后端` | ✅ |
| 2 | 表建全了 | `mysql -e "SHOW TABLES FROM dining"` | 15 张表 |
| 3 | 关键数据行数 | `SELECT COUNT(*) FROM tb_table;` | 与源库一致（种子 8） |
| 4 | 中文无乱码 | 管理端看菜品名 | 正常中文 |
| 5 | 敏感配置可解密 | 系统配置页看微信支付密钥字段 | 不报错、值正确 |
| 6 | 能登录 | `admin / admin123` | 进入后台 |
| 7 | 能下单 | 顾客端点餐提交 | 返回订单号 |
| 8 | 能结账 | 管理端订单 → 结账 | 订单变已完成、桌台释放 |
| 9 | 报表正常 | 报表页 | 有数据、金额正确 |
| 10 | 桌台码唯一 | 新增桌台 | 自动分配 8 位码，无重复 |
| 11 | 内置角色齐 | `SELECT role_key FROM tb_role;` | admin/manager/cashier/staff 4 个 |
| 12 | 有可用超管 | `SELECT username FROM tb_user WHERE status=1;` | 至少 1 个（默认 `admin`） |

命令行快速自检：

```bash
# 表数量与行数
mysql -u dining -p dining -e "
SELECT COUNT(*) AS tables FROM information_schema.tables WHERE table_schema='dining';
SELECT 'table' t,COUNT(*) n FROM tb_table
UNION ALL SELECT 'dish',COUNT(*) FROM tb_dish
UNION ALL SELECT 'config',COUNT(*) FROM tb_config
UNION ALL SELECT 'order',COUNT(*) FROM tb_order;"

# 接口自检
curl -s http://localhost:8080/prod-api/api/dining/table/1
curl -s -X POST http://localhost:8080/prod-api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin123"}'
```

---

## 七、回滚（改回 SQLite）

1. 停服；
2. 把 `backend/.env` 里的 `DB_DRIVER` 改回 `sqlite`（或直接删掉 `.env`）；
3. 确认 `backend/dining.db` 与 `backend/data/master.key` 还在；
4. 重启。

**注意**：切换期间在 MySQL 上产生的新数据（新订单等）不会自动回到 SQLite。
若需要双向同步，建议在 MySQL 侧用 `mysqldump` 导出后再反向转换，或干脆以 MySQL 为准。

---

## 八、两库差异与注意事项

### 已经自动适配的（无需操心）

自增主键、建表引擎/字符集、索引与列的存在性检查、`INSERT IGNORE` / `REPLACE INTO`、
连接池参数、连接串构建、连接失败的中文排查提示。

### 需要你留意的

| 事项 | 说明 |
|---|---|
| **VARCHAR 长度强校验** | SQLite 不校验长度（超长会静默通过），MySQL 严格模式下**直接报错**。字段长度见 `migrations/full/mysql/schema.sql`；如需放宽请改 Go 定义后重新生成（`go run ./cmd/gensql`）。 |
| **严格模式** | MySQL 默认 `sql_mode` 含 `STRICT_TRANS_TABLES`，类型不匹配（如把空串写进 INT）会报错而非静默转换。 |
| **重复执行建索引会报错** | MySQL 无 `CREATE INDEX IF NOT EXISTS`，重复跑 `schema.sql` 会报 `Duplicate key name`，正常现象，可忽略。 |
| **加列脚本不幂等** | `ALTER TABLE ADD COLUMN` 两库都没有 `IF NOT EXISTS`，列已存在会报 `Duplicate column name`，同样可忽略。执行前用 `SHOW COLUMNS FROM 表名;` 自查。 |
| **部分索引降级** | `idx_table_code` 在 SQLite 是部分唯一索引，MySQL 降级为普通索引；唯一性由 `store.NewUniqueTableCode()` 在应用层保证。 |
| **时区** | 时间统一存 `VARCHAR(32)` 本地时间字符串，不依赖数据库时区。连接参数 `loc=Local` 保证一致；若容器时区不对，改 `TZ` 环境变量而不是改数据库。 |
| **大小写敏感** | Linux 上 MySQL 默认表名区分大小写。建表语句已统一小写，无需处理；但**库名**在 `DB_NAME` 里要与实际一致。 |

---

## 九、常见问题

**Q1：启动报 `Unknown database 'dining'`**
库没建。执行第四节第 1 步的 `CREATE DATABASE`。

**Q2：启动报 `Access denied for user ...`**
账号或密码不对。检查 `DB_USER` / `DB_PASSWORD`；注意 MySQL 8 的账号有 `user@host` 概念，
若程序不在本机，需要 `'dining'@'%'` 而不是 `'dining'@'localhost'`。

**Q3：启动报 `connections refused` / `i/o timeout`**
连不上 MySQL 服务。检查 `DB_HOST` / `DB_PORT`、MySQL 是否在跑、防火墙与云服务器安全组。
（后端已经会把这三类错误翻译成中文提示，直接看日志即可。）

**Q4：中文变成 `???` 或乱码**
三处都要 `utf8mb4`：① 建库 `DEFAULT CHARACTER SET`；② 连接串 `charset=utf8mb4`；
③ 客户端导入时 `SET NAMES utf8mb4`（迁移脚本里已带）。
老库若是 `latin1` 建的，需要 `ALTER TABLE ... CONVERT TO CHARACTER SET utf8mb4`。

**Q5：微信支付密钥读不出来 / 变空**
`data/master.key` 没带过来，或换了新的主密钥。把原 `master.key` 放回 `backend/data/`
重启即可；若原密钥已丢失，只能在系统配置页重新填一遍支付密钥。

**Q6：订单号重复报 `Duplicate entry`**
`idx_order_no` 唯一索引在起作用。订单号由 `service.GenOrderNo()` 生成（时间戳 + 随机段），
正常不会重复；若手工导入过数据造成冲突，清理冲突行即可。

**Q7：想直接用连接串而不想拆成一堆变量**
```ini
DB_DRIVER=mysql
DB_DSN=dining:密码@tcp(10.0.0.1:3306)/dining?charset=utf8mb4&parseTime=true&loc=Local
```
设置了 `DB_DSN` 后，`DB_HOST` / `DB_USER` 等分项不再生效。
（注意：密码里若含 `@` `:` `/` 等字符需要 URL 转义。）

**Q8：`.env` / `config.yaml` 改了不生效**
① 真实环境变量优先级更高 —— 检查是不是已经有同名系统环境变量（`echo $DB_DRIVER`）；
② 文件位置要对：默认读**当前工作目录**下的 `.env` 与 `config.yaml`，即从 `backend/` 目录启动；
③ Windows 下路径写 `C:/path/.env` 形式，`/tmp/xx` 这类 Unix 路径会被解析成 `C:\tmp\xx`；
④ 两个文件同时存在时 **`config.yaml` 优先** —— 启动日志会打印
   `其中 N 项覆盖了 .env 同键`，把 N 和你要改的项对一下就知道是谁在生效。

**Q9：配置文件里键名拼错了会怎样？**
启动直接失败并提示行号，例如 `解析 config.yaml 失败: yaml: unmarshal errors: line 8: field portt not found in type store.ServerSection`。
这是有意设计 —— 静默忽略拼错的键名会导致「配了却没生效」这类极难排查的问题。

**Q10：`config.yaml` 和 `.env` 该选哪个？**
功能等价，按团队习惯选：喜欢分层可读、能写注释 → `config.yaml`；
已有扁平环境变量体系或走容器注入 → `.env`。两者都只是「键值表」，
最终都会写回进程环境，业务代码只认 `store.Getenv(key, def)`，所以随时可以互换。

---

## 十、生产环境建议

1. **账号最小权限**：业务账号只给 `dining.*` 的 `SELECT, INSERT, UPDATE, DELETE`；
   建表阶段临时给 `CREATE, ALTER, INDEX`，稳定后可收回（程序启动时会尝试建表，
   权限不足会记日志但不影响已有表的使用 —— 更稳妥的做法是给足 `CREATE` 权限，
   因为新版本可能新增表/列）。
2. **连接池**：代码已设 `MaxOpenConns=64 / MaxIdleConns=16 / ConnMaxLifetime=30m /
   ConnMaxIdleTime=5m`，避免被服务端 `wait_timeout` 掐断。库压力大时调
   `dialect.go` 的 `applyPool`，而不是加机器。
3. **备份**：
   ```bash
   mysqldump -u dining -p --single-transaction --routines --triggers \
     dining > dining-$(date +%F).sql
   ```
   主密钥 `data/master.key` 也要纳入备份（它是解密配置的前提）。
4. **灾备**：MySQL 建议开 binlog 并做主从；应用侧无状态，可多实例 + Nginx 负载均衡。
5. **监控**：关注慢查询、`Threads_connected`、`Innodb_row_lock_waits`。
   点餐高峰是写密集（下单 + 明细 + 催菜），`tb_order` 与 `tb_order_item` 是热点表。
6. **Nginx 反代记得配 `TRUSTED_PROXIES`**，否则登录限流会退化成全局粒度（所有请求共享
   一个来源 IP），容易误伤正常用户。
7. **迁移窗口**：搬迁期间建议停写（停服 5~10 分钟），避免搬迁过程中产生新订单丢失。

---

## 附录 · 相关文件索引

| 路径 | 说明 |
|---|---|
| `backend/config.example.yaml` | **部署配置模板（YAML，复制为 `config.yaml` 使用）** |
| `backend/config.yaml` | 实际部署配置（**已被 .gitignore 排除，勿提交**） |
| `backend/.env.example` | 环境变量模板（复制为 `.env` 使用） |
| `backend/internal/store/dialect.go` | 数据库方言层（连接配置、方言差异） |
| `backend/internal/store/configfile.go` | `config.yaml` 解析 + 三层来源优先级编排 |
| `backend/internal/store/env.go` | `.env` 轻量加载器 |
| `backend/internal/store/schema.go` | 表结构定义（两库共用来源） |
| `backend/migrations/full/mysql/schema.sql` | MySQL 全量建表脚本（含建库语句） |
| `backend/migrations/full/mysql/seed.sql` | MySQL 种子数据 |
| `backend/migrations/incr/mysql/*.sql` | MySQL 增量升级脚本（老库用） |
| `backend/scripts/sqlite2mysql.py` | SQLite → MySQL 数据搬运脚本 |
| `backend/cmd/gensql/` | 脚本生成器（`go run ./cmd/gensql`，改结构后重新生成两库脚本） |
| `backend/migrations/README.md` | 迁移脚本目录说明与命名规则 |
