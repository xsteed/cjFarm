package service

import (
	"testing"

	"dining-system/infrastructure/store/dao"
	"dining-system/interfaces/dto"
)

// 复现「管理端设置售罄后没生效」的写读闭环：
// 设置 soldOut=1 → DishGet（管理端）与 CustomerMenu（顾客端）都应读回 1。
func TestUpdateDishSoldOutRoundtrip(t *testing.T) {
	menuInitDB(t)
	cat := menuSeedCategory(t, "测试分类")
	dishID := menuSeedDish(t, cat, "售罄测试菜", 1, 1000, "份")
	specs, err := dao.LoadDishSpecs(dishID)
	if err != nil {
		t.Fatalf("加载规格失败: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("规格数=%d, want 1", len(specs))
	}

	err = UpdateDish(dto.Dish{
		DishID:     dishID,
		CategoryID: cat,
		DishName:   "售罄测试菜",
		Status:     1,
		SoldOut:    1,
		Specs:      []dto.Spec{{SpecID: specs[0].SpecID, SpecName: "份", Price: 10, Stock: -1}},
	})
	if err != nil {
		t.Fatalf("UpdateDish 失败: %v", err)
	}

	d, err := DishGet(dishID)
	if err != nil {
		t.Fatalf("DishGet 失败: %v", err)
	}
	if d.SoldOut != 1 {
		t.Fatalf("DishGet soldOut=%d, want 1（管理端读回未生效）", d.SoldOut)
	}

	menu, err := CustomerMenu()
	if err != nil {
		t.Fatalf("CustomerMenu 失败: %v", err)
	}
	for _, c := range menu {
		for _, dish := range c.Dishes {
			if dish.DishID == dishID {
				if dish.SoldOut != 1 {
					t.Fatalf("顾客菜单 soldOut=%d, want 1（顾客端未生效）", dish.SoldOut)
				}
				return
			}
		}
	}
	t.Fatalf("顾客菜单里没找到 dishID=%d", dishID)
}
