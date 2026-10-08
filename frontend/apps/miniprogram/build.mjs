// ============================================================================
// 小程序构建脚本（esbuild）
//
// 为什么需要构建：原生小程序不能直接跑 TS，也不能在运行时解析 node_modules 里的
// 裸包名（`@dining/api`、`axios`）。这里用 esbuild 把 `src/**/*.ts` 各自打成
// 自包含的 CommonJS 文件，并把 `@dining/api`、`@dining/shared`、`@dining/types`、
// `axios` 一并内联进产物，微信开发者工具直接指向 `dist/` 即可，无需「构建 npm」。
//
// 产物布局：src 的相对结构原样保留（src/pages/menu/index.ts → dist/pages/menu/index.js），
// 同时把 .wxml/.wxss/.json/.png 等非 TS 文件按同结构拷过去。
//
// 为什么是 CommonJS 而不是 ESM+splitting：CJS 是小程序兼容性最好的模块形态；
// 代价是共享代码会在每个入口各打一份（见 docs/miniprogram.md 5.3 节）。
// ============================================================================
import { build, context } from 'esbuild';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = path.dirname(fileURLToPath(import.meta.url));
const SRC = path.join(ROOT, 'src');
const DIST = path.join(ROOT, 'dist');
const watch = process.argv.includes('--watch');

/** 递归收集 src 下的所有文件（相对路径）。 */
function walk(dir, out = []) {
  for (const e of fs.readdirSync(path.join(SRC, dir), { withFileTypes: true })) {
    const rel = dir ? `${dir}/${e.name}` : e.name;
    if (e.isDirectory()) walk(rel, out);
    else out.push(rel);
  }
  return out;
}

/**
 * 入口 = app + 每个页面的 index.ts + 每个组件的 index.ts。
 *
 * 只把「入口」交给 esbuild，而不把 src 下所有 .ts 都当入口：后者会把
 * store/cart.ts、api/client.ts 这类模块**额外再输出一份**（它们已经内联进各页面了），
 * 那些文件没有任何 require 指向它们，纯属让代码包白白变大。
 */
function entryPoints() {
  const files = walk('').filter(f => f.endsWith('.ts'));
  const roots = files.filter(f => f === 'app.ts' || /^(pages|components)\/[^/]+\/index\.ts$/.test(f));
  if (roots.length < 2) throw new Error('没有找到入口文件，检查 src/ 结构（app.ts / pages/*/index.ts）');
  return { roots, files };
}

/** 把非 TS 资源（wxml/wxss/json/图片等）拷到 dist，保持目录结构。 */
function copyAssets() {
  let count = 0;
  for (const rel of walk('')) {
    if (rel.endsWith('.ts')) continue;
    const dest = path.join(DIST, rel);
    fs.mkdirSync(path.dirname(dest), { recursive: true });
    fs.copyFileSync(path.join(SRC, rel), dest);
    count++;
  }
  return count;
}

const { roots } = entryPoints();

const options = {
  entryPoints: roots.map(f => path.join(SRC, f)),
  outdir: DIST,
  // outbase=src → 产物路径与 src 相对结构一致（页面/组件目录名不能变）
  outbase: SRC,
  bundle: true,
  format: 'cjs',
  platform: 'browser',
  target: 'es2019',
  minify: true,
  sourcemap: false,
  logLevel: 'info',
  // 不保留 node 风格的注释/注释剥离交给 minify；下面这行让产物里保留许可证注释
  legalComments: 'none'
};

if (watch) {
  const ctx = await context(options);
  await ctx.watch();
  fs.watch(SRC, { recursive: true }, () => {
    const n = copyAssets();
    console.log(`[assets] 同步 ${n} 个非 TS 文件`);
  });
  console.log('[watch] 监听中（Ctrl+C 退出）…');
} else {
  fs.rmSync(DIST, { recursive: true, force: true });
  await build(options);
  const n = copyAssets();
  console.log(`\n✓ 小程序产物已生成：dist/（含 ${n} 个非 TS 资源）`);
  console.log('  用微信开发者工具「导入项目」指向 apps/miniprogram（miniprogramRoot=dist/）。');
}
