import { fileURLToPath } from 'node:url';

// ============================================================================
// 工作区别名(唯一来源,被 vite.config.ts 与 vitest.config.ts 共用)
//
// 为什么用别名而不是「纯 node_modules 解析」:
//   1) 各包的入口是 TS/Vue 源码,直接指到 src/index.ts 省掉 build 步骤,
//      也让 HMR 能穿透到 packages/ 里改动;
//   2) 顺带避免 npm 提升带来的「同一依赖两份副本」问题。
//
// ⚠️ 顺序敏感:Vite 的 alias 解析是「首个命中即返回」,且字符串 find 属于
//    前缀匹配(`@dining/shared` 会命中 `@dining/shared/dicts`)。
//    因此所有 `/子路径` 的正则必须排在对应裸包名之前。
// ============================================================================

const dir = (rel: string): string => fileURLToPath(new URL(rel, import.meta.url));

interface AliasEntry {
  find: string | RegExp;
  replacement: string;
}

function pkg(name: string, srcRoot: string): AliasEntry[] {
  const base = dir(srcRoot);
  return [
    // 子路径:允许 `@dining/shared/dicts` 这类精确到文件的引用
    { find: new RegExp(`^${name.replace('/', '\\/')}\\/(.*)$`), replacement: `${base}/$1` },
    { find: name, replacement: `${base}/index.ts` }
  ];
}

export const workspaceAliases: AliasEntry[] = [
  ...pkg('@dining/types', './packages/types/src'),
  ...pkg('@dining/shared', './packages/shared/src'),
  ...pkg('@dining/api', './packages/api/src'),
  ...pkg('@dining/auth', './packages/auth/src'),
  ...pkg('@dining/customer-core', './packages/customer-core/src'),
  ...pkg('@dining/app-admin', './apps/admin/src'),
  ...pkg('@dining/app-customer', './apps/customer/src'),
  // Web shell 自身(测试工具等)
  { find: /^@shell\/(.*)$/, replacement: `${dir('./src')}/$1` }
];
