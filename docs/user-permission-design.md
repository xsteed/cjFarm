# 员工账号与权限体系 · 整体设计

> 状态：**设计与实施全部完成（P1~P5）**。后端 `go test ./...` 全绿、`go run ./cmd/gensql -check` 通过、
> 接口级权限回归（3 角色 × 只读/写接口 403 矩阵）通过、前端 `npm run build` 通过。
> 仅「MySQL 上 15 表全量建库实测」与「浏览器端菜单渲染 E2E」两项待补，见第十节实施状态。
> 目标版本：在现有单管理员版本上增量引入「多员工 + 角色 + 接口级权限」。
> 关联文档：`README.md`、`backend/migrations/README.md`、`docs/mysql-migration.md`

---

## 一、背景与目标

### 1.1 现状盘点（代码事实）

| 维度 | 现状 | 位置 |
|---|---|---|
| 登录账号 | **只有 1 个**，用户名取 `ADMIN_USER`（默认 `admin`） | `internal/handler/auth.go: AdminLogin` |
| 密码存储 | 优先 `tb_config.admin_pass_hash`（bcrypt），否则 `ADMIN_PASS` 明文 | `auth.go: verifyAdminPassword` |
| 令牌载荷 | `base64(用户名\|过期时间) + "." + HMAC-SHA256`，无角色字段 | `auth.go: genToken` |
| 鉴权中间件 | 只判断「令牌有效」，**不做任何接口授权** | `auth.go: AdminAuth` |
| 用户/角色表 | **不存在**（12 张业务表里没有 user / role / permission） | `internal/store/schema.go` |
| 操作人留痕 | 订单 4 处写死字符串 `'收银员'`，其余走 `adminName(c)` 取令牌用户名 | `handler/order.go:178,524,566,624` |
| 前端守卫 | 只有 `meta.requiresAuth` 布尔值 | `frontend/src/router/index.js` |
| 前端菜单 | `AdminLayout.vue` 硬编码 10 项，人人相同 | `frontend/src/layout/AdminLayout.vue:82` |

**结论**：任何登录者都能操作全部 40+ 个管理接口（含退款、删桌台、改系统配置）。这是当前最主要的安全缺口。

### 1.2 设计目标

1. **多员工**：可创建任意数量的员工账号，各自独立密码。
2. **角色化权限**：内置 4 个开箱可用角色，另支持自定义角色。
3. **接口级强制**：权限在后端强制校验，**不依赖前端隐藏按钮**。
4. **零锁死**：任何操作组合都不会导致「再也无法管理权限」。
5. **平滑升级**：老库直接启动即完成升级；原有 `admin/admin123` 照常可登录，且自动成为超级管理员。
6. **零配置启动不变**：不设任何环境变量时，`go run .` 仍能直接跑起来。

### 1.3 非目标（本期不做，预留）

- 第三方登录 / 手机验证码登录
- 员工排班、考勤、提成
- 操作审计日志独立页面（本期仅在业务表留 `update_by` 姓名）
- 数据行级权限（如「只能看自己开的单」）—— 表里预留 `data_scope` 字段但暂不启用
- 细到按钮粒度的自定义权限（本期最小粒度 = 权限点，已足够覆盖所有接口）

---

## 二、设计原则

| 原则 | 说明 |
|---|---|
| **Fail-closed** | 路由未登记权限点 → **拒绝**，而不是放行。宁可漏配报警，不可默认开放。 |
| **后端强制** | 前端隐藏按钮只是体验优化；所有权限都必须由后端接口校验。 |
| **单一事实来源** | 权限点定义在 Go 代码里一处（`store/permission.go`），前端从接口拉取，不各自硬编码。 |
| **沿用既有风格** | 软删除 `del_flag`、时间存 `VARCHAR(32)`、金额存分、响应体 `{code,msg,data}`、错误用中文 msg。 |
| **不破坏现有 API** | 顾客端接口、登录接口的响应结构与鉴权语义全部保持兼容。 |
| **可测试** | 权限矩阵、路由覆盖率、防锁死规则全部要有自动化测试。 |

---

## 三、数据模型

新增 **2 张表**（本设计贡献 12 → 14；另有并行会话的 `tb_print_log`，全库 15 张），沿用 `{{PK}}` / `{{OPTS}}` 占位符与方言生成机制。

### 3.1 `tb_role`（角色）

```sql
CREATE TABLE IF NOT EXISTS tb_role (
	role_id     {{PK}},
	role_key    VARCHAR(32)   NOT NULL,   -- 代码内标识,唯一,如 admin / manager / cashier / staff
	role_name   VARCHAR(64)   NOT NULL,   -- 展示名,如 超级管理员
	perms       VARCHAR(4000) DEFAULT '', -- 权限码 CSV,如 'order:view,order:settle'
	data_scope  VARCHAR(16)   DEFAULT 'all', -- 预留:all=全部数据 / self=仅本人
	is_builtin  INTEGER       DEFAULT 0,  -- 1=内置(不可删除;admin 角色的权限每次启动强制恢复为全量)
	sort_order  INTEGER       DEFAULT 0,
	status      INTEGER       DEFAULT 1,  -- 1=启用 / 0=停用
	del_flag    VARCHAR(2)    DEFAULT '0',
	create_time VARCHAR(32),
	update_time VARCHAR(32),
	remark      VARCHAR(500)  DEFAULT ''
){{OPTS}}
```

**为什么权限用 CSV 而不是独立 `tb_role_perm` 表**

- 全部权限点共 28 个，最长 CSV 约 480 字符，`VARCHAR(4000)` 仍有 8 倍余量。
- 角色数量是个位数，读取场景永远是「一次性读全量」，没有「反查哪些角色拥有 X」的需求。
- 省掉一张表和一次事务，与项目现有务实风格一致（`tb_config` 也是键值对而非多表）。
- 代价：不能用 SQL 直接按权限点筛选角色（当前不需要）。若将来需要，再加映射表并做一次回填即可。

**为什么 `role_key` 与 `role_id` 并存**

- `role_id`（整数）供 `tb_user` 外键引用，保证改名不破坏关联。
- `role_key` 供代码识别内置角色（如「`admin` 角色权限强制全量」这条规则），内置角色的 key **不可修改**。

### 3.2 `tb_user`（员工）

```sql
CREATE TABLE IF NOT EXISTS tb_user (
	user_id         {{PK}},
	username        VARCHAR(64)  NOT NULL,   -- 登录名,唯一(仅约束未删除行)
	password_hash   VARCHAR(255) DEFAULT '', -- bcrypt;空表示未设密码,禁止登录
	real_name       VARCHAR(64)  DEFAULT '', -- 姓名,用于订单操作留痕
	role_id         INTEGER      NOT NULL DEFAULT 0,
	phone           VARCHAR(32)  DEFAULT '',
	status          INTEGER      DEFAULT 1,  -- 1=启用 / 0=停用(停用后立即失去访问权)
	last_login_time VARCHAR(32),
	last_login_ip   VARCHAR(64)  DEFAULT '',
	login_count     INTEGER      DEFAULT 0,
	pwd_update_time VARCHAR(32),
	token_version   INTEGER      DEFAULT 0,  -- 改密/强制下线时 +1,使旧令牌失效
	del_flag        VARCHAR(2)   DEFAULT '0',
	create_by       VARCHAR(64)  DEFAULT '',
	create_time     VARCHAR(32),
	update_by       VARCHAR(64)  DEFAULT '',
	update_time     VARCHAR(32),
	remark          VARCHAR(500) DEFAULT ''
){{OPTS}}
```

**字段说明**

- `password_hash VARCHAR(255)`：bcrypt 输出 60 字符，留余量以兼容未来算法标识前缀。
- `real_name`：订单 `create_by` / `update_by` / `settle_operator` 写这个值（而非登录名），
  对账时看到「张三」比看到「zhangsan」直观。为空时回退写 `username`。
- `token_version`：改密 → `token_version+1` → 旧令牌立即失效（比等 24 小时过期更符合直觉）。
  校验时把 `token_version` 一并放进令牌载荷。
- 不复用 `tb_config` 存员工，因为员工是**多行实体**（有列表、分页、启停用），键值对不适用。

### 3.3 索引（本设计新增 3 个：11 → 14；与并行会话的 `tb_print_log` 合并后全库共 18 个）

```go
{Name: "idx_user_username", Table: "tb_user", Columns: "username", Unique: true, Where: "del_flag='0'"},
{Name: "idx_user_role",     Table: "tb_user", Columns: "role_id"},
{Name: "idx_role_key",      Table: "tb_role", Columns: "role_key", Unique: true, Where: "del_flag='0'"},
```

> 全库索引清单以 `backend/migrations/README.md` 第五节为准（当前 18 个）。

> **注意部分索引的方言差异**（既有约定，见 `migrations/README.md` 第五节）：
> SQLite 支持 `WHERE del_flag='0'` 的部分唯一索引；MySQL 会降级为普通索引。
> 因此 MySQL 侧的「用户名唯一」由应用层 `EnsureUsernameAvailable()` 保证 ——
> 与既有的 `store.NewUniqueTableCode()` 完全同构，属于项目已有做法，不是新引入的风险。
> 新增用户时在读事务内先查重再插入。

### 3.4 数据关系

```
tb_role 1 ──── n tb_user          (user.role_id → role.role_id)
tb_user       ──→ 订单操作留痕         (create_by / update_by / settle_operator / handle_by = real_name)
tb_user       ──→ tb_refund.operator(退款操作人 = real_name)
```

**不加外键约束**：与现有表一致（`tb_order_item.dish_id` 也没有 FK），
删除角色时由应用层检查「是否仍有员工引用」并拒绝，避免历史数据被级联破坏。

---

## 四、权限模型

### 4.1 权限点目录（28 个，13 个模块）

定义在 `backend/internal/store/permission.go`，是**唯一事实来源**；前端通过 `GET /perm/catalog` 拉取，不硬编码。

| 模块 | 权限点 | 名称 |
|---|---|---|
| 桌台 | `table:view` / `table:edit` | 桌台查看 / 桌台管理 |
| 分类 | `category:view` / `category:edit` | 分类查看 / 分类管理 |
| 菜品 | `dish:view` / `dish:edit` | 菜品查看 / 菜品管理 |
| 备注 | `remark:view` / `remark:edit` | 备注查看 / 备注管理 |
| 打印机 | `printer:view` / `printer:edit` | 打印机查看 / 打印机管理 |
| 订单 | `order:view` | 订单查看（列表 / 看板 / 明细） |
| | `order:operate` | 订单流转（接单、上菜、完成、处理催菜） |
| | `order:settle` | 收款结账（收款、免单、挂账、撤销结算） |
| | `order:edit` | 订单改单（人数、菜品、折扣、备注） |
| | `order:cancel` | 订单取消 |
| 挂账 | `credit:view` / `credit:settle` | 挂账查看 / 挂账核销 |
| 退款 | `refund:view` / `refund:operate` | 退款查看 / 发起退款 |
| 报表 | `report:view` | 数据报表 |
| 系统 | `config:view` / `config:edit` | 系统配置查看 / 修改 |
| 员工 | `user:view` / `user:edit` | 员工查看 / 员工管理 |
| 角色 | `role:view` / `role:edit` | 角色查看 / 角色权限管理 |
| 操作日志 | `log:view` / `log:manage` | 操作日志查看 / 操作日志清理 |

**核心规则：`edit` 隐含 `view`**

同模块内授予 `xxx:edit` 时，服务端**自动补上** `xxx:view`（写库前归一化）。
理由：避免出现「能改但看不到」这种自相矛盾的配置 —— 前端页面都进不去，编辑按钮无从点起。
例外：`order:operate` / `order:settle` / `order:edit` / `order:cancel` 均隐含 `order:view`；
`credit:settle` 隐含 `credit:view`；`refund:operate` 隐含 `refund:view`。
`user:edit` 隐含 `user:view`，`role:edit` 隐含 `role:view`。

### 4.2 内置角色矩阵（开箱可用）

| 权限点 | 超级管理员<br>`admin` | 店长<br>`manager` | 收银员<br>`cashier` | 员工<br>`staff` |
|---|:--:|:--:|:--:|:--:|
| `table:view` | ✓ | ✓ | ✓ | ✓ |
| `table:edit` | ✓ | ✓ | | |
| `category:view` | ✓ | ✓ | ✓ | ✓ |
| `category:edit` | ✓ | ✓ | | |
| `dish:view` | ✓ | ✓ | ✓ | ✓ |
| `dish:edit` | ✓ | ✓ | | |
| `remark:view` | ✓ | ✓ | ✓ | ✓ |
| `remark:edit` | ✓ | ✓ | | |
| `printer:view` | ✓ | ✓ | ✓ | |
| `printer:edit` | ✓ | ✓ | | |
| `order:view` | ✓ | ✓ | ✓ | ✓ |
| `order:operate` | ✓ | ✓ | ✓ | ✓ |
| `order:settle` | ✓ | ✓ | ✓ | |
| `order:edit` | ✓ | ✓ | ✓ | |
| `order:cancel` | ✓ | ✓ | ✓ | |
| `credit:view` | ✓ | ✓ | ✓ | |
| `credit:settle` | ✓ | ✓ | ✓ | |
| `refund:view` | ✓ | ✓ | ✓ | |
| `refund:operate` | ✓ | **✗** | | |
| `report:view` | ✓ | ✓ | | |
| `config:view` | ✓ | ✓ | | |
| `config:edit` | ✓ | ✓ | | |
| `user:view` | ✓ | | | |
| `user:edit` | ✓ | | | |
| `role:view` | ✓ | | | |
| `role:edit` | ✓ | | | |
| `log:view` | ✓ | ✓ | | |
| `log:manage` | ✓ | | | |
| **合计** | **28** | **22** | **13** | **6** |

**角色定位说明**

- **超级管理员 `admin`**：全权。**权限不可被修改**（写接口直接拒绝 + 每次启动强制恢复为全量），这是防锁死的最后一道保险。
- **店长 `manager`**：日常最高权限，能改配置、看报表，但**不能管员工和权限**，也**不能发起退款**
  （退款通常需要老板本人操作；如需放开，复制一个自定义角色即可）。
- **收银员 `cashier`**：只做前厅收银 —— 看基础资料、处理订单全流程、核销挂账、查退款记录，
  不碰配置、报表、退款发起。
- **员工 `staff`**（后厨 / 服务员通用）：只读基础资料 + 订单查看与状态流转（接单、上菜、处理催菜），
  不能收款、不能改单、不能取消。

> 需要更细的岗位（如「后厨」「服务员」「值班经理」）由商户在「角色管理」里复制一份再调整，
> 内置角色仅保证开箱可用，不限制扩展。

### 4.3 权限与接口的映射（完整清单）

| 方法 + 路径 | 权限点 |
|---|---|
| `POST /api/admin/auth/password` | （免权限，登录即可改自己密码） |
| `GET /api/admin/auth/profile` | （免权限，返回自己的信息与权限，见 5.4） |
| `GET /api/admin/table/list` | `table:view` |
| `POST /api/admin/table/save` / `update` | `table:edit` |
| `DELETE /api/admin/table/:id` | `table:edit` |
| `GET /api/admin/category/list` | `category:view` |
| `POST /api/admin/category/save` / `update` | `category:edit` |
| `DELETE /api/admin/category/:id` | `category:edit` |
| `GET /api/admin/dish/list` / `dish/:id` | `dish:view` |
| `POST /api/admin/dish/save` / `update` | `dish:edit` |
| `DELETE /api/admin/dish/:id` | `dish:edit` |
| `GET /api/admin/remark/list` | `remark:view` |
| `POST /api/admin/remark/save` / `update` | `remark:edit` |
| `DELETE /api/admin/remark/:id` | `remark:edit` |
| `GET /api/admin/printer/list` | `printer:view` |
| `POST /api/admin/printer/save` / `update` / `test/:id` | `printer:edit` |
| `DELETE /api/admin/printer/:id` | `printer:edit` |
| `GET /api/admin/order/list` / `board` / `order/:id` | `order:view` |
| `GET /api/admin/order/urge/list` | `order:view` |
| `POST /api/admin/order/status` | `order:operate` |
| `POST /api/admin/order/urge/handle` | `order:operate` |
| `POST /api/admin/order/finish` | `order:operate` |
| `POST /api/admin/order/pay` | `order:settle` |
| `POST /api/admin/order/settle` | `order:settle` |
| `POST /api/admin/order/settle/cancel` | `order:settle` |
| `POST /api/admin/order/credit/settle` | `credit:settle` |
| `POST /api/admin/order/edit` | `order:edit` |
| `GET /api/admin/log/list` | `log:view` |
| `POST /api/admin/log/clean` | `log:manage` |
| `POST /api/admin/order/cancel` | `order:cancel` |
| `POST /api/admin/pay/refund` / `refund/query` | `refund:operate` |
| `GET /api/admin/pay/refund/list` | `refund:view` |
| `GET /api/admin/report/summary` / `dailyTrend` / `monthlyTrend` / `dishRank` | `report:view` |
| `GET /api/admin/config/list` | `config:view` |
| `POST /api/admin/config/save` | `config:edit` |
| `GET /api/admin/user/list` | `user:view` |
| `POST /api/admin/user/save` / `update` / `resetPassword` / `toggleStatus` | `user:edit` |
| `DELETE /api/admin/user/:id` | `user:edit` |
| `GET /api/admin/role/list` | `role:view` |
| `POST /api/admin/role/save` / `update` | `role:edit` |
| `DELETE /api/admin/role/:id` | `role:edit` |
| `GET /api/admin/perm/catalog` | `role:view`（权限目录，给角色编辑页用） |
| `POST /api/common/upload` | （保持现状：登录即可，不额外收紧） |

**实现方式：集中式路由→权限映射表（fail-closed）**

```go
// internal/handler/perm.go
var routePerms = map[string]string{
    "GET /api/admin/table/list": "table:view",
    "POST /api/admin/user/save": "user:edit",
    // ... 与上表一一对应
}

// RequirePerm 从 c.FullPath() 取注册时的路由模板(含 :id,与路由表 key 严格一致)。
// 未登记的路由 → 403 + 启动时/测试中报警(fail-closed)。
func RequirePerm() gin.HandlerFunc {
    return func(c *gin.Context) {
        key := c.Request.Method + " " + c.FullPath()
        perm, ok := routePerms[key]
        if !ok {
            log.Printf("[perm] 路由未登记权限,已拒绝: %s", key)
            c.AbortWithStatusJSON(403, gin.H{"code": 403, "msg": "没有操作权限"})
            return
        }
        if perm != "" && !HasPerm(c, perm) {
            c.AbortWithStatusJSON(403, gin.H{"code": 403, "msg": "没有操作权限"})
            return
        }
        c.Next()
    }
}
```

- 采用 `c.FullPath()`（返回路由模板而非真实路径），`/api/admin/table/:id` 与映射表 key 精确匹配。
- 集中式而非逐路由挂中间件：一屏可审计全部权限，且**可以写覆盖率测试**（见 9.1）。
- 兜底白名单显式列出：`POST .../auth/password`、`GET .../auth/profile` 映射到空串 `""`（= 登录即可）。

**HTTP 状态码约定**

- `401` 未登录 / 令牌失效 → 前端跳登录页（既有行为）
- `403` 已登录但无权限 → 前端弹「没有操作权限」，**不跳转**
- `400` 业务错误（既有约定不变）

> 这是对「业务错误一律 400」的一处**有意例外**：鉴权失败属于传输层语义，
> 用 401/403 才能让网关、监控、浏览器 DevTools 准确识别。
> 前端 `api/index.js` 已内置 403 → `没有操作权限` 的兜底文案，无需改动拦截器逻辑。

---

## 五、认证与鉴权链路

### 5.1 登录流程（改造 `AdminLogin`）

```
POST /api/auth/login { username, password }
  ↓
1. loginGuard 限流检查（沿用现状：同 IP 5 次失败锁 15 分钟）
  ↓
2. 查 tb_user WHERE username=? AND del_flag='0'
  ├─ 命中 → 继续
  └─ 未命中 → 【兼容兜底】若 user 表为空 且 username == ADMIN_USER 配置
              → 走老逻辑（admin_pass_hash / ADMIN_PASS）校验，成功后自动建超级管理员行
              → 否则返回「用户名或密码错误」
  ↓
3. 校验 status；status=0 → 「账号已停用，请联系管理员」
  ↓
4. verifyPassword（复用现有 bcrypt + 旧格式自动升级逻辑）
  ↓
5. 成功 → 更新 last_login_time / last_login_ip / login_count；loginGuard.clear(ip)
         → 签发新格式令牌
         → 返回 { token, username, realName, roleKey, roleName, perms[] }
```

- **防用户名枚举**：账号不存在与密码错误返回**同一句**「用户名或密码错误」。
- **停用提示的取舍**：明确提示「账号已停用」会暴露账号存在性。
  但餐饮场景下「登不上去」比「账号存在性」重要得多（服务员要立刻知道该找谁），
  因此选择明确提示，并在文档中记录这一取舍。
- 限流仍按 IP，未做「按账号限流」（可后续增强）。

### 5.2 令牌格式（`genToken` / `verifyToken`）

```
旧:  base64( 用户名 | 过期时间 ) . HMAC-SHA256
新:  base64( 用户名 | uid | token_version | 过期时间 ) . HMAC-SHA256
        ↑ fields[0]
```

- **`fields[0]` 仍是用户名** —— 现有 `adminName(c)`（`handler/pay.go:430`）取 `fields[0]`，
  因此**该函数无需任何改动**，订单留痕逻辑天然继续工作。
- **无需兼容旧令牌**：`authSecret` 是进程启动时随机生成的（`auth.go:37 init()`），
  重启即全部失效。因此可以自由改变载荷格式，不存在历史令牌需要解析的问题。
- **不把权限放进令牌**：权限随角色变化，塞进令牌会「改了角色不生效」。
  改为每次请求按 `uid` 查一次库（见 5.3）。令牌只携带身份（uid + 版本号）。

### 5.3 鉴权中间件（`AdminAuth` 改造）

```
admin group 中间件链：AdminAuth → RequirePerm
  ↓
AdminAuth:
  1. 取 Authorization: Bearer <token>
  2. 验签 + 校验过期 + 校验 token_version 与库中一致
  3. 按 uid 查 tb_user JOIN tb_role（单次查询，走主键）
     - 查不到 / del_flag='1' → 401
     - status=0（已停用）→ 401「账号已停用」
     - 角色 role_id=0 或角色已停用/删除 → 403「角色已失效，请联系管理员」
  4. 解析 perms 为 map 写入 gin.Context：uid / username / realName / roleKey / roleName / perms
  ↓
RequirePerm: 查 routePerms[method + FullPath]，判定 perms 是否包含
```

**为什么每次请求查一次库（而不是内存缓存）**

- 管理端请求频率极低（餐饮门店一天几十到几百次），SQLite 主键查询是微秒级。
- 换来的是**权限变更立刻生效**：改角色、停用账号、改密码，无需等令牌过期或重启。
- 更重要的是**代码简单、行为显然正确、易于测试** —— 缓存需要处理失效时机，是 bug 温床。
- SQLite 已开 WAL（`dining.db-wal` 可见），读操作不阻塞。
- 若将来确有性能压力，再加一层「按 uid 的短 TTL 缓存 + 写操作主动失效"，接口无需改动。

### 5.4 新增接口：`GET /api/admin/auth/profile`

前端刷新页面、或从 localStorage 拿到过期权限时，用它重新拉取自己的身份与权限：

```json
{
  "code": 200, "msg": "操作成功",
  "data": {
    "userId": 1, "username": "admin", "realName": "超级管理员",
    "roleKey": "admin", "roleName": "超级管理员", "perms": ["table:view", "..."]
  }
}
```

**为什么必须有这个接口**：前端把 perms 存在 localStorage，商户在后台改了某人的角色后，
该员工不重新登录就还带着旧权限（前端会显示不该显示的按钮）。
应用启动时（`App.vue` / 路由守卫首次进入）调一次 profile 刷新，
可以做到「后端拒绝 + 前端也随之收敛」，避免出现「看得见按钮但点了报错」的迷惑体验。

### 5.5 操作人留痕修正

| 位置 | 现状 | 改为 |
|---|---|---|
| `handler/order.go:178`（OrderStatus） | `update_by='收银员'` | `adminName(c)` |
| `handler/order.go:524`（OrderFinish） | `update_by='收银员'` | `adminName(c)` |
| `handler/order.go:566`（OrderCancel） | `update_by='收银员'` | `adminName(c)` |
| `handler/order.go:624`（OrderEdit） | `update_by='收银员'` | `adminName(c)` |

`adminName(c)` 从令牌取**登录名**。为让对账显示中文姓名，将 `adminName` 改为优先返回
context 中的 `realName`（为空才回退用户名）—— 一处改动，全部留痕点受益。

---

## 六、兼容性与防锁死

### 6.1 老库升级（零手工操作）

启动时 `migrate()` 已有「CREATE TABLE IF NOT EXISTS + 补列」机制，新增两表天然被覆盖。
另外 `incr/` 下补一份手工脚本供 DBA 离线升级：

```
migrations/incr/{sqlite,mysql}/20260921000000_tb_user_role_create.sql
```

`full/` 下 4 个脚本由 `go run ./cmd/gensql` 重新生成（**不手写**），并同步更新
`migrations/README.md` 的表清单（12 → 15）与索引清单（11 → 18）。

### 6.2 启动引导（三步，全部幂等）

```go
store.SyncBuiltinRoles()   // 1. 内置角色不存在则插入;admin 角色 perms 强制恢复为全量
store.EnsureAdminUser()    // 2. 若「启用中的超级管理员」数量为 0 → 用 ADMIN_USER/ADMIN_PASS 建一个
store.NormalizeRolePerms() // 3. 修正历史脏数据:edit 隐含 view、剔除已下线的权限码
```

**`SyncBuiltinRoles` 细节**

- 内置角色用 `INSERT IGNORE` 补齐（沿用 `InsertIgnoreInto`），**不覆盖**商户改过的权限。
- **唯一例外**：`role_key='admin'` 的角色，`perms` 每次启动**强制重置为全量（28 个）**。
  这是防锁死的最后保险 —— 即使有人（或某个 bug）清空了 admin 权限，重启即自愈。

### 6.3 防锁死规则（写入层强制）

| 规则 | 拒绝场景 | 提示 |
|---|---|---|
| 不能改自己的角色 | 把自己从超级管理员改成收银员 | 「不能修改自己的角色」 |
| 不能停用自己 | 停用当前登录账号 | 「不能停用当前登录账号」 |
| 不能删除自己 | 删除当前登录账号 | 「不能删除当前登录账号」 |
| 至少保留 1 个启用的超级管理员 | 停用/删除/改角色后超级管理员数为 0 | 「系统必须保留至少一个启用的超级管理员」 |
| admin 角色权限不可改 | 编辑 `role_key='admin'` 的权限 | 「超级管理员角色权限不可修改」 |
| admin 角色不可删 | 删除 `role_key='admin'` | 「内置角色不可删除」 |
| 内置角色不可删 | 删除任 `is_builtin=1` 的角色 | 同上 |
| 角色被员工引用时不可删 | 删除仍有员工的角色 | 「该角色下还有 N 名员工，请先调整」 |
| 用户名不可重复 | 新增/改名撞已有未删除用户名 | 「用户名已存在」 |

> 「至少保留 1 个启用的超级管理员」的判定必须在**同一事务内**完成
> （先查数量、再改、再复查），避免两个并发请求各自通过检查。

### 6.4 配置项与环境变量的角色变化

| 配置 | 升级前 | 升级后 |
|---|---|---|
| `ADMIN_USER` | 登录用户名 | **仅首次启动**用于生成超级管理员账号（后续以数据库为准） |
| `ADMIN_PASS` | 登录密码 | 同上（仅当库中无 `admin_pass_hash` 时生效） |
| `tb_config.admin_pass_hash` | 密码来源 | 首次启动迁移进 `tb_user.password_hash`；之后不再读写 |
| `TOKEN_TTL_HOURS` | 令牌有效期 | 不变，继续生效 |

**改密码的写入目标变化**：`POST /api/admin/auth/password` 从「写 `tb_config`」
改为「写当前登录员工的 `password_hash`」并 `token_version+1`。
这样才符合多员工语义（张三改密不该影响李四）。

> 兼容保险：若当前登录者是配置管理员、且 `tb_user` 中对应行缺失（理论不会发生），
> 则回退写 `admin_pass_hash`，保证任何情况下都能改密。

### 6.5 前端旧登录态

老用户的 localStorage 里只有 `admin_token` / `admin_user`，没有 `admin_role` / `admin_perms`。
处理：

1. 路由守卫发现「有 token 但缺 perms」→ 调 `/api/admin/auth/profile` 补一次；失败则跳登录页。
2. 绝不允许「perms 为空数组」被当成「有 token 但没权限」而白屏 ——
   区分「未加载（null）」与「已加载但为空（`[]`）」两种状态，前者先加载，后者展示无权限页。

---

## 七、后端改动清单

### 7.1 新增文件

| 文件 | 内容 |
|---|---|
| `internal/store/permission.go` | 权限点目录（28 个 + 模块分组 + 中文名）、`EditImpliesView` 归一化、权限码校验 |
| `internal/store/user.go` | 员工/角色 CRUD、`GetUserByUsername`、`GetAuthByID`（user JOIN role）、`CountActiveAdmins`、`EnsureAdminUser`、`SyncBuiltinRoles`、`NormalizeRolePerms`、`EnsureUsernameAvailable`、`EnsureRoleNotReferenced` |
| `internal/handler/user.go` | 员工接口：list / save / update / resetPassword / toggleStatus / delete |
| `internal/handler/role.go` | 角色接口：list / save / update / delete / catalog（权限目录）+ 防锁死校验 |
| `internal/handler/perm.go` | `routePerms` 映射表、`RequirePerm()`、ctx 取值工具（`CurrentUID` / `CurrentRealName` / `HasPerm`） |
| `routes_test.go`（包 main，复用生产的 `setupAPI()`） | 路由↔权限一致性四项：覆盖率、死条目、公开路由未被收紧、**每个权限码都有归属**（详见 9.1） |
| `internal/store/user_test.go` | 员工/角色业务测试（含全部防锁死规则、edit⊃view 归一化、停用/删除边界） |

### 7.2 修改文件

| 文件 | 改动 |
|---|---|
| `internal/store/schema.go` | 新增 2 张建表模板 + 3 条索引定义 |
| `internal/store/seed.go` | 新增内置角色种子数据（`seedRoleRows`），纳入 `SeedSQL` / `SeedCounts` |
| `internal/store/sqlgen.go` | `SeedSQL` 增加角色插入语句（供生成的种子脚本使用） |
| `internal/model/model.go` | 新增 `User` / `Role` / `PermGroup` / `LoginResult` / `Profile` 结构体 |
| `internal/handler/auth.go` | 登录查库、令牌格式升级、`AdminAuth` 加载用户与角色、`ChangePassword` 改写 user 表 |
| `internal/handler/pay.go` | `adminName` 优先返回 `realName`（订单留痕显示中文姓名）；定义搬移到 `perm.go` |
| `internal/handler/order.go` | 4 处硬编码 `'收银员'` 改为 `adminName(c)` |
| `main.go` | 启动引导三步调用；`admin` 路由组挂 `RequirePerm()`；注册 11 个新路由；启动日志打印账号/角色统计 |
| `migrations/README.md` | 表清单 12→15、索引 11→18（含并行会话的 `tb_print_log` 3 个索引）、命名示例、升级说明 |
| `docs/mysql-migration.md` | 表数量相关表述（`12 张表` → `15 张表`、`26 条索引列条目` → `18 个索引`）、校验清单补充员工/角色两项 |
| `README.md` | 新增「员工与权限」章节、账号说明、环境变量表补充 |
| `backend/.env.example` | `ADMIN_USER` / `ADMIN_PASS` 注释改为「仅首次启动生效」 |

### 7.3 新增接口一览（11 个）

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| `GET` | `/api/admin/auth/profile` | 登录即可 | 我的信息与权限（5.4） |
| `GET` | `/api/admin/user/list` | `user:view` | 员工列表（分页 + 关键字 + 角色/状态筛选） |
| `POST` | `/api/admin/user/save` | `user:edit` | 新增员工（含初始密码） |
| `POST` | `/api/admin/user/update` | `user:edit` | 修改员工（姓名/角色/手机/备注） |
| `POST` | `/api/admin/user/resetPassword` | `user:edit` | 重置指定员工密码 |
| `POST` | `/api/admin/user/toggleStatus` | `user:edit` | 启用 / 停用 |
| `DELETE` | `/api/admin/user/:id` | `user:edit` | 删除（软删除） |
| `GET` | `/api/admin/role/list` | `role:view` | 角色列表（含员工数统计） |
| `POST` | `/api/admin/role/save` | `role:edit` | 新增角色 |
| `POST` | `/api/admin/role/update` | `role:edit` | 修改角色（名称/权限/排序/备注） |
| `DELETE` | `/api/admin/role/:id` | `role:edit` | 删除角色 |
| `GET` | `/api/admin/perm/catalog` | `role:view` | 权限点目录（含模块分组与中文名，供前端渲染勾选框） |

---

## 八、前端改动清单

### 8.1 新增文件

| 文件 | 内容 |
|---|---|
| `src/utils/authKeys.js` | 登录态 localStorage 键名的**唯一来源**（`LS_TOKEN` … `LS_PERMS` + `AUTH_KEYS`）。`perm.js` 与 `api/index.js` 都从这里取，避免两处各写一份字符串导致改名漏改（曾出现「401 只清了 token、权限缓存残留」） |
| `src/utils/perm.js` | `hasPerm(code)` / `setAuth(data)` / `clearAuth()` / `getRoleName()` / `refreshProfile()`；perms 用 `ref(Set)` 保证 computed 可追踪 |
| `src/views/Users.vue` | 员工管理：列表（关键字/角色/状态筛选）+ 新增/编辑弹窗 + 重置密码 + 启停用 + 删除 |
| `src/views/Roles.vue` | 角色管理：角色列表 + 权限勾选（按模块分组，模块级全选，编辑自动勾上查看）+ 内置角色只读提示 |
| `src/views/NoPermission.vue` | 「无可用功能」提示页（员工角色被清空时兜底，避免白屏） |

### 8.2 修改文件

| 文件 | 改动 |
|---|---|
| `src/api/index.js` | 新增 12 个接口函数；`login` 返回值含 perms |
| `src/router/index.js` | 每条管理端路由加 `meta.perm`；守卫改为「requiresAuth → 补载 profile → 校验 perm → 无权限跳 NoPermission」 |
| `src/layout/AdminLayout.vue` | `menus` 每项加 `perm` 并过滤；顶栏显示「姓名（角色名）」；无权限菜单不渲染 |
| `src/views/Login.vue` | 登录成功后 `setAuth(res)` 存 token/用户名/姓名/角色/perms |
| `src/App.vue` | 首次挂载时刷新一次 profile（6.5 的登录态收敛） |
| `src/views/Orders.vue` | 按 `order:settle` / `order:cancel` / `order:edit` / `refund:operate` 显隐按钮 |
| `src/views/Credit.vue` | 按 `credit:settle` 显隐核销按钮 |
| `src/views/Tables.vue` / `Categories.vue` / `Dishes.vue` / `Remarks.vue` / `Printers.vue` | 按 `xxx:edit` 显隐新增/编辑/删除按钮与弹窗入口 |
| `src/views/Config.vue` | 按 `config:edit` 禁用保存按钮（`config:view` 只读） |
| `src/views/Report.vue` | 无 `report:view` 由路由守卫拦截，页面内无需改动 |

> **前端隐藏按钮 ≠ 安全**。所有隐藏都只是体验优化，真正的拦截在后面两个中间件。
> 每个按钮显隐都对应一个后端权限点，一一对齐（第 4.3 节表格）。

### 8.3 菜单与权限对照

| 菜单 | 路由 `meta.perm` |
|---|---|
| 仪表盘 | `order:view` |
| 桌台管理 | `table:view` |
| 分类管理 | `category:view` |
| 菜品管理 | `dish:view` |
| 订单管理 | `order:view` |
| 挂账管理 | `credit:view` |
| 打印机管理 | `printer:view` |
| 打印记录 | `printer:view` |
| 备注管理 | `remark:view` |
| 系统配置 | `config:view` |
| 员工管理 | `user:view` |
| 角色权限 | `role:view` |
| 数据报表 | `report:view` |

#### 8.3.1 跨权限取数（每个页面都可能有「额外要的权限」）

页面能打开 ≠ 页面能完整渲染：一个页面往往要调多个模块的接口。
下表列出**「本页所需权限」之外的额外依赖**，实现上都做了降级处理（有权限才发请求 / 失败不阻断页面）：

| 页面 | 额外依赖 | 处理方式 |
|---|---|---|
| 仪表盘 | `report:view`（`report/summary`） | 无则不请求，KPI 区只留「桌台占用」卡（`.kpis.solo`） |
| 挂账管理 | `order:view`（`order/list`）、`report:view` | `report/summary` 用 `canReport` 门控；**`order:view` 由隐含规则自动连带授予**（见下方） |
| 菜品管理 | `category:view`（`category/list`） | try/catch 兜底，无权限时分类下拉为空 |
| 打印机管理 | `category:view`（`category/list`） | try/catch 兜底（同上） |
| 桌台管理 | `config:view`（`config/list`，取店铺名/Logo 画二维码） | try/catch 兜底，无权限时二维码不带 Logo |
| 员工管理 | `role:view`（`role/list`，角色下拉） | try/catch 兜底 + 明确文案提示；**`role:view` 由隐含规则自动连带授予**（见下方） |

> **已修复（2026-09-22）**：原先只勾「挂账查看」或「员工查看」会出现「菜单能进、一开页 403」。
> 现采用**方案①：跨模块隐含规则**（`store.permImplies`）：
>
> ```go
> var permImplies = map[string]string{
>     "credit:view": "order:view", // 挂账列表就是订单数据,走 order/list
>     "user:view":   "role:view",  // 员工管理页要读角色下拉
> }
> ```
>
> `NormalizePerms` 改为**迭代到不动点**补齐，因此链可以串起来：
> `credit:settle` → `credit:view`（同模块规则）→ `order:view`（跨模块规则）。
> 管理员勾不掉（保存与启动时都会被强制补全），前端 `Roles.vue` 同步维护同一个闭包，
> 并在勾选框旁标注「连带授予 订单查看」，避免出现「取消了又被补回来」的假取消。
>
> 选①而不是②（只做前端门控）的理由：②只是把 403 换成一句提示，权限依然残缺；
> ①是根因治理。代价是权限语义从「模块独立」扩展为「模块间可声明依赖」。
> **内置 4 角色权限数不受影响**（含 `credit:view` 的 manager/cashier 本来就同时有
> `order:view`，无一含 `user:view`），仍是 28 / 22 / 13 / 6。
>
> 注：`credit:view` 本身仍是「菜单级」权限（后端无路由引用它，已在
> `handler.menuOnlyPerms` 里显式声明）—— 真正把守数据的是它连带授予的 `order:view`。

---

## 九、测试与验收

### 9.1 自动化测试（go test，已实施）

**路由 ↔ 权限一致性（`backend/routes_test.go`，package main，复用生产的 `setupAPI()`）**

| 测试 | 断言内容 |
|---|---|
| `TestAdminRoutePermCoverage` | 遍历 `r.Routes()`，断言每条 `/api/admin/*` 路由都在 `routePerms` 中登记 —— **新增路由忘登记权限会直接测试失败**（fail-closed 下会线上 403）。当前覆盖 65 条 |
| `TestRoutePermsHaveNoDeadEntries` | 反向校验：权限表里的条目必须真实存在于路由树（防「删了接口忘删条目」与路径拼错） |
| `TestCustomerRoutesStayPublic` | 14 条顾客端/登录公开路由必须存在**且**不得出现在权限表里（防误挂 AdminAuth 把扫码点餐打断） |
| `TestEveryPermCodeIsUsedOrDeclaredMenuOnly` | 目录里 28 个权限码，每个都要么被至少一条路由引用、要么在 `handler.menuOnlyPerms` 显式声明为菜单级（当前 27 + 1）。**这是补上 `credit:view` 那类「权限码没人用」漏洞的守卫** |
| `TestAdminWriteRoutesHaveAuditMeta` | 遍历写操作路由（POST/PUT/PATCH/DELETE），断言每条都在 `handler.routeAudit` 中登记审计元信息 —— 审计中间件是 fail-open 的，漏登记的后果是**静默不记录**，只能靠测试拦 |
| `TestAuditMetasHaveNoDeadEntries` | 反向校验：审计表条目必须真实存在于路由树 |

**数据层与鉴权（`backend/internal/store/user_test.go`）**

| 测试 | 断言内容 |
|---|---|
| `TestPermCatalogIntegrity` | 目录 28 个权限码 / 13 个模块结构完整，无重复、无非法命名 |
| `TestNormalizePermsEditImpliesView` | 「非 view 隐含同模块 view」规则 |
| `TestNormalizePermsCrossModuleImplies` | 跨模块隐含依赖（`credit:view`→`order:view`、`user:view`→`role:view`），含两环串联 `credit:settle`→`credit:view`→`order:view`；**并断言隐含是单向的**（`order:view` 不得反推 `credit:view`） |
| `TestPermImpliesTable` | 隐含表自身没写坏：key/value 都必须是已登记权限码（**拼错会静默失效**，最难发现的一类 bug）、不可自指、不可同模块、不可成环 |
| `TestImpliedByMatchesNormalize` | `ImpliedBy`（前端勾选提示）与 `NormalizePerms`（实际落库）必须用同一套规则 —— 二者不一致时前端提示会骗人 |
| `TestNormalizePermsDropsUnknownAndDedupes` | 剔除已下线权限码、去重、稳定排序 |
| `TestBuiltinRoleMatrix` | 4 个内置角色权限数 28 / 22 / 13 / 6，且 `refund:operate` 与 `log:manage` 只属 admin |
| `TestSyncBuiltinRolesCreatesFourRoles` / `TestSyncBuiltinRolesAdminSelfHeals` | 内置角色建立；admin 权限被改后重启恢复全量；商户改过的 cashier 权限不被覆盖 |
| `TestEnsureAdminUser` / `TestEnsureAdminUserRestoresDisabledAdmin` | 空库建引导超管；已有启用超管不重复建；超管被停用后重启补一个 |
| `TestAntiLockoutRules` / `TestRoleWriteRules` | 9 条写入规则逐条断言（见 6.3） |
| `TestUsernameUniqueness` | 同名不可重复；软删除后同名可复用 |
| `TestPasswordResetInvalidatesToken` | 重置密码后旧令牌立即失效（`token_version` +1） |
| `TestListUsersFilters` | 分页 / 关键字 / 角色 / 状态筛选（含 `Status *int` 回归） |
| `TestDisplayNameFallback` | 操作人留痕优先中文姓名、未填回退登录名 |

**令牌与登录（`backend/internal/handler/auth_test.go`）**：`TestTokenRoundTrip` / `TestTokenTampered` / `TestTokenPayloadShape`（载荷四段结构、签名校验、用户名仍在第一段）。

**脚本一致性（`backend/internal/store/sqlgen_test.go`，既有）**：自动断言 `migrations/full/` 两个方言的脚本与 Go 定义一致；表数用 `len(schemaTemplate)` 而非写死数字。

### 9.2 接口回归（沿用 09-21 的脚本化实测）

在既有 46 项 SQLite 回归脚本基础上，追加权限用例：

1. 用 `admin` 令牌创建 4 个员工，各绑定一个内置角色。
2. **对每个角色，逐个权限点验证**：有权限的接口返回 200，无权限的返回 **403**（且 msg = `没有操作权限`）。
   这是本次改造最核心的验收项 —— 28 个权限点 × 4 角色，用脚本批量断言，不靠人工点。
3. 顾客端 13 个公开接口在**不带任何令牌**下仍全部 200（回归确认未被误收紧）。
4. 停用某员工后，其旧令牌立即 401。
5. 修改某角色权限后，该角色员工的旧令牌立即按新权限判定（验证「无缓存、即时生效」）。
6. 重置密码后，旧令牌立即失效（`token_version`）。

### 9.3 前端验证

- 用 09-21 验证过的手法：在项目根建**临时自检页**，通过 `<script type="module">` 由 vite dev
  直接加载 `src/utils/perm.js`，一次性验证菜单过滤与按钮显隐逻辑，验完删除该文件并重新构建。
  （避开浏览器自动化在 SPA 内点击跳转不稳定的问题。）
- 手工确认：4 个角色分别登录，看到 5 种不同的菜单组合；直接手敲无权限路由 URL 被守卫拦下。
- 确认 `npm run build` 成功，后端静态托管下功能正常。

### 9.4 文档与一致性

- `go build` / `go vet` / `gofmt` / `go test -count=1 ./...` 全绿
- `go run ./cmd/gensql -check` 显示 4 个脚本均为最新
- MySQL 路径同样跑一遍（用 09-21 搭的进程内 MySQL 桩），确认 15 张表 / 18 个索引与全链路接口（**待补测**）

---

## 十、实施阶段（每阶段可独立验证）

| 阶段 | 内容 | 产出与验证 | 状态 |
|---|---|---|---|
| **P1 数据层** | 表结构 + 权限目录 + 内置角色种子 + 启动引导三步 + incr 脚本 + gensql 重生成 | `migrate()` 自动建表；`gensql -check` 通过；`user_test.go` 通过 | ✅ 完成 |
| **P2 认证鉴权层** | 令牌升级 + 登录查库 + `AdminAuth` 改造 + `RequirePerm` + 路由表 + 覆盖率测试 | 单管理员场景功能不回退；`routes_test.go` 覆盖率 100% | ✅ 完成 |
| **P3 管理接口** | 员工 CRUD + 角色 CRUD + 权限目录 + 9 条防锁死规则 | `user_test.go` 覆盖全部规则 | ✅ 完成（接口回归待做） |
| **P4 前端** | `perm.js` + 守卫 + 菜单过滤 + 按钮显隐 + 两个新页面 + 登录页 | 4 角色登录看到不同菜单；无权限路由被拦；构建通过 | ✅ 完成（`npm run build` 通过；`perm.js` 权限集改为 `shallowRef` 响应式；13 个页面按权限点显隐按钮；Dashboard/Credit 的跨权限取数已做「有权限才请求」兜底）。**浏览器端菜单渲染未做 E2E** |
| **P5 收尾** | 文档（README / migrations README / mysql-migration）+ 全量回归 + MySQL 实测 | 全绿；生成《员工与权限使用说明》 | ✅ 文档已同步（README 新增「员工与权限」章节与鉴权状态码说明；migrations README 表/索引清单 12/11 → **15/18**；mysql-migration 验证清单补员工/角色项）；接口级 403 矩阵回归通过。**MySQL 15 表全量建库实测待补** |

### 实施过程中的两处偏差（已记录）

1. **放弃了「配置管理员密码兜底登录」分支**。原设计保留了一条「user 表为空且用户名 == `ADMIN_USER` 时走老密码校验」的兼容路径，实施时确认它是**死代码**：`EnsureAdminUser()` 每次启动都会把老商户的账号密码（`admin_pass_hash` 或 `ADMIN_PASS`）迁移成真实账号，因此那条分支永远不会触发。删掉后登录链路只有一条，分支更少、更易推理。

2. **`UserQuery.Status` 改用指针**。原设计里 `Status int`（-1 表示不限）存在陷阱：账号状态 `0` 就是「停用」，零值 `UserQuery{}` 会被误判为「只看停用账号」，列表恒为空。已改为 `*int`（nil = 不限），并补了回归断言。

### 实施期间发现的工作区并发改动（需注意）

实施过程中检测到**同一工作区有另一个会话在同时改代码**（打印子系统重构：拆分 `internal/print/{escpos,feie,ticket}.go`，新增 `tb_print_log` 表、`model.PrintLog`、`feie_*` 打印配置）。带来的三点影响：

- 生成脚本的表数由 12 → **15**（12 原有 + `tb_print_log` + `tb_role` + `tb_user`）。`gensql` 会把双方的 schema 一起生成，这是正确行为（`schema.go` 是唯一事实来源）。
- `sqlgen_test.go` 里写死的「12 张表」已改为 `len(schemaTemplate)`，避免每次加表都要改测试、反复冲突。
- 对方的 8 个新管理端路由（打印探测/绑定/补打/打印记录）**起初未登记权限**，被本设计的路由覆盖率测试直接拦下（fail-closed 下会线上 403）。已在 `routePerms` 中补登：查看类走 `printer:view`，会触发打印或改动打印机状态的动作走 `printer:edit`。


**预估改动规模**

- 后端新增约 1200 行（含测试），修改约 250 行
- 前端新增约 800 行，修改约 200 行
- 新增 2 张表、11 个接口、2 个页面、1 份文档

---

## 十一、风险与取舍

| 风险 | 影响 | 应对 |
|---|---|---|
| 锁死自己无法管理权限 | 系统不可用 | admin 角色权限每次启动强制恢复全量 + 9 条防锁死规则 + 不能改自己角色 |
| MySQL 侧用户名唯一性依赖应用层 | 极端并发下可能重名 | 与既有 `table_code` 同构；创建走事务内查重；文档记录该已知差异 |
| 每请求一次查库 | 轻微延迟增加 | 管理端 QPS 极低；主键查询微秒级；必要时可加短 TTL 缓存而不改接口 |
| 权限点粒度不够细 | 商户想更细控制 | 28 个点已覆盖全部接口；未来可细分（如`order:settle:refund`）而不改变现有结构 |
| 前端按钮显隐漏改 | 露出不该有的按钮 | 后端强制拦截（403）；前端按第 4.3 节表格逐项对齐，并用自检页验证 |
| 商户不熟悉角色配置 | 配错权限 | 4 个内置角色开箱可用；角色页按模块分组 + 中文名 + 模块全选；admin 不可改 |
| 订单历史 `update_by` 是「收银员」 | 老数据无法追溯 | 不追溯（无数据源）；新数据开始记录真实姓名；文档说明分界线 |

---

## 十二、待确认决策点

在开始写代码前，请确认以下 4 项（我会按你的选择实施）：

**Q1 内置角色数量**
建议 4 个（超级管理员 / 店长 / 收银员 / 员工）。
是否要拆得更细，比如额外内置「后厨」「服务员」「值班经理」？

**Q2 退款权限**
建议**只给超级管理员**（店长也不行），理由是退款涉及资金、风险最高。
是否同意？还是给店长也放开？

**Q3 登录后默认落地页**
建议按「有权限的第一个菜单」决定（而不是固定跳仪表盘），
这样后厨登录后直接进订单看板，不用先看一个自己没权限的页面。
是否同意？仪表盘是否也要求 `order:view` 才可见？

**Q4 员工自己改密码**
建议员工可在顶栏自助改密码（沿用现有弹窗，只是写入自己的账号）。
是否需要额外的「首次登录强制改密」？（会增加一点复杂度，建议本期不做）

---

## 附：设计要点速查

```
权限判定三层：路由守卫(体验) → AdminAuth(401 身份) → RequirePerm(403 权限)
权限来源    ：tb_user.role_id → tb_role.perms(CSV) → 28 个权限点
唯一事实来源：internal/store/permission.go（后端定义，前端拉取）
防锁死三板斧：admin 权限启动强制恢复 + 9 条写入规则 + 不能动自己
兼容保障    ：老库自动建表；admin/admin123 照常登录并成为超级管理员；顾客端零改动
即时生效    ：无权限缓存，改角色/停用/改密后旧令牌立刻按新状态判定
```
