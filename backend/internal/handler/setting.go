package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"dining-system/internal/dto"
	"dining-system/internal/service"
)

// ============ 店铺配置 ============
//
// 配置域业务逻辑已下沉到 internal/service:
//   - 回显策略(普通项回显 / 敏感项脱敏 / 密钥材料只回显路径);
//   - 乐观锁指纹与受管键清单;
//   - 保存校验、默认值告警、逐项落库与变更摘要生成。
//
// 本文件只保留 HTTP 层职责:query/body 参数解析、调用 service、组装响应。

// settingFlag 供 customer.go 等仍按旧名调用的兼容转发。
// 开关归一化逻辑见 service.SettingFlag。
func settingFlag(key string) string {
	return service.SettingFlag(key)
}

func SettingList(c *gin.Context) {
	ok(c, service.ListSettings())
}

func SettingSave(c *gin.Context) {
	var settings dto.Settings
	if err := c.ShouldBindJSON(&settings); err != nil {
		fail(c, "参数错误")
		return
	}

	detail, conflict := service.SaveSettings(settings, adminName(c))
	if conflict {
		c.JSON(http.StatusConflict, NewRsp(c, 409, "配置已被他人修改，页面已重新加载最新值，请核对后再次保存"))
		return
	}
	SetAuditDetail(c, "config", "", detail)
	okMsg(c, "保存成功")
}

// ConfigAgentTokenIssue 签发 legacy 全局打印代理令牌。
//
// 与「保存配置」里手填 agent_token 的区别:本接口后端生成即落库、立即生效,
// 不依赖管理员再去点一次保存;明文令牌只在本响应里出现一次,库里只有密文。
func ConfigAgentTokenIssue(c *gin.Context) {
	token, hint, version, err := service.IssueAgentToken()
	if err != nil {
		fail(c, "签发令牌失败")
		return
	}
	// 摘要只记「签发过」,令牌值一律不进审计日志(拿到它就能拉走整个待打印队列)。
	SetAuditDetail(c, "config", "", "签发打印代理全局令牌(敏感项,值不记录)")
	ok(c, gin.H{
		"token":     token,
		"tokenHint": hint,
		// 新指纹:前端据此刷新本地 version,避免下次保存整份配置撞 409。
		"version": version,
	})
}
