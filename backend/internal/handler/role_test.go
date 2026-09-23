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

// roleInitDB 建临时库并补齐内置角色/管理员,供角色 handler 测试使用。
func roleInitDB(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "role.db"))
	dao.BootstrapRoles()
	t.Cleanup(func() { _ = store.DB.Close() })
}

// roleCall 直接调用一个角色 handler。
func roleCall(t *testing.T, h gin.HandlerFunc, method, target, body string, params gin.Params) *httptest.ResponseRecorder {
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

// roleJSON 解析响应为 map。
func roleJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	out := map[string]interface{}{}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应不是合法 JSON(HTTP %d): %s", w.Code, w.Body.String())
	}
	return out
}

// roleData 取响应里的 data 对象。
func roleData(t *testing.T, resp map[string]interface{}) map[string]interface{} {
	t.Helper()
	d, ok := resp["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("响应缺少 data 对象: %v", resp)
	}
	return d
}

// roleInsert 直接插入一个自定义角色并返回 ID。
func roleInsert(t *testing.T, key, name string) int {
	t.Helper()
	id, err := dao.InsertRole(po.Role{RoleKey: key, RoleName: name, SortOrder: 9}, []string{"table:view"}, "tester")
	if err != nil {
		t.Fatalf("插入角色失败: %v", err)
	}
	return int(id)
}

// TestRoleList 角色列表成功路径(内置角色应全部返回)。
func TestRoleList(t *testing.T) {
	roleInitDB(t)

	w := roleCall(t, RoleList, http.MethodGet, "/api/admin/role/list", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("角色列表应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	data := roleData(t, roleJSON(t, w))
	items, ok := data["items"].([]interface{})
	if !ok || len(items) < 4 {
		t.Fatalf("角色列表 items 应至少含 4 个内置角色, got %v", data["items"])
	}
	if data["total"].(float64) < 4 {
		t.Fatalf("角色列表 total 应至少为 4, got %v", data["total"])
	}
}

// TestPermCatalog 权限目录成功路径。
func TestPermCatalog(t *testing.T) {
	roleInitDB(t)

	w := roleCall(t, PermCatalog, http.MethodGet, "/api/admin/perm/catalog", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("权限目录应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	data := roleData(t, roleJSON(t, w))
	groups, ok := data["groups"].([]interface{})
	if !ok || len(groups) < 1 {
		t.Fatalf("权限目录 groups 应非空, got %v", data["groups"])
	}
	if data["total"].(float64) < 1 {
		t.Fatalf("权限目录 total 应大于 0, got %v", data["total"])
	}
}

// TestRoleSave 新增自定义角色成功路径。
func TestRoleSave(t *testing.T) {
	roleInitDB(t)

	w := roleCall(t, RoleSave, http.MethodPost, "/api/admin/role/save",
		`{"roleKey":"custom1","roleName":"自定义角色","permList":["table:view"],"sortOrder":9,"remark":"测试"}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("新增角色应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	data := roleData(t, roleJSON(t, w))
	if _, ok := data["roleId"]; !ok {
		t.Fatalf("新增角色应返回 roleId, got %v", data)
	}
}

// TestRoleSaveMissingFields 缺少角色标识/名称时被拒绝。
func TestRoleSaveMissingFields(t *testing.T) {
	roleInitDB(t)

	w := roleCall(t, RoleSave, http.MethodPost, "/api/admin/role/save",
		`{"roleKey":"","roleName":"","permList":[],"sortOrder":1}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("缺少角色标识/名称应返回 400, got %d body=%s", w.Code, w.Body.String())
	}
	resp := roleJSON(t, w)
	if !strings.Contains(resp["msg"].(string), "请填写角色标识与角色名称") {
		t.Fatalf("缺少角色标识/名称应返回业务错误提示, got %v", resp)
	}
}

// TestRoleSaveInvalidPerm 非法权限码被拒绝。
func TestRoleSaveInvalidPerm(t *testing.T) {
	roleInitDB(t)

	w := roleCall(t, RoleSave, http.MethodPost, "/api/admin/role/save",
		`{"roleKey":"custom2","roleName":"非法权限角色","permList":["bad:perm"],"sortOrder":9}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法权限码应返回 400, got %d body=%s", w.Code, w.Body.String())
	}
	resp := roleJSON(t, w)
	if !strings.Contains(resp["msg"].(string), "存在无效的权限项") {
		t.Fatalf("非法权限码应返回业务错误提示, got %v", resp)
	}
}

// TestRoleUpdate 修改自定义角色成功路径。
func TestRoleUpdate(t *testing.T) {
	roleInitDB(t)
	id := roleInsert(t, "custom3", "待更新角色")

	w := roleCall(t, RoleUpdate, http.MethodPost, "/api/admin/role/update",
		`{"roleId":`+strconv.Itoa(id)+`,"roleKey":"custom3","roleName":"已更新角色","permList":["table:view","dish:view"],"sortOrder":10}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("修改角色应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	resp := roleJSON(t, w)
	if resp["msg"] != "修改成功" {
		t.Fatalf("修改角色 msg 错误: %v", resp)
	}
}

// TestRoleUpdateMissingID 修改角色缺少 roleId 时返回参数错误。
func TestRoleUpdateMissingID(t *testing.T) {
	roleInitDB(t)

	w := roleCall(t, RoleUpdate, http.MethodPost, "/api/admin/role/update",
		`{"roleKey":"custom","roleName":"无ID角色","permList":["table:view"]}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("缺少 roleId 应返回 400, got %d body=%s", w.Code, w.Body.String())
	}
	resp := roleJSON(t, w)
	if resp["msg"] != "参数错误" {
		t.Fatalf("缺少 roleId 应返回参数错误, got %v", resp)
	}
}

// TestRoleDelete 删除自定义角色成功路径。
func TestRoleDelete(t *testing.T) {
	roleInitDB(t)
	id := roleInsert(t, "custom4", "待删除角色")

	w := roleCall(t, RoleDelete, http.MethodDelete, "/api/admin/role/"+strconv.Itoa(id), "",
		gin.Params{{Key: "id", Value: strconv.Itoa(id)}})
	if w.Code != http.StatusOK {
		t.Fatalf("删除角色应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	resp := roleJSON(t, w)
	if resp["msg"] != "删除成功" {
		t.Fatalf("删除角色 msg 错误: %v", resp)
	}
}

// TestRoleDeleteBuiltin 内置角色不可删除。
func TestRoleDeleteBuiltin(t *testing.T) {
	roleInitDB(t)
	adminID := dao.RoleIDByKey(store.RoleKeyAdmin)

	w := roleCall(t, RoleDelete, http.MethodDelete, "/api/admin/role/"+strconv.Itoa(adminID), "",
		gin.Params{{Key: "id", Value: strconv.Itoa(adminID)}})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("删除内置角色应返回 400, got %d body=%s", w.Code, w.Body.String())
	}
	resp := roleJSON(t, w)
	if !strings.Contains(resp["msg"].(string), "不可删除") {
		t.Fatalf("删除内置角色应返回业务错误提示, got %v", resp)
	}
}

// TestRoleDeleteInvalidID 非法路径参数返回无效 ID 错误。
func TestRoleDeleteInvalidID(t *testing.T) {
	roleInitDB(t)

	w := roleCall(t, RoleDelete, http.MethodDelete, "/api/admin/role/abc", "",
		gin.Params{{Key: "id", Value: "abc"}})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法 ID 应返回 400, got %d body=%s", w.Code, w.Body.String())
	}
	resp := roleJSON(t, w)
	if resp["msg"] != "无效的ID" {
		t.Fatalf("非法 ID 应返回无效的ID, got %v", resp)
	}
}
