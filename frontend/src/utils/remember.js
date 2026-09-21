// 「记住我(免登录)」本地存储
//
// 只存后端签发的不透明令牌(rememberToken),绝不存账号密码。
// 令牌是否过期完全由后端查库裁决(见后端 tb_remember_token.expire_time),
// 前端不做任何本地过期判断 —— 打开页面时拿令牌去 /auth/remember-login 校验,
// 过期/失效后端直接返 401,前端再清掉本地令牌回到登录页。窗口控制权完全在服务端,
// 前端无法伪造或篡改有效期(此前用 localStorage 时间戳本地判断的做法已被取代)。

const LS_REMEMBER = 'admin_remember'

export function saveRemember(token) {
  localStorage.setItem(LS_REMEMBER, token)
}

export function getRemember() {
  const t = localStorage.getItem(LS_REMEMBER)
  return t && t.length > 0 ? t : null
}

export function clearRemember() {
  localStorage.removeItem(LS_REMEMBER)
}
