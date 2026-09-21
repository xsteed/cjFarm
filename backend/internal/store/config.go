package store

// GetCfg 读取单个配置项。敏感项(cipher 存储)在此透明解密,调用方无感知。
func GetCfg(key string) string {
	var v string
	DB.QueryRow(`SELECT cfg_value FROM tb_config WHERE cfg_key=?`, key).Scan(&v)
	return DecryptSecret(v)
}

// LoadConfig 一次性读取全部配置,返回 map(敏感项同样透明解密)。
// 用于需要同时读取多项配置的场景(如金额重算),避免逐项 GetCfg 造成的多次查库。
func LoadConfig() map[string]string {
	out := map[string]string{}
	rows, err := DB.Query(`SELECT cfg_key, cfg_value FROM tb_config`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		rows.Scan(&k, &v)
		out[k] = DecryptSecret(v)
	}
	return out
}

// HasNonEmptyDefault 报告该配置项在出厂默认值中是否为非空。
// 用于保存校验:出厂默认非空却被保存为空,通常是「少东西」的前兆。
func HasNonEmptyDefault(key string) bool {
	v, ok := cfgDefaults[key]
	return ok && v != ""
}

// DefaultValue 返回配置项出厂默认值(不存在时返回空串)。
func DefaultValue(key string) string {
	return cfgDefaults[key]
}
