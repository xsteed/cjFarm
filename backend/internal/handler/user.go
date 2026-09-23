package handler

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"dining-system/internal/dto"
	"dining-system/internal/service"
)

// ============ 员工管理 ============
//
// 权限点:user:view(列表) / user:edit(新增、修改、重置密码、启停用、删除)。
// 业务规则与数据访问已下沉到 service(auth_service.go),handler 只做入参解析与出参组装。

func UserList(c *gin.Context) {
	pageNum, pageSize := pageParams(c)
	roleID, _ := strconv.Atoi(c.DefaultQuery("roleId", "0"))
	// status 不传或非法值 => nil(不限);传 0/1 才按状态过滤。
	var status *int
	if s := c.Query("status"); s != "" {
		if v, err := strconv.Atoi(s); err == nil {
			status = &v
		}
	}
	total, list, err := service.ListUsers(c.Query("keyword"), roleID, status, pageNum, pageSize)
	if err != nil {
		fail(c, err.Error())
		return
	}
	items := make([]dto.User, 0, len(list))
	for _, u := range list {
		items = append(items, dto.FromUser(u.User, u.RoleKey, u.RoleName, nil))
	}
	tableResult(c, total, items)
}

func UserSave(c *gin.Context) {
	var p struct {
		Username string `json:"username"`
		Password string `json:"password"`
		RealName string `json:"realName"`
		RoleID   int    `json:"roleId"`
		Phone    string `json:"phone"`
		Remark   string `json:"remark"`
	}
	if err := c.ShouldBindJSON(&p); err != nil {
		fail(c, "参数错误")
		return
	}
	username := strings.TrimSpace(p.Username)
	id, err := service.CreateUser(username, p.Password, p.RealName, p.RoleID, p.Phone, p.Remark, adminName(c))
	if err != nil {
		fail(c, err.Error())
		return
	}
	// 新增员工是权限体系的入口动作,必须能查到「谁建了谁、给了什么角色」。
	SetAuditDetail(c, "user", strconv.FormatInt(id, 10), "新增员工 "+username+"，角色 "+strconv.Itoa(p.RoleID))
	ok(c, gin.H{"userId": id})
}

func UserUpdate(c *gin.Context) {
	var p struct {
		UserID   int    `json:"userId"`
		RealName string `json:"realName"`
		RoleID   int    `json:"roleId"`
		Phone    string `json:"phone"`
		Remark   string `json:"remark"`
	}
	if err := c.ShouldBindJSON(&p); err != nil || p.UserID == 0 {
		fail(c, "参数错误")
		return
	}
	err := service.UpdateUser(p.UserID, p.RealName, p.RoleID, p.Phone, p.Remark, currentUID(c), adminName(c))
	if err != nil {
		fail(c, err.Error())
		return
	}
	SetAuditDetail(c, "user", strconv.Itoa(p.UserID), "修改员工资料，角色变更为 "+strconv.Itoa(p.RoleID))
	okMsg(c, "修改成功")
}

func UserResetPassword(c *gin.Context) {
	var p struct {
		UserID   int    `json:"userId"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&p); err != nil || p.UserID == 0 {
		fail(c, "参数错误")
		return
	}
	if err := service.ResetUserPassword(p.UserID, p.Password, adminName(c)); err != nil {
		fail(c, err.Error())
		return
	}
	// 新密码不落日志(请求体已脱敏),只留「谁重置了谁的密码」。
	SetAuditDetail(c, "user", strconv.Itoa(p.UserID), "重置员工密码")
	okMsg(c, "密码已重置，请通知该员工使用新密码登录")
}

func UserToggleStatus(c *gin.Context) {
	var p struct {
		UserID int `json:"userId"`
		Status int `json:"status"`
	}
	if err := c.ShouldBindJSON(&p); err != nil || p.UserID == 0 {
		fail(c, "参数错误")
		return
	}
	detail, msg, err := service.SetUserStatus(p.UserID, p.Status, currentUID(c), adminName(c))
	if err != nil {
		fail(c, err.Error())
		return
	}
	SetAuditDetail(c, "user", strconv.Itoa(p.UserID), detail)
	okMsg(c, msg)
}

func UserDelete(c *gin.Context) {
	id, valid := idParam(c)
	if !valid {
		return
	}
	if err := service.DeleteUser(id, currentUID(c), adminName(c)); err != nil {
		fail(c, err.Error())
		return
	}
	SetAuditDetail(c, "user", strconv.Itoa(id), "删除员工")
	okMsg(c, "删除成功")
}
