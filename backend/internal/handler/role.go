package handler

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"dining-system/internal/model"
	"dining-system/internal/store"
)

// ============ 角色与权限 ============
//
// 权限点:role:view(列表、权限目录) / role:edit(新增、修改、删除)。
// 权限点的权威定义在 store/permission.go,前端通过 /perm/catalog 拉取同一份。

func RoleList(c *gin.Context) {
	list, err := store.ListRoles()
	if err != nil {
		fail(c, err.Error())
		return
	}
	tableResult(c, len(list), list)
}

// PermCatalog 返回权限点目录(按模块分组)与全部内置角色的推荐权限,
// 供「角色管理」页渲染勾选框,避免前端硬编码权限点。
func PermCatalog(c *gin.Context) {
	ok(c, gin.H{
		"groups": store.PermGroups(),
		// implies 是「勾选 A 会连带授予 B」的跨模块依赖表(还有一条同模块规则:
		// 非 xxx:view 自动隐含同模块 xxx:view,这条前端自己就能推出来)。
		// 前端拿它做勾选提示 —— 管理员勾「挂账查看」时要知道会连带拿到「订单查看」。
		"implies": store.PermImplies(),
		"total":   len(store.AllPermCodes()),
	})
}

func RoleSave(c *gin.Context) {
	var p struct {
		RoleKey   string   `json:"roleKey"`
		RoleName  string   `json:"roleName"`
		PermList  []string `json:"permList"`
		SortOrder int      `json:"sortOrder"`
		Remark    string   `json:"remark"`
	}
	if err := c.ShouldBindJSON(&p); err != nil {
		fail(c, "参数错误")
		return
	}
	if strings.TrimSpace(p.RoleKey) == "" || strings.TrimSpace(p.RoleName) == "" {
		fail(c, "请填写角色标识与角色名称")
		return
	}
	if bad := store.UnknownPerms(p.PermList); len(bad) > 0 {
		fail(c, "存在无效的权限项:"+strings.Join(bad, "、"))
		return
	}
	id, err := store.InsertRole(model.Role{
		RoleKey:   strings.TrimSpace(p.RoleKey),
		RoleName:  strings.TrimSpace(p.RoleName),
		PermList:  p.PermList,
		SortOrder: p.SortOrder,
		Remark:    strings.TrimSpace(p.Remark),
	}, adminName(c))
	if err != nil {
		fail(c, err.Error())
		return
	}
	SetAuditDetail(c, "role", strconv.FormatInt(id, 10), "新增角色 "+strings.TrimSpace(p.RoleName)+"，权限："+permNames(p.PermList))
	ok(c, gin.H{"roleId": id})
}

func RoleUpdate(c *gin.Context) {
	var p struct {
		RoleID    int      `json:"roleId"`
		RoleKey   string   `json:"roleKey"`
		RoleName  string   `json:"roleName"`
		PermList  []string `json:"permList"`
		SortOrder int      `json:"sortOrder"`
		Remark    string   `json:"remark"`
	}
	if err := c.ShouldBindJSON(&p); err != nil || p.RoleID == 0 {
		fail(c, "参数错误")
		return
	}
	if strings.TrimSpace(p.RoleName) == "" {
		fail(c, "请填写角色名称")
		return
	}
	if bad := store.UnknownPerms(p.PermList); len(bad) > 0 {
		fail(c, "存在无效的权限项:"+strings.Join(bad, "、"))
		return
	}
	// 改权限是权限体系里最敏感的动作,摘要里写清「加了什么、去掉了什么」,
	// 而不是笼统一句「修改角色」——事后追责要的是差异,不是动作名。
	oldPerms := ""
	if old, err := store.GetRoleByID(p.RoleID); err == nil && old != nil {
		oldPerms = old.Perms
	}
	err := store.UpdateRole(model.Role{
		RoleID:    p.RoleID,
		RoleKey:   strings.TrimSpace(p.RoleKey),
		RoleName:  strings.TrimSpace(p.RoleName),
		PermList:  p.PermList,
		SortOrder: p.SortOrder,
		Remark:    strings.TrimSpace(p.Remark),
	}, adminName(c))
	if err != nil {
		fail(c, err.Error())
		return
	}
	SetAuditDetail(c, "role", strconv.Itoa(p.RoleID), "修改角色 "+strings.TrimSpace(p.RoleName)+"："+describePermChange(oldPerms, p.PermList))
	okMsg(c, "修改成功")
}

func RoleDelete(c *gin.Context) {
	id, valid := idParam(c)
	if !valid {
		return
	}
	if err := store.SoftDeleteRole(id, adminName(c)); err != nil {
		fail(c, err.Error())
		return
	}
	SetAuditDetail(c, "role", strconv.Itoa(id), "删除角色")
	okMsg(c, "删除成功")
}

// permNames 把权限码列表转成中文名串(摘要用;超长时截断,避免撑爆 detail 列)。
func permNames(codes []string) string {
	names := []string{}
	for _, c := range store.NormalizePerms(codes) {
		if n := store.PermName(c); n != "" {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return "无"
	}
	s := strings.Join(names, "、")
	if len([]rune(s)) > 200 {
		return string([]rune(s)[:200]) + "…"
	}
	return s
}

// describePermChange 描述权限差异:新增了哪些、去掉了哪些。
func describePermChange(oldCSV string, newCodes []string) string {
	oldSet, newSet := map[string]bool{}, map[string]bool{}
	for _, c := range store.ParsePerms(oldCSV) {
		oldSet[c] = true
	}
	for _, c := range store.NormalizePerms(newCodes) {
		newSet[c] = true
	}
	added, removed := []string{}, []string{}
	for _, c := range store.AllPermCodes() {
		switch {
		case newSet[c] && !oldSet[c]:
			added = append(added, store.PermName(c))
		case oldSet[c] && !newSet[c]:
			removed = append(removed, store.PermName(c))
		}
	}
	if len(added) == 0 && len(removed) == 0 {
		return "权限未变化"
	}
	parts := []string{}
	if len(added) > 0 {
		parts = append(parts, "新增权限 "+strings.Join(added, "、"))
	}
	if len(removed) > 0 {
		parts = append(parts, "移除权限 "+strings.Join(removed, "、"))
	}
	s := strings.Join(parts, "；")
	if len([]rune(s)) > 400 {
		return string([]rune(s)[:400]) + "…"
	}
	return s
}
