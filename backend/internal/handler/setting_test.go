package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	"dining-system/internal/dto"
	"dining-system/internal/print"
	"dining-system/internal/service"
	"dining-system/internal/store"
	"dining-system/internal/store/dao"
)

// TestDescribeSettingChange 回归防护:变更摘要必须基于「保存前的旧值快照」生成。
// 此前实现在全部 SetSetting 落库后才调用 LoadSettings 取旧值,读到的已是新值,
// 普通配置项的「旧值 → 新值」恒相等,审计日志从未记录到任何普通项变更。
// 摘要生成逻辑已下沉到 service,这里经 SettingSave handler 的行为断言。
func TestDescribeSettingChange(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 无任何变更时给固定文案,便于审计检索。
	// 先保存一次建立基线,再提交相同内容,应得到「无字段变更」摘要。
	store.Init(filepath.Join(t.TempDir(), "cfgchange_none.db"))
	w0, c0 := newSaveContext(map[string]interface{}{})
	SettingSave(c0)
	if w0.Code != http.StatusOK {
		t.Fatalf("建立配置基线失败, got %d body=%s", w0.Code, w0.Body.String())
	}
	w, c := newSaveContext(map[string]interface{}{})
	SettingSave(c)
	if w.Code != http.StatusOK {
		t.Fatalf("无变更保存应成功, got %d body=%s", w.Code, w.Body.String())
	}
	same := currentAuditExtra(c).detail
	if !strings.Contains(same, "无字段变更") {
		t.Fatalf("无变更时应提示无字段变更, got %q", same)
	}
	store.DB.Close()

	// 旧值 → 新值分支:摘要必须记录普通项变更、跳过未变更项、敏感项只记键名。
	store.Init(filepath.Join(t.TempDir(), "cfgchange_diff.db"))
	if err := dao.SetSetting("shop_name", "旧店名"); err != nil {
		t.Fatalf("设置旧值失败: %v", err)
	}
	if err := dao.SetSetting("seat_fee", "6"); err != nil {
		t.Fatalf("设置旧值失败: %v", err)
	}
	// 提交面补齐全部出厂默认值:保持「只变化 shop_name 与敏感项」的测试语义。
	// 零值字段会被整表覆盖并在摘要里记为变更,新加配置项(如页脚文案)默认非空,
	// 不补齐会把摘要撑到截断,shop_name 的断言反而被挤掉。
	w2, c2 := newSaveContext(map[string]interface{}{
		"shop_name": "新店名",
		"seat_fee":  "6", // 未变化,不应出现在摘要
		// 以下均为出厂默认值(与 settingDefaults 一致),提交后不应出现在摘要。
		"h5_base_url":               "http://localhost:8080",
		"pay_qr_wx":                 "/uploads/pay_wx.png",
		"pay_qr_ali":                "/uploads/pay_ali.jpg",
		"promotion_threshold":       "100",
		"promotion_discount":        "0",
		"feie_api_url":              "https://api.de.feieyun.com/Api/Open/",
		"print_enabled":             "1",
		"print_guest_footer":        "谢谢惠顾,欢迎再次光临",
		"print_guest_show_seat_fee": "1",
		"print_guest_show_discount": "1",
		"feie_ukey":                 "new-secret-value", // 敏感项只记键名,值不落日志
	})
	SettingSave(c2)
	if w2.Code != http.StatusOK {
		t.Fatalf("变更保存应成功, got %d body=%s", w2.Code, w2.Body.String())
	}

	got := currentAuditExtra(c2).detail
	if !strings.Contains(got, "shop_name: 旧店名 → 新店名") {
		t.Fatalf("摘要应记录 shop_name 的旧值→新值, got %q", got)
	}
	if strings.Contains(got, "seat_fee:") {
		t.Fatalf("未变更的 seat_fee 不应出现在摘要, got %q", got)
	}
	if !strings.Contains(got, "feie_ukey(敏感项,值不记录)") {
		t.Fatalf("敏感项应只记键名, got %q", got)
	}
	if strings.Contains(got, "new-secret-value") {
		t.Fatalf("敏感项的值不得进入日志, got %q", got)
	}
	store.DB.Close()
}

// newSaveContext 构造一个绑定 JSON body 的保存请求上下文。
func newSaveContext(body interface{}) (*httptest.ResponseRecorder, *gin.Context) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	raw, _ := json.Marshal(body)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/config/save", bytes.NewReader(raw))
	c.Request.Header.Set("Content-Type", "application/json")
	return w, c
}

// settingVersion 经 SettingList handler 获取当前配置指纹(乐观锁 version)。
func settingVersion(t *testing.T) string {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/config/list", nil)
	SettingList(c)
	var rsp struct {
		Data dto.Settings `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &rsp); err != nil {
		t.Fatalf("解析 SettingList 响应失败: %v", err)
	}
	if rsp.Data.Version == "" {
		t.Fatalf("SettingList 未下发 version 指纹")
	}
	return rsp.Data.Version
}

// TestSettingSaveVersionConflict 覆盖配置保存乐观锁的三个分支:
// 过期 version 拒绝(409)、最新 version 放行、保存后指纹随之更新。
func TestSettingSaveVersionConflict(t *testing.T) {
	store.Init(filepath.Join(t.TempDir(), "cfg409.db"))
	defer store.DB.Close()
	gin.SetMode(gin.TestMode)

	// 1. 过期的 version:与库内容指纹不符,拒绝旧快照的全量覆盖。
	w, c := newSaveContext(map[string]interface{}{"shop_name": "旧快照", "version": "stale-version"})
	SettingSave(c)
	if w.Code != http.StatusConflict {
		t.Fatalf("过期 version 应返回 409, got %d body=%s", w.Code, w.Body.String())
	}

	// 2. 最新 version:校验通过并保存成功。
	version := settingVersion(t)
	w2, c2 := newSaveContext(map[string]interface{}{"shop_name": "新值", "version": version})
	SettingSave(c2)
	if w2.Code != http.StatusOK {
		t.Fatalf("最新 version 应保存成功, got %d body=%s", w2.Code, w2.Body.String())
	}
	if got := dao.GetSetting("shop_name"); got != "新值" {
		t.Fatalf("保存后取值=%q, 期望 %q", got, "新值")
	}

	// 3. 模拟「构造请求之后、提交之前有其他人保存」:请求携带的是旧指纹,应 409。
	oldVersion := settingVersion(t)
	if err := dao.SetSetting("seat_fee", "88"); err != nil { // 他人先行修改
		t.Fatalf("模拟他人修改失败: %v", err)
	}
	w3, c3 := newSaveContext(map[string]interface{}{"shop_name": "再次旧快照", "version": oldVersion})
	SettingSave(c3)
	if w3.Code != http.StatusConflict {
		t.Fatalf("被他人抢先修改后应 409, got %d body=%s", w3.Code, w3.Body.String())
	}
}

// TestSettingSaveConcurrentConflict 两个请求携带同一份过期指纹并发提交,
// 必须恰好一个成功、其余 409。指纹校验在保存互斥锁内串行执行——若校验
// 在锁外(回归到旧实现),两个请求会同时通过校验并先后覆盖,互斥形同虚设。
func TestSettingSaveConcurrentConflict(t *testing.T) {
	store.Init(filepath.Join(t.TempDir(), "cfgconcurrent.db"))
	defer store.DB.Close()
	gin.SetMode(gin.TestMode)

	// 两个请求共享同一份「提交前」的指纹
	fp := settingVersion(t)

	const workers = 2
	start := make(chan struct{})
	results := make([]int, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start // 同步起跑,尽量制造同时校验的窗口
			w, c := newSaveContext(map[string]interface{}{
				"shop_name": fmt.Sprintf("并发提交-%d", idx),
				"version":   fp,
			})
			SettingSave(c)
			results[idx] = w.Code
		}(i)
	}
	close(start)
	wg.Wait()

	okCount, conflictCount := 0, 0
	for _, code := range results {
		switch code {
		case http.StatusOK:
			okCount++
		case http.StatusConflict:
			conflictCount++
		}
	}
	if okCount != 1 || conflictCount != workers-1 {
		t.Fatalf("并发提交应恰好 1 成功、%d 冲突, got OK=%d CONFLICT=%d codes=%v",
			workers-1, okCount, conflictCount, results)
	}
}

// TestManagedSettingKeysComplete 守护受管键清单与提交面的一致性:
// SettingSave 提交一份全字段非空的表单后,每个受管键在库里都应非空。
// 漏键意味着乐观锁指纹存在盲区——该键的变化不会触发 409 保护。
func TestManagedSettingKeysComplete(t *testing.T) {
	store.Init(filepath.Join(t.TempDir(), "cfgkeys.db"))
	defer store.DB.Close()
	gin.SetMode(gin.TestMode)

	cfg := dto.Settings{
		ShopName: "键集测试", ShopLogo: "/uploads/logo.png", H5BaseURL: "http://h.example",
		SeatFeeEnabled: "1", SeatFee: "9",
		PromotionEnabled: "1", PromotionThreshold: "100", PromotionDiscount: "10",
		PayQrWx: "/uploads/wx.png", PayQrAli: "/uploads/ali.png",
		WxpayEnabled: "1", AlipayEnabled: "1",
		WxpayMchid: "mchid-x", WxpayAppid: "appid-x", WxpaySerialNo: "serial-x",
		WxpayPrivateKeyPath: "/k.pem", WxpayPlatformCertPath: "/c.pem",
		WxpayPubkeyID: "pubid-x", WxpayPubkeyPath: "/pub.pem", WxpayNotifyURL: "http://n.example/cb",
		AlipayAppid: "alappid-x", AlipayPrivateKeyPath: "/ak.pem", AlipayPublicKey: "alpub-x", AlipayNotifyURL: "http://an.example/cb",
		PrintEnabled: "1", PrintKitchenShowPrice: "1",
		PrintGuestFooter: "页脚测试", PrintGuestShowSeatFee: "1", PrintGuestShowDiscount: "1",
		FeieUser: "feie-user", FeieApiURL: "http://feie.example/api",
		AgentLatestVersion: "1.0.0",
		WxpayApiv3Key:      "apiv3-secret", FeieUkey: "feie-secret", AgentToken: "agent-secret",
	}
	w, c := newSaveContext(cfg)
	SettingSave(c)
	if w.Code != http.StatusOK {
		t.Fatalf("全字段保存应成功, got %d body=%s", w.Code, w.Body.String())
	}
	for _, k := range service.ManagedSettingKeys {
		if dao.GetSetting(k) == "" {
			t.Fatalf("受管键 %s 未被保存覆盖,ManagedSettingKeys 与提交面不一致", k)
		}
	}
}

// TestConfigAgentTokenIssue 全局代理令牌的后端签发链路。
//
// 回归防护三件事:
//  1. 签发即落库 —— 明文能被 print.VerifyAgentToken 认可,不需要管理员再点一次保存;
//  2. 覆盖旧令牌 —— 重新签发后旧令牌立即失效(代理侧不改配就会报「令牌不正确」);
//  3. 回传新指纹 —— agent_token 参与乐观锁指纹,不回传会让下一次保存整份配置撞 409,
//     而 409 分支会用后端数据整表覆盖表单,冲掉管理员未保存的改动。
func TestConfigAgentTokenIssue(t *testing.T) {
	initAgentTestDB(t)

	issue := func() (string, string, string) {
		t.Helper()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/config/agent/token", nil)
		ConfigAgentTokenIssue(c)
		if w.Code != http.StatusOK {
			t.Fatalf("签发令牌应成功, got %d body=%s", w.Code, w.Body.String())
		}
		var body map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("解析响应失败: %v body=%s", err, w.Body.String())
		}
		data, _ := body["data"].(map[string]interface{})
		token, _ := data["token"].(string)
		hint, _ := data["tokenHint"].(string)
		version, _ := data["version"].(string)
		return token, hint, version
	}

	old, _, version := issue()
	if old == "" {
		t.Fatalf("签发的令牌不应为空")
	}
	// 库里存的是密文,GetSetting 透明解密;明文必须原样可取回。
	if got := dao.GetSetting("agent_token"); got != old {
		t.Fatalf("令牌未落库: 库值=%q 期望=%q", got, old)
	}
	// 签发即可用:不需要再走一次「保存配置」。
	if !print.VerifyAgentToken(old) {
		t.Fatalf("签发后的令牌应通过校验")
	}
	// 指纹必须跟着变,否则管理端下次保存会被 409 拦下。
	if want := dao.SettingsFingerprint(service.ManagedSettingKeys); version != want {
		t.Fatalf("应回传签发后的新指纹, got %q want %q", version, want)
	}

	// 重新签发:旧令牌立即失效,hint 是新令牌的前 4 位。
	again, hint, _ := issue()
	if again == old {
		t.Fatalf("重新签发应产生新令牌")
	}
	if hint != again[:4] {
		t.Fatalf("tokenHint=%q 应为令牌前 4 位 %q", hint, again[:4])
	}
	if print.VerifyAgentToken(old) {
		t.Fatalf("重新签发后旧令牌应失效")
	}
	if !print.VerifyAgentToken(again) {
		t.Fatalf("新令牌应通过校验")
	}
}
