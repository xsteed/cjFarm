// 敏感配置加密存储。
//
// 设计要点:
//   - 本机主密钥(master key)为 32 字节,来源优先级:
//     CONFIG_MASTER_KEY 环境变量 > MASTER_KEY_PATH 指定文件 > 默认 ./data/master.key(不存在则自动生成);
//   - 敏感配置项(tb_config 表)落库前用 AES-256-GCM 加密,密文带 enc:v1: 前缀;
//   - GetSetting/SetSetting 透明加解密,业务层无感知;历史明文值在启动时自动迁移为密文;
//   - 主密钥缺失时降级为明文存储并告警,不阻断服务启动(避免升级即不可用)。
package dao

import (
	"dining-system/infra"
	"dining-system/infra/logger"
	"dining-system/internal/store"
)

// SetSetting 写入单个配置项,敏感项自动加密。
// 语句由方言层生成:SQLite 为 INSERT OR REPLACE,MySQL 为 REPLACE(按主键整体替换)。
func SetSetting(key, value string) error {
	if infra.IsSensitiveKey(key) {
		value = infra.EncryptSecret(value)
	}
	_, err := store.DB.Exec(store.InsertReplaceInto("tb_config", "cfg_key", "cfg_value"), key, value)
	return err
}

// SetSettingIfEmpty 仅当配置项当前为空时写入(幂等回填),已有值绝不覆盖。
// 用于环境变量预置初始值(如 AGENT_TOKEN):本地调试 / 容器部署一次配置即可,
// 之后管理后台「系统配置」保存的值始终优先。
func SetSettingIfEmpty(key, value string) error {
	if GetSetting(key) != "" {
		return nil
	}
	return SetSetting(key, value)
}

// RotateSecrets 把旧密钥加密的存量密文改写为当前密钥加密(幂等,可重复执行)。
//
// 场景:master.key 更换或丢失后配置了新密钥,存量密文仍是旧密钥加密的,
// 不轮换的话解密一律返回空,支付/打印代理等功能静默失效。
// 逐项处理:当前密钥能解开的跳过(已轮换过);旧密钥能解开的改写;两者都
// 解不开的告警(密文损坏或旧密钥不符),保留原值、不阻断启动。
// 轮换完成后应移除 MASTER_KEY_OLD,避免旧密钥长期驻留在环境里。
func RotateSecrets(oldKey []byte) {
	if !infra.SecretsEncrypted() || len(oldKey) == 0 {
		return
	}
	rotated := 0
	for _, k := range infra.SensitiveKeys() {
		var stored string
		if err := store.DB.QueryRow(`SELECT cfg_value FROM tb_config WHERE cfg_key=?`, k).Scan(&stored); err != nil {
			continue // 键不存在,无需轮换
		}
		if !infra.IsEncrypted(stored) {
			continue // 明文,交给 MigrateSecrets 处理
		}
		if _, ok := infra.TryDecrypt(nil, stored); ok {
			continue // 已是当前密钥的密文
		}
		plain, ok := infra.TryDecrypt(oldKey, stored)
		if !ok {
			logger.Warnf("[secret] 配置 %s 无法用旧密钥解密,保留原值(密文可能已损坏或旧密钥不符)", k)
			continue
		}
		if _, err := store.DB.Exec(`UPDATE tb_config SET cfg_value=? WHERE cfg_key=?`, infra.EncryptSecret(plain), k); err != nil {
			logger.Warnf("[secret] 配置 %s 轮换重写失败: %v", k, err)
			continue
		}
		rotated++
	}
	if rotated > 0 {
		logger.Infof("[secret] 已用新主密钥重加密 %d 项敏感配置,请移除 MASTER_KEY_OLD", rotated)
	}
}

// MigrateSecrets 把历史明文敏感配置加密回写(幂等,可重复执行)。
func MigrateSecrets() {
	if !infra.SecretsEncrypted() {
		return
	}
	for _, k := range infra.SensitiveKeys() {
		var v string
		if err := store.DB.QueryRow(`SELECT cfg_value FROM tb_config WHERE cfg_key=?`, k).Scan(&v); err != nil {
			continue
		}
		if v == "" || infra.IsEncrypted(v) {
			continue
		}
		if _, err := store.DB.Exec(`UPDATE tb_config SET cfg_value=? WHERE cfg_key=?`, infra.EncryptSecret(v), k); err != nil {
			logger.Warnf("[secret] 配置 %s 加密迁移失败: %v", k, err)
			continue
		}
		logger.Infof("[secret] 历史明文配置 %s 已加密存储", k)
	}
}
