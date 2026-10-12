# 后端运维脚本（deploy/op）

后端服务的日常运维脚本。**仓库里在 `deploy/op/`，安装后整体装到 `<INSTALL_DIR>/op/`**
（`/opt/dining-system/op/`，属主 `dining`，随包一起更新）。全部脚本只依赖系统命令
（systemctl/curl/mysql/df...），服务器上**只剩安装目录**时也照样能用。

| 脚本 | 作用 | 需要 root |
|---|---|---|
| `install.sh` | **首次安装**（只在发布包根目录里执行，委托包内 `deploy/deploy.sh`；装完后用 `upgrade.sh`） | ✅ |
| `upgrade.sh` | **升级**：`sudo bash op/upgrade.sh <新包.tar.gz>` —— 解包、定位包根、转发参数，升级/回滚全部由新包的 `deploy.sh` 执行 | ✅ |
| `start.sh` | **启动**；未安装时给安装引导 | ✅ |
| `stop.sh` | **停止**（自启动保留，重启后自动恢复） | ✅ |
| `restart.sh` | **重启 + 健康检查**。**改完 `config.yaml` 必须执行**（配置只在启动时读一次） | ✅ |
| `p.sh` | **状态一览**：服务/版本/端口/健康接口/数据库连通性与 `max_allowed_packet`/磁盘/日志尾。门店报障先跑这个 | ❌ |
| `check-health.sh` | **健康探测**：HTTP 200 口径，`--json`/`--quiet` 适合挂 cron 或监控系统 | ❌ |
| `backup.sh` | **业务备份**：数据库 + `data/master.key` + `data/auth.key` + `config.yaml` + `VERSION` → `backups/<时间戳>/`，保留 14 份 | ✅ |
| `restore.sh` | **数据还原**（会覆盖现网，动手前自动再备份一次）：`sudo bash op/restore.sh <备份目录>` | ✅ |
| `rollback.sh` | **程序回滚**到升级前版本（`backup/<时间戳>/`，`deploy.sh` 升级时自动留存）；`--list` 可看可滚哪些 | ✅ |
| `uninstall.sh` | **卸载**：移除 systemd 单元与 Nginx 站点；默认保留全部数据，`--all` 才连数据删（先自动备份） | ✅ |
| `_common.sh` | 内部共用库（路径常量/输出/config.yaml 读取/MySQL 连接），**不是执行入口** | — |

## 常用流程

```bash
cd /opt/dining-system

# 日常巡检
bash op/p.sh                     # 门店报障先跑这个

# 改配置(配置只在启动时读一次)
vim config.yaml && sudo bash op/restart.sh

# 升级(把新包 scp 到服务器任意位置)
sudo bash op/backup.sh                                # 升级前先备份数据
sudo bash op/upgrade.sh /tmp/dining-<版本>-<时间戳>.tar.gz
sudo bash op/upgrade.sh /tmp/dining-….tar.gz --refresh-config   # 让 config.yaml 整份按新包重建
sudo bash op/rollback.sh                              # 升级出问题, 一键回滚

# 灾备
sudo bash op/backup.sh                                # 定期跑(或挂 cron)
sudo bash op/restore.sh backups/20261013-033000       # 灾难恢复

# 卸载(数据保留; --all 连数据删, 会先自动备份)
sudo bash op/uninstall.sh
```

## 两类备份, 别混

| 目录 | 谁写的 | 里面是什么 | 用途 |
|---|---|---|---|
| `backups/<时间戳>/` | `backup.sh` | **数据**: `db.sql`(或 `dining.db`) + `master.key` + `auth.key` + `config.yaml` + `VERSION` + `MANIFEST` | 灾备/迁移(用 `restore.sh`) |
| `backup/<时间戳>/` | `deploy.sh`(升级前自动) | **程序**: `dining-backend` + `dist/` + `config.yaml` + `VERSION` + `op/` | 升级回滚(用 `rollback.sh`) |

> ⚠️ 备份必须**成套**：库里的微信支付密钥/小程序配置是密文，解密靠 `data/master.key`，
> 少了它备份出来的库就是解不开的乱码。`backup.sh` 会把这几样一起打包，缺一样直接判为不可用。

## 与部署的边界

- **装/升级/自动回滚**：`deploy.sh`（发布包里 `deploy/op/install.sh`、装好后的 `op/upgrade.sh` 都是它的入口别名）；
- **本目录**负责装好之后的日常运维（状态/配置生效/备份/还原/回滚/卸载）；
- 数据迁移(SQLite→MySQL)脚本在发布包 `backend/scripts/sqlite2mysql.py`，用法见
  [`docs/mysql-migration.md`](../../docs/mysql-migration.md)。

详细部署说明见仓库根目录 [`README.md`](../../README.md) 与 [`deploy/deploy.sh`](../deploy.sh) 头部注释。
