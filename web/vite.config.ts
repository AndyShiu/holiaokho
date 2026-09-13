import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { fileURLToPath } from 'node:url'

// The SPA is served by the Go binary under /ui/. During development, API and
// repository traffic is proxied to a locally running holiaokho (:18081).
export default defineConfig({
  base: '/ui/',
  plugins: [react()],
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  build: { outDir: 'dist', emptyOutDir: true, chunkSizeWarningLimit: 1500 },
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://localhost:18081',
      '/repository': 'http://localhost:18081',
      '/healthz': 'http://localhost:18081',
    },
  },
})
