# Feature：订单评价（仅商家可见）——内部反馈通道设计

**作者**：ZCode（代码调研 + 起草）
**日期**：2026-10-24
**状态**：Implemented（2026-10-24 落地,后端 6 测试 + 前端 7 测试全绿）
**关联代码**：
`backend/infrastructure/store/{schema.go,permission.go,dao/review.go}`、
`backend/domain/po/review.go`、`backend/application/service/review_service.go`、
`backend/interfaces/handler/{review.go,perm.go,audit.go}`、`backend/main.go`、
`frontend/apps/customer/src/order/{OrderTrackView.vue,ReviewSheet.vue}`、
`frontend/apps/customer/src/OrderPage.vue`、`frontend/apps/admin/src/views/Reviews.vue`

> 一句话：顾客吃完付款后可在订单页提交**一次**「1-5 星 + 快捷标签 + 文字」的评价；
> 评价内容与列表**仅商家可见**，用于后台管理与改进；每条评价自含订单与顾客身份
> **快照**（会员昵称/手机号、匿名者胡椒哈希 IP），注销会员不断追溯链。

---

## 0. 决策记录

1. **纯内部反馈通道，不公示**：评价与列表只有商家看得到，不向其他顾客展示。
   这砍掉了公开展示带来的一切复杂度（审核、刷评对抗、公关），核心价值收敛为
   「让老板知道哪桌吃得不满、为什么不满」。
2. **评价模型 = 总分（1-5 星，必填）+ 快捷标签（多选，≤6 个）+ 文字（选填，≤200 字）**。
   不做口味/速度/服务多维评分：多维对顾客是负担、对小店是过度分析；标签分布聚合
   已能回答「差评差在哪」。标签 v1 用固定两组，**七个正交维度、好/差一一对称**
   （对称是硬要求：标签云的核心价值是「同维度好/差对比」，不对称的词让聚合失效），
   差评侧用顾客口语且可行动。定稿：味道好/不合口味、上菜快/上菜慢、分量足/分量少、
   服务热情/服务差、环境舒适/环境差、干净卫生/不够干净、实惠/偏贵。商户自定义
   留待后续配置化。
3. **入口时机 = `pay_status=1 且 order_status ∈ {3 已上齐, 4 已完成}`**。
   后付模式下顾客付款后停在 settled 视图（菜已上齐）；先付模式付款早于用餐，
   等轮询把状态推到「已上齐」入口才出现。仅等 status=4 会错过顾客——管理端点
   「完成」常发生在顾客离店之后。
4. **匿名与会员同一接口**：挂 `CustomerAuthOptional`，会员身份只认服务端令牌解析、
   不信请求体（与下单归属同防线）。
5. **一单一评、提交后不可改**：`tb_review(order_no)` 唯一索引兜底并发；管理端可
   删除垃圾内容（`review:edit` + 操作审计）。
6. **追溯信息提交时一次性快照，不联表**：会员注销是物理删除档案且订单归属置 0
   （`customer_member_service.go`「档案与登录方式物理删除」），联表方案在注销后
   追溯链断裂。快照让评价行自含「当时是谁」，与 `tb_order` 快照桌台名、
   `tb_print_log` 快照店名同一设计哲学。**注销时评价保留、快照不回删**（评价是
   店方经营记录，与「历史订单保留」同一先例）。
7. **明文 IP 永不落库**：匿名滥用追溯只存 `sha256(auth.key|IP)` 胡椒哈希（复用
   `visit_service.hashVisitValue`，同包零成本），与访问统计同密钥体系。
8. **v1 只做 H5 顾客端提交入口**：小程序是平行实现，放 v2（挂会员「历史订单」页，
   兼作离店补评通道）。管理端一次做全。
9. **金额/菜品不冗余进评价表**：`order_id` 关联即得，双份事实源会在退款/撤销结算后
   误导（评价里冻结旧金额）。

---

## 1. 需求背景

当前订单生命周期走完（付款 → 上齐 → 完成）后，顾客没有任何反馈出口；商家想知道
「今天哪桌吃得不满、为什么」只能靠当面询问。需要一个轻量的评价通道：

- 匿名顾客和登录会员都能评（大部分扫码客不是会员）；
- 评价与订单、顾客身份强关联，方便差评回访与责任追溯；
- 内容仅商家内部可见，用于管理与提升，不对外公示。

## 2. 数据模型：新表 `tb_review`

```
review_id    主键
order_id / order_no          关联订单（管理端点单号直跳订单详情弹层）
biz_date                     营业日快照（筛选与报表同口径）
table_id / table_no / table_name   桌台快照（桌台改名不影响历史评价）
person_count                 就餐人数快照（差评语境：2 人慢还是 10 人慢）
customer_id                  0=匿名；会员注销后仅作历史编号
nickname / phone             会员昵称/手机号快照（phone 取 tb_customer_identity
                             provider='phone'，weapp-only 会员为空；匿名为空串）
rating                       1..5
tags                         逗号分隔，≤6 个，每个 ≤12 字
content                      文字评价 ≤200 字（列宽 500 留余，按 rune 截断）
ip_hash / ua_hash            胡椒哈希（复用 hashVisitValue），明文不落库
create_time / update_time    同各表惯例
```

索引：`UNIQUE(order_no)`（一单一评数据库兜底）、`(create_time)`、`(rating, create_time)`。

加表流程遵循「表结构唯一真源」：只改 `store/schema.go`，`go run ./cmd/gensql`
重新生成两库脚本；启动时 `CREATE TABLE IF NOT EXISTS` 自动建表，存量部署升级重启
即得，无需 incr 迁移（无数据回填）。

## 3. 接口设计

顾客端（`/api/customer` 公开组 + IP 限流，与 `/customer/order/no/:orderNo` 同款）：

| 接口 | 说明 |
|---|---|
| `GET /customer/review?orderNo=&tableId=` | `{reviewed, rating, tags, content}`；未评 `reviewed:false` |
| `POST /customer/review` | 提交。校验链：归属（order.table_id==tableId）→ 状态（已付且已上齐/完成）→ 未评过 → 参数合法 → 快照落库 |

管理端（`/api/admin/review/*`）：

| 接口 | 权限码 | 说明 |
|---|---|---|
| `GET /review/list` | `review:view` | 分页列表 + 同口径汇总（总均分、评分分布、标签频次、差评率）一次返回 |
| `DELETE /review/:id` | `review:edit` | 删除不当内容（操作审计留痕，物理删除） |

权限三处注册（fail-closed，routes_test 覆盖率测试强制）：`routePerms`、
`permGroups` 新增 `{Key:"review", Name:"评价"}` 组、`main.go` 路由。

## 4. 前端

- **顾客 H5**：`order/ReviewSheet.vue` 底部弹层（复用 SpecSheet/RemarkSheet 的
  弹层基建与手势/无障碍 composable）：5 星（大热区）、标签 chips、带字数
  textarea、提交防重锁。文案明示「仅店家可见」。入口在 `OrderTrackView` 的
  settled/done 态；已评变为「已评价 ✓」。
- **管理端**：`views/Reviews.vue` 挂「订单中心」菜单组（path `reviews`，
  perm `review:view`）：汇总卡（均分/总数/差评率/近 7 日均分）+ 标签云 +
  列表（时间/订单号可点/桌台/人数/星级/标签/内容/顾客列）+ 筛选（日期、评分档、
  仅看有文字、关键词）。顾客列三态：会员（昵称+脱敏手机号）/ 已注销会员 /
  匿名。进页拉取，不做自动轮询（低频数据）。

## 5. 追溯与隐私

- 手机号/昵称快照仅管理端可见；顾客端任何接口不回显他人评价与身份。
- 明文 IP 不落库；哈希仅供排查刷评时库内比对。
- 删除评价走操作审计（谁/何时/哪条 + 摘要），物理删除。

## 5a. 评价图片（独立表 `tb_review_image`）

- **独立于 `tb_image` 的两张表**：菜品图/收款码是商家管理的资产，评价图是顾客
  上传的用户内容——生命周期（级联删除/孤儿清理）与合规属性完全不同，混存一张
  表既无法区分业务，将来迁对象存储（COS）时也难以整表筛选搬迁。
- **「先传后评」**：选图即上传（`POST /customer/review/upload`，multipart），
  图片先落在 `tb_review_image(review_id=0)`；提交评价时在**同一事务**里绑定
  （`BindReviewImagesTx` 只认未绑定行，先到先得，部分失效整体回滚）。
- **限制**：每单 ≤3 张、单张 ≤5MB、扩展名白名单 + 魔数/解码校验（与商家上传
  同一套 `isValidImage`）。
- **展示**：本体 BLOB 入库（与 `tb_image` 同一运行时模型），展示统一走
  `/uploads/{name}`——`service.GetImage` 两级回落（先查评价表再查资产表），
  ServeImage 与前端零感知。
- **回收**：管理端删评价级联删图；`StartReviewImageOrphanCleaner` 每 6 小时
  清理「上传后 24h 未绑定」的孤儿行。
- 迁 COS 路径：整表搬迁 `tb_review_image`，把 `img_data` 上传对象桶、表结构
  换成 `object_key`（或加 `url` 列改读侧），业务表仍存 `/uploads/{name}` 或
  直接存 COS URL，筛选范围就是这一张表。

## 6. 非目标（v1 不做）

- 评价公示、商家回复、菜品粒度评价、修改/追评；（图片评价已由 5a 节落地）
- 小程序提交入口（v2：会员历史订单详情页）；
- 报表页评分卡片（v2 可挂 ReportSummary「今日平均分」）；
- 商户自定义标签（v2 配置项化）。

## 7. 测试清单

- service：校验链（未付款/未上齐/已评过/参数越界）、匿名与会员快照、
  注销后评价保留（dao 层面：删除会员不触评价）、并发重复提交唯一索引兜底；
- dao：分页 + 筛选 + 汇总口径一致（total 与 items 同条件）；
- routes：权限覆盖率测试自动覆盖新管理端路由；
- 前端：ReviewSheet 交互（星级/标签/字数/防重）、OrderTrackView 入口三态
  （可评/已评/不满足条件）。
