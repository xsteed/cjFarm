package dto

import "dining-system/internal/po"

// OperLog 对应 tb_oper_log 表:一条操作日志(API 出参)。
type OperLog struct {
	LogID        int    `json:"logId"`
	Module       string `json:"module"`       // 模块中文名:订单管理 / 员工管理 / 登录
	BusinessType string `json:"businessType"` // 见 OperType* 常量
	Action       string `json:"action"`       // 动作中文名:修改菜品 / 重置密码
	Method       string `json:"method"`       // 路由模板 "POST /api/admin/dish/update"
	RequestURL   string `json:"requestUrl"`   // 含 query 的真实 URL
	OperatorID   int    `json:"operatorId"`   // 0 表示未登录 / 无法识别(如密码错误)
	Operator     string `json:"operator"`     // 显示名快照:改名后历史日志不变
	OperatorRole string `json:"operatorRole"` // 当时角色名快照:改角色后历史日志不变
	OperIP       string `json:"operIp"`
	TargetType   string `json:"targetType"` // 业务对象类型:order / user / dish / table ...
	TargetID     string `json:"targetId"`   // 业务对象标识(订单号 / 主键);VARCHAR 以兼容单号
	OperParam    string `json:"operParam"`  // 脱敏后的请求参数 JSON
	Detail       string `json:"detail"`     // 人话摘要:高风险动作由业务代码显式填写
	Status       int    `json:"status"`     // 见 OperStatus* 常量
	ErrorMsg     string `json:"errorMsg"`   // 失败原因(取自响应 msg)
	CostMs       int    `json:"costMs"`
	CreateTime   string `json:"createTime"`
}

// FromOperLog 将持久化对象转为 API 出参。
func FromOperLog(p po.OperLog) OperLog {
	return OperLog{
		LogID:        p.LogID,
		Module:       p.Module,
		BusinessType: p.BusinessType,
		Action:       p.Action,
		Method:       p.Method,
		RequestURL:   p.RequestURL,
		OperatorID:   p.OperatorID,
		Operator:     p.Operator,
		OperatorRole: p.OperatorRole,
		OperIP:       p.OperIP,
		TargetType:   p.TargetType,
		TargetID:     p.TargetID,
		OperParam:    p.OperParam,
		Detail:       p.Detail,
		Status:       p.Status,
		ErrorMsg:     p.ErrorMsg,
		CostMs:       p.CostMs,
		CreateTime:   p.CreateTime,
	}
}

// ToPO 将 API 出参转回持久化对象。
func (o OperLog) ToPO() po.OperLog {
	return po.OperLog{
		LogID:        o.LogID,
		Module:       o.Module,
		BusinessType: o.BusinessType,
		Action:       o.Action,
		Method:       o.Method,
		RequestURL:   o.RequestURL,
		OperatorID:   o.OperatorID,
		Operator:     o.Operator,
		OperatorRole: o.OperatorRole,
		OperIP:       o.OperIP,
		TargetType:   o.TargetType,
		TargetID:     o.TargetID,
		OperParam:    o.OperParam,
		Detail:       o.Detail,
		Status:       o.Status,
		ErrorMsg:     o.ErrorMsg,
		CostMs:       o.CostMs,
		CreateTime:   o.CreateTime,
	}
}
