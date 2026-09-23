package dao

import (
	"strings"

	"dining-system/infra"
	"dining-system/infra/logger"
	"dining-system/internal/conf"
	"dining-system/internal/po"
	"dining-system/internal/store"
)

// BootstrapRoles 依次执行员工与权限体系的启动引导(幂等,每次启动调用)。
//
// 顺序不可颠倒:SyncBuiltinRoles 先保证角色存在(含把 admin 权限恢复为全量),
// EnsureAdminUser 才能把管理员账号挂到 admin 角色上;最后 NormalizeRolePerms
// 归一化存量角色的脏权限。
func BootstrapRoles() {
	SyncBuiltinRoles()
	EnsureAdminUser()
	NormalizeRolePerms()
}

// SyncBuiltinRoles 补齐内置角色。
//
// 策略:已存在的角色保持商户改过的权限不动(INSERT OR IGNORE 语义),
// 唯一例外是 admin —— 它的权限每次启动强制恢复为全量,防止有人误改/误清后
// 把自己锁在系统外面。这是防锁死的最后一道保险,不可省略。
func SyncBuiltinRoles() {
	stmt := store.InsertIgnoreInto("tb_role",
		"role_key", "role_name", "perms", "data_scope", "is_builtin", "sort_order", "status", "del_flag", "create_time", "update_time", "remark")
	now := store.Now()
	for _, s := range store.BuiltinRoleSeeds() {
		perms := store.BuiltinRolePerms(s.Key)
		if _, err := store.DB.Exec(stmt, s.Key, s.Name, perms, "all", 1, s.Sort, po.UserStatusEnabled, "0", now, now, s.Remark); err != nil {
			logger.Warnf("[role] 初始化内置角色 %s 失败: %v", s.Key, err)
		}
	}
	// admin 权限强制恢复为全量(幂等,每次都写;仅在值不一致时真正变更)。
	if id := RoleIDByKey(store.RoleKeyAdmin); id > 0 {
		full := store.JoinPerms(store.AllPermCodes())
		var cur string
		store.DB.QueryRow(`SELECT COALESCE(perms,'') FROM tb_role WHERE role_id=?`, id).Scan(&cur)
		if cur != full {
			if _, err := store.DB.Exec(`UPDATE tb_role SET perms=?, is_builtin=1, update_time=? WHERE role_id=?`, full, now, id); err == nil {
				logger.Infof("[role] 超级管理员角色权限已恢复为全量(%d 项)", len(store.AllPermCodes()))
			}
		}
	}
}

// NormalizeRolePerms 归一化存量角色的权限码:剔除已下线的权限、补齐隐含的 view。
// 用于老库升级后清理脏数据(幂等,无变更时不写库)。
func NormalizeRolePerms() {
	rows, err := store.DB.Query(`SELECT role_id, COALESCE(perms,'') FROM tb_role WHERE del_flag='0'`)
	if err != nil {
		return
	}
	type pair struct {
		id  int
		old string
	}
	items := []pair{}
	for rows.Next() {
		var p pair
		if rows.Scan(&p.id, &p.old) == nil {
			items = append(items, p)
		}
	}
	rows.Close()
	for _, p := range items {
		if fixed := store.JoinPerms(store.ParsePerms(p.old)); fixed != p.old {
			if _, err := store.DB.Exec(`UPDATE tb_role SET perms=?, update_time=? WHERE role_id=?`, fixed, store.Now(), p.id); err == nil {
				logger.Infof("[role] 角色 %d 权限已归一化: %q -> %q", p.id, p.old, fixed)
			}
		}
	}
}

// EnsureAdminUser 保证系统里始终有一个可用的超级管理员。
//
// 触发条件:启用中的超级管理员数量为 0。此时用 ADMIN_USER / ADMIN_PASS 配置
// 生成(或恢复)一个超级管理员账号,让商户永远能进系统。
// 已有同名账号时只恢复其角色与启用状态,不覆盖密码 —— 避免把商户改过的密码重置掉。
func EnsureAdminUser() {
	if CountActiveAdmins() > 0 {
		return
	}
	adminRoleID := RoleIDByKey(store.RoleKeyAdmin)
	if adminRoleID == 0 {
		logger.Warnf("[user] 超级管理员角色不存在,跳过管理员账号引导")
		return
	}

	username := strings.TrimSpace(infra.Getenv(conf.EnvAdminUser, conf.DefaultAdminUser))
	if username == "" {
		username = conf.DefaultAdminUser
	}
	now := store.Now()

	// 已有同名账号(可能被停用或改了角色):恢复它,保留其密码。
	if u, err := GetUserByUsername(username); err == nil && u != nil {
		if _, err := store.DB.Exec(`UPDATE tb_user SET status=?, role_id=?, del_flag='0', update_time=?
			WHERE user_id=?`, po.UserStatusEnabled, adminRoleID, now, u.UserID); err == nil {
			logger.Infof("[user] 已恢复超级管理员账号 %s(角色与启用状态)", username)
		}
		return
	}

	// 全新创建:密码优先取库中已配置的 admin_pass_hash(老版本改过密码的商户),
	// 否则用 ADMIN_PASS 环境变量,再否则用默认密码。
	hash := GetSetting("admin_pass_hash")
	if hash == "" {
		hash = store.HashPassword(infra.Getenv(conf.EnvAdminPass, conf.DefaultAdminPass))
	}
	id, err := InsertUser(po.User{
		Username: username,
		RealName: "超级管理员",
		RoleID:   adminRoleID,
		Remark:   "由启动引导自动创建",
	}, hash, "system")
	if err != nil {
		logger.Warnf("[user] 创建超级管理员账号失败: %v", err)
		return
	}
	logger.Infof("[user] 已创建超级管理员账号 %s(id=%d),请尽快修改初始密码", username, id)
}
