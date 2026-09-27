import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url))
    }
  },
  server: {
    host: '::', // 双栈监听：IPv6 + IPv4（bindv6only=0）
    port: 5173,
    // 开发模式代理：前端与后端分端口时，同源相对路径 /api 由 vite 转发到后端
    //（与单文件部署的同源形态保持一致；后端地址可用环境变量 KY_API 覆盖）
    proxy: {
      '/api': {
        target: process.env.KY_API || 'http://192.168.61.127:8080',
        changeOrigin: true
      }
    }
  }
})
