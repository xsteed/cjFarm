package dao

import (
	"path/filepath"
	"testing"

	"dining-system/internal/po"
	"dining-system/internal/store"
)

// catDaoInitDB 初始化分类测试用的临时库。
func catDaoInitDB(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "category.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
}

func TestListCategoriesSeeded(t *testing.T) {
	catDaoInitDB(t)
	list, err := ListCategories()
	if err != nil {
		t.Fatalf("查询分类失败: %v", err)
	}
	if len(list) == 0 {
		t.Fatal("种子数据应包含分类")
	}
	// 列表必须按 sort_order 升序返回。
	for i := 1; i < len(list); i++ {
		if list[i-1].SortOrder > list[i].SortOrder {
			t.Fatalf("分类排序错误: %d > %d", list[i-1].SortOrder, list[i].SortOrder)
		}
		if list[i-1].DelFlag != po.DelFlagOK {
			t.Fatalf("未删除分类不应出现 del_flag=%q", list[i-1].DelFlag)
		}
	}
}

func TestCategoryInsertUpdateDeleteRoundTrip(t *testing.T) {
	catDaoInitDB(t)

	baseline, err := ListCategories()
	if err != nil {
		t.Fatalf("查询基线失败: %v", err)
	}

	id, err := InsertCategory(po.Category{CategoryName: "测试分类", SortOrder: 999})
	if err != nil {
		t.Fatalf("新增分类失败: %v", err)
	}
	if id <= 0 {
		t.Fatalf("新增分类应返回自增 ID, got %d", id)
	}

	afterInsert, err := ListCategories()
	if err != nil {
		t.Fatalf("新增后查询失败: %v", err)
	}
	if len(afterInsert) != len(baseline)+1 {
		t.Fatalf("新增后应有 %d 条, got %d", len(baseline)+1, len(afterInsert))
	}

	if err := UpdateCategory(po.Category{CategoryID: int(id), CategoryName: "改名分类", SortOrder: 888}); err != nil {
		t.Fatalf("更新分类失败: %v", err)
	}

	// 软删除后列表应回到基线数量。
	if err := DeleteCategory(int(id)); err != nil {
		t.Fatalf("删除分类失败: %v", err)
	}
	afterDelete, err := ListCategories()
	if err != nil {
		t.Fatalf("删除后查询失败: %v", err)
	}
	if len(afterDelete) != len(baseline) {
		t.Fatalf("软删除后应回到 %d 条, got %d", len(baseline), len(afterDelete))
	}
}

func TestUpdateCategoryNonexistentIsNoop(t *testing.T) {
	catDaoInitDB(t)
	// 不存在的 ID 更新不报错(无匹配行)。
	if err := UpdateCategory(po.Category{CategoryID: 999999, CategoryName: "x"}); err != nil {
		t.Fatalf("更新不存在的分类不应报错: %v", err)
	}
}

func TestCategoryNameMapByIDs(t *testing.T) {
	catDaoInitDB(t)

	// 空 ID 列表直接返回空 map。
	if m := CategoryNameMapByIDs(nil); len(m) != 0 {
		t.Fatalf("空 ID 列表应返回空 map, got %v", m)
	}

	id1, err := InsertCategory(po.Category{CategoryName: "映射分类A", SortOrder: 1})
	if err != nil {
		t.Fatalf("新增分类A失败: %v", err)
	}
	id2, err := InsertCategory(po.Category{CategoryName: "映射分类B", SortOrder: 2})
	if err != nil {
		t.Fatalf("新增分类B失败: %v", err)
	}

	m := CategoryNameMapByIDs([]int{int(id1), int(id2), 999999})
	if m[int(id1)] != "映射分类A" || m[int(id2)] != "映射分类B" {
		t.Fatalf("分类名映射错误: %v", m)
	}
	if _, ok := m[999999]; ok {
		t.Fatalf("不存在的分类不应出现在映射中: %v", m)
	}
}
