import path from 'node:path'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

export default defineConfig({
  // Relative base: this build is a copy of the real app with a mocked API
  // (see src/lib/demoApi.ts) and gets served from a subpath on GitHub Pages
  // (docs/demo/), not from the domain root — asset URLs have to work either way.
  base: './',
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': path.resolve(import.meta.dirname, './src'),
    },
  },
  build: {
    outDir: 'dist',
  },
})
