# 「记住我(免登录)」机制 · 设计文档

> 状态：**设计与实施全部完成（含 2026-09-22 升级：环境绑定 + 登录设备管理页 + 竞态/吊销修复）**。
> 后端 `go build` + `go vet` + `go test ./...` 全绿；前端 `vite build` 通过。
> 关联文档：`docs/user-permission-design.md`（账号/角色/权限体系，本机制依赖其 `AdminAuth`/`RequirePerm`）、`backend/migrations/README.md`。

---

## 一、背景与目标

### 1.1 背景

餐饮门店员工每天在固定设备（店内收银电脑 / 手机）上重复登录，输入密码是高频摩擦。引入「记住我(免登录)」：勾选后 7/30 天内打开系统无需密码，同时保证：

- 有效期**由服务端裁决**，前端无法伪造或篡改；
- 令牌可**随时吊销**（退出 / 改密 / 停用 / 手动），且对多设备场景不误伤；
- 令牌**绑定签发环境**，被复制到其它浏览器/系统即失效；
- 用户**能看到并管理**自己名下所有免登录设备。

### 1.2 非目标（本期明确不做，见第十节）

- 令牌轮换与重放检测、HttpOnly Cookie 存储 —— 记录决策理由与演进路径，不做实现。
- 设备指纹深度绑定（硬件级指纹、IP 段绑定）。

---

## 二、总体方案：两级令牌

```
登录(账密) ──→ 访问令牌 admin_token(默认 24h, HMAC 自校验)
              └─ 勾选「记住我」时同时签发 ──→ 记住令牌 admin_remember(7/30 天, 随机数查库)

访问令牌过期 ──→ 登录页拿记住令牌静默换发新的 24h 访问令牌(免登录)
记住令牌过期/吊销 ──→ 回到账密登录
```

| 令牌 | 有效期 | 校验方式 | 存储 |
|---|---|---|---|
| 访问令牌 `admin_token` | 24h（`TOKEN_TTL_HOURS` 可配，≤30 天） | 后端 HMAC-SHA256 验签 + 每请求按 uid 查库（账号/角色/token_version） | 前端 localStorage |
| 记住令牌 `admin_remember` | 7 / 30 天 | 后端查 `tb_remember_token` 裁决（存在/未过期/环境一致/账号可用） | 前端 localStorage；服务端存表 |

**为什么访问令牌短期 + 记住令牌长期**：访问令牌自校验（免查库，快），短期缩小泄露窗口；记住令牌不透明随机串 + 查库裁决，随时可删行吊销——这是自包含令牌（JWT 类）做不到的。

---

## 三、数据模型

`tb_remember_token`（`internal/store/schema.go`，`ua` 列为 2026-09-22 新增）：

| 列 | 类型 | 说明 |
|---|---|---|
| token_id | 主键自增 | 登录设备页吊销时的标识 |
| user_id | INTEGER | 属主 |
| token | VARCHAR(64) | 32 字节 CSPRNG 的 64 hex，唯一索引，**不透明** |
| expire_time | VARCHAR(32) | 到期时间，字符串按 `YYYY-MM-DD HH:mm:ss` 字典序比较即可判过期 |
| create_time | VARCHAR(32) | 签发时间（前端反推 7/30 档位） |
| last_used_time | VARCHAR(32) | 最后换发时间（设备页展示 + 风控线索） |
| ua | VARCHAR(255) | 签发环境指纹（见第六节） |

**迁移**：建表以 Go 定义为唯一来源（`gensql` 生成 `migrations/full/*`）；新增列走 `store/alterCols`（启动时幂等补列，老库自动升级）+ 手工脚本 `migrations/incr/*/20260922020000_tb_remember_token_add_ua.sql`。

---

## 四、签发

`internal/handler/auth.go: AdminLogin`：

1. 登录校验通过后，若请求带 `remember=true`，签发一条记住令牌：
   - `days` 归一化：仅 `30` 特判，其余一律 7（前端只有 7/30 两个选项，防御非法值）；
   - `store.CreateRememberToken(userID, days, uaFingerprint(User-Agent))`，记录环境指纹；
   - 新令牌随登录结果 `rememberToken` 字段回传。
2. **未勾选时不作废已有令牌**。历史上一度「未勾选 → 删除该账号全部令牌」，导致 A 设备记住的令牌被 B 设备一次未勾选的密码登录连坐作废（多设备用户表现为「免登录莫名失效」）。现改为：取消记住由前端把**本设备**旧令牌交回 `/api/auth/logout` 精确吊销，只动本设备。

---

## 五、静默换发（免登录）

### 5.1 后端 `RememberLogin`

`POST /api/auth/remember-login`，请求体 `{ rememberToken }`。核心判定链在 `store.ConsumeRememberToken(token, uaFP)`，依次为：

1. 令牌存在（不存在 → 删 + 401）；
2. 未过期（过期/时间格式异常 → 删 + 401）；
3. **环境一致**（指纹不一致 → 作废 + 401，见第六节）；
4. 账号启用且所属角色启用（否则 → 删 + 401，账号停用即时生效）。

全部通过后：刷新 `last_used_time`（存量行顺手补写 `ua`）、`RecordLogin`（最后登录时间）、写操作日志（Module「登录账号」/ Action「记住我免登录」），按普通登录同样结构返回新访问令牌。**不轮换**：同一记住令牌用满 7/30 天（原因见第十节）。

### 5.2 前端 `Login.vue: autoLogin`

登录页 `onMounted` 时若本地有令牌，静默调 `/auth/remember-login`：

- **silent 标记**：失败不弹全局 toast（用户还没做任何操作，弹「网络连接失败」只会造成困惑）；
- **竞态保护 `manualSubmit`**：用户开始输入（`@input="markManual"`）或提交登录即置位，自动登录的结果一律丢弃——防止慢返回的静默结果把登录态覆盖成另一个账号、或趁用户输入到一半突然跳转冲掉表单；
- **失败处理**：仅当 **401**（后端明确判定令牌无效）才清本地；网络错误/超时保留令牌，一次弱网不应作废 7/30 天资格；
- 刻意不占用 loading（避免移动弱网下登录按钮被 disable 造成「手机登不上」）。

### 5.3 前端 `onSubmit` 的令牌落库

先 `revokeRemember()`（吊销本设备旧令牌：清本地 + `POST /api/auth/logout`），再决定存不存新令牌：

- 勾选且后端回传新令牌 → 存新；
- 未勾选 → 这就是「取消记住」的生效路径，旧令牌已吊销，其他设备不受影响。

---

## 六、环境绑定

### 6.1 指纹提取 `handler/auth.go: uaFingerprint`

从 User-Agent 提取「浏览器·系统」family 名，形如 `Chrome·macOS`、`微信·Android`：

- 浏览器识别顺序（Edge/Opera 的 UA 同时含 Chrome 字样，须先判）：微信(MicroMessenger) → Edge(Edg/) → Opera(OPR/) → Firefox → Chrome → Safari；
- 系统：Windows / iOS / Android / macOS / Linux；
- **只取 family、不带版本号**：UA 字符串随浏览器小版本升级变化，全等比对会误踢正常用户；「同一浏览器 + 同一系统」的粒度已足以拦住跨环境使用。

### 6.2 校验规则（`ConsumeRememberToken`）

| 场景 | 行为 |
|---|---|
| 指纹一致 | 放行 |
| 指纹不一致 | 视为令牌被复制到其它环境：**作废该令牌 + 401** |
| 存量行 `ua` 为空（升级前签发） | 跳过校验，首次使用时**补写**当前指纹，平滑完成绑定升级 |
| 本次请求无 UA | 跳过校验 |

### 6.3 边界

- 同浏览器同系统内（换设备同环境）依然可用——指纹是「环境大类」不是硬件 ID，这是有意取舍；
- 攻击者偷到令牌后伪造 UA 理论上可绕过，但通常能读 localStorage 的 XSS 同样能读 UA，本机制主要防「凭据外泄到异构环境」的常见情形。

---

## 七、吊销矩阵

| 触发场景 | 作用范围 | 实现 |
|---|---|---|
| 退出登录 | **本设备** | 前端 `revokeRemember()` → `POST /api/auth/logout`（凭令牌本身鉴权，不挂 AdminAuth，幂等） |
| 取消记住（登录未勾选） | **本设备** | 同上（前端登录成功后自动交回旧令牌） |
| 登录设备页手动吊销 | 指定一台 / 全部 | `POST /api/admin/auth/remember/revoke`（属主校验）/ 前端循环单台 |
| 修改本人密码 | **该账号全部设备** | `ChangePassword` → `DeleteRememberTokensByUser` + `token_version+1`（访问令牌同步失效） |
| 管理员重置密码 | 该账号全部设备 | `user.go` → `DeleteRememberTokensByUser` |
| 停用 / 删除账号 | 该账号全部设备 | 事务内 `deleteRememberTokensByUserTx`，与业务变更同生共死 |
| 令牌过期 / 账号状态失效 | 单条 | `ConsumeRememberToken` 判定失败时顺手删行（避免脏数据累积） |

**防越权**：吊销/查询一律带 `user_id` 属主条件——传别人的 tokenId 既删不掉、也不泄露「是否存在」。

---

## 八、登录设备管理页

**后端接口**（`/api/admin/*` 管理端分组，`AdminAuth` 保护，权限登记 `PermFree` 即登录可用）：

| 接口 | 说明 |
|---|---|
| `GET /api/admin/auth/remember/sessions` | 列出当前账号全部有效会话；**令牌只回传前 8 位**（识别本机用），完整令牌不出服务端 |
| `POST /api/admin/auth/remember/revoke` | 吊销一条 `{ tokenId }`；审计登记「吊销记住登录」（OperTypeDelete） |

**前端页面** `frontend/src/views/Devices.vue`（侧栏「登录设备」菜单，`menuOrder: 15`，无权限点）：

- 桌面表格 + 窄屏卡片流，展示：设备指纹、**7/30 天档位**（由 `expire_time - create_time` 反推）、剩余有效期、签发/最后使用时间；
- **本机识别**：本地令牌前 8 位与服务端回传 `tokenPrefix` 比对，标「本机」；
- 单台吊销（确认框区分本机/他机文案）与全部吊销（循环单台）；吊销本机时同步 `clearRemember()`，避免残留死令牌白跑一次 401。

---

## 九、前端存储与安全边界

| 键 | 内容 | 位置 |
|---|---|---|
| `admin_token` | 访问令牌 | `utils/authKeys.js` |
| `admin_remember` | 记住令牌（不透明串，不存密码） | `utils/remember.js` |

- **本地过期预判 `tokenExpired()`**（`utils/perm.js`）：解析访问令牌 payload 的 `exp` 字段（不验签的乐观判断），路由守卫据此在 SPA 内直接带 redirect 去登录页，避免「进页面 → 请求撞 401 → 整页弹回」的绕圈。篡改 exp 骗过它没有意义——安全校验仍在后端验签；服务端侧失效（改密/停用/后端重启）照旧走 401 兜底。
- **401 统一处理**（`api/http.ts`）：清全部登录态键 + 带 `?redirect=` 整页回跳，静默换发成功后可回到被打断的页面；`safeRedirect()` 只接受站内路径（防开放重定向）。
- **已知取舍**：令牌存 localStorage，同源 XSS 可读取（HttpOnly Cookie 方案见第十节）；依赖 Vue 模板自动转义、无 `v-html` 等既有防护作为缓解。

---

## 十、安全取舍与演进路径

| 议题 | 现状 | 决策理由 | 演进路径 |
|---|---|---|---|
| 令牌轮换 + 重放检测 | 不轮换，同一令牌用满 7/30 天 | 多标签页并发换发会互相作废对方的新令牌，需「宽限期」机制才能正确实现，复杂度/收益不匹配当前威胁模型 | 出现异常登录迹象或做 HttpOnly 迁移时一并做（Auth0 式 60s 宽限 + 旧令牌重现=全家撤销） |
| 存储位置（HttpOnly Cookie） | localStorage | 迁移是认证架构整体改造，上线瞬间全量掉线；当前已有 24h 短令牌 + 吊销矩阵 + 环境绑定多层缓解 | 双轨迁移：后端 Cookie/header 并存 → 前端切换 → 下线 header 通道，可做到不掉线平滑迁移 |
| 环境绑定粒度 | 浏览器·系统 family | 防「凭据外泄到异构环境」，不误伤正常浏览器升级 | 需要时升级为设备指纹/常用 IP 段 |

**升级触发条件**（出现任一即建议启动上述演进）：系统暴露公网且多人共用电脑登录；操作日志出现无法解释的登录记录；前端引入第三方脚本（XSS 面变大）；账号体系开放多租户。

---

## 附录

### A. 接口清单

| 接口 | 鉴权 | 说明 |
|---|---|---|
| `POST /api/auth/login` | 公开 | 登录；勾选时签发记住令牌 |
| `POST /api/auth/remember-login` | 公开 | 静默换发（免登录） |
| `POST /api/auth/logout` | 公开 | 吊销本设备令牌（幂等） |
| `GET /api/admin/auth/remember/sessions` | AdminAuth + PermFree | 我的免登录设备列表 |
| `POST /api/admin/auth/remember/revoke` | AdminAuth + PermFree | 吊销一条（属主校验） |

### B. 代码索引

| 模块 | 文件 |
|---|---|
| 令牌签发/换发/吊销 handler、指纹提取 | `backend/internal/handler/auth.go` |
| 令牌存取（store 层） | `backend/internal/store/remember.go` |
| 表结构 / 补列 | `backend/internal/store/schema.go`（`alterCols`） |
| 增量迁移 | `backend/migrations/incr/*/20260922020000_tb_remember_token_add_ua.sql` |
| 权限登记 | `backend/internal/handler/perm.go`（PermFree） |
| 审计登记 | `backend/internal/handler/audit.go` |
| 前端存储工具 | `frontend/src/utils/remember.js` |
| 登录页（静默换发/竞态保护） | `frontend/src/views/Login.vue` |
| 登录设备页 | `frontend/src/views/Devices.vue` |
| API 定义 | `frontend/src/api/auth.ts`（登录类）/ `frontend/src/api/http.ts`（拦截器） |
| 类型定义 | `frontend/src/types/entities.ts`（`RememberSession` 等） |
| 路由守卫 / 登录设备菜单 | `frontend/src/router/index.js` |
