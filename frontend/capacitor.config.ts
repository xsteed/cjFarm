import type { CapacitorConfig } from '@capacitor/cli';

// ============================================================================
// 安卓壳（Capacitor）配置
//
// 定位：这是「远程 URL 壳」，不是把 dist 打进 APK 的离线包。
// WebView 直接加载线上前端地址，页面与 /api、/uploads 同源，因此：
//   - 前端 baseURL('/api') 无需改造
//   - 不涉及 CORS（同源请求）
//   - 路由 createWebHistory 由 Nginx 的 SPA 回退兜底
//   - 前端更新后刷新即生效，不用重新发 APK
//
// 地址在构建期注入，改 IP 不需要动代码：
//   SERVER_URL=http://111.230.154.50 bash deploy/build-android-shell.sh
//
// 注意：server.url 会取代本地 webDir 资源。webDir 仍要存在（cap sync 会校验），
// 因此构建脚本在 dist 缺失时会先跑一次 npm run build。
// ============================================================================

/** 去掉末尾斜杠，避免拼出 `http://host//` 这种地址。 */
function normalize(raw: string): string {
  return raw.trim().replace(/\/+$/, '');
}

const serverUrl = normalize(process.env.SERVER_URL || 'http://111.230.154.50');

const config: CapacitorConfig = {
  appId: 'com.cjfarm.dining',
  appName: '点餐管理',
  webDir: 'dist',
  server: {
    url: serverUrl,
    // 生产环境是纯 IP + HTTP，必须放行明文；否则 WebView 报 ERR_CLEARTEXT_NOT_PERMITTED。
    // 该开关会让 cap sync 在 AndroidManifest 里写入 android:usesCleartextTraffic="true"。
    cleartext: serverUrl.startsWith('http://')
  },
  android: {
    // 页面本身是 http，若日后前端换 https 而图片仍走 http，需要这个开关避免混合内容拦截。
    allowMixedContent: true
  }
};

export default config;
