package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestVersionHandler 验证运行期版本接口:无需鉴权即返回注入的版本信息。
func TestVersionHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	InitBuildInfo("9.9.9", "deadbee", "2026-10-09T00:00:00Z")

	r := gin.New()
	r.GET("/api/version", Version)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body struct {
		Code int `json:"code"`
		Data struct {
			Version   string `json:"version"`
			GitCommit string `json:"gitCommit"`
			BuildTime string `json:"buildTime"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("json 解包失败: %v", err)
	}
	if body.Code != 200 {
		t.Errorf("code = %d, want 200", body.Code)
	}
	if body.Data.Version != "9.9.9" || body.Data.GitCommit != "deadbee" || body.Data.BuildTime != "2026-10-09T00:00:00Z" {
		t.Errorf("返回版本信息不符: %+v", body.Data)
	}
}
