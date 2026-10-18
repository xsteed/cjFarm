// 顾客会员令牌在本地存储里的键名(唯一来源)。
//
// 各端共用这一个键名。小程序历史上用的是 dining_mp_customer_token,统一后旧键被弃用:
// 老用户升级后本地令牌读不到,下一次会员请求会静默重登一次(wx.login 免费无感),
// 而令牌本身只有 2h 有效期,实际影响可忽略。
export const LS_CUSTOMER_TOKEN = 'dining_customer_token';
