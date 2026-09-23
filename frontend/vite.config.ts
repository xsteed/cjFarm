import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue';
import Components from 'unplugin-vue-components/vite';
import { TDesignResolver } from 'unplugin-vue-components/resolvers';

export default defineConfig({
  plugins: [
    vue(),
    // TDesign 组件按需自动导入(模板里的 t-xxx 自动 import + 注册),只打用到的组件 JS;
    // 样式仍走 main.ts 的全量 CSS —— MessagePlugin/DialogPlugin 等函数式组件依赖全量样式,
    // 按需样式(importStyle: 'css')对函数式组件不生效,反而要逐个手动补。
    Components({
      resolvers: [TDesignResolver({ library: 'vue-next' })]
    })
  ],
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true
      },
      '/uploads': {
        target: 'http://localhost:8080',
        changeOrigin: true
      }
    }
  },
  build: {
    // 产物静态资源输出到 dist/static,与后端 Gin 的 /static 托管路径对齐
    assetsDir: 'static',
    // 警告线 1500 的现实背景:tdesign 全量引入(MessagePlugin/DialogPlugin 等函数式组件
    // 依赖全量样式)的 chunk 约 1.4MB,压线以下;echarts 已按需引入(约 520KB)。
    // 若未来 echarts 意外回到全量(>1MB)或 tdesign 显著膨胀,这里会报警提示回归。
    chunkSizeWarningLimit: 1500,
    rollupOptions: {
      output: {
        // 按库拆分 vendor,提升缓存命中率与首屏加载速度
        manualChunks(id) {
          if (!id.includes('node_modules')) return;
          if (id.includes('tdesign')) return 'tdesign';
          if (id.includes('echarts') || id.includes('zrender')) return 'echarts';
          if (id.includes('axios')) return 'axios';
          if (id.includes('vue')) return 'vue';
        }
      }
    }
  }
});
