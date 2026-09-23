# 门店运维脚本（print-agent/op）

门店本地打印代理的日常运维脚本，**按操作系统分文件夹**，每个文件夹六种脚本：

| 文件夹 | 操作系统 | 脚本 | 说明 |
|---|---|---|---|
| `mac/` | macOS（Intel / M 系列通用） | `install.sh` / `start.sh` / `stop.sh` / `p.sh` / `upgrade.sh` / `uninstall.sh` | launchctl |
| `win/` | Windows | `install.bat` / `start.bat` / `stop.bat` / `p.bat` / `upgrade.bat` / `uninstall.bat` | 任务计划，双击即可 |
| `linux/` | Linux（小主机 / 树莓派） | `install.sh` / `start.sh` / `stop.sh` / `p.sh` / `upgrade.sh` / `uninstall.sh` | systemd |
| `cloud/` | **云端侧**（开发/运维人员） | `check-online.sh` | 用管理端账号登录查询各门店代理在线状态与队列积压，**不随部署包发门店** |

## 六种脚本的作用

| 脚本 | 作用 |
|---|---|
| `install.sh`（win 为 `.bat`） | **安装**：写 agent.env 配置 + 注册开机自启。用法 `./install.sh [管理后台地址] [代理令牌]`，省略参数则**交互式询问** |
| `start.sh` | **启动**代理；未安装时给出 `install` 引导 |
| `stop.sh` | **停止**代理（只停运行实例，自启动保留，重启后自动恢复） |
| `p.sh` | **打印状态**：进程/任务运行状态 + 最近 15 行日志 + 云端连通自检提示 |
| `upgrade.sh` | **自助升级**：从云端下载最新版本替换自身（前提：服务端已部署新产物且管理后台配置了「代理最新版本号」）；Windows 升级后自动触发任务计划重启 |
| `uninstall.sh` | **卸载**：停进程 + 移除开机自启 + 清理日志/状态/升级残留。`--all` 额外删除产物与 agent.env |

> `stop` 与 `uninstall` 的区别：`stop` 只停本次运行、自启动保留（重启后自动恢复）；
> `uninstall` 移除自启动本身，不再开机拉起。默认都**保留 `agent.env`**，重装不必重填令牌。

## 用法

门店部署时，把对应平台文件夹里的脚本与代理产物放到**同一目录**（脚本自动探测同目录产物）：

```bash
# macOS / Linux(到产物所在目录执行;Linux 的 install/upgrade 需要 sudo)
./install.sh https://dining.example.com 你的令牌   # 安装(省略参数则交互式询问)
./start.sh     # 启动
./stop.sh      # 停止
./p.sh         # 打印状态
./upgrade.sh   # 自助升级(云端配置最新版本号后)
./uninstall.sh # 卸载(移除自启 + 停进程 + 清运行文件;--all 连产物与配置一起删)
```

Windows：双击 `install.bat` / `start.bat` / `stop.bat` / `p.bat` / `upgrade.bat` / `uninstall.bat`（或命令行执行）。

- 连通性自检：`<产物> --probe 192.168.1.100`（多个 IP 用逗号分隔，端口缺省 9100）：只查
  「本机 → 打印机」这一段，不用在门店电脑上装 `nc`；加 `--probe-print` 会吐一张自检页；
- 卸载自启：`<产物> --uninstall`（保留 agent.env 配置，重装不用重新填令牌）；
  想连产物和一起清掉时用脚本 `./uninstall.sh --all`（Windows 为 `uninstall.bat --all`）；
- 实时跟踪日志：`tail -f print-agent.log`（Windows 用 `powershell -Command "Get-Content print-agent.log -Wait"`）。

详细说明见 [`docs/print-agent.md`](../../docs/print-agent.md) 的「日常操作速查」章节。
