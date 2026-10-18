// 管理端会员管理:列表与启停用。
//
// 权限点:member:view(列表) / member:edit(启停用)。
// 刻意**不提供**「后台新建会员」与「后台删除会员」:
//   - 会员档案由顾客在小程序里自助注册(openid 是唯一入口);
//   - 后台代建会产生没有登录方式、谁也登不进去的孤儿档案;
//   - 删除是顾客的权利(注销入口在顾客端「我的」页),后台只做启停用。
package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"dining-system/application/service"
)

// MemberList 会员列表:按昵称模糊搜索、按状态筛选、按入会时间倒序分页。
func MemberList(c *gin.Context) {
	pageNum, pageSize := pageParams(c)
	// status 不传或非法值 => nil(不限);传 0/1 才按状态过滤
	// (与员工列表同口径,避免零值被误当成「只看停用」)。
	var status *int
	if s := c.Query("status"); s != "" {
		if v, err := strconv.Atoi(s); err == nil {
			status = &v
		}
	}
	total, list, err := service.ListMembers(c.Query("keyword"), status, pageNum, pageSize)
	if err != nil {
		fail(c, err.Error())
		return
	}
	tableResult(c, total, list)
}

// MemberToggleStatus 启用/停用会员。停用后该会员的现有令牌在下一次校验时立即失效。
func MemberToggleStatus(c *gin.Context) {
	var p struct {
		CustomerID int `json:"customerId"`
		Status     int `json:"status"`
	}
	if err := c.ShouldBindJSON(&p); err != nil || p.CustomerID <= 0 {
		fail(c, "参数错误")
		return
	}
	if err := service.SetMemberStatus(p.CustomerID, p.Status); err != nil {
		fail(c, err.Error())
		return
	}
	if p.Status == 0 {
		okMsg(c, "已停用该会员")
		return
	}
	okMsg(c, "已启用该会员")
}
