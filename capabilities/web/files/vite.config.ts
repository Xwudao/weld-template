import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Build output is embedded by internal/server/assets_web.go. emptyOutDir is off
// so the tracked dist/.gitkeep placeholder survives a build.
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: '../internal/server/dist',
    emptyOutDir: false,
  },
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
    },
  },
})
