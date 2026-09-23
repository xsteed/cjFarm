#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""SQLite -> MySQL 数据搬运脚本（扫码点餐管理系统）

用途：把现有 SQLite 库（dining.db）里的业务数据导出为 MySQL 可直接执行的
      INSERT 语句，配合 backend/migrations/full/mysql/schema.sql 完成搬家。

为什么不用现成工具：
  · 本项目金额是「整数分」、时间统一存 VARCHAR，不存在类型转换问题；
  · tb_config 里的敏感值是 AES 密文（enc:v1: 前缀），必须原样搬运，
    任何"智能推断类型"的工具都可能把它改坏；
  · 表结构两边由同一份 Go 定义生成，不需要工具做 schema 映射。

用法：
    # 1) 先建好 MySQL 空库并导入结构
    mysql -u dining -p dining < migrations/full/mysql/schema.sql

    # 2) 导出数据
    python scripts/sqlite2mysql.py --sqlite data/dining.db --out /tmp/tb_data.sql

    # 3) 导入 MySQL
    mysql -u dining -p dining < /tmp/tb_data.sql

说明：tb_image 的图片二进制以 X'..' 十六进制导出，导入 MySQL 无损。

常用参数：
    --tables a,b,c   只迁移指定表（默认全部）
    --truncate       导入前 TRUNCATE（默认：用 INSERT IGNORE，已存在的行跳过）
    --batch 500      每条 INSERT 合并多少行（默认 200）
    --no-verify      跳过迁移后的行数核对提示

退出码：0 成功；非 0 表示有表读取失败。
"""

from __future__ import annotations

import argparse
import os
import sqlite3
import sys

# 需要迁移的业务表（顺序无关，但按依赖关系排列更有利于排查）
DEFAULT_TABLES = [
    "tb_config",
    "tb_table",
    "tb_category",
    "tb_dish",
    "tb_spec",
    "tb_remark",
    "tb_printer",
    "tb_order",
    "tb_order_item",
    "tb_order_urge",
    "tb_payment",
    "tb_refund",
    "tb_image",
]


def q_ident(name: str) -> str:
    """MySQL 标识符加反引号，内部反引号需翻倍。"""
    return "`" + name.replace("`", "``") + "`"


def q_value(v) -> str:
    """把 Python 值转成 MySQL 字面量。"""
    if v is None:
        return "NULL"
    if isinstance(v, bool):
        return "1" if v else "0"
    if isinstance(v, (int, float)):
        return repr(v)
    if isinstance(v, (bytes, bytearray)):
        return "X'" + v.hex() + "'"
    s = str(v)
    # 反斜杠与单引号是 MySQL 字符串里唯二需要转义的字符；
    # 换行/制表符在 MySQL 字符串字面量中可直接出现，但为可读性这里也转义。
    s = (
        s.replace("\\", "\\\\")
        .replace("'", "''")
        .replace("\x00", "\\0")
        .replace("\n", "\\n")
        .replace("\r", "\\r")
    )
    return "'" + s + "'"


def sqlite_tables(conn: sqlite3.Connection) -> list[str]:
    rows = conn.execute(
        "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'"
    ).fetchall()
    return [r[0] for r in rows]


def table_columns(conn: sqlite3.Connection, table: str) -> list[str]:
    return [r[1] for r in conn.execute(f'PRAGMA table_info({q_ident(table)})')]


def main() -> int:
    ap = argparse.ArgumentParser(description="SQLite -> MySQL 数据导出")
    ap.add_argument("--sqlite", default="data/dining.db", help="SQLite 数据库文件路径")
    ap.add_argument("--out", required=True, help="输出 .sql 文件路径")
    ap.add_argument("--tables", default="", help="只迁移指定表，逗号分隔")
    ap.add_argument("--batch", type=int, default=200, help="每条 INSERT 合并的行数")
    ap.add_argument("--truncate", action="store_true", help="导入前先 TRUNCATE 清表")
    ap.add_argument("--no-verify", action="store_true", help="不打印行数核对清单")
    args = ap.parse_args()

    if not os.path.exists(args.sqlite):
        print(f"找不到 SQLite 文件：{args.sqlite}", file=sys.stderr)
        return 2

    conn = sqlite3.connect(args.sqlite)
    conn.row_factory = None

    present = set(sqlite_tables(conn))
    wanted = [t.strip() for t in args.tables.split(",") if t.strip()] or DEFAULT_TABLES
    tables = [t for t in wanted if t in present]
    missing = [t for t in wanted if t not in present]

    lines: list[str] = []
    lines.append("-- ============================================================================")
    lines.append("-- 扫码点餐管理系统 — SQLite 数据导出（由 scripts/sqlite2mysql.py 生成）")
    lines.append(f"-- 源库：{args.sqlite}")
    lines.append("-- 目标：MySQL（需先执行 migrations/full/mysql/schema.sql 建表）")
    lines.append("-- 说明：tb_config 中敏感值为 AES 密文，已原样搬运，请一并迁移 data/master.key。")
    lines.append("-- ============================================================================")
    lines.append("")
    lines.append("SET NAMES utf8mb4;")
    lines.append("SET FOREIGN_KEY_CHECKS = 0;")
    lines.append("")

    counts: dict[str, int] = {}
    failed: list[str] = []

    for t in tables:
        try:
            cols = table_columns(conn, t)
            rows = conn.execute(f'SELECT * FROM {q_ident(t)}').fetchall()
        except sqlite3.Error as e:
            print(f"  ✗ {t}: {e}", file=sys.stderr)
            failed.append(t)
            continue

        counts[t] = len(rows)
        verb = "TRUNCATE TABLE" if args.truncate else None

        lines.append(f"-- ---------- {t} ({len(rows)} 行) ----------")
        if verb:
            lines.append(f"{verb} {q_ident(t)};")
        if not rows:
            lines.append("")
            continue

        col_sql = ", ".join(q_ident(c) for c in cols)
        insert_verb = "INSERT INTO" if args.truncate else "INSERT IGNORE INTO"
        # 切块双重上限:行数上限沿用 --batch;字节上限防 tb_image 的二进制行
        # (X'..' 十六进制约 2×blob 体积)被合并成超 max_allowed_packet(64M) 的巨型
        # INSERT,mysql 客户端导入会直接报 ER_NET_PACKET_TOO_LARGE,违背「迁移无损」。
        # 48MB 预算给 64M 服务端上限留出余量。
        max_packet = 48 * 1024 * 1024
        chunk: list[str] = []
        cur = 0
        for row in rows:
            row_s = "(" + ", ".join(q_value(v) for v in row) + ")"
            if chunk and (len(chunk) >= args.batch or cur + len(row_s) + 2 > max_packet):
                lines.append(
                    f"{insert_verb} {q_ident(t)} ({col_sql}) VALUES\n  "
                    + ",\n  ".join(chunk)
                    + ";"
                )
                chunk, cur = [], 0
            chunk.append(row_s)
            cur += len(row_s) + 2
        if chunk:
            lines.append(
                f"{insert_verb} {q_ident(t)} ({col_sql}) VALUES\n  "
                + ",\n  ".join(chunk)
                + ";"
            )
        lines.append("")

    lines.append("SET FOREIGN_KEY_CHECKS = 1;")
    lines.append("")

    with open(args.out, "w", encoding="utf-8", newline="\n") as f:
        f.write("\n".join(lines))

    total = sum(counts.values())
    print(f"已导出 {len(counts)} 张表 / {total} 行 -> {args.out}")
    if not args.no_verify:
        print("\n迁移后请核对行数：")
        for t in tables:
            if t in counts:
                print(f"  {t:22s} {counts[t]:>6d}")
    if missing:
        print(f"\n注意：以下表在 SQLite 中不存在，已跳过：{', '.join(missing)}")
    if failed:
        print(f"\n以下表读取失败：{', '.join(failed)}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
