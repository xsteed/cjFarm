import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  server: {
    port: 5173,
    proxy: {
      '/prod-api': {
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
    chunkSizeWarningLimit: 1500,
    rollupOptions: {
      output: {
        // 按库拆分 vendor,提升缓存命中率与首屏加载速度
        manualChunks(id) {
          if (!id.includes('node_modules')) return
          if (id.includes('tdesign')) return 'tdesign'
          if (id.includes('echarts') || id.includes('zrender')) return 'echarts'
          if (id.includes('axios')) return 'axios'
          if (id.includes('vue')) return 'vue'
        }
      }
    }
  }
})
