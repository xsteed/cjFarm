package store

import (
	"sort"
	"strings"
)

// ============================================================================
// 权限点目录 —— 权限体系的唯一事实来源
//
// 后端据此校验权限,前端通过 GET /prod-api/dining/perm/catalog 拉取同一份定义
// 渲染勾选框。两边不各自硬编码,避免「新增权限点后前端漏改」。
//
// 命名规则: <模块>:<动作>,动作取值 view / edit / operate / settle / cancel。
//
// 隐含规则(由 NormalizePerms 强制,管理员无法手动取消):
//   ① 同模块:除 xxx:view 以外的任意权限码,都自动包含同模块的 xxx:view ——
//      避免出现「能改但看不到」这种自相矛盾的配置(页面都进不去,编辑按钮无从点起)。
//   ② 跨模块:见下方 permImplies。某些菜单页的数据实际来自别的模块的接口,
//      只给本模块权限会出现「菜单能进、一开页就 403」。
// ============================================================================

// PermDef 单个权限点。
type PermDef struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// PermGroup 权限模块分组(前端按组渲染与「全选」)。
type PermGroup struct {
	Key   string    `json:"key"`
	Name  string    `json:"name"`
	Perms []PermDef `json:"perms"`
}

// permGroups 权限目录: 13 个模块 / 28 个权限点。
var permGroups = []PermGroup{
	{Key: "table", Name: "桌台", Perms: []PermDef{
		{Code: "table:view", Name: "桌台查看"},
		{Code: "table:edit", Name: "桌台管理"},
	}},
	{Key: "category", Name: "分类", Perms: []PermDef{
		{Code: "category:view", Name: "分类查看"},
		{Code: "category:edit", Name: "分类管理"},
	}},
	{Key: "dish", Name: "菜品", Perms: []PermDef{
		{Code: "dish:view", Name: "菜品查看"},
		{Code: "dish:edit", Name: "菜品管理"},
	}},
	{Key: "remark", Name: "备注", Perms: []PermDef{
		{Code: "remark:view", Name: "备注查看"},
		{Code: "remark:edit", Name: "备注管理"},
	}},
	{Key: "printer", Name: "打印机", Perms: []PermDef{
		{Code: "printer:view", Name: "打印机查看"},
		{Code: "printer:edit", Name: "打印机管理"},
	}},
	{Key: "order", Name: "订单", Perms: []PermDef{
		{Code: "order:view", Name: "订单查看"},
		{Code: "order:operate", Name: "订单流转(接单/上菜/完成/处理催菜)"},
		{Code: "order:settle", Name: "收款结账(收款/免单/撤销结算)"},
		{Code: "order:edit", Name: "订单改单(人数/菜品/折扣)"},
		{Code: "order:cancel", Name: "订单取消"},
	}},
	{Key: "credit", Name: "挂账", Perms: []PermDef{
		{Code: "credit:view", Name: "挂账查看"},
		{Code: "credit:settle", Name: "挂账核销"},
	}},
	{Key: "refund", Name: "退款", Perms: []PermDef{
		{Code: "refund:view", Name: "退款查看"},
		{Code: "refund:operate", Name: "发起退款"},
	}},
	{Key: "report", Name: "报表", Perms: []PermDef{
		{Code: "report:view", Name: "数据报表查看"},
	}},
	{Key: "config", Name: "系统配置", Perms: []PermDef{
		{Code: "config:view", Name: "系统配置查看"},
		{Code: "config:edit", Name: "系统配置修改"},
	}},
	{Key: "user", Name: "员工", Perms: []PermDef{
		{Code: "user:view", Name: "员工查看"},
		{Code: "user:edit", Name: "员工管理"},
	}},
	{Key: "role", Name: "角色", Perms: []PermDef{
		{Code: "role:view", Name: "角色查看"},
		{Code: "role:edit", Name: "角色权限管理"},
	}},
	{Key: "log", Name: "操作日志", Perms: []PermDef{
		{Code: "log:view", Name: "操作日志查看"},
		{Code: "log:manage", Name: "操作日志清理"},
	}},
}

// permCodeIndex 权限码 -> true 的索引(包加载时构建,只读)。
var permCodeIndex = func() map[string]bool {
	m := map[string]bool{}
	for _, g := range permGroups {
		for _, p := range g.Perms {
			m[p.Code] = true
		}
	}
	return m
}()

// permNameIndex 权限码 -> 中文名。
var permNameIndex = func() map[string]string {
	m := map[string]string{}
	for _, g := range permGroups {
		for _, p := range g.Perms {
			m[p.Code] = p.Name
		}
	}
	return m
}()

// permImplies 跨模块隐含依赖:拥有 key 会自动连带拥有 value(可传递)。
//
// 起因(2026-09-22 审计发现):有些菜单页的数据实际来自别的模块的接口。
//   - 挂账管理:展示的是 settle_type='credit' 的订单,数据走 GET /dining/order/list,
//     需要 order:view。只勾 credit:view → 菜单能进、列表接口 403。
//   - 员工管理:新增/编辑员工要拉角色下拉框,需要 role:view。只勾 user:view →
//     角色下拉永远为空,建号改角色静默不可用。
//
// 内置 4 个角色恰好都同时拥有这两对权限,所以问题只在**自定义角色**上暴露。
// 与规则①一样由 NormalizePerms 在写库与每次启动时强制补全。
//
// ⚠️ 维护注意
//   - 隐含链**不允许成环**:否则勾上任意一环都会把整条链拉满,权限就失去意义。
//     由 TestPermImpliesAcyclic 守住。
//   - 新增条目后,perm/catalog 接口的 implies 字段会自动带上(前端据此提示),
//     无需改前端。
var permImplies = map[string]string{
	"credit:view": "order:view", // 挂账列表就是订单列表
	"user:view":   "role:view",  // 员工管理页要读角色下拉
}

// PermImplies 返回跨模块隐含依赖表(供 /perm/catalog 接口回给前端做勾选提示)。
func PermImplies() map[string]string {
	out := make(map[string]string, len(permImplies))
	for k, v := range permImplies {
		out[k] = v
	}
	return out
}

// ImpliedBy 返回勾选 code 后会被连带自动授予的全部权限码(不含 code 自身)。
// 两条隐含规则都覆盖(同模块 view + 跨模块 permImplies 链),按目录顺序输出。
func ImpliedBy(code string) []string {
	set := map[string]bool{}
	var walk func(c string, depth int)
	walk = func(c string, depth int) {
		// 成环保护:链长不可能超过权限点总数,超过说明配置写错了。
		if depth > len(permCodeIndex)+1 {
			return
		}
		if mod := moduleOf(c); mod != "" && c != mod+":view" {
			if v := mod + ":view"; permCodeIndex[v] && !set[v] {
				set[v] = true
				walk(v, depth+1)
			}
		}
		if v, ok := permImplies[c]; ok && !set[v] {
			set[v] = true
			walk(v, depth+1)
		}
	}
	walk(code, 0)
	out := []string{}
	for _, c := range AllPermCodes() {
		if set[c] {
			out = append(out, c)
		}
	}
	return out
}

// PermGroups 返回权限点目录(供 /perm/catalog 接口与前端渲染)。
func PermGroups() []PermGroup {
	out := make([]PermGroup, len(permGroups))
	copy(out, permGroups)
	return out
}

// AllPermCodes 返回全部权限码(按目录顺序,顺序稳定以便生成确定性脚本)。
func AllPermCodes() []string {
	out := make([]string, 0, len(permCodeIndex))
	for _, g := range permGroups {
		for _, p := range g.Perms {
			out = append(out, p.Code)
		}
	}
	return out
}

// IsPermCode 判断是否为已登记的权限码。
func IsPermCode(code string) bool {
	return permCodeIndex[code]
}

// PermName 返回权限码的中文名,未登记返回空串。
func PermName(code string) string {
	return permNameIndex[code]
}

// moduleOf 取权限码的模块前缀(无冒号返回空串)。
func moduleOf(code string) string {
	if i := strings.Index(code, ":"); i > 0 {
		return code[:i]
	}
	return ""
}

// UnknownPerms 返回列表中未登记的权限码(去重、排序),供写入校验报错用。
func UnknownPerms(codes []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, c := range codes {
		c = strings.TrimSpace(c)
		if c == "" || permCodeIndex[c] || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// NormalizePerms 归一化权限码集合:
//   - 剔除空串与未登记的权限码(静默丢弃,调用方如需报错请先跑 UnknownPerms);
//   - 去重;
//   - 递归补齐两条隐含规则:
//     ① 同模块:除 xxx:view 外,任意权限码自动包含同模块的 xxx:view
//     (order:settle -> order:view、table:edit -> table:view);
//     ② 跨模块:permImplies 声明的数据依赖(credit:view -> order:view)。
//     **必须迭代到不动点**:隐含链可以串起来(credit:settle -> credit:view
//     -> order:view),单趟补全会漏掉第二环。
//   - 按目录顺序输出,保证同一组权限的落库字符串永远一致(便于比对与测试)。
//
// 遍历用 AllPermCodes() 的固定顺序而非 range map,保证结果与 Go 的随机
// map 迭代顺序无关(否则落库字符串会飘,测试与比对都不稳)。
func NormalizePerms(codes []string) []string {
	set := map[string]bool{}
	for _, c := range codes {
		c = strings.TrimSpace(c)
		if permCodeIndex[c] {
			set[c] = true
		}
	}
	for {
		grew := false
		for _, c := range AllPermCodes() {
			if !set[c] {
				continue
			}
			if mod := moduleOf(c); mod != "" && c != mod+":view" {
				if v := mod + ":view"; permCodeIndex[v] && !set[v] {
					set[v] = true
					grew = true
				}
			}
			if v, ok := permImplies[c]; ok && !set[v] {
				set[v] = true
				grew = true
			}
		}
		if !grew {
			break
		}
	}
	out := make([]string, 0, len(set))
	for _, c := range AllPermCodes() {
		if set[c] {
			out = append(out, c)
		}
	}
	return out
}

// ParsePerms 把 CSV 字符串解析为归一化后的权限码列表。
func ParsePerms(csv string) []string {
	if strings.TrimSpace(csv) == "" {
		return []string{}
	}
	return NormalizePerms(strings.Split(csv, ","))
}

// JoinPerms 把权限码列表归一化后拼成 CSV(落库用)。
func JoinPerms(codes []string) string {
	return strings.Join(NormalizePerms(codes), ",")
}

// HasPermCode 判断权限码集合是否包含指定权限。
// 权限码本身已是归一化结果(edit 已补 view),这里只做直查。
func HasPermCode(perms []string, code string) bool {
	for _, p := range perms {
		if p == code {
			return true
		}
	}
	return false
}

// ============================================================================
// 内置角色矩阵
//
// 4 个开箱可用角色。商户可在此基础上新增自定义角色;
// 内置角色不可删除,其中 admin 的权限每次启动强制恢复为全量(防锁死的最后保险)。
// ============================================================================

// RoleKey 内置角色标识(供代码判断,不允许商户修改)。
const (
	RoleKeyAdmin   = "admin"   // 超级管理员:全权
	RoleKeyManager = "manager" // 店长:日常最高权限,不管员工与权限,不能发起退款
	RoleKeyCashier = "cashier" // 收银员:前厅收银
	RoleKeyStaff   = "staff"   // 员工:后厨/服务员通用,只读资料 + 订单流转
)

// builtinRoleSeed 内置角色定义(权限矩阵)。
//
// 权限用显式 CSV 而不是「排除法」计算:矩阵一眼可审,改动有迹可循。
// 仅 admin 用 AllPermCodes() —— 它必须永远等于全量,写死列表反而会漏。
type builtinRoleSeed struct {
	Key    string
	Name   string
	Perms  []string // 归一化前的原始列表;空表示全量(admin)
	Sort   int
	Remark string
}

var builtinRoleSeeds = []builtinRoleSeed{
	{
		Key: RoleKeyAdmin, Name: "超级管理员", Sort: 1,
		Remark: "拥有全部权限。权限不可修改,每次启动强制恢复为全量,防止锁死系统。",
	},
	{
		Key: RoleKeyManager, Name: "店长", Sort: 2,
		Perms: []string{
			"table:view", "table:edit",
			"category:view", "category:edit",
			"dish:view", "dish:edit",
			"remark:view", "remark:edit",
			"printer:view", "printer:edit",
			"order:view", "order:operate", "order:settle", "order:edit", "order:cancel",
			"credit:view", "credit:settle",
			"refund:view",
			"report:view",
			"config:view", "config:edit",
			"log:view",
		},
		Remark: "日常最高权限:可改配置、看报表、查操作日志,但不能管理员工与权限,也不能发起退款。",
	},
	{
		Key: RoleKeyCashier, Name: "收银员", Sort: 3,
		Perms: []string{
			"table:view",
			"category:view",
			"dish:view",
			"remark:view",
			"printer:view",
			"order:view", "order:operate", "order:settle", "order:edit", "order:cancel",
			"credit:view", "credit:settle",
			"refund:view",
		},
		Remark: "前厅收银:处理订单全流程与挂账核销,不涉及配置、报表与退款发起。",
	},
	{
		Key: RoleKeyStaff, Name: "员工", Sort: 4,
		Perms: []string{
			"table:view",
			"category:view",
			"dish:view",
			"remark:view",
			"order:view", "order:operate",
		},
		Remark: "后厨/服务员通用:只读基础资料,订单可流转(接单/上菜/完成/处理催菜)。",
	},
}

// BuiltinRoleSeeds 返回内置角色定义的副本(供启动引导与种子脚本生成使用)。
func BuiltinRoleSeeds() []builtinRoleSeed {
	out := make([]builtinRoleSeed, len(builtinRoleSeeds))
	copy(out, builtinRoleSeeds)
	return out
}

// BuiltinRolePerms 返回指定内置角色归一化后的权限 CSV;admin 返回全量。
func BuiltinRolePerms(key string) string {
	for _, s := range builtinRoleSeeds {
		if s.Key == key {
			if s.Key == RoleKeyAdmin {
				return JoinPerms(AllPermCodes())
			}
			return JoinPerms(s.Perms)
		}
	}
	return ""
}

// IsBuiltinRoleKey 判断是否为内置角色标识。
func IsBuiltinRoleKey(key string) bool {
	for _, s := range builtinRoleSeeds {
		if s.Key == key {
			return true
		}
	}
	return false
}
