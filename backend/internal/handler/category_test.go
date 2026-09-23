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

// catInitDB 建一个临时 SQLite 库并跑完迁移与种子数据,供分类 handler 测试使用。
func catInitDB(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "category.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
}

// catCall 以指定 method/target/body/路径参数直接调用一个 handler。
func catCall(t *testing.T, h gin.HandlerFunc, method, target, body string, params gin.Params) *httptest.ResponseRecorder {
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

// catJSON 解析 handler 响应为 map。
func catJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	out := map[string]interface{}{}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应不是合法 JSON(HTTP %d): %s", w.Code, w.Body.String())
	}
	return out
}

// catData 取响应里的 data 对象。
func catData(t *testing.T, resp map[string]interface{}) map[string]interface{} {
	t.Helper()
	d, ok := resp["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("响应缺少 data 对象: %v", resp)
	}
	return d
}

// catInsert 直接插入一条分类并返回 ID。
func catInsert(t *testing.T, name string, sort int) int {
	t.Helper()
	id, err := dao.InsertCategory(po.Category{CategoryName: name, SortOrder: sort})
	if err != nil {
		t.Fatalf("插入分类失败: %v", err)
	}
	return int(id)
}

// TestCategoryList 分类列表成功路径:种子分类应全部返回。
func TestCategoryList(t *testing.T) {
	catInitDB(t)

	w := catCall(t, CategoryList, http.MethodGet, "/api/admin/category/list", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("分类列表应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	resp := catJSON(t, w)
	if resp["code"] != float64(200) {
		t.Fatalf("响应 code 应为 200, got %v", resp)
	}
	data := catData(t, resp)
	items, ok := data["items"].([]interface{})
	if !ok || len(items) < 1 {
		t.Fatalf("分类列表 items 应非空, got %v", data["items"])
	}
	if data["total"].(float64) < 1 {
		t.Fatalf("分类列表 total 应大于 0, got %v", data["total"])
	}
}

// TestCategorySave 新增分类成功路径。
func TestCategorySave(t *testing.T) {
	catInitDB(t)

	w := catCall(t, CategorySave, http.MethodPost, "/api/admin/category/save",
		`{"categoryName":"测试分类","sortOrder":88}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("新增分类应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	data := catData(t, catJSON(t, w))
	if _, ok := data["categoryId"]; !ok {
		t.Fatalf("新增分类应返回 categoryId, got %v", data)
	}

	// 能经列表接口读回。
	w2 := catCall(t, CategoryList, http.MethodGet, "/api/admin/category/list", "", nil)
	data2 := catData(t, catJSON(t, w2))
	items := data2["items"].([]interface{})
	found := false
	for _, it := range items {
		m := it.(map[string]interface{})
		if m["categoryName"] == "测试分类" {
			found = true
		}
	}
	if !found {
		t.Fatalf("新增的分类应出现在列表中: %v", items)
	}
}

// TestCategorySaveEmptyName 分类名为空时被 service 校验拒绝。
func TestCategorySaveEmptyName(t *testing.T) {
	catInitDB(t)

	w := catCall(t, CategorySave, http.MethodPost, "/api/admin/category/save",
		`{"categoryName":"","sortOrder":1}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("空分类名应返回 400, got %d body=%s", w.Code, w.Body.String())
	}
	resp := catJSON(t, w)
	if resp["code"] != float64(400) || !strings.Contains(resp["msg"].(string), "请填写分类名称") {
		t.Fatalf("空分类名应返回业务错误提示, got %v", resp)
	}
}

// TestCategoryUpdate 修改分类成功路径。
func TestCategoryUpdate(t *testing.T) {
	catInitDB(t)
	id := catInsert(t, "待更新分类", 10)

	w := catCall(t, CategoryUpdate, http.MethodPost, "/api/admin/category/update",
		`{"categoryId":`+strconv.Itoa(id)+`,"categoryName":"已更新分类","sortOrder":11}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("修改分类应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	resp := catJSON(t, w)
	if resp["msg"] != "修改成功" {
		t.Fatalf("修改分类 msg 错误: %v", resp)
	}
}

// TestCategoryUpdateMissingID 修改分类缺少 categoryId 时返回参数错误。
func TestCategoryUpdateMissingID(t *testing.T) {
	catInitDB(t)

	w := catCall(t, CategoryUpdate, http.MethodPost, "/api/admin/category/update",
		`{"categoryName":"无ID分类"}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("缺少 categoryId 应返回 400, got %d body=%s", w.Code, w.Body.String())
	}
	resp := catJSON(t, w)
	if resp["msg"] != "参数错误" {
		t.Fatalf("缺少 categoryId 应返回参数错误, got %v", resp)
	}
}

// TestCategoryDelete 删除分类成功路径(逻辑删除)。
func TestCategoryDelete(t *testing.T) {
	catInitDB(t)
	id := catInsert(t, "待删除分类", 20)

	w := catCall(t, CategoryDelete, http.MethodDelete, "/api/admin/category/"+strconv.Itoa(id), "",
		gin.Params{{Key: "id", Value: strconv.Itoa(id)}})
	if w.Code != http.StatusOK {
		t.Fatalf("删除分类应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	resp := catJSON(t, w)
	if resp["msg"] != "删除成功" {
		t.Fatalf("删除分类 msg 错误: %v", resp)
	}

	var delFlag string
	if err := store.DB.QueryRow(`SELECT del_flag FROM tb_category WHERE category_id=?`, id).Scan(&delFlag); err != nil {
		t.Fatalf("查询删除标记失败: %v", err)
	}
	if delFlag != "1" {
		t.Fatalf("分类应被逻辑删除, del_flag=%q", delFlag)
	}
}

// TestCategoryDeleteInvalidID 非法路径参数返回无效 ID 错误。
func TestCategoryDeleteInvalidID(t *testing.T) {
	catInitDB(t)

	w := catCall(t, CategoryDelete, http.MethodDelete, "/api/admin/category/abc", "",
		gin.Params{{Key: "id", Value: "abc"}})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法 ID 应返回 400, got %d body=%s", w.Code, w.Body.String())
	}
	resp := catJSON(t, w)
	if resp["msg"] != "无效的ID" {
		t.Fatalf("非法 ID 应返回无效的ID, got %v", resp)
	}
}
