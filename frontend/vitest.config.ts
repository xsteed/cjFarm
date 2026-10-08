import vue from '@vitejs/plugin-vue';
import { configDefaults, defineConfig } from 'vitest/config';
import { workspaceAliases } from './aliases';

export default defineConfig({
  plugins: [vue()],
  resolve: {
    // 与 vite.config.ts 共用同一份别名;否则测试里 import '@dining/api' 会解析不到
    alias: workspaceAliases
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['src/test/setup.ts'],
    // 测试随代码一起搬进了 apps/ 与 packages/,这里要一并覆盖
    include: ['src/**/__tests__/**/*.test.ts', 'apps/**/__tests__/**/*.test.ts', 'packages/**/__tests__/**/*.test.ts'],
    // apps/miniprogram 是独立工程（node 环境 + 自己的 wx 桩），
    // 有自己的 vitest.config.ts，不要被 Web 端的 jsdom 环境跑一遍
    exclude: [...configDefaults.exclude, 'apps/miniprogram/**']
  }
});
