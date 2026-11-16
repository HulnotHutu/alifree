import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// 开发期把 /api 代理到本机 Go 服务（quota serve --port 8377）；
// 生产构建产物 web-ui/dist 可由 Go 侧 go:embed 直接托管。
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8377',
        changeOrigin: true,
      },
    },
  },
})
