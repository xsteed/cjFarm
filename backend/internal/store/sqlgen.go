package store

import (
	"fmt"
	"sort"
	"strings"
)

// ============================================================================
// SQL 脚本生成(供 cmd/gensql 使用)
//
// 目的:让「后端建表逻辑」与「migrations 下的 SQL 脚本」共用同一份定义,
// 避免 schema.go / full 脚本 / incr 脚本三处手工维护、逐渐漂移。
//
// 生成的是「可独立执行」的完整脚本(DBA 审阅、离线建库、CI 校验均可直接用),
// 运行期并不读取这些文件 —— 后端始终用同一份 Go 定义建表。
// ============================================================================

// SchemaSQL 返回指定方言的完整建表 + 建索引语句(每条以分号结尾,可直接执行)。
func SchemaSQL(d Dialect) []string {
	raw := schemaStatements(d)
	out := make([]string, 0, len(raw)+len(indexDefs))
	for _, s := range raw {
		out = append(out, withSemicolon(s))
	}
	for _, ix := range indexDefs {
		stmt := indexStatement(d, ix)
		if d == DialectSQLite {
			// SQLite 支持 CREATE INDEX IF NOT EXISTS,脚本可安全重复执行;
			// MySQL 无对应写法,重复执行会报 "Duplicate key name",属正常现象。
			stmt = strings.Replace(stmt, "INDEX ", "INDEX IF NOT EXISTS ", 1)
		}
		out = append(out, withSemicolon(stmt))
	}
	return out
}

// withSemicolon 给语句补上结尾分号(脚本化执行必需;运行期逐条执行则不需要)。
func withSemicolon(s string) string {
	s = strings.TrimRight(s, " \t\n")
	if strings.HasSuffix(s, ";") {
		return s
	}
	return s + ";"
}

// SeedCounts 返回种子数据各表的行数,用于脚本头注释与文档,避免手工维护数字。
func SeedCounts() map[string]int {
	specs := 0
	for _, d := range seedDishRows {
		specs += len(d.specs)
	}
	return map[string]int{
		"setting":  len(settingDefaults),
		"role":     len(builtinRoleSeeds),
		"table":    len(seedTableRows),
		"category": len(seedCategoryRows),
		"dish":     len(seedDishRows),
		"spec":     specs,
		"remark":   len(seedRemarkRows),
		"printer":  len(seedPrinterRows),
	}
}

// SeedSQL 返回指定方言的种子数据插入语句(显式指定主键,可重复执行)。
//
// 之所以显式写主键而不依赖自增:脚本要可重复执行(冲突即忽略),
// 且菜品与规格之间存在父子关系,显式 ID 才能让规格精确挂到菜品上。
func SeedSQL(d Dialect) []string {
	out := []string{}

	// ---- 系统配置(主键是 cfg_key,按 key 排序保证生成的脚本稳定) ----
	keys := make([]string, 0, len(settingDefaults))
	for k := range settingDefaults {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out = append(out, fmt.Sprintf(
			"INSERT%s INTO tb_config(cfg_key, cfg_value) VALUES(%s, %s);%s",
			ignoreClause(d), sqlStr(k), sqlStr(settingDefaults[k]), commentIf(k == "wxpay_apiv3_key", "敏感项,落库前会自动加密")))
	}

	// ---- 桌台(固定桌台码,便于提前印制二维码) ----
	out = append(out, "")
	out = append(out, "-- 桌台:桌台码为固定值,保证「先出脚本、后印二维码」也能对应上;运行期新建的桌台仍使用随机码。")
	if len(seedTableRows) != len(seedTableCodes) {
		panic("seedTableRows 与 seedTableCodes 数量不一致")
	}
	for i, t := range seedTableRows {
		out = append(out, fmt.Sprintf(
			"INSERT%s INTO tb_table(table_id, table_no, table_name, capacity, status, sort_order, del_flag, table_code, create_time, update_time) VALUES(%d, %s, %s, %d, %d, %d, '0', %s, %s, %s);",
			ignoreClause(d), i+1, sqlStr(t.TableNo), sqlStr(t.TableName), t.Capacity, t.Status, t.SortOrder,
			sqlStr(seedTableCodes[i]), sqlStr(seedCreateTime), sqlStr(seedTableUpdTime)))
	}

	// ---- 分类 ----
	out = append(out, "")
	out = append(out, "-- 菜品分类")
	catID := map[string]int{}
	for i, c := range seedCategoryRows {
		catID[c.CategoryName] = i + 1
		out = append(out, fmt.Sprintf(
			"INSERT%s INTO tb_category(category_id, category_name, sort_order, del_flag, create_time, update_time) VALUES(%d, %s, %d, '0', %s, %s);",
			ignoreClause(d), i+1, sqlStr(c.CategoryName), c.SortOrder, sqlStr(seedSysCreateTime), sqlStr(seedSysCreateTime)))
	}

	// ---- 菜品 + 规格 ----
	out = append(out, "")
	out = append(out, "-- 菜品(21 道):category_id 与上面的分类按顺序一一对应。")
	dishID := 0
	for i, s := range seedDishRows {
		dishID = i + 1
		cid, ok := catID[s.cat]
		if !ok {
			panic("菜品引用了不存在的分类: " + s.cat)
		}
		out = append(out, fmt.Sprintf(
			"INSERT%s INTO tb_dish(dish_id, category_id, dish_name, dish_image, description, status, sort_order, del_flag, create_time, update_time) VALUES(%d, %d, %s, %s, %s, 1, %d, '0', %s, %s);",
			ignoreClause(d), dishID, cid, sqlStr(s.name), sqlStr(s.img), sqlStr(s.desc), dishID, sqlStr(seedSysCreateTime), sqlStr(seedSysCreateTime)))
	}

	out = append(out, "")
	out = append(out, "-- 菜品规格(32 条):price 单位为「分」(如 2800 = 28.00 元)。")
	specID := 0
	for i, s := range seedDishRows {
		for _, sp := range s.specs {
			specID++
			out = append(out, fmt.Sprintf(
				"INSERT%s INTO tb_spec(spec_id, dish_id, spec_name, price) VALUES(%d, %d, %s, %d);",
				ignoreClause(d), specID, i+1, sqlStr(sp.name), sp.price))
		}
	}

	// ---- 备注常用语 ----
	out = append(out, "")
	out = append(out, "-- 备注常用语")
	for i, r := range seedRemarkRows {
		out = append(out, fmt.Sprintf(
			"INSERT%s INTO tb_remark(remark_id, option_name, sort_order, del_flag, create_time, update_time) VALUES(%d, %s, %d, '0', %s, %s);",
			ignoreClause(d), i+1, sqlStr(r), i+1, sqlStr(seedSysCreateTime), sqlStr(seedSysCreateTime)))
	}

	// ---- 打印机 ----
	out = append(out, "")
	out = append(out, "-- 打印机(默认停用,录入真实 IP 后再启用)")
	for i, p := range seedPrinterRows {
		out = append(out, fmt.Sprintf(
			"INSERT%s INTO tb_printer(printer_id, printer_name, printer_type, ip, port, paper_width, status, del_flag, create_time, update_time) VALUES(%d, %s, %d, %s, %d, %d, 0, '0', %s, %s);",
			ignoreClause(d), i+1, sqlStr(p.Name), p.PType, sqlStr(p.IP), p.Port, p.Width, sqlStr(seedSysCreateTime), sqlStr(seedSysCreateTime)))
	}

	// ---- 内置角色(4 个) ----
	//
	// 权限矩阵定义在 internal/store/permission.go,此处按同一份定义生成,避免两边漂移。
	// 注意:不生成 tb_user 的插入语句 —— 管理员密码是 bcrypt(随机盐)哈希,
	// 无法在静态脚本里预置;账号由后端启动引导 EnsureAdminUser 自动创建。
	out = append(out, "")
	out = append(out, "-- 内置角色(4 个):admin 为全量权限,权限矩阵见 internal/store/permission.go")
	out = append(out, "-- 员工账号(tb_user)由后端启动引导自动创建,不在此脚本中预置。")
	for i, r := range BuiltinRoleSeeds() {
		out = append(out, fmt.Sprintf(
			"INSERT%s INTO tb_role(role_id, role_key, role_name, perms, data_scope, is_builtin, sort_order, status, del_flag, create_time, update_time, remark) VALUES(%d, %s, %s, %s, 'all', 1, %d, 1, '0', %s, %s, %s);",
			ignoreClause(d), i+1, sqlStr(r.Key), sqlStr(r.Name), sqlStr(BuiltinRolePerms(r.Key)),
			r.Sort, sqlStr(seedSysCreateTime), sqlStr(seedSysCreateTime), sqlStr(r.Remark)))
	}

	return out
}

// seedTableCodes 种子桌台的固定桌台码(仅用于生成的 SQL 脚本,运行期仍随机生成)。
// 字符集与 NewTableCode 保持一致(去掉易混的 0/O/1/I/L)。
var seedTableCodes = []string{
	"A3F7K9M2", "B4G8L2N3", "C5H9M3P4", "D6J2N4Q5",
	"E7K3P5R6", "F8L4Q6S7", "G9M5R7T8", "H2N6S8U9",
}

// ignoreClause 返回「冲突忽略」关键字:SQLite 为 OR IGNORE,MySQL 为空(由 INSERT IGNORE 承担)。
func ignoreClause(d Dialect) string {
	if d == DialectSQLite {
		return " OR IGNORE"
	}
	return " IGNORE"
}

// sqlStr 把 Go 字符串转成 SQL 单引号字面量(转义单引号)。
func sqlStr(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// commentIf 条件成立时返回行尾注释。
func commentIf(cond bool, text string) string {
	if cond {
		return "  -- " + text
	}
	return ""
}
