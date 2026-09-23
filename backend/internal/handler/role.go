package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"dining-system/internal/dto"
	"dining-system/internal/service"
)

// ============ 角色与权限 ============
//
// 权限点:role:view(列表、权限目录) / role:edit(新增、修改、删除)。
// 权限点的权威定义在 store/permission.go,前端通过 /perm/catalog 拉取同一份;
// 角色业务规则已下沉到 service(auth_service.go),handler 只做入参解析与出参组装。

func RoleList(c *gin.Context) {
	list, err := service.ListRoles()
	if err != nil {
		fail(c, err.Error())
		return
	}
	items := make([]dto.Role, 0, len(list))
	for _, r := range list {
		items = append(items, dto.FromRole(r.Role, r.PermList, r.UserCount))
	}
	tableResult(c, len(items), items)
}

// PermCatalog 返回权限点目录(按模块分组)与全部内置角色的推荐权限,
// 供「角色管理」页渲染勾选框,避免前端硬编码权限点。
func PermCatalog(c *gin.Context) {
	ok(c, gin.H{
		"groups": service.PermGroups(),
		// implies 是「勾选 A 会连带授予 B」的跨模块依赖表(还有一条同模块规则:
		// 非 xxx:view 自动隐含同模块 xxx:view,这条前端自己就能推出来)。
		// 前端拿它做勾选提示 —— 管理员勾「挂账查看」时要知道会连带拿到「订单查看」。
		"implies": service.PermImplies(),
		"total":   len(service.AllPermCodes()),
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
	id, detail, err := service.SaveRole(p.RoleKey, p.RoleName, p.PermList, p.SortOrder, p.Remark, adminName(c))
	if err != nil {
		fail(c, err.Error())
		return
	}
	SetAuditDetail(c, "role", strconv.FormatInt(id, 10), detail)
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
	detail, err := service.UpdateRole(p.RoleID, p.RoleKey, p.RoleName, p.PermList, p.SortOrder, p.Remark, adminName(c))
	if err != nil {
		fail(c, err.Error())
		return
	}
	SetAuditDetail(c, "role", strconv.Itoa(p.RoleID), detail)
	okMsg(c, "修改成功")
}

func RoleDelete(c *gin.Context) {
	id, valid := idParam(c)
	if !valid {
		return
	}
	if err := service.DeleteRole(id, adminName(c)); err != nil {
		fail(c, err.Error())
		return
	}
	SetAuditDetail(c, "role", strconv.Itoa(id), "删除角色")
	okMsg(c, "删除成功")
}
