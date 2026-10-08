import { createRouter, createWebHistory } from 'vue-router';
import { adminRoutes, firstAccessiblePath, NO_PERMISSION_PATH } from '@dining/app-admin';
import { customerRoutes } from '@dining/app-customer';
import { clearAuth, hasPerm, hasToken, permsLoaded, refreshProfile, tokenExpired } from '@dining/auth';

// 路由 meta 的统一类型声明（模块增强，全局生效）。菜单、守卫与 AdminLayout 共用。
declare module 'vue-router' {
  interface RouteMeta {
    title?: string;
    perm?: string;
    icon?: string;
    menuOrder?: number;
    /** 菜单分组 key(见 apps/admin/src/menu.ts 的 MENU_GROUPS):同组的菜单收进一个可折叠子菜单;不填即一级菜单。 */
    group?: string;
    requiresAuth?: boolean;
  }
}

// ============================================================================
// Web 单页应用的路由装配点
//
// 这里只做「把两端路由拼起来 + 全局守卫」,不再承载具体页面或菜单定义:
//   - 顾客点餐/打印票据 → @dining/app-customer(公开访问)
//   - 登录 + 管理后台     → @dining/app-admin (需登录 + 权限点)
// 菜单与默认落地页的推导在 @dining/app-admin/menu(纯函数,入参为路由表),
// 因此本文件不再被「菜单」反向依赖,也就不存在 seed 循环。
//
// 未来接小程序时,只需新增一个 apps/miniprogram 复用 packages/*,
// 本文件(Web 版装配)保持不动。
// ============================================================================
const router = createRouter({
  history: createWebHistory(),
  routes: [...customerRoutes, ...adminRoutes]
});

// 全局守卫:
//   1) 管理端需登录 → 未登录跳登录页(带上 redirect 便于登录后回到原页面);
//   2) 权限未加载(localStorage 缺失)"或本次会话首次进入" → 拉一次自己的权限,
//      这样商户改过某人的角色后,该员工刷新页面即可收敛,不会一直看到过期菜单;
//   3) 目标页面超出权限 → 落到「第一个有权限的菜单」,一个都没有才显示无权限页。
//
// 这样即便员工手敲了没有权限的地址,看到的也是自己能用的页面,而不是功能入口一片空白。
let profileRefreshed = false;

router.beforeEach(async to => {
  const requiresAuth = to.matched.some(r => r.meta.requiresAuth);

  // 令牌缺失、或本地即可判定已过期(解析令牌 payload 的 exp,见 @dining/auth 的 tokenExpired):
  // 直接在 SPA 内带 redirect 去登录页。否则过期令牌会先进页面、发请求撞 401、
  // 被拦截器整页弹回登录页,白绕一圈;这里一次跳转就位,「记住我」静默换发后
  // 还能凭 redirect 回到原页面。服务端侧失效(改密/停用/后端重启)仍走 401 兜底。
  if (requiresAuth && (!hasToken() || tokenExpired())) {
    return { path: '/login', query: { redirect: to.fullPath } };
  }
  if (to.path === '/login') {
    // 本地令牌已过期时别送去 dashboard —— 那里第一次请求就会 401,
    // 被拦截器整页弹回登录页,白绕一圈;留在登录页让「记住我」静默换发
    // (若令牌仍有效)或手动登录接手。
    return hasToken() && !tokenExpired() ? '/dining/dashboard' : true;
  }
  if (!requiresAuth) {
    return true;
  }

  // 本次会话首次进入管理端时刷新一次权限;之后沿用缓存,避免每次跳转都打接口。
  if (!permsLoaded() || !profileRefreshed) {
    profileRefreshed = true;
    try {
      await refreshProfile();
    } catch {
      // 401 已由 axios 拦截器统一处理(清登录态并跳登录页);
      // 其它错误(网络抖动等)只要本地还有缓存就继续用,不把人踢出去。
      if (!permsLoaded()) {
        clearAuth();
        return { path: '/login', query: { redirect: to.fullPath } };
      }
    }
  }

  const perm = to.meta.perm;
  if (perm && !hasPerm(perm)) {
    const fallback = firstAccessiblePath(router.getRoutes());
    return { path: fallback === to.path ? NO_PERMISSION_PATH : fallback };
  }
  return true;
});

router.afterEach(to => {
  if (to.meta.title) {
    document.title = `${to.meta.title} - 扫码点餐管理系统`;
  }
});

export default router;
