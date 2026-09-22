<template>
  <div class="login-wrap">
    <div class="login-card">
      <div class="login-logo">点</div>
      <div class="login-title">扫码点餐管理系统</div>
      <div class="login-sub">管理端登录</div>

      <div class="field">
        <label>用户名</label>
        <input v-model="form.username" placeholder="请输入用户名" autocomplete="username" />
      </div>
      <div class="field">
        <label>密码</label>
        <div class="pwd-wrap">
          <input
            v-model="form.password"
            :type="showPassword ? 'text' : 'password'"
            placeholder="请输入密码"
            autocomplete="current-password"
            @keyup.enter="onSubmit"
          />
          <button
            type="button"
            class="pwd-toggle"
            :aria-label="showPassword ? '隐藏密码' : '显示密码'"
            :title="showPassword ? '隐藏密码' : '显示密码'"
            @mousedown.prevent
            @click="showPassword = !showPassword"
          >
            <browse-off-icon v-if="showPassword" />
            <browse-icon v-else />
          </button>
        </div>
      </div>

      <div class="remember">
        <label class="remember-check">
          <input type="checkbox" v-model="remember" />
          <span>记住我（免登录）</span>
        </label>
        <div class="remember-days" :class="{ off: !remember }">
          <label :class="{ active: rememberDays === 7 }">
            <input type="radio" :value="7" v-model="rememberDays" :disabled="!remember" />
            <span>7 天</span>
          </label>
          <label :class="{ active: rememberDays === 30 }">
            <input type="radio" :value="30" v-model="rememberDays" :disabled="!remember" />
            <span>30 天</span>
          </label>
        </div>
      </div>

      <button class="login-btn" :disabled="loading" @click="onSubmit">
        <span v-if="loading" class="spin light"></span>
        {{ loading ? '登录中…' : '登 录' }}
      </button>
    </div>
  </div>
</template>

<script setup>
import { reactive, ref, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { MessagePlugin } from 'tdesign-vue-next'
import { BrowseIcon, BrowseOffIcon } from 'tdesign-icons-vue-next'
import { login, rememberLogin } from '../api'
import { setAuth } from '../utils/perm'
import { firstAccessiblePath } from '../router'
import { getRemember, saveRemember, clearRemember } from '../utils/remember'

const router = useRouter()
const route = useRoute()
const form = reactive({ username: '', password: '' })
const loading = ref(false)
const remember = ref(false)
const rememberDays = ref(7)
// 密码明文/密文切换:默认密文,由右侧小眼睛图标控制。
const showPassword = ref(false)
// 用户是否已手动提交登录。一旦提交,自动登录的返回结果就要丢弃,
// 否则两个请求先后回到时,后到的那个会把登录态覆盖成另一个账号。
let manualSubmit = false

// 只接受站内路径。redirect 来自 URL query,不校验的话 "//evil.com"
// 会被浏览器当作协议相对地址,配合下面的整页跳转就构成开放重定向。
function safeRedirect() {
  const raw = String(route.query.redirect || '')
  return raw.startsWith('/') && !raw.startsWith('//') ? raw : firstAccessiblePath()
}

// 写入登录态并跳转到落地页(被守卫踹来登录页时带 redirect 优先回原页)。
async function enter(res) {
  // 一次性写入 token/姓名/角色/权限:写入后 permSet 立即可用,
  // 下面的 firstAccessiblePath() 才能正确算出该账号的落地页。
  setAuth(res)
  // 若来时是被守卫从某个页面踹到登录页的(带 redirect),优先回原页面;
  // 否则落到「第一个有权限的菜单」——收银员没有 report:view 也能直接进订单页,
  // 不必先撞一次权限守卫再被弹走。
  const redirect = safeRedirect()
  try {
    await router.replace(redirect)
  } catch {
    // 跳转失败最常见的原因是:发版后浏览器(尤其微信内置浏览器)仍在用缓存的旧入口,
    // 它引用的懒加载 chunk 已被 rsync --delete 删除,动态 import 失败 ——
    // 此时页面停在登录页且没有任何提示,用户看到的就是「点了登录没反应」。
    // 整页跳转可强制浏览器重新拉取最新入口,从根上跳出旧的缓存世界。
    window.location.replace(redirect)
  }
}

async function onSubmit() {
  if (!form.username || !form.password) {
    MessagePlugin.warning('请输入用户名和密码')
    return
  }
  manualSubmit = true
  loading.value = true
  try {
    // 勾选「记住我」时把 remember / days 一并发给后端,由后端签发并回传令牌。
    const res = await login({
      username: form.username,
      password: form.password,
      remember: remember.value,
      days: rememberDays.value,
    })
    // 仅当本次启用了记住我且后端成功回传令牌时才落本地;否则清掉旧令牌。
    if (remember.value && res.rememberToken) {
      saveRemember(res.rememberToken)
    } else {
      clearRemember()
    }
    await enter(res)
  } catch (e) {
    // 网络/业务错误已由全局拦截器弹过提示(带 friendlyMessage 标记),这里不重复打扰;
    // 剩下的是拦截器管不到的本地异常(存储写入失败 / 路由跳转失败),必须给出反馈,
    // 否则用户看到的就是「点了登录没反应」。
    if (!e?.friendlyMessage) MessagePlugin.error('登录失败，请刷新页面后重试')
  } finally {
    loading.value = false
  }
}

// 挂载时若本地存有「记住我」令牌,拿它去后端静默换发登录态,实现免登录。
// 过期/失效由后端查库裁决:后端返 401 即清掉本地令牌并留在登录页。
//
// 注意这里刻意不碰 loading:自动登录若占用 loading,移动网络下请求稍慢就会把
// 登录按钮置为 disabled,用户点击毫无反应(只能干等),这正是「手机登不上」的常见成因。
async function autoLogin() {
  const token = getRemember()
  if (!token) return
  try {
    const res = await rememberLogin({ rememberToken: token })
    // 用户已经手动登录过了(可能换了账号),丢弃这次静默结果,别覆盖登录态。
    if (manualSubmit) return
    await enter(res)
  } catch {
    // 令牌已过期 / 账号状态变更:清掉本地令牌,回到手动登录。
    clearRemember()
  }
}

onMounted(() => {
  autoLogin()
})
</script>

<style scoped>
.login-wrap {
  height: 100vh;
  height: 100dvh;
  box-sizing: border-box;
  padding: 20px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(135deg, #fff4ec 0%, #ffe3d4 100%);
  position: relative;
  overflow: hidden;
}
.login-wrap::before {
  content: '';
  position: absolute;
  width: 560px;
  height: 560px;
  border-radius: 50%;
  background: radial-gradient(circle, rgba(255, 107, 53, 0.18), transparent 70%);
  top: -140px;
  left: -100px;
}
.login-wrap::after {
  content: '';
  position: absolute;
  width: 420px;
  height: 420px;
  border-radius: 50%;
  background: radial-gradient(circle, rgba(240, 72, 31, 0.12), transparent 70%);
  bottom: -140px;
  right: -80px;
}

.login-card {
  position: relative;
  width: 380px;
  /* 窄屏(<=360px 的机型)下不能横向溢出,交给 max-width 收敛 */
  max-width: 100%;
  background: #fff;
  border-radius: 20px;
  padding: 38px 36px 32px;
  box-shadow: 0 16px 48px rgba(240, 72, 31, 0.18);
  text-align: center;
}
.login-logo {
  width: 58px;
  height: 58px;
  border-radius: 16px;
  background: var(--grad-brand);
  color: #fff;
  font-size: 26px;
  font-weight: 800;
  display: flex;
  align-items: center;
  justify-content: center;
  margin: 0 auto 16px;
  box-shadow: 0 8px 20px rgba(240, 72, 31, 0.32);
}
.login-title {
  font-size: 20px;
  font-weight: 800;
  color: var(--ink);
}
.login-sub {
  font-size: 12.5px;
  color: var(--ink-3);
  margin: 6px 0 26px;
}

.field {
  text-align: left;
  margin-bottom: 16px;
}
.field label {
  font-size: 12.5px;
  color: var(--ink-2);
  font-weight: 600;
  display: block;
  margin-bottom: 7px;
}
.field input {
  width: 100%;
  box-sizing: border-box;
  height: 46px;
  border: 1.5px solid var(--line);
  border-radius: 12px;
  padding: 0 15px;
  font-size: 14px;
  color: var(--ink);
  background: #fafafa;
  outline: none;
  transition: all 0.15s;
}
.field input:focus {
  border-color: var(--brand);
  background: #fff;
  box-shadow: 0 0 0 3px var(--brand-soft);
}

/* 密码框:右侧留出图标位,避免文字与图标重叠 */
.pwd-wrap {
  position: relative;
}
.pwd-wrap input {
  padding-right: 46px;
}
.pwd-toggle {
  position: absolute;
  right: 5px;
  top: 50%;
  transform: translateY(-50%);
  width: 36px;
  height: 36px;
  padding: 0;
  border: none;
  border-radius: 9px;
  background: transparent;
  color: var(--ink-3);
  font-size: 18px;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: color 0.15s, background 0.15s;
}
.pwd-toggle:hover {
  color: var(--brand);
  background: var(--brand-soft);
}

.login-btn {
  width: 100%;
  height: 48px;
  border: none;
  border-radius: 12px;
  background: var(--grad-brand-soft);
  color: #fff;
  font-size: 16px;
  font-weight: 700;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  margin-top: 10px;
  box-shadow: 0 8px 22px rgba(240, 72, 31, 0.32);
  cursor: pointer;
  transition: all 0.15s;
}
.login-btn:hover {
  background: var(--grad-brand);
  box-shadow: 0 10px 26px rgba(240, 72, 31, 0.4);
}
.login-btn:disabled {
  opacity: 0.7;
  cursor: not-allowed;
}

.remember {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin: -2px 0 18px;
  font-size: 12.5px;
  color: var(--ink-2);
}
.remember-check {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  cursor: pointer;
  user-select: none;
}
.remember-check input {
  width: 15px;
  height: 15px;
  accent-color: var(--brand);
  cursor: pointer;
}
.remember-days {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  transition: opacity 0.15s;
}
.remember-days.off {
  opacity: 0.4;
  pointer-events: none;
}
.remember-days label {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 4px 9px;
  border: 1.5px solid var(--line);
  border-radius: 9px;
  cursor: pointer;
  color: var(--ink-3);
  transition: all 0.15s;
}
.remember-days label.active {
  border-color: var(--brand);
  background: var(--brand-soft);
  color: var(--brand-deep);
  font-weight: 600;
}
.remember-days input {
  width: 14px;
  height: 14px;
  accent-color: var(--brand);
  cursor: pointer;
}

.login-tip {
  font-size: 11.5px;
  color: var(--ink-4);
  margin-top: 18px;
}

.spin {
  width: 15px;
  height: 15px;
  border: 2px solid rgba(255, 255, 255, 0.4);
  border-top-color: #fff;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

/* 移动端:收紧留白,让卡片在 390px 屏上不至于顶满 */
@media (max-width: 767px) {
  .login-wrap {
    padding: 16px;
  }
  .login-card {
    padding: 28px 22px 24px;
    border-radius: 16px;
  }
  .login-logo {
    width: 50px;
    height: 50px;
    font-size: 22px;
    border-radius: 14px;
    margin-bottom: 12px;
  }
  .login-title {
    font-size: 18px;
  }
  .login-sub {
    margin: 6px 0 20px;
  }
  .field input {
    height: 44px;
  }
}
</style>
