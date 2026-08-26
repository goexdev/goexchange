import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 3000,
    strictPort: true,
    host: '0.0.0.0',
    proxy: {
      '/api': {
        target: 'http://localhost:8099',
        changeOrigin: true,
        secure: false,
      },
      '/ws': {
        target: 'ws://localhost:8099',
        ws: true,
        changeOrigin: true,
      },
    },
  },
})
