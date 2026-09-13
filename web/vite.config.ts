import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { fileURLToPath } from 'node:url'

// The SPA is served by the Go binary under /ui/. During development, API and
// repository traffic is proxied to a locally running holiaokho (:18081).
export default defineConfig({
  base: '/ui/',
  plugins: [react()],
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    chunkSizeWarningLimit: 1200,
    rollupOptions: {
      output: {
        // Split the slow-moving vendor code so a UI change does not invalidate
        // the whole bundle in the browser cache.
        manualChunks(id) {
          if (!id.includes('node_modules')) return
          if (id.includes('/antd/') || id.includes('@ant-design') || id.includes('rc-')) return 'antd'
          if (id.includes('/react') || id.includes('/scheduler/')) return 'react'
          if (id.includes('i18next') || id.includes('dayjs')) return 'intl'
          return 'vendor'
        },
      },
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://localhost:18081',
      '/repository': 'http://localhost:18081',
      '/healthz': 'http://localhost:18081',
    },
  },
})
