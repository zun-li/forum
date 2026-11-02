import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  root: 'src',
  publicDir: '../public',
  plugins: [vue()],
  server: { proxy: { '/api': 'http://127.0.0.1:8080' } },
  build: { outDir: '../dist', assetsDir: 'static', emptyOutDir: true }
})
