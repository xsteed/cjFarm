import js from '@eslint/js';
import tseslint from 'typescript-eslint';
import prettierConfig from 'eslint-config-prettier';

// ============================================================================
// 小程序端独立的 ESLint 配置
//
// 不复用 frontend/eslint.config.js：那份配置把 globals.browser 打开给 TS 文件，
// 而小程序运行时没有 window/document，只有 wx / App / Page / Component。
// 各用各的配置，才能让「误用浏览器 API」在 lint 阶段就暴露出来。
// 根配置里已经把 apps/miniprogram/** 排除，两者不会互相干扰。
// ============================================================================
export default [
  // src/**/*.js 是开发者工具 TS 插件落盘的编译产物（与 .ts 同名同目录），
  // 不属于源码：不进 lint，也不进版本库（见 .gitignore）。
  { ignores: ['dist/**', 'node_modules/**', 'src/**/*.js'] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  // prettier 放最后：关闭格式类规则，避免与 prettier --write 互相改写
  prettierConfig,
  {
    files: ['src/**/*.ts'],
    languageOptions: {
      ecmaVersion: 'latest',
      sourceType: 'module',
      globals: {
        // 小程序运行时全局。类型由 miniprogram-api-typings 提供，这里只是给 ESLint 补名字。
        wx: 'readonly',
        App: 'readonly',
        Page: 'readonly',
        Component: 'readonly',
        Behavior: 'readonly',
        getApp: 'readonly',
        getCurrentPages: 'readonly',
        console: 'readonly',
        setTimeout: 'readonly',
        clearTimeout: 'readonly',
        setInterval: 'readonly',
        clearInterval: 'readonly'
      }
    },
    rules: {
      // 小程序里 this.setData / this.xxx（自定义页面/组件方法）是惯用写法，
      // 允许未使用表达式之外的空调用（如 this.noop()）。
      '@typescript-eslint/no-unused-expressions': ['error', { allowShortCircuit: true, allowTernary: true }]
    }
  },
  {
    files: ['*.mjs', 'scripts/**/*.mjs'],
    languageOptions: {
      ecmaVersion: 'latest',
      sourceType: 'module',
      globals: {
        console: 'readonly',
        process: 'readonly',
        setTimeout: 'readonly'
      }
    }
  }
];
