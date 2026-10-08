// ============================================================================
// WXSS 一致性审计
//
// WXSS 没有官方 lint（stylelint 默认也不认 .wxss），而类名/变量写错同样只在真机上
// 表现为「样式没生效」。这个脚本用 postcss 挡住四类问题：
//   1) 语法错误（括号不闭合等）
//   2) var(--x) 引用了没声明过的变量（token 名拼错）
//   3) 模板里用到的 class 在**它的作用域内**没有定义
//   4) 声明了却没被用到的 class（提示，人工判断）
//
// 第 3 条按作用域判定，这一点很关键：
//   ・页面：自身 wxss + app.wxss 都算数
//   ・自定义组件：只认它自己的 wxss —— 组件默认样式隔离（isolated），
//     全局/页面样式不会透进组件，写了也不会生效，必须在组件内自带。
//
// 用法：npm run audit（已包含在 npm run verify 里）
// ============================================================================
import fs from 'node:fs';
import path from 'node:path';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';

const ROOT = path.dirname(path.dirname(fileURLToPath(import.meta.url)));
const SRC = path.join(ROOT, 'src');
const APP_WXSS = path.join(SRC, 'app.wxss');
// postcss 由前端工程提供（stylelint 的依赖），按项目根解析
const require = createRequire(path.join(ROOT, '..', '..', 'package.json'));
const postcss = require('postcss');

function walk(dir, out = []) {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) walk(p, out);
    else out.push(p);
  }
  return out;
}

const files = walk(SRC);
const wxssFiles = files.filter(f => f.endsWith('.wxss'));
const wxmlFiles = files.filter(f => f.endsWith('.wxml'));

let problems = 0;
const declaredVars = new Set();
/** class 名 -> 定义它的文件集合（用于「未使用」提示） */
const declaredIn = new Map();

/** 解析一个 wxss，把 class 名收进 target。 */
function collectClasses(file, target, { collectVars = false } = {}) {
  if (!fs.existsSync(file)) return;
  let root;
  try {
    root = postcss.parse(fs.readFileSync(file, 'utf8'), { from: file });
  } catch (e) {
    console.log(`✗ 语法错误 ${path.relative(SRC, file)}: ${e.reason || e.message}`);
    problems++;
    return;
  }
  root.walkDecls(d => {
    if (collectVars && d.prop.startsWith('--')) declaredVars.add(d.prop);
  });
  root.walkRules(rule => {
    for (const sel of rule.selectors || []) {
      for (const m of sel.matchAll(/\.([A-Za-z_][\w-]*)/g)) {
        target.add(m[1]);
        if (!declaredIn.has(m[1])) declaredIn.set(m[1], new Set());
        declaredIn.get(m[1]).add(path.relative(SRC, file));
      }
    }
  });
}

// ---- 1) 全量解析 + 收集变量 ----
for (const f of wxssFiles) collectClasses(f, new Set(), { collectVars: true });

// ---- 2) var() 引用检查 ----
for (const f of wxssFiles) {
  const css = fs.readFileSync(f, 'utf8');
  for (const m of css.matchAll(/var\(\s*(--[\w-]+)\s*(?:,|\))/g)) {
    if (!declaredVars.has(m[1])) {
      console.log(`✗ ${path.relative(SRC, f)}: 使用了未声明的变量 ${m[1]}`);
      problems++;
    }
  }
}

// ---- 3) 按作用域检查模板里的 class ----
const usedClasses = new Set();
for (const f of wxmlFiles) {
  const rel = path.relative(SRC, f);
  const wxml = fs.readFileSync(f, 'utf8');
  const isComponent = rel.startsWith(`components${path.sep}`);

  const allowed = new Set();
  collectClasses(f.replace(/\.wxml$/, '.wxss'), allowed);
  if (!isComponent) collectClasses(APP_WXSS, allowed);
  const scope = isComponent ? '组件自身 wxss' : '页面 wxss + app.wxss';

  const check = (cls, where) => {
    usedClasses.add(cls);
    if (!allowed.has(cls)) {
      console.log(`✗ ${rel}: class「${cls}」(${where}) 在${scope}里没有定义`);
      problems++;
    }
  };

  for (const m of wxml.matchAll(/class="([^"]*)"/g)) {
    for (const cls of m[1]
      .replace(/\{\{[^}]*\}\}/g, ' ')
      .split(/\s+/)
      .filter(Boolean)) {
      check(cls, '静态');
    }
  }
  // 三元里的类名（{{on ? 'active' : ''}}）：只看 ?/: 之后的分支，
  // 免得把比较操作用的字面量（payTab === 'wx'）当成类名。
  for (const m of wxml.matchAll(/\{\{[^}]*\}\}/g)) {
    for (const lit of m[0].matchAll(/[?:]\s*'([a-z][\w-]*)'/g)) check(lit[1], '动态');
  }
}

// ---- 4) 未使用的 class（提示）----
// TS 里拼接/字面量的类名也算「用过」，否则 tag-${theme} 这类会被误报
for (const f of files.filter(f => f.endsWith('.ts'))) {
  const ts = fs.readFileSync(f, 'utf8');
  for (const m of ts.matchAll(/'([a-z][\w-]*)'/g)) usedClasses.add(m[1]);
}
const unused = [...declaredIn.keys()].filter(c => !usedClasses.has(c)).sort();

console.log(`WXSS 审计：${wxssFiles.length} 个样式 / ${wxmlFiles.length} 个模板 / class 定义 ${declaredIn.size} 个`);
if (unused.length) {
  console.log(`提示：以下 class 未在模板/TS 中出现，确认是否为死代码 —— ${unused.join(', ')}`);
}
if (problems) {
  console.error(`✗ 共 ${problems} 处问题`);
  process.exit(1);
}
console.log('✓ WXSS 审计通过：语法、变量、类名作用域一致');
