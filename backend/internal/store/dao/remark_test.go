package dao

import (
	"path/filepath"
	"testing"

	"dining-system/internal/po"
	"dining-system/internal/store"
)

// remarkDaoInitDB 初始化备注测试用的临时库。
func remarkDaoInitDB(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "remark.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
}

func TestListRemarksSeeded(t *testing.T) {
	remarkDaoInitDB(t)
	list, err := ListRemarks()
	if err != nil {
		t.Fatalf("查询备注失败: %v", err)
	}
	if len(list) == 0 {
		t.Fatal("种子数据应包含备注")
	}
	for i := 1; i < len(list); i++ {
		if list[i-1].SortOrder > list[i].SortOrder {
			t.Fatalf("备注排序错误: %d > %d", list[i-1].SortOrder, list[i].SortOrder)
		}
	}
}

func TestRemarkInsertUpdateDeleteRoundTrip(t *testing.T) {
	remarkDaoInitDB(t)

	baseline, err := ListRemarks()
	if err != nil {
		t.Fatalf("查询基线失败: %v", err)
	}

	id, err := InsertRemark(po.Remark{OptionName: "测试备注", SortOrder: 999})
	if err != nil {
		t.Fatalf("新增备注失败: %v", err)
	}
	if id <= 0 {
		t.Fatalf("新增备注应返回自增 ID, got %d", id)
	}

	if err := UpdateRemark(po.Remark{RemarkID: int(id), OptionName: "改名备注", SortOrder: 888}); err != nil {
		t.Fatalf("更新备注失败: %v", err)
	}
	if err := DeleteRemark(int(id)); err != nil {
		t.Fatalf("删除备注失败: %v", err)
	}

	after, err := ListRemarks()
	if err != nil {
		t.Fatalf("删除后查询失败: %v", err)
	}
	if len(after) != len(baseline) {
		t.Fatalf("软删除后应回到 %d 条, got %d", len(baseline), len(after))
	}
}

func TestRemarkUpdateNonexistentIsNoop(t *testing.T) {
	remarkDaoInitDB(t)
	if err := UpdateRemark(po.Remark{RemarkID: 999999, OptionName: "x"}); err != nil {
		t.Fatalf("更新不存在的备注不应报错: %v", err)
	}
}
