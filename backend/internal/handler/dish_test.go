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

// dishInitDB 建临时库并跑完迁移/种子,供菜品 handler 测试使用。
func dishInitDB(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "dish.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
}

// dishCall 直接调用一个菜品 handler。
func dishCall(t *testing.T, h gin.HandlerFunc, method, target, body string, params gin.Params) *httptest.ResponseRecorder {
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

// dishJSON 解析响应为 map。
func dishJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	out := map[string]interface{}{}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应不是合法 JSON(HTTP %d): %s", w.Code, w.Body.String())
	}
	return out
}

// dishData 取响应里的 data 对象。
func dishData(t *testing.T, resp map[string]interface{}) map[string]interface{} {
	t.Helper()
	d, ok := resp["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("响应缺少 data 对象: %v", resp)
	}
	return d
}

// dishInsert 直接插入一道带规格的菜品并返回 ID。
func dishInsert(t *testing.T, name string) int {
	t.Helper()
	id, err := dao.CreateDish(po.Dish{CategoryID: 1, DishName: name, Status: 1},
		[]po.Spec{{SpecName: "份", Price: 2800}})
	if err != nil {
		t.Fatalf("插入菜品失败: %v", err)
	}
	return int(id)
}

// TestDishList 菜品列表成功路径。
func TestDishList(t *testing.T) {
	dishInitDB(t)

	w := dishCall(t, DishList, http.MethodGet, "/api/admin/dish/list", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("菜品列表应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	resp := dishJSON(t, w)
	data := dishData(t, resp)
	items, ok := data["items"].([]interface{})
	if !ok || len(items) < 1 {
		t.Fatalf("菜品列表 items 应非空, got %v", data["items"])
	}
	if data["total"].(float64) < 1 {
		t.Fatalf("菜品列表 total 应大于 0, got %v", data["total"])
	}
}

// TestDishListCategoryFilter 菜品列表按分类过滤。
func TestDishListCategoryFilter(t *testing.T) {
	dishInitDB(t)

	w := dishCall(t, DishList, http.MethodGet, "/api/admin/dish/list?categoryId=1", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("菜品列表应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	data := dishData(t, dishJSON(t, w))
	if data["total"].(float64) < 1 {
		t.Fatalf("分类 1 下应有菜品, got %v", data["total"])
	}
}

// TestDishGet 单道菜品查询成功路径(含规格)。
func TestDishGet(t *testing.T) {
	dishInitDB(t)

	w := dishCall(t, DishGet, http.MethodGet, "/api/admin/dish/1", "",
		gin.Params{{Key: "id", Value: "1"}})
	if w.Code != http.StatusOK {
		t.Fatalf("查询菜品应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	data := dishData(t, dishJSON(t, w))
	if data["dishName"] != "凉拌青瓜" {
		t.Fatalf("菜品名称错误: %v", data["dishName"])
	}
	specs, ok := data["specs"].([]interface{})
	if !ok || len(specs) < 1 {
		t.Fatalf("菜品应返回规格列表, got %v", data["specs"])
	}
}

// TestDishGetNotFound 查询不存在的菜品。
func TestDishGetNotFound(t *testing.T) {
	dishInitDB(t)

	w := dishCall(t, DishGet, http.MethodGet, "/api/admin/dish/99999", "",
		gin.Params{{Key: "id", Value: "99999"}})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("不存在的菜品应返回 400, got %d body=%s", w.Code, w.Body.String())
	}
	resp := dishJSON(t, w)
	if resp["msg"] != "菜品不存在" {
		t.Fatalf("不存在的菜品应返回菜品不存在, got %v", resp)
	}
}

// TestDishGetInvalidID 非法 ID 返回无效 ID 错误。
func TestDishGetInvalidID(t *testing.T) {
	dishInitDB(t)

	w := dishCall(t, DishGet, http.MethodGet, "/api/admin/dish/abc", "",
		gin.Params{{Key: "id", Value: "abc"}})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法 ID 应返回 400, got %d body=%s", w.Code, w.Body.String())
	}
	resp := dishJSON(t, w)
	if resp["msg"] != "无效的ID" {
		t.Fatalf("非法 ID 应返回无效的ID, got %v", resp)
	}
}

// TestDishSave 新增菜品成功路径(含规格)。
func TestDishSave(t *testing.T) {
	dishInitDB(t)

	w := dishCall(t, DishSave, http.MethodPost, "/api/admin/dish/save",
		`{"categoryId":1,"dishName":"测试菜品","status":1,"specs":[{"specName":"份","price":28}]}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("新增菜品应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	data := dishData(t, dishJSON(t, w))
	if _, ok := data["dishId"]; !ok {
		t.Fatalf("新增菜品应返回 dishId, got %v", data)
	}
}

// TestDishSaveMissingSpec 未添加规格时被拒绝。
func TestDishSaveMissingSpec(t *testing.T) {
	dishInitDB(t)

	w := dishCall(t, DishSave, http.MethodPost, "/api/admin/dish/save",
		`{"categoryId":1,"dishName":"无规格菜品","specs":[]}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("无规格菜品应返回 400, got %d body=%s", w.Code, w.Body.String())
	}
	resp := dishJSON(t, w)
	if !strings.Contains(resp["msg"].(string), "请至少添加一个规格") {
		t.Fatalf("无规格菜品应返回业务错误提示, got %v", resp)
	}
}

// TestDishUpdate 修改菜品成功路径。
func TestDishUpdate(t *testing.T) {
	dishInitDB(t)
	id := dishInsert(t, "待更新菜品")

	w := dishCall(t, DishUpdate, http.MethodPost, "/api/admin/dish/update",
		`{"dishId":`+strconv.Itoa(id)+`,"categoryId":1,"dishName":"已更新菜品","status":1,"specs":[{"specName":"大份","price":38}]}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("修改菜品应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	resp := dishJSON(t, w)
	if resp["msg"] != "修改成功" {
		t.Fatalf("修改菜品 msg 错误: %v", resp)
	}
}

// TestDishUpdateMissingID 修改菜品缺少 dishId 时返回参数错误。
func TestDishUpdateMissingID(t *testing.T) {
	dishInitDB(t)

	w := dishCall(t, DishUpdate, http.MethodPost, "/api/admin/dish/update",
		`{"categoryId":1,"dishName":"无ID菜品","specs":[{"specName":"份","price":28}]}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("缺少 dishId 应返回 400, got %d body=%s", w.Code, w.Body.String())
	}
	resp := dishJSON(t, w)
	if resp["msg"] != "参数错误" {
		t.Fatalf("缺少 dishId 应返回参数错误, got %v", resp)
	}
}

// TestDishDelete 删除菜品成功路径。
func TestDishDelete(t *testing.T) {
	dishInitDB(t)
	id := dishInsert(t, "待删除菜品")

	w := dishCall(t, DishDelete, http.MethodDelete, "/api/admin/dish/"+strconv.Itoa(id), "",
		gin.Params{{Key: "id", Value: strconv.Itoa(id)}})
	if w.Code != http.StatusOK {
		t.Fatalf("删除菜品应返回 200, got %d body=%s", w.Code, w.Body.String())
	}

	var delFlag string
	if err := store.DB.QueryRow(`SELECT del_flag FROM tb_dish WHERE dish_id=?`, id).Scan(&delFlag); err != nil {
		t.Fatalf("查询删除标记失败: %v", err)
	}
	if delFlag != "1" {
		t.Fatalf("菜品应被逻辑删除, del_flag=%q", delFlag)
	}
}

// TestDishDeleteInvalidID 非法路径参数返回无效 ID 错误。
func TestDishDeleteInvalidID(t *testing.T) {
	dishInitDB(t)

	w := dishCall(t, DishDelete, http.MethodDelete, "/api/admin/dish/abc", "",
		gin.Params{{Key: "id", Value: "abc"}})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法 ID 应返回 400, got %d body=%s", w.Code, w.Body.String())
	}
	resp := dishJSON(t, w)
	if resp["msg"] != "无效的ID" {
		t.Fatalf("非法 ID 应返回无效的ID, got %v", resp)
	}
}
