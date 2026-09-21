package handler

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"dining-system/internal/model"
	"dining-system/internal/store"
)

// ============ 员工管理 ============
//
// 权限点:user:view(列表) / user:edit(新增、修改、重置密码、启停用、删除)。
// 防锁死规则集中在 store 层(见 store/user.go),handler 只做入参校验与错误翻译。

// usernamePattern 用户名允许的字符集。禁止 '|' 是因为令牌载荷用它做字段分隔符。
var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_.@-]{2,32}$`)

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
	total, list, err := store.ListUsers(store.UserQuery{
		Keyword: strings.TrimSpace(c.Query("keyword")),
		RoleID:  roleID,
		Status:  status,
	}, pageNum, pageSize)
	if err != nil {
		fail(c, err.Error())
		return
	}
	tableResult(c, total, list)
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
	if !usernamePattern.MatchString(username) {
		fail(c, "用户名只能包含字母、数字、下划线、点、@ 或连字符，长度 2~32 位")
		return
	}
	if err := validatePassword(p.Password); err != nil {
		fail(c, err.Error())
		return
	}
	if err := validateRole(p.RoleID); err != nil {
		fail(c, err.Error())
		return
	}
	id, err := store.InsertUser(model.User{
		Username: username,
		RealName: strings.TrimSpace(p.RealName),
		RoleID:   p.RoleID,
		Phone:    strings.TrimSpace(p.Phone),
		Remark:   strings.TrimSpace(p.Remark),
	}, store.HashPassword(p.Password), adminName(c))
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
	if err := validateRole(p.RoleID); err != nil {
		fail(c, err.Error())
		return
	}
	err := store.UpdateUserProfile(model.User{
		UserID:   p.UserID,
		RealName: strings.TrimSpace(p.RealName),
		RoleID:   p.RoleID,
		Phone:    strings.TrimSpace(p.Phone),
		Remark:   strings.TrimSpace(p.Remark),
	}, currentUID(c), adminName(c))
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
	if err := validatePassword(p.Password); err != nil {
		fail(c, err.Error())
		return
	}
	if err := store.SetUserPassword(p.UserID, store.HashPassword(p.Password), adminName(c)); err != nil {
		fail(c, err.Error())
		return
	}
	// 密码被重置:作废该账号的全部「记住我」会话,强制用新密码重新登录。
	store.DeleteRememberTokensByUser(p.UserID)
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
	if err := store.SetUserStatus(p.UserID, p.Status, currentUID(c), adminName(c)); err != nil {
		fail(c, err.Error())
		return
	}
	action := "停用员工"
	if p.Status == model.UserStatusEnabled {
		action = "启用员工"
	}
	SetAuditDetail(c, "user", strconv.Itoa(p.UserID), action)
	msg := "已停用"
	if p.Status == model.UserStatusEnabled {
		msg = "已启用"
	}
	okMsg(c, msg)
}

func UserDelete(c *gin.Context) {
	id, valid := idParam(c)
	if !valid {
		return
	}
	if err := store.SoftDeleteUser(id, currentUID(c), adminName(c)); err != nil {
		fail(c, err.Error())
		return
	}
	SetAuditDetail(c, "user", strconv.Itoa(id), "删除员工")
	okMsg(c, "删除成功")
}

// validatePassword 密码长度校验(与修改密码保持一致)。
func validatePassword(pw string) error {
	if len(pw) < 6 {
		return &store.RuleError{Msg: "密码至少 6 位"}
	}
	if len(pw) > 64 {
		return &store.RuleError{Msg: "密码过长"}
	}
	return nil
}

// validateRole 校验角色存在且启用。
func validateRole(roleID int) error {
	if roleID <= 0 {
		return &store.RuleError{Msg: "请选择角色"}
	}
	r, err := store.GetRoleByID(roleID)
	if err != nil || r == nil {
		return &store.RuleError{Msg: "所选角色不存在"}
	}
	if r.Status != model.UserStatusEnabled {
		return &store.RuleError{Msg: "所选角色已停用"}
	}
	return nil
}
