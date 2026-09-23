package dao

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"sort"

	"dining-system/infra"
	"dining-system/infra/logger"
	"dining-system/internal/store"
)

// GetSetting 读取单个配置项。敏感项(cipher 存储)在此透明解密,调用方无感知。
// 键不存在时返回空串(ErrNoRows 属正常情况,不告警);读取失败同样返回空串,
// 但记一条 warn——调用方(如金额计算)把「读不到」按「未开启」处理,
// 没有日志的话这类静默降级将无从排查。
func GetSetting(key string) string {
	var v string
	if err := store.DB.QueryRow(`SELECT cfg_value FROM tb_config WHERE cfg_key=?`, key).Scan(&v); err != nil && !errors.Is(err, sql.ErrNoRows) {
		logger.Warnf("[setting] 读取配置项 %s 失败,按空值处理: %v", key, err)
	}
	return infra.DecryptSecret(v)
}

// LoadSettings 一次性读取全部配置,返回 map(敏感项同样透明解密)。
// 用于需要同时读取多项配置的场景(如金额重算),避免逐项 GetSetting 造成的多次查库。
// 读取失败时返回部分/空 map 并记 warn:调用方无法区分「读失败」与「配置为空」,
// 金额计算等资损敏感路径须能从日志追溯到这类静默降级。
func LoadSettings() map[string]string {
	out := map[string]string{}
	rows, err := store.DB.Query(`SELECT cfg_key, cfg_value FROM tb_config`)
	if err != nil {
		logger.Warnf("[setting] 读取配置表失败,按空配置处理: %v", err)
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			logger.Warnf("[setting] 读取配置行失败,已跳过: %v", err)
			continue
		}
		out[k] = infra.DecryptSecret(v)
	}
	if err := rows.Err(); err != nil {
		logger.Warnf("[setting] 读取配置表中断,结果可能不完整: %v", err)
	}
	return out
}

// SettingsFingerprint 返回指定键集的配置内容指纹(键排序后拼接取 SHA-256 前 16 位 hex),
// 用作配置保存的乐观锁:SettingList 下发,SettingSave 带回校验,不一致说明库已被
// 其他人改过,拒绝旧快照的全量覆盖。
//
// 指纹基于解密后的明文,不受加密层影响(密钥轮换/随机 nonce 不改变指纹);
// 键集由调用方指定,只覆盖配置页管理的键——agent_last_seen(代理心跳,秒级变化)、
// admin_pass_hash 等内部键的变化不应把管理员的保存误判为并发冲突。
func SettingsFingerprint(keys []string) string {
	cfg := LoadSettings()
	sorted := append([]string(nil), keys...)
	sort.Strings(sorted)
	h := sha256.New()
	for _, k := range sorted {
		h.Write([]byte(k))
		h.Write([]byte{0})
		h.Write([]byte(cfg[k]))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// HasNonEmptyDefault 报告该配置项在出厂默认值中是否为非空。
// 用于保存校验:出厂默认非空却被保存为空,通常是「少东西」的前兆。
func HasNonEmptyDefault(key string) bool {
	return store.SettingDefault(key) != ""
}

// DefaultValue 返回配置项出厂默认值(不存在时返回空串)。
func DefaultValue(key string) string {
	return store.SettingDefault(key)
}
