package handler

import (
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"dining-system/infrastructure/store"
	"dining-system/infrastructure/store/dao"
)

func TestDbgMask(t *testing.T) {
	store.Init(filepath.Join(t.TempDir(), "dbg.db"))
	defer store.DB.Close()
	gin.SetMode(gin.TestMode)
	if err := dao.SetSetting("feie_ukey", "real-ukey-plain"); err != nil {
		t.Fatalf("set: %v", err)
	}
	t.Logf("GetSetting=%q", dao.GetSetting("feie_ukey"))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/admin/config/list", nil)
	SettingList(c)
	t.Logf("body=%s", w.Body.String())
}
