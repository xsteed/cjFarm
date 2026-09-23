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

// remarkInitDB 建临时库并跑完迁移/种子,供备注 handler 测试使用。
func remarkInitDB(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "remark.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
}

// remarkCall 直接调用一个备注 handler。
func remarkCall(t *testing.T, h gin.HandlerFunc, method, target, body string, params gin.Params) *httptest.ResponseRecorder {
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

// remarkJSON 解析响应为 map。
func remarkJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	out := map[string]interface{}{}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应不是合法 JSON(HTTP %d): %s", w.Code, w.Body.String())
	}
	return out
}

// remarkData 取响应里的 data 对象。
func remarkData(t *testing.T, resp map[string]interface{}) map[string]interface{} {
	t.Helper()
	d, ok := resp["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("响应缺少 data 对象: %v", resp)
	}
	return d
}

// remarkInsert 直接插入一条备注并返回 ID。
func remarkInsert(t *testing.T, name string, sort int) int {
	t.Helper()
	id, err := dao.InsertRemark(po.Remark{OptionName: name, SortOrder: sort})
	if err != nil {
		t.Fatalf("插入备注失败: %v", err)
	}
	return int(id)
}

// TestRemarkList 备注列表成功路径。
func TestRemarkList(t *testing.T) {
	remarkInitDB(t)

	w := remarkCall(t, RemarkList, http.MethodGet, "/api/admin/remark/list", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("备注列表应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	resp := remarkJSON(t, w)
	data := remarkData(t, resp)
	items, ok := data["items"].([]interface{})
	if !ok || len(items) < 1 {
		t.Fatalf("备注列表 items 应非空, got %v", data["items"])
	}
	if data["total"].(float64) < 1 {
		t.Fatalf("备注列表 total 应大于 0, got %v", data["total"])
	}
}

// TestRemarkSave 新增备注成功路径。
func TestRemarkSave(t *testing.T) {
	remarkInitDB(t)

	w := remarkCall(t, RemarkSave, http.MethodPost, "/api/admin/remark/save",
		`{"optionName":"不要葱","sortOrder":9}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("新增备注应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	data := remarkData(t, remarkJSON(t, w))
	if _, ok := data["remarkId"]; !ok {
		t.Fatalf("新增备注应返回 remarkId, got %v", data)
	}
}

// TestRemarkSaveEmptyName 备注名为空时被拒绝。
func TestRemarkSaveEmptyName(t *testing.T) {
	remarkInitDB(t)

	w := remarkCall(t, RemarkSave, http.MethodPost, "/api/admin/remark/save",
		`{"optionName":""}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("空备注名应返回 400, got %d body=%s", w.Code, w.Body.String())
	}
	resp := remarkJSON(t, w)
	if !strings.Contains(resp["msg"].(string), "请填写备注名称") {
		t.Fatalf("空备注名应返回业务错误提示, got %v", resp)
	}
}

// TestRemarkUpdate 修改备注成功路径。
func TestRemarkUpdate(t *testing.T) {
	remarkInitDB(t)
	id := remarkInsert(t, "待更新备注", 10)

	w := remarkCall(t, RemarkUpdate, http.MethodPost, "/api/admin/remark/update",
		`{"remarkId":`+strconv.Itoa(id)+`,"optionName":"已更新备注","sortOrder":11}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("修改备注应返回 200, got %d body=%s", w.Code, w.Body.String())
	}
	resp := remarkJSON(t, w)
	if resp["msg"] != "修改成功" {
		t.Fatalf("修改备注 msg 错误: %v", resp)
	}
}

// TestRemarkUpdateMissingID 修改备注缺少 remarkId 时返回参数错误。
func TestRemarkUpdateMissingID(t *testing.T) {
	remarkInitDB(t)

	w := remarkCall(t, RemarkUpdate, http.MethodPost, "/api/admin/remark/update",
		`{"optionName":"无ID备注"}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("缺少 remarkId 应返回 400, got %d body=%s", w.Code, w.Body.String())
	}
	resp := remarkJSON(t, w)
	if resp["msg"] != "参数错误" {
		t.Fatalf("缺少 remarkId 应返回参数错误, got %v", resp)
	}
}

// TestRemarkDelete 删除备注成功路径。
func TestRemarkDelete(t *testing.T) {
	remarkInitDB(t)
	id := remarkInsert(t, "待删除备注", 20)

	w := remarkCall(t, RemarkDelete, http.MethodDelete, "/api/admin/remark/"+strconv.Itoa(id), "",
		gin.Params{{Key: "id", Value: strconv.Itoa(id)}})
	if w.Code != http.StatusOK {
		t.Fatalf("删除备注应返回 200, got %d body=%s", w.Code, w.Body.String())
	}

	var delFlag string
	if err := store.DB.QueryRow(`SELECT del_flag FROM tb_remark WHERE remark_id=?`, id).Scan(&delFlag); err != nil {
		t.Fatalf("查询删除标记失败: %v", err)
	}
	if delFlag != "1" {
		t.Fatalf("备注应被逻辑删除, del_flag=%q", delFlag)
	}
}

// TestRemarkDeleteInvalidID 非法路径参数返回无效 ID 错误。
func TestRemarkDeleteInvalidID(t *testing.T) {
	remarkInitDB(t)

	w := remarkCall(t, RemarkDelete, http.MethodDelete, "/api/admin/remark/abc", "",
		gin.Params{{Key: "id", Value: "abc"}})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法 ID 应返回 400, got %d body=%s", w.Code, w.Body.String())
	}
	resp := remarkJSON(t, w)
	if resp["msg"] != "无效的ID" {
		t.Fatalf("非法 ID 应返回无效的ID, got %v", resp)
	}
}
