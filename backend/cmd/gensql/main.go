// Command gensql 从 internal/store 的表结构与学生成数据库脚本。
//
// 用法(在 backend 目录下执行):
//
//	go run ./cmd/gensql          # 生成 migrations/full/{sqlite,mysql}/*.sql
//	go run ./cmd/gensql -check   # 只校验磁盘文件是否与生成结果一致(CI 用)
//
// 之所以用「生成」而不是「手写两份」:SQLite 与 MySQL 的建表语句只有少量差异
// (自增关键字、VARCHAR 长度、索引 IF NOT EXISTS、引擎字符集),若各自手写,
// 一旦表结构变更极易漏改其中一份。统一以 Go 定义为唯一来源,改完重新生成即可。
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"dining-system/internal/store"
)

func main() {
	outDir := flag.String("out", "migrations", "脚本输出根目录")
	checkOnly := flag.Bool("check", false, "只校验文件是否已与生成结果一致,不写盘")
	flag.Parse()

	type target struct {
		rel     string
		content string
	}

	targets := []target{
		{
			rel:     filepath.Join("full", "sqlite", "schema.sql"),
			content: render(sqliteSchemaHeader(), store.SchemaSQL(store.DialectSQLite)),
		},
		{
			rel:     filepath.Join("full", "sqlite", "seed.sql"),
			content: render(sqliteSeedHeader(), store.SeedSQL(store.DialectSQLite)),
		},
		{
			rel:     filepath.Join("full", "mysql", "schema.sql"),
			content: render(mysqlSchemaHeader(), store.SchemaSQL(store.DialectMySQL)),
		},
		{
			rel:     filepath.Join("full", "mysql", "seed.sql"),
			content: render(mysqlSeedHeader(), store.SeedSQL(store.DialectMySQL)),
		},
	}

	stale := 0
	for _, tg := range targets {
		path := filepath.Join(*outDir, tg.rel)
		if *checkOnly {
			old, err := os.ReadFile(path)
			if err != nil {
				fmt.Printf("✗ 缺失 %s (%v)\n", path, err)
				stale++
				continue
			}
			if string(old) != tg.content {
				fmt.Printf("✗ 不一致 %s(请重新执行 go run ./cmd/gensql)\n", path)
				stale++
				continue
			}
			fmt.Printf("✓ %s\n", path)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "创建目录失败 %s: %v\n", path, err)
			os.Exit(1)
		}
		if err := os.WriteFile(path, []byte(tg.content), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "写入失败 %s: %v\n", path, err)
			os.Exit(1)
		}
		fmt.Printf("✓ 已生成 %s\n", path)
	}

	if *checkOnly {
		if stale > 0 {
			fmt.Printf("\n共 %d 个文件与生成结果不一致。\n", stale)
			os.Exit(1)
		}
		fmt.Println("\n全部脚本均为最新。")
	}
	fmt.Println("统计:", seedSummary())
}

// seedSummary 汇总种子数据行数,写进脚本头注释,避免文档数字与代码脱节。
func seedSummary() string {
	c := store.SeedCounts()
	return fmt.Sprintf("系统配置 %d 项、内置角色 %d 个、桌台 %d 张、分类 %d 个、菜品 %d 道、规格 %d 条、备注 %d 项、打印机 %d 台",
		c["setting"], c["role"], c["table"], c["category"], c["dish"], c["spec"], c["remark"], c["printer"])
}

// render 拼接「头部说明 + 生成的语句」。
func render(header []string, body []string) string {
	var b strings.Builder
	b.WriteString(strings.Join(header, "\n"))
	b.WriteString("\n\n")
	for _, s := range body {
		if s == "" {
			b.WriteString("\n")
			continue
		}
		// 建表模板里用制表符对齐,落盘时换成 4 空格,便于在各种编辑器/网页里查看。
		b.WriteString(strings.ReplaceAll(strings.TrimRight(s, " \t"), "\t", "    "))
		b.WriteString("\n")
	}
	return b.String()
}

func sqliteSchemaHeader() []string {
	return []string{
		"-- ============================================================================",
		"-- 扫码点餐管理系统 — 全量建表脚本(SQLite)",
		"-- 文件名:schema.sql —— 全量建表脚本(通用命名)",
		"-- ----------------------------------------------------------------------------",
		"-- 本文件由 `go run ./cmd/gensql` 从 backend/internal/store 的表结构定义生成,",
		"-- 请勿手工修改;需要变更表结构时改 Go 定义后重新生成。",
		"--",
		"-- 适用场景:全新部署 / 从零建库,执行后得到与后端代码完全一致的库结构",
		"--           (已包含历史上通过 ALTER TABLE 追加的所有列)。",
		"-- 执行方式:sqlite3 data/dining.db < schema.sql",
		"-- 幂等性  :全部使用 IF NOT EXISTS,可重复执行。",
		"--",
		"-- 金额约定:所有金额字段以「分」为单位存 INTEGER,API 层由 po.ToYuan 转元。",
		"-- ============================================================================",
		"",
		"PRAGMA foreign_keys = OFF;",
	}
}

func mysqlSchemaHeader() []string {
	return []string{
		"-- ============================================================================",
		"-- 扫码点餐管理系统 — 全量建表脚本(MySQL 8.0 / 5.7,MariaDB 10.3+)",
		"-- 文件名:schema.sql —— 全量建表脚本(通用命名)",
		"-- ----------------------------------------------------------------------------",
		"-- 本文件由 `go run ./cmd/gensql` 从 backend/internal/store 的表结构定义生成,",
		"-- 请勿手工修改;需要变更表结构时改 Go 定义后重新生成。",
		"--",
		"-- 前置步骤(只需执行一次):",
		"--   CREATE DATABASE IF NOT EXISTS dining",
		"--     DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci;",
		"--   CREATE USER IF NOT EXISTS 'dining'@'%' IDENTIFIED BY '改成你的强密码';",
		"--   GRANT ALL PRIVILEGES ON dining.* TO 'dining'@'%';",
		"--   FLUSH PRIVILEGES;",
		"--",
		"-- 执行方式:mysql -u dining -p dining < schema.sql",
		"--",
		"-- 注意事项:",
		"--   1. MySQL 不支持 CREATE INDEX IF NOT EXISTS;重复执行时建表语句会被",
		"--      IF NOT EXISTS 跳过,但建索引语句会报 'Duplicate key name',",
		"--      该错误可安全忽略(可用 `mysql --force` 继续执行)。",
		"--   2. 表引擎 InnoDB、字符集 utf8mb4,需在 MySQL 5.7+ 上执行。",
		"--   3. 与 SQLite 版的唯一语义差异:MySQL 会强制校验 VARCHAR 长度,",
		"--      SQLite 不校验,导入超长数据时 MySQL 会报 'Data too long'。",
		"--",
		"-- 金额约定:所有金额字段以「分」为单位存 INT,API 层由 po.ToYuan 转元。",
		"-- ============================================================================",
		"",
		"SET NAMES utf8mb4;",
	}
}

func sqliteSeedHeader() []string {
	return []string{
		"-- ============================================================================",
		"-- 扫码点餐管理系统 — 种子/存量数据(SQLite)",
		"-- 文件名:seed.sql —— 种子/存量数据(通用命名)",
		"-- ----------------------------------------------------------------------------",
		"-- 本文件由 `go run ./cmd/gensql` 从 backend/internal/store 的种子数据生成,",
		"-- 请勿手工修改;需要调整初始数据时改 Go 定义后重新生成。",
		"--",
		"-- 内容:" + seedSummary() + "。",
		"-- 执行方式:sqlite3 data/dining.db < seed.sql",
		"-- 幂等性  :全部使用 INSERT OR IGNORE + 显式主键,可重复执行不会产生重复数据。",
		"--",
		"-- 注意:金额一律为「分」;图片内容存于 tb_image 表(数据库),",
		"--       出厂图片内嵌后端二进制、首次启动自动写入;业务表仅存 /uploads/<文件名> 虚拟路径,",
		"--       展示时由后端从库读取,seed.sql 不含图片数据。",
		"-- ============================================================================",
	}
}

func mysqlSeedHeader() []string {
	return []string{
		"-- ============================================================================",
		"-- 扫码点餐管理系统 — 种子/存量数据(MySQL 8.0 / 5.7,MariaDB 10.3+)",
		"-- 文件名:seed.sql —— 种子/存量数据(通用命名)",
		"-- ----------------------------------------------------------------------------",
		"-- 本文件由 `go run ./cmd/gensql` 从 backend/internal/store 的种子数据生成,",
		"-- 请勿手工修改;需要调整初始数据时改 Go 定义后重新生成。",
		"--",
		"-- 内容:" + seedSummary() + "。",
		"-- 执行方式:mysql -u dining -p dining < seed.sql",
		"-- 幂等性  :全部使用 INSERT IGNORE + 显式主键,可重复执行不会产生重复数据。",
		"--",
		"-- 注意:金额一律为「分」;图片内容存于 tb_image 表(数据库),",
		"--       出厂图片内嵌后端二进制、首次启动自动写入;业务表仅存 /uploads/<文件名> 虚拟路径,",
		"--       展示时由后端从库读取,seed.sql 不含图片数据。",
		"-- ============================================================================",
		"",
		"SET NAMES utf8mb4;",
	}
}
