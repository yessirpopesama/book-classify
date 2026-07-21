import { defineConfig } from 'vite'

export default defineConfig({
  server: {
    port: 8887,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
        timeout: 0,
        proxyTimeout: 0
      }
    }
  }
})
