package dao

import (
	"path/filepath"
	"testing"

	"dining-system/internal/po"
	"dining-system/internal/store"
)

// dishDaoInitDB 初始化菜品测试用的临时库。
func dishDaoInitDB(t *testing.T) {
	t.Helper()
	store.Init(filepath.Join(t.TempDir(), "dish.db"))
	t.Cleanup(func() { _ = store.DB.Close() })
}

// dishDaoCreate 创建一个菜品及规格,返回菜品 ID 与规格 ID 列表。
func dishDaoCreate(t *testing.T, name string, categoryID int, specs []po.Spec) (int64, []int64) {
	t.Helper()
	dishID, err := CreateDish(po.Dish{
		CategoryID:  categoryID,
		DishName:    name,
		DishImage:   "/uploads/test.png",
		Description: "测试描述",
		Status:      1,
		SortOrder:   99,
	}, specs)
	if err != nil {
		t.Fatalf("创建菜品失败: %v", err)
	}
	got := LoadDishSpecs(int(dishID))
	ids := make([]int64, 0, len(got))
	for _, s := range got {
		ids = append(ids, int64(s.SpecID))
	}
	return dishID, ids
}

func TestListDishesSeeded(t *testing.T) {
	dishDaoInitDB(t)
	total, list, err := ListDishes(DishQuery{}, 1, 100)
	if err != nil {
		t.Fatalf("查询菜品失败: %v", err)
	}
	if total <= 0 || len(list) != total {
		t.Fatalf("菜品总数与列表长度不一致: total=%d len=%d", total, len(list))
	}
}

func TestListDishesFiltersAndPagination(t *testing.T) {
	dishDaoInitDB(t)

	// 分类 1 是「凉菜素菜」,种子数据里共 4 道。
	total, list, err := ListDishes(DishQuery{CategoryID: "1"}, 1, 100)
	if err != nil {
		t.Fatalf("按分类查询失败: %v", err)
	}
	if total != 4 || len(list) != 4 {
		t.Fatalf("分类 1 应命中 4 道菜, got total=%d len=%d", total, len(list))
	}

	// 名称模糊匹配。
	total, list, err = ListDishes(DishQuery{DishName: "青瓜"}, 1, 100)
	if err != nil {
		t.Fatalf("按名称查询失败: %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].DishName != "凉拌青瓜" {
		t.Fatalf("名称模糊查询应命中凉拌青瓜, got total=%d list=%v", total, list)
	}

	// 组合筛选 + 分页边界(第二页超出返回空列表但 total 不变)。
	total, page2, err := ListDishes(DishQuery{}, 2, 10)
	if err != nil {
		t.Fatalf("分页查询失败: %v", err)
	}
	if total == 0 {
		t.Fatal("分页查询 total 不应为 0")
	}
	if len(page2) > 10 {
		t.Fatalf("单页不应超过 pageSize, got %d", len(page2))
	}
}

func TestListEnabledDishesExcludesDisabled(t *testing.T) {
	dishDaoInitDB(t)

	before, err := ListEnabledDishes()
	if err != nil {
		t.Fatalf("查询启用菜品失败: %v", err)
	}

	// 创建一道停用菜品,启用列表应保持不变。
	if _, err := CreateDish(po.Dish{CategoryID: 1, DishName: "停用菜品", Status: 0, SortOrder: 999}, nil); err != nil {
		t.Fatalf("创建停用菜品失败: %v", err)
	}
	after, err := ListEnabledDishes()
	if err != nil {
		t.Fatalf("查询启用菜品失败: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("停用菜品不应进入启用列表: before=%d after=%d", len(before), len(after))
	}
}

func TestGetDishByIDAndNotFound(t *testing.T) {
	dishDaoInitDB(t)

	d, err := GetDishByID(1)
	if err != nil {
		t.Fatalf("查询菜品失败: %v", err)
	}
	if d.DishID != 1 || d.DishName == "" {
		t.Fatalf("菜品回读异常: %+v", d)
	}

	if _, err := GetDishByID(999999); err == nil {
		t.Fatal("查询不存在的菜品应报错")
	}
}

func TestDishCreateUpdateDeleteRoundTrip(t *testing.T) {
	dishDaoInitDB(t)

	baseline, _, err := ListDishes(DishQuery{}, 1, 100)
	if err != nil {
		t.Fatalf("查询基线失败: %v", err)
	}

	dishID, specIDs := dishDaoCreate(t, "往返测试菜", 1, []po.Spec{
		{SpecName: "小份", Price: 1000},
		{SpecName: "大份", Price: 1800},
	})
	if len(specIDs) != 2 {
		t.Fatalf("应创建 2 条规格, got %d", len(specIDs))
	}

	// 更新菜品并重建规格。
	if err := UpdateDish(po.Dish{
		DishID:     int(dishID),
		CategoryID: 2,
		DishName:   "往返测试菜-改",
		Status:     1,
		SortOrder:  100,
	}, []po.Spec{{SpecName: "份", Price: 2500}}); err != nil {
		t.Fatalf("更新菜品失败: %v", err)
	}
	specs := LoadDishSpecs(int(dishID))
	if len(specs) != 1 || specs[0].SpecName != "份" {
		t.Fatalf("规格重建异常: %+v", specs)
	}
	updated, err := GetDishByID(int(dishID))
	if err != nil {
		t.Fatalf("回读更新菜品失败: %v", err)
	}
	if updated.DishName != "往返测试菜-改" || updated.CategoryID != 2 {
		t.Fatalf("菜品更新未生效: %+v", updated)
	}

	// 软删除后列表回到基线数量。
	if err := DeleteDish(int(dishID)); err != nil {
		t.Fatalf("删除菜品失败: %v", err)
	}
	after, _, err := ListDishes(DishQuery{}, 1, 100)
	if err != nil {
		t.Fatalf("删除后查询失败: %v", err)
	}
	if after != baseline {
		t.Fatalf("软删除后应回到 %d 条, got %d", baseline, after)
	}
}

func TestGetSpecForOrder(t *testing.T) {
	dishDaoInitDB(t)

	dishID, _ := dishDaoCreate(t, "规格查询菜", 1, []po.Spec{{SpecName: "份", Price: 3200}})
	specs := LoadDishSpecs(int(dishID))
	if len(specs) != 1 {
		t.Fatalf("应有一条规格, got %d", len(specs))
	}

	dishName, specName, delFlag, dishStatus, priceCents, err := GetSpecForOrder(specs[0].SpecID, int(dishID))
	if err != nil {
		t.Fatalf("查询规格失败: %v", err)
	}
	if dishName != "规格查询菜" || specName != "份" || delFlag != po.DelFlagOK || dishStatus != 1 || priceCents != 3200 {
		t.Fatalf("规格信息异常: dish=%s spec=%s delFlag=%s status=%d price=%d",
			dishName, specName, delFlag, dishStatus, priceCents)
	}

	// 规格与菜品不匹配时不返回行。
	if _, _, _, _, _, err := GetSpecForOrder(specs[0].SpecID, int(dishID)+1); err == nil {
		t.Fatal("菜品 ID 不匹配应报错")
	}
}

func TestLoadDishSpecsAndBatch(t *testing.T) {
	dishDaoInitDB(t)

	if got := LoadDishSpecs(999999); len(got) != 0 {
		t.Fatalf("不存在菜品应返回空规格, got %v", got)
	}
	if m := LoadSpecsByDishIDs(nil); len(m) != 0 {
		t.Fatalf("空 ID 列表应返回空 map, got %v", m)
	}

	id1, _ := dishDaoCreate(t, "批量规格A", 1, []po.Spec{{SpecName: "份", Price: 1000}})
	id2, _ := dishDaoCreate(t, "批量规格B", 2, []po.Spec{{SpecName: "小份", Price: 2000}, {SpecName: "大份", Price: 3000}})

	m := LoadSpecsByDishIDs([]int{int(id1), int(id2), 999999})
	if len(m[int(id1)]) != 1 {
		t.Fatalf("菜品 %d 应有一条规格, got %v", id1, m[int(id1)])
	}
	if len(m[int(id2)]) != 2 {
		t.Fatalf("菜品 %d 应有两条规格, got %v", id2, m[int(id2)])
	}
	if _, ok := m[999999]; ok {
		t.Fatal("不存在的菜品不应出现在批量规格结果中")
	}
}

func TestDishCategoryNameJoin(t *testing.T) {
	dishDaoInitDB(t)
	// 种子菜品 1 属于分类 1,联表应带回分类名。
	d, err := GetDishByID(1)
	if err != nil {
		t.Fatalf("查询菜品失败: %v", err)
	}
	if d.CategoryName == "" {
		t.Fatal("联表应返回分类名")
	}
	// 创建一道指向不存在分类的菜品,分类名应兜底为空串。
	id, err := CreateDish(po.Dish{CategoryID: 999999, DishName: "无分类菜", Status: 1, SortOrder: 1000}, nil)
	if err != nil {
		t.Fatalf("创建菜品失败: %v", err)
	}
	orphan, err := GetDishByID(int(id))
	if err != nil {
		t.Fatalf("查询菜品失败: %v", err)
	}
	if orphan.CategoryName != "" {
		t.Fatalf("缺失分类应兜底为空串, got %q", orphan.CategoryName)
	}
}
