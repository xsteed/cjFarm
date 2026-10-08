import js from '@eslint/js';
import globals from 'globals';
import vue from 'eslint-plugin-vue';
import tseslint from 'typescript-eslint';
import prettierConfig from 'eslint-config-prettier';

export default [
  {
    // dist 出现在多个层级（frontend/dist、apps/miniprogram/dist），故用 **/dist/**。
    // apps/miniprogram 有自己的 eslint.config.js：它的运行时全局是 wx/App/Page/Component，
    // 而本配置给 TS 打开的是 globals.browser，混在一起会既漏报又误报。
    ignores: ['**/dist/**', 'node_modules/**', 'apps/miniprogram/**']
  },
  js.configs.recommended,
  // TS 文件用 @typescript-eslint 的规则集:替换 core 的 no-unused-vars / no-undef
  // (后者会误报 TS 类型位置的引用),自带 *.ts/*.tsx files 过滤,不影响 .js/.vue。
  ...tseslint.configs.recommended,
  ...vue.configs['flat/recommended'],
  {
    // <script setup lang="ts"> 需要显式指定 TS parser,否则 vue-eslint-parser 会用 espree 解析 TS 语法。
    files: ['**/*.vue'],
    languageOptions: {
      parserOptions: {
        parser: tseslint.parser,
        ecmaVersion: 'latest',
        sourceType: 'module'
      }
    }
  },
  // eslint-config-prettier 必须放最后:关闭所有与 Prettier 输出冲突的格式类规则
  // (否则 eslint --fix 与 prettier --write 会互相改写,如 vue/html-closing-bracket-newline)。
  prettierConfig,
  {
    files: ['**/*.{js,vue}'],
    languageOptions: {
      ecmaVersion: 'latest',
      sourceType: 'module',
      globals: {
        ...globals.browser,
        ...globals.es2024
      }
    },
    rules: {
      'vue/html-self-closing': 'off',
      'vue/multi-word-component-names': 'off'
    }
  },
  {
    files: ['**/*.{ts,tsx}'],
    languageOptions: {
      parser: tseslint.parser,
      ecmaVersion: 'latest',
      sourceType: 'module',
      globals: {
        ...globals.browser,
        ...globals.es2024
      }
    }
  },
  {
    // 测试文件启用 vitest globals(describe/it/expect/vi…),与 vitest.config globals: true 一致
    files: ['**/__tests__/**/*.test.{ts,js}', 'src/test/**/*.{ts,js}'],
    languageOptions: {
      globals: {
        ...globals.vitest
      }
    }
  }
];
