import { defineConfig } from 'vitest/config';
import { workspaceAliases } from '../../aliases';

// ============================================================================
// 小程序端的单测配置（与 Web 端 frontend/vitest.config.ts 相互独立）
//
// ・environment: node —— 被测代码不该依赖 DOM（这一点由 scripts/smoke.mjs 另外保证），
//   用 node 环境能让「误用浏览器 API」在单测里也直接暴露。
// ・globals: false —— 测试文件显式 `import { describe, it, expect } from 'vitest'`，
//   这样 src 的 tsconfig 不必为测试打开全局类型。
// ・复用 frontend/aliases.ts 的工作区别名，@dining/* 才能解析到 packages 源码。
// ============================================================================
export default defineConfig({
  resolve: {
    alias: workspaceAliases
  },
  test: {
    environment: 'node',
    globals: false,
    include: ['src/**/__tests__/**/*.test.ts'],
    setupFiles: ['test/setup.ts']
  }
});
