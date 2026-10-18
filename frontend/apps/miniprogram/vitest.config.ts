import { defineConfig } from 'vitest/config';

// ============================================================================
// 小程序端的单测配置（与 Web 端 frontend/vitest.config.ts 相互独立）
//
// ・environment: node —— 被测代码不该依赖 DOM（这一点由 scripts/smoke.mjs 另外保证），
//   用 node 环境能让「误用浏览器 API」在单测里也直接暴露。
// ・globals: false —— 测试文件显式 `import { describe, it, expect } from 'vitest'`，
//   这样 src 的 tsconfig 不必为测试打开全局类型。
// ・小程序端类型 / 接口 / 工具已全部自包含（src/types、src/shared、src/core、
//   src/api），不需要工作区别名。
// ============================================================================
export default defineConfig({
  resolve: {
    // .ts 优先于 .js：开发者工具的 TS 插件会把编译产物 index.js 写在源码旁边
    // （原生 TS 项目的标准行为）。不调整顺序的话，vite 默认 .js 先于 .ts，
    // 测试会加载到编译产物而不是源码 —— 产物是被转译过的 CJS，页面实例形状
    // 不同，几十个用例会集体失败。让测试永远钉住 src 源码。
    extensions: ['.ts', '.mjs', '.js', '.json']
  },
  test: {
    environment: 'node',
    globals: false,
    // 测试与小程序源码分目录：tests/ 在 miniprogramRoot(src/) 之外，
    // DevTools 扫描 src/ 时不会把 `import 'vitest'` 的用例当成项目文件去编译。
    include: ['tests/**/*.test.ts'],
    setupFiles: ['test/setup.ts']
  }
});
