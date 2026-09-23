package service

import (
	"errors"
	"strings"

	"dining-system/infra/logger"
	"dining-system/internal/dto"
	"dining-system/internal/store"
	"dining-system/internal/store/dao"
)

// ============ 桌台 ============
//
// 桌台域业务逻辑:桌台管理、稳定桌台码兜底补发、状态占用。
// handler 只负责参数解析与响应组装。

// TableList 分页查询桌台列表,并对缺失稳定码的历史数据即时补发。
func TableList(keyword string, pageNum, pageSize int) (int, []dto.Table, error) {
	total, rows, err := dao.ListTables(dao.TableQuery{Keyword: keyword}, pageNum, pageSize)
	if err != nil {
		return 0, nil, err
	}
	out := make([]dto.Table, 0, len(rows))
	for _, t := range rows {
		// 兜底:历史数据若缺稳定码则即时补发,保证二维码可生成。
		if t.TableCode == "" {
			if code, err := store.EnsureTableCode(t.TableID); err == nil {
				t.TableCode = code
			}
		}
		out = append(out, dto.FromTable(t))
	}
	return total, out, nil
}

// SaveTable 校验并新增桌台,返回桌台 ID;插入后补发稳定桌台码。
func SaveTable(t dto.Table) (int64, error) {
	if strings.TrimSpace(t.TableNo) == "" || strings.TrimSpace(t.TableName) == "" {
		return 0, errors.New("请填写桌号和桌台名称")
	}
	id, err := dao.InsertTable(t.ToPO())
	if err != nil {
		return 0, err
	}
	// 兜底:若随机码插入冲突(唯一索引),重试补发,确保新桌台必有稳定码。
	if _, err := store.EnsureTableCode(int(id)); err != nil {
		logger.Warnf("EnsureTableCode(table %d): %v", id, err)
	}
	return id, nil
}

// UpdateTable 校验并更新桌台基础信息。
func UpdateTable(t dto.Table) error {
	if strings.TrimSpace(t.TableNo) == "" || strings.TrimSpace(t.TableName) == "" {
		return errors.New("请填写桌号和桌台名称")
	}
	return dao.UpdateTable(t.ToPO())
}

// DeleteTable 校验无进行中订单后逻辑删除桌台。
//
// 删除前先查该桌是否存在进行中订单(1已下单/2制作中/3已上齐):
// 顾客端 CustomerTable 只按 del_flag='0' 过滤桌台,若把正在用餐的桌台删掉,
// 进行中订单会失去可进入的桌台入口,加菜/收款/结账全部卡死,故先拦截并透传提示。
func DeleteTable(id int) error {
	if _, ok := dao.GetActiveOrderByTable(id); ok {
		return errors.New("该桌台存在进行中订单,请先完成或取消订单")
	}
	return dao.DeleteTable(id)
}
