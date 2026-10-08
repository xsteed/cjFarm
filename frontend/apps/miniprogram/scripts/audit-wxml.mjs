// ============================================================================
// WXML 一致性审计
//
// 小程序的模板是「字符串里的弱类型绑定」：写错一个字段名不会在 tsc / eslint 阶段报错，
// 只会在真机上渲染成空白。这个脚本用静态分析把三类最容易犯的错挡住：
//   1) {{ xxx }} 引用了 data / properties 里不存在的字段
//   2) bindtap / catchtap 绑定了不存在的方法
//   3) 传给子组件的 kebab-case 属性在组件 properties 里没有对应项
//
// 用法：npm run audit（已包含在 npm run verify 里）
// ============================================================================
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const SRC = path.join(path.dirname(path.dirname(fileURLToPath(import.meta.url))), 'src');

function walk(dir, out = []) {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) walk(p, out);
    else out.push(p);
  }
  return out;
}

/** data 块的顶层键（按 4 空格缩进识别）。 */
function dataKeys(ts) {
  const keys = new Set();
  const m = /\n {2}data:\s*\{([\s\S]*?)\n {2}\}/.exec(ts);
  if (m) {
    for (const line of m[1].split('\n')) {
      const km = /^\s{4}([A-Za-z_$][\w$]*)\s*:/.exec(line);
      if (km) keys.add(km[1]);
    }
  }
  return keys;
}

/** 组件 properties 的键。 */
function propKeys(ts) {
  const keys = new Set();
  const m = /\n {2}properties:\s*\{([\s\S]*?)\n {2}\}/.exec(ts);
  if (m) {
    for (const line of m[1].split('\n')) {
      const km = /^\s{4}([A-Za-z_$][\w$]*)\s*:/.exec(line);
      if (km) keys.add(km[1]);
    }
  }
  return keys;
}

/** 方法名（对象字面量里的 `name(` / `async name(`）。 */
function methodNames(ts) {
  const names = new Set();
  for (const m of ts.matchAll(/^\s{2}(?:async\s+)?([A-Za-z_$][\w$]*)\s*\(/gm)) names.add(m[1]);
  for (const m of ts.matchAll(/^\s{4}(?:async\s+)?([A-Za-z_$][\w$]*)\s*\(/gm)) names.add(m[1]);
  return names;
}

/** WXML 表达式里的关键字/内置，不算字段引用。 */
const RESERVED = new Set(['true', 'false', 'null', 'undefined', 'item', 'index', 'wx', 'Math', 'JSON']);

const files = walk(SRC);
const wxmlFiles = files.filter(f => f.endsWith('.wxml'));
let problems = 0;
let checkedRefs = 0;
let checkedHandlers = 0;

for (const file of wxmlFiles) {
  const rel = path.relative(SRC, file);
  const tsFile = file.replace(/\.wxml$/, '.ts');
  if (!fs.existsSync(tsFile)) {
    console.log(`✗ ${rel}: 缺少同名 .ts`);
    problems++;
    continue;
  }
  const wxml = fs.readFileSync(file, 'utf8');
  const ts = fs.readFileSync(tsFile, 'utf8');
  const known = new Set([...dataKeys(ts), ...propKeys(ts), ...methodNames(ts)]);

  // 1) 数据引用
  const refs = new Set();
  for (const m of wxml.matchAll(/\{\{([^}]*)\}\}/g)) {
    for (const id of m[1].matchAll(/(^|[^.\w$'"`])([A-Za-z_$][\w$]*)/g)) refs.add(id[2]);
  }
  for (const r of [...refs].sort()) {
    if (RESERVED.has(r)) continue;
    checkedRefs++;
    if (!known.has(r)) {
      console.log(`✗ ${rel}: 模板引用了未定义的「${r}」`);
      problems++;
    }
  }

  // 2) 事件处理函数
  for (const m of wxml.matchAll(/\b(?:bind|catch|capture-bind|capture-catch):?([a-zA-Z]+)\s*=\s*"([^"]+)"/g)) {
    checkedHandlers++;
    if (!known.has(m[2])) {
      console.log(`✗ ${rel}: 绑定了不存在的方法「${m[2]}」`);
      problems++;
    }
  }

  // 3) 子组件属性：kebab-case → camelCase 必须在组件的 properties 里
  for (const m of wxml.matchAll(/<([a-z][a-z0-9-]*)\s([^>]*?)\/?>/g)) {
    const tag = m[1];
    if (!tag.includes('-')) continue;
    const compTs = path.join(SRC, 'components', tag, 'index.ts');
    if (!fs.existsSync(compTs)) continue;
    const compProps = propKeys(fs.readFileSync(compTs, 'utf8'));
    for (const a of m[2].matchAll(/(?:^|\s)([a-z][a-z0-9-]*)\s*=/g)) {
      const attr = a[1];
      if (attr.startsWith('bind') || attr.startsWith('catch') || attr.startsWith('wx:')) continue;
      const camel = attr.replace(/-([a-z])/g, (_, c) => c.toUpperCase());
      if (!compProps.has(camel)) {
        console.log(`✗ ${rel}: <${tag} ${attr}="..."> 在组件里没有对应属性（期待 ${camel}）`);
        problems++;
      }
    }
  }
}

console.log(`WXML 审计：${wxmlFiles.length} 个模板 / 校验 ${checkedRefs} 处字段引用、${checkedHandlers} 处事件绑定`);
if (problems) {
  console.error(`✗ 共 ${problems} 处问题`);
  process.exit(1);
}
console.log('✓ WXML 审计通过：字段、事件、组件属性全部对得上');
