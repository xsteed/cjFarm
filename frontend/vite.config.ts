import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue';
import Components from 'unplugin-vue-components/vite';
import { TDesignResolver } from 'unplugin-vue-components/resolvers';
import { workspaceAliases } from './aliases';

export default defineConfig({
  plugins: [
    vue(),
    // TDesign 组件按需自动导入(模板里的 t-xxx 自动 import + 注册),只打用到的组件 JS;
    // 样式仍走 main.ts 的全量 CSS —— MessagePlugin/DialogPlugin 等函数式组件依赖全量样式,
    // 按需样式(importStyle: 'css')对函数式组件不生效,反而要逐个手动补。
    // dirs 指向管理端 app 的组件目录:这里有少数组件在模板里按名字使用(不显式 import)。
    Components({
      resolvers: [TDesignResolver({ library: 'vue-next' })],
      dirs: ['apps/admin/src/components']
    })
  ],
  resolve: {
    // 工作区包(packages/* 与 apps/*)统一走别名,见 aliases.ts
    alias: workspaceAliases
  },
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
    // 警告线 600:去掉 tdesign 的强制合块后,当前最大的块是 echarts(521kB,仅管理端
    // 报表懒加载),已经没人接近 1MB。再出现超线的大块(比如某处又把 UI 库整包引入)
    // 就值得停下来看一眼 —— 顾客扫码点菜的首屏体积直接决定「订单卡多久画出来」。
    chunkSizeWarningLimit: 600,
    rollupOptions: {
      output: {
        // 按库拆分 vendor,提升缓存命中率与首屏加载速度。
        //
        // ⚠️ 刻意**不**把 tdesign 合成一个 chunk:顾客端(/order/:tableId 扫码点菜)与
        // 管理后台共用同一个 SPA 入口,强行按包名归并会把「只有后台用到的组件」也塞进
        // 顾客端必下的那一块 —— 实测顾客扫码首屏要多下 ~1MB(995kB JS + 449kB CSS)
        // 才开始画订单卡。交给 Rollup 按真实引用拆,顾客端只带自己用到的几个组件。
        manualChunks(id) {
          // 只认 node_modules 下的精确包目录:用 includes('vue') 这种模糊匹配会把
          // tdesign-vue-next 一起吞进 vue chunk(实测直接把 107kB 撑成 1.1MB)。
          const pkg = /[\\/]node_modules[\\/](.+?)[\\/]/.exec(id)?.[1];
          if (!pkg) return;
          if (pkg === 'echarts' || pkg === 'zrender') return 'echarts';
          if (pkg === 'axios') return 'axios';
          if (pkg === 'vue' || pkg === '@vue' || pkg === 'vue-router' || pkg === 'vue-demi') return 'vue';
        }
      }
    }
  }
});
