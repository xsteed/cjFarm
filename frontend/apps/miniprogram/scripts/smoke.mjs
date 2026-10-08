// ============================================================================
// 产物冒烟测试
//
// 在**没有 window / document 的宿主环境**里逐个执行 dist 下的 JS 产物。
// 它要回答的问题是：这份代码包在真机（小程序 JSCore，同样没有 DOM）里能不能加载？
//
// 为什么必须有：packages/* 是按 Web 端写的，接口层虽然已把提示/跳转抽成可注入的
// 适配器，但「有没有哪个模块在顶层碰了 DOM」靠人眼审查不可靠；类型检查也指望不上
// （tsconfig 为了能检查 packages 不得不打开 DOM lib）。这里用一个最小的小程序全局桩
// 跑一遍，能在构建期就拦住这类回归 —— 比类型检查更硬的保证。
//
// 实现细节：产物是 CommonJS，但本目录 package.json 是 "type": "module"，
// Node 会拒绝用 require 加载 .js；而且真机也不是 Node 的模块系统。因此这里用
// vm 以「模块包装函数」的形式直接执行源码，最接近小程序加载器的行为。
//
// 用法：npm run smoke（会先 build）
// ============================================================================
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';

const ROOT = path.dirname(path.dirname(fileURLToPath(import.meta.url)));
const DIST = path.join(ROOT, 'dist');
const require = createRequire(import.meta.url);

const failures = [];

// ---- 0) 先把宿主里「真机上不存在」的全局清掉，否则测不出东西 ----
// 注意 Node 18+ 自带 fetch，必须主动删掉，否则「小程序里误用 fetch」会漏检。
for (const g of ['fetch', 'XMLHttpRequest', 'localStorage', 'sessionStorage', 'document', 'window']) {
  try {
    delete globalThis[g];
  } catch {
    /* 某些版本不可删除，下面的一致性检查会报出来 */
  }
}
for (const g of ['window', 'document', 'localStorage', 'sessionStorage', 'XMLHttpRequest']) {
  if (typeof globalThis[g] !== 'undefined') {
    failures.push(`宿主环境里仍存在全局 ${g}，冒烟结果不可信`);
  }
}

// ---- 1) 最小的小程序全局桩 ----
// wx 用 Proxy：任何属性访问都返回空实现，这样「漏桩」不会伪装成失败，
// 只有真正引用了不存在的全局（如 window）才会抛错。
globalThis.wx = new Proxy({}, { get: () => () => undefined });

let appInstance = null;
const counts = { page: 0, component: 0 };

globalThis.App = options => {
  // 小程序里 App 实例就是传入的 options 本身（与 miniprogram-api-typings 的
  // Instance<T> = Option & T 一致）
  appInstance = options;
};
globalThis.getApp = () => appInstance;
globalThis.Page = () => {
  counts.page += 1;
};
globalThis.Component = () => {
  counts.component += 1;
};
globalThis.getCurrentPages = () => [];

// ---- 2) 以 CommonJS 包装函数的形式执行产物 ----
function loadAsCommonJs(file) {
  const code = fs.readFileSync(file, 'utf8');
  const module = { exports: {} };
  const wrapper = vm.runInThisContext(`(function (module, exports, require, __filename, __dirname) {\n${code}\n})`, {
    filename: file
  });
  wrapper(module, module.exports, require, file, path.dirname(file));
  return module.exports;
}

function collect(dir, out = []) {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) collect(p, out);
    else if (e.name.endsWith('.js')) out.push(p);
  }
  return out;
}

if (!fs.existsSync(DIST)) {
  console.error('dist/ 不存在，请先执行 npm run build');
  process.exit(1);
}

// app.js 必须先执行：它负责把 axios 实例装配到 wx.request（见 src/api/client.ts）
const files = collect(DIST).sort((a, b) => {
  const score = f => (f.endsWith(`${path.sep}app.js`) ? 0 : 1);
  return score(a) - score(b) || a.localeCompare(b);
});

let loaded = 0;
for (const file of files) {
  const rel = path.relative(DIST, file);
  try {
    loadAsCommonJs(file);
    loaded += 1;
  } catch (e) {
    failures.push(`${rel}: ${e && e.message ? e.message : String(e)}`);
  }
}

// ---- 3) 断言 ----
if (!appInstance) failures.push('app.js 没有调用 App()');
if (appInstance && !appInstance.session) failures.push('App 实例上缺少跨页状态 session');
if (appInstance && !Array.isArray(appInstance.cart)) failures.push('App 实例上缺少跨页状态 cart');

const pageFiles = files.filter(f => f.includes(`${path.sep}pages${path.sep}`)).length;
const compFiles = files.filter(f => f.includes(`${path.sep}components${path.sep}`)).length;
if (counts.page !== pageFiles) failures.push(`页面产物 ${pageFiles} 个，但只有 ${counts.page} 个调用了 Page()`);
if (counts.component !== compFiles) {
  failures.push(`组件产物 ${compFiles} 个，但只有 ${counts.component} 个调用了 Component()`);
}

console.log(`冒烟：执行 ${loaded}/${files.length} 个产物（页面 ${counts.page} / 组件 ${counts.component}）`);
if (failures.length) {
  console.error('\n✗ 冒烟失败：');
  for (const f of failures) console.error(`  - ${f}`);
  process.exit(1);
}
console.log('✓ 冒烟通过：产物在不含 DOM 的宿主里可加载，且页面/组件注册完整');
