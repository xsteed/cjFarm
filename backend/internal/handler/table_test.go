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

// tableInitDB 建临时库并跑完迁移/种子,供桌台 handler 测试使用。
func tableInitDB(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "table.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
}

// tableCall 直接调用一个桌台 handler。
func tableCall(t *testing.T, h gin.HandlerFunc, method, target, body string, params gin.Params) *httptest.ResponseRecorder {
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

// tableJSON 解析响应为 map。
func tableJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	out := map[string]interface{}{}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应不是合法 JSON(HTTP %d): %s", w.Code, w.Body.String())
	}
	return out
}

// tableData 取响应里的 data 对象。
func tableData(t *testing.T, resp map[string]interface{}) map[string]interface{} {
	t.Helper()
	d, ok := resp["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("响应缺少 data 对象: %v", resp)
	}
	return d
}

// tableInsert 直接插入一张桌台并返回 ID。
func tableInsert(t *testing.T, no, name string) int {
	t.Helper()
	id, err := dao.InsertTable(po.Table{TableNo: no, TableName: name, Capacity: 4})
	if err != nil {
		t.Fatalf("插入桌台失败: %v", err)
	}
	return int(id)
}

// TestTableList 桌台列表成功路径。
func TestTableList(t *testing.T) {
	tableInitDB(t)

	w := tableCall(t, TableList, http.MethodGet, "/api/admin/table/list", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("桌台列表应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	resp := tableJSON(t, w)
	data := tableData(t, resp)
	items, ok := data["items"].([]interface{})
	if !ok || len(items) < 1 {
		t.Fatalf("桌台列表 items 应非空, got %v", data["items"])
	}
	if data["total"].(float64) < 1 {
		t.Fatalf("桌台列表 total 应大于 0, got %v", data["total"])
	}
}

// TestTableListKeywordFilter 桌台列表按名称关键字过滤。
func TestTableListKeywordFilter(t *testing.T) {
	tableInitDB(t)
	tableInsert(t, "V1", "VIP Room")

	w := tableCall(t, TableList, http.MethodGet, "/api/admin/table/list?tableName=VIP", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("桌台列表应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	data := tableData(t, tableJSON(t, w))
	if data["total"].(float64) != 1 {
		t.Fatalf("按关键字过滤应命中 1 张桌, got %v", data["total"])
	}
}

// TestTableSave 新增桌台成功路径。
func TestTableSave(t *testing.T) {
	tableInitDB(t)

	w := tableCall(t, TableSave, http.MethodPost, "/api/admin/table/save",
		`{"tableNo":"99","tableName":"测试桌","capacity":6}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("新增桌台应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	data := tableData(t, tableJSON(t, w))
	if _, ok := data["tableId"]; !ok {
		t.Fatalf("新增桌台应返回 tableId, got %v", data)
	}
}

// TestTableSaveMissingFields 桌号或名称缺失时被拒绝。
func TestTableSaveMissingFields(t *testing.T) {
	tableInitDB(t)

	w := tableCall(t, TableSave, http.MethodPost, "/api/admin/table/save",
		`{"tableNo":"","tableName":"","capacity":4}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("缺少桌号/名称应返回 400, got %d body=%s", w.Code, w.Body.String())
	}
	resp := tableJSON(t, w)
	if !strings.Contains(resp["msg"].(string), "请填写桌号和桌台名称") {
		t.Fatalf("缺少桌号/名称应返回业务错误提示, got %v", resp)
	}
}

// TestTableUpdate 修改桌台成功路径。
func TestTableUpdate(t *testing.T) {
	tableInitDB(t)
	id := tableInsert(t, "T88", "待更新桌")

	w := tableCall(t, TableUpdate, http.MethodPost, "/api/admin/table/update",
		`{"tableId":`+strconv.Itoa(id)+`,"tableNo":"T88","tableName":"已更新桌","capacity":8}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("修改桌台应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	resp := tableJSON(t, w)
	if resp["msg"] != "修改成功" {
		t.Fatalf("修改桌台 msg 错误: %v", resp)
	}
}

// TestTableUpdateMissingID 修改桌台缺少 tableId 时返回参数错误。
func TestTableUpdateMissingID(t *testing.T) {
	tableInitDB(t)

	w := tableCall(t, TableUpdate, http.MethodPost, "/api/admin/table/update",
		`{"tableNo":"T1","tableName":"无ID桌"}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("缺少 tableId 应返回 400, got %d body=%s", w.Code, w.Body.String())
	}
	resp := tableJSON(t, w)
	if resp["msg"] != "参数错误" {
		t.Fatalf("缺少 tableId 应返回参数错误, got %v", resp)
	}
}

// TestTableDelete 删除桌台成功路径。
func TestTableDelete(t *testing.T) {
	tableInitDB(t)
	id := tableInsert(t, "T77", "待删除桌")

	w := tableCall(t, TableDelete, http.MethodDelete, "/api/admin/table/"+strconv.Itoa(id), "",
		gin.Params{{Key: "id", Value: strconv.Itoa(id)}})
	if w.Code != http.StatusOK {
		t.Fatalf("删除桌台应返回 200, got %d body=%s", w.Code, w.Body.String())
	}

	var delFlag string
	if err := store.DB.QueryRow(`SELECT del_flag FROM tb_table WHERE table_id=?`, id).Scan(&delFlag); err != nil {
		t.Fatalf("查询删除标记失败: %v", err)
	}
	if delFlag != "1" {
		t.Fatalf("桌台应被逻辑删除, del_flag=%q", delFlag)
	}
}

// TestTableDeleteInvalidID 非法路径参数返回无效 ID 错误。
func TestTableDeleteInvalidID(t *testing.T) {
	tableInitDB(t)

	w := tableCall(t, TableDelete, http.MethodDelete, "/api/admin/table/abc", "",
		gin.Params{{Key: "id", Value: "abc"}})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法 ID 应返回 400, got %d body=%s", w.Code, w.Body.String())
	}
	resp := tableJSON(t, w)
	if resp["msg"] != "无效的ID" {
		t.Fatalf("非法 ID 应返回无效的ID, got %v", resp)
	}
}
