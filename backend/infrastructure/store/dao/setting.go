package dao

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"dining-system/infrastructure"
	"dining-system/infrastructure/logger"
	"dining-system/infrastructure/store"
)

// settingQueryCount 累计 GetSetting 真正发起的 SQL 次数(缓存命中不计)。
//
// 只为「一次打印不该把同一份配置查几十遍」这类断言与排障服务:生产路径上只多一次原子加,
// 可忽略。配置读取散落在渲染、落地、适配器多层,没有这个数字就只能靠读代码估算,
// 而估算恰恰最容易漏掉「按行调用」的那种调用点(见 BoldLine 的历史实现)。
var settingQueryCount int64

// SettingQueries 返回进程内累计的配置查询次数(测试与诊断用)。
func SettingQueries() int64 { return atomic.LoadInt64(&settingQueryCount) }

// ResetSettingQueries 清零计数器。配合分阶段跑,可把「一次下单查了多少次配置」
// 拆到具体环节上,而不必凭代码估算。
func ResetSettingQueries() { atomic.StoreInt64(&settingQueryCount, 0) }

// ============================================================================
// 配置缓存
// ============================================================================
//
// 为什么缓存:GetSetting 是全系统读得最频繁的函数之一(打印渲染、开关判断、心跳
// 回退……),此前「每次调用都真查库」。单行主键查询本身不慢,但调用点一旦混进
// 按行/按票的循环就放大成上百次单行查询(实测一菜一单 150 次/单)。
//
// 为什么敢缓存,以及时效承诺:
//   - 本进程写入统一走 SetSetting,写穿失效 —— 管理后台保存**立即生效**;
//   - 多实例部署下另一实例的写入,由短 TTL 兜底,最多延迟 TTL 才可见
//     (受影响的只有跨实例保存配置的那几秒,业务上可接受);
//   - 启动期的迁移/种子/密钥轮换都在第一次读取之前完成,且 store.Init 换库句柄时
//     缓存整体作废(测试每个用例各建一个库,正是靠这一点保持隔离)。
//
// 什么时候不能用缓存:SettingsFingerprint 走 LoadSettings 直查库 —— 配置保存的
// 乐观锁必须看到最新的库内容,吃了缓存就会漏判并发保存。
var (
	settingCacheMu  sync.RWMutex
	settingCache    = map[string]settingCacheEntry{}
	settingCacheDB  *sql.DB // 缓存所属的库句柄:store.Init 换库(重启/测试)即整体作废
	settingCacheTTL = 5 * time.Second
)

type settingCacheEntry struct {
	value string
	at    time.Time
}

// InvalidateSettingCache 清空配置缓存。
//
// 常规写入不需要手动调 —— SetSetting 自己会失效。需要它的只有「绕过 dao 直接改
// tb_config」的场合:启动迁移(store 包,不能反向 import dao,但那时缓存必然为空)、
// 以及测试里用裸 SQL 改写/删除配置行之后。
func InvalidateSettingCache() {
	settingCacheMu.Lock()
	settingCache = map[string]settingCacheEntry{}
	settingCacheMu.Unlock()
}

// cachedSetting 返回缓存命中的值;未命中(无记录/过期/换了库句柄)返回 false。
func cachedSetting(key string) (string, bool) {
	settingCacheMu.RLock()
	db, e := settingCacheDB, settingCache[key]
	settingCacheMu.RUnlock()
	if db == nil || db != store.DB || time.Since(e.at) >= settingCacheTTL {
		return "", false
	}
	return e.value, true
}

// storeSettingCache 把一次真实查询的结果写进缓存。
func storeSettingCache(key, value string) {
	settingCacheMu.Lock()
	if settingCacheDB != store.DB {
		settingCache = map[string]settingCacheEntry{} // 换了库句柄,旧库的缓存整体作废
		settingCacheDB = store.DB
	}
	settingCache[key] = settingCacheEntry{value: value, at: time.Now()}
	settingCacheMu.Unlock()
}

// GetSetting 读取单个配置项(带进程内缓存)。敏感项(cipher 存储)在此透明解密,
// 缓存存的是解密后的明文,调用方无感知。
// 键不存在时返回空串(ErrNoRows 属正常情况,不告警);读取失败同样返回空串,
// 但记一条 warn——调用方(如金额计算)把「读不到」按「未开启」处理,
// 没有日志的话这类静默降级将无从排查。
//
// 时效:本进程的写入立即生效(SetSetting 写穿失效);其它实例的写入最多延迟
// settingCacheTTL 可见。对时效敏感的批量读取请走 LoadSettings(直查库)。
func GetSetting(key string) string {
	if v, ok := cachedSetting(key); ok {
		return v
	}
	atomic.AddInt64(&settingQueryCount, 1)
	var v string
	if err := store.DB.QueryRow(`SELECT cfg_value FROM tb_config WHERE cfg_key=?`, key).Scan(&v); err != nil && !errors.Is(err, sql.ErrNoRows) {
		logger.Warnf("[setting] 读取配置项 %s 失败,按空值处理: %v", key, err)
	}
	value := infrastructure.DecryptSecret(v)
	storeSettingCache(key, value)
	return value
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
		out[k] = infrastructure.DecryptSecret(v)
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
