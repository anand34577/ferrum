import path from 'node:path'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// FERRUM_API overrides the dev proxy target (e.g. when 8080 is reserved on Windows).
const apiTarget = process.env.FERRUM_API ?? 'http://localhost:8080'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': path.resolve(import.meta.dirname, './src'),
    },
  },
  server: {
    proxy: {
      // Object form (not the string shorthand, which implies changeOrigin):
      // keep the browser's Host so the backend's CSRF origin check sees
      // Origin and Host agree — otherwise every dev-mode login/POST is 403.
      '/api': { target: apiTarget, changeOrigin: false },
      '/ws': {
        target: apiTarget.replace(/^http/, 'ws'),
        ws: true,
      },
    },
  },
  build: {
    outDir: 'dist',
  },
})
