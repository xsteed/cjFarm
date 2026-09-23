package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"dining-system/internal/po"
	"dining-system/internal/store"
	"dining-system/internal/store/dao"
)

// userInitDB 建临时库并补齐内置角色/管理员,供员工 handler 测试使用。
func userInitDB(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "user.db"))
	dao.BootstrapRoles()
	t.Cleanup(func() { _ = store.DB.Close() })
}

// userCall 直接调用一个员工 handler。
func userCall(t *testing.T, h gin.HandlerFunc, method, target, body string, params gin.Params) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, target, nil)
	} else {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	c.Request = req
	if params != nil {
		c.Params = params
	}
	h(c)
	return w
}

// userCallAuth 以指定登录者身份调用员工 handler(供需要 currentUID/adminName 的接口使用)。
func userCallAuth(t *testing.T, h gin.HandlerFunc, method, target, body string, params gin.Params, uid int) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, target, nil)
	} else {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	c.Request = req
	if params != nil {
		c.Params = params
	}
	userSetAuth(c, uid)
	h(c)
	return w
}

// userSetAuth 注入管理员身份快照。
func userSetAuth(c *gin.Context, uid int) {
	setAuth(c, &dao.AuthInfo{
		UserID: uid, Username: "admin", RealName: "超级管理员",
		RoleID: dao.RoleIDByKey(store.RoleKeyAdmin), RoleKey: store.RoleKeyAdmin, RoleName: "超级管理员",
		Status: po.UserStatusEnabled, Perms: []string{"user:view", "user:edit"},
	})
}

// userJSON 解析响应为 map。
func userJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	out := map[string]interface{}{}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应不是合法 JSON(HTTP %d): %s", w.Code, w.Body.String())
	}
	return out
}

// userData 取响应里的 data 对象。
func userData(t *testing.T, resp map[string]interface{}) map[string]interface{} {
	t.Helper()
	d, ok := resp["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("响应缺少 data 对象: %v", resp)
	}
	return d
}

// userAdminID 返回引导创建的超级管理员账号 ID。
func userAdminID(t *testing.T) int {
	t.Helper()
	u, err := dao.GetUserByUsername("admin")
	if err != nil || u == nil {
		t.Fatalf("未找到超级管理员账号: %v", err)
	}
	return u.UserID
}

// userInsert 直接插入一个指定角色的员工并返回 ID。
func userInsert(t *testing.T, username string, roleID int) int {
	t.Helper()
	id, err := dao.InsertUser(po.User{Username: username, RealName: username, RoleID: roleID},
		store.HashPassword("secret1"), "tester")
	if err != nil {
		t.Fatalf("插入员工失败: %v", err)
	}
	return int(id)
}

// TestUserList 员工列表成功路径。
func TestUserList(t *testing.T) {
	userInitDB(t)

	w := userCall(t, UserList, http.MethodGet, "/api/admin/user/list", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("员工列表应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	data := userData(t, userJSON(t, w))
	items, ok := data["items"].([]interface{})
	if !ok || len(items) < 1 {
		t.Fatalf("员工列表 items 应非空, got %v", data["items"])
	}
	if data["total"].(float64) < 1 {
		t.Fatalf("员工列表 total 应大于 0, got %v", data["total"])
	}
}

// TestUserListStatusFilter 员工列表按状态过滤。
func TestUserListStatusFilter(t *testing.T) {
	userInitDB(t)
	roleID := dao.RoleIDByKey(store.RoleKeyCashier)
	userInsert(t, "disabled_user", roleID)
	if _, err := store.DB.Exec(`UPDATE tb_user SET status=0 WHERE username='disabled_user'`); err != nil {
		t.Fatalf("停用测试员工失败: %v", err)
	}

	w := userCall(t, UserList, http.MethodGet, "/api/admin/user/list?status=0", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("员工列表应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	data := userData(t, userJSON(t, w))
	if data["total"].(float64) != 1 {
		t.Fatalf("status=0 应只命中 1 名停用员工, got %v", data["total"])
	}
}

// TestUserSave 新增员工成功路径。
func TestUserSave(t *testing.T) {
	userInitDB(t)
	roleID := dao.RoleIDByKey(store.RoleKeyCashier)

	w := userCall(t, UserSave, http.MethodPost, "/api/admin/user/save",
		`{"username":"zhangsan","password":"secret1","realName":"张三","roleId":`+strconv.Itoa(roleID)+`}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("新增员工应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	data := userData(t, userJSON(t, w))
	if _, ok := data["userId"]; !ok {
		t.Fatalf("新增员工应返回 userId, got %v", data)
	}
}

// TestUserSaveBadUsername 非法用户名被拒绝。
func TestUserSaveBadUsername(t *testing.T) {
	userInitDB(t)
	roleID := dao.RoleIDByKey(store.RoleKeyCashier)

	w := userCall(t, UserSave, http.MethodPost, "/api/admin/user/save",
		`{"username":"a","password":"secret1","roleId":`+strconv.Itoa(roleID)+`}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法用户名应返回 400, got %d body=%s", w.Code, w.Body.String())
	}
	resp := userJSON(t, w)
	if !strings.Contains(resp["msg"].(string), "用户名只能包含") {
		t.Fatalf("非法用户名应返回业务错误提示, got %v", resp)
	}
}

// TestUserSaveBadPassword 过短密码被拒绝。
func TestUserSaveBadPassword(t *testing.T) {
	userInitDB(t)
	roleID := dao.RoleIDByKey(store.RoleKeyCashier)

	w := userCall(t, UserSave, http.MethodPost, "/api/admin/user/save",
		`{"username":"lisi","password":"123","roleId":`+strconv.Itoa(roleID)+`}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("过短密码应返回 400, got %d body=%s", w.Code, w.Body.String())
	}
	resp := userJSON(t, w)
	if !strings.Contains(resp["msg"].(string), "密码至少 6 位") {
		t.Fatalf("过短密码应返回业务错误提示, got %v", resp)
	}
}

// TestUserSaveMissingRole 未选择角色被拒绝。
func TestUserSaveMissingRole(t *testing.T) {
	userInitDB(t)

	w := userCall(t, UserSave, http.MethodPost, "/api/admin/user/save",
		`{"username":"wangwu","password":"secret1","roleId":0}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("未选择角色应返回 400, got %d body=%s", w.Code, w.Body.String())
	}
	resp := userJSON(t, w)
	if !strings.Contains(resp["msg"].(string), "请选择角色") {
		t.Fatalf("未选择角色应返回业务错误提示, got %v", resp)
	}
}

// TestUserUpdate 修改员工资料成功路径。
func TestUserUpdate(t *testing.T) {
	userInitDB(t)
	adminID := userAdminID(t)
	cashierID := dao.RoleIDByKey(store.RoleKeyCashier)
	id := userInsert(t, "update_user", cashierID)

	w := userCallAuth(t, UserUpdate, http.MethodPost, "/api/admin/user/update",
		`{"userId":`+strconv.Itoa(id)+`,"realName":"更新姓名","roleId":`+strconv.Itoa(cashierID)+`,"phone":"13800000000","remark":"改备注"}`,
		nil, adminID)
	if w.Code != http.StatusOK {
		t.Fatalf("修改员工应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	resp := userJSON(t, w)
	if resp["msg"] != "修改成功" {
		t.Fatalf("修改员工 msg 错误: %v", resp)
	}
}

// TestUserUpdateMissingID 修改员工缺少 userId 时返回参数错误。
func TestUserUpdateMissingID(t *testing.T) {
	userInitDB(t)

	w := userCall(t, UserUpdate, http.MethodPost, "/api/admin/user/update",
		`{"realName":"无ID","roleId":1}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("缺少 userId 应返回 400, got %d body=%s", w.Code, w.Body.String())
	}
	resp := userJSON(t, w)
	if resp["msg"] != "参数错误" {
		t.Fatalf("缺少 userId 应返回参数错误, got %v", resp)
	}
}

// TestUserResetPassword 重置密码成功路径。
func TestUserResetPassword(t *testing.T) {
	userInitDB(t)
	adminID := userAdminID(t)
	cashierID := dao.RoleIDByKey(store.RoleKeyCashier)
	id := userInsert(t, "reset_user", cashierID)

	w := userCallAuth(t, UserResetPassword, http.MethodPost, "/api/admin/user/resetPassword",
		`{"userId":`+strconv.Itoa(id)+`,"password":"newpass1"}`, nil, adminID)
	if w.Code != http.StatusOK {
		t.Fatalf("重置密码应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	resp := userJSON(t, w)
	if !strings.Contains(resp["msg"].(string), "密码已重置") {
		t.Fatalf("重置密码 msg 错误: %v", resp)
	}
}

// TestUserResetPasswordMissingID 重置密码缺少 userId 时返回参数错误。
func TestUserResetPasswordMissingID(t *testing.T) {
	userInitDB(t)

	w := userCall(t, UserResetPassword, http.MethodPost, "/api/admin/user/resetPassword",
		`{"password":"newpass1"}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("缺少 userId 应返回 400, got %d body=%s", w.Code, w.Body.String())
	}
	resp := userJSON(t, w)
	if resp["msg"] != "参数错误" {
		t.Fatalf("缺少 userId 应返回参数错误, got %v", resp)
	}
}

// TestUserToggleStatus 停用/启用员工成功路径。
func TestUserToggleStatus(t *testing.T) {
	userInitDB(t)
	adminID := userAdminID(t)
	cashierID := dao.RoleIDByKey(store.RoleKeyCashier)
	id := userInsert(t, "toggle_user", cashierID)

	w := userCallAuth(t, UserToggleStatus, http.MethodPost, "/api/admin/user/toggleStatus",
		`{"userId":`+strconv.Itoa(id)+`,"status":0}`, nil, adminID)
	if w.Code != http.StatusOK {
		t.Fatalf("停用员工应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	resp := userJSON(t, w)
	if resp["msg"] != "已停用" {
		t.Fatalf("停用员工 msg 错误: %v", resp)
	}
}

// TestUserToggleStatusSelfDisable 不能停用当前登录账号。
func TestUserToggleStatusSelfDisable(t *testing.T) {
	userInitDB(t)
	adminID := userAdminID(t)

	w := userCallAuth(t, UserToggleStatus, http.MethodPost, "/api/admin/user/toggleStatus",
		`{"userId":`+strconv.Itoa(adminID)+`,"status":0}`, nil, adminID)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("停用自己应返回 400, got %d body=%s", w.Code, w.Body.String())
	}
	resp := userJSON(t, w)
	if !strings.Contains(resp["msg"].(string), "不能停用当前登录账号") {
		t.Fatalf("停用自己应返回业务错误提示, got %v", resp)
	}
}

// TestUserDelete 删除员工成功路径。
func TestUserDelete(t *testing.T) {
	userInitDB(t)
	adminID := userAdminID(t)
	cashierID := dao.RoleIDByKey(store.RoleKeyCashier)
	id := userInsert(t, "delete_user", cashierID)

	w := userCallAuth(t, UserDelete, http.MethodDelete, "/api/admin/user/"+strconv.Itoa(id), "",
		gin.Params{{Key: "id", Value: strconv.Itoa(id)}}, adminID)
	if w.Code != http.StatusOK {
		t.Fatalf("删除员工应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	resp := userJSON(t, w)
	if resp["msg"] != "删除成功" {
		t.Fatalf("删除员工 msg 错误: %v", resp)
	}
}

// TestUserDeleteSelf 不能删除当前登录账号。
func TestUserDeleteSelf(t *testing.T) {
	userInitDB(t)
	adminID := userAdminID(t)

	w := userCallAuth(t, UserDelete, http.MethodDelete, "/api/admin/user/"+strconv.Itoa(adminID), "",
		gin.Params{{Key: "id", Value: strconv.Itoa(adminID)}}, adminID)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("删除自己应返回 400, got %d body=%s", w.Code, w.Body.String())
	}
	resp := userJSON(t, w)
	if !strings.Contains(resp["msg"].(string), "不能删除当前登录账号") {
		t.Fatalf("删除自己应返回业务错误提示, got %v", resp)
	}
}

// TestUserDeleteInvalidID 非法路径参数返回无效 ID 错误。
func TestUserDeleteInvalidID(t *testing.T) {
	userInitDB(t)

	w := userCall(t, UserDelete, http.MethodDelete, "/api/admin/user/abc", "",
		gin.Params{{Key: "id", Value: "abc"}})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法 ID 应返回 400, got %d body=%s", w.Code, w.Body.String())
	}
	resp := userJSON(t, w)
	if resp["msg"] != "无效的ID" {
		t.Fatalf("非法 ID 应返回无效的ID, got %v", resp)
	}
}
