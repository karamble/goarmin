// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig, type Plugin } from 'vite'

const outDir = resolve(__dirname, '../internal/webui/dist')

// The production policy (internal/api/server.go). The dev server reports what would break it.
const csp =
  "default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; " +
  "object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'"

// emptyOutDir removes the tracked dist/.gitkeep that keeps the Go embed pattern valid; put it back.
const keepGitkeep: Plugin = {
  name: 'goarmin-keep-gitkeep',
  apply: 'build',
  closeBundle() {
    writeFileSync(resolve(outDir, '.gitkeep'), '')
  },
}

export default defineConfig({
  plugins: [react(), tailwindcss(), keepGitkeep],
  build: {
    outDir,
    emptyOutDir: true,
    chunkSizeWarningLimit: 1200,
    rollupOptions: {
      output: {
        manualChunks(id) {
          // React gets its own chunk: a manual chunk takes in its unassigned dependencies, so
          // React (used by dnd-kit) landed in the dndkit chunk and the first page preloaded
          // the editor's library to get React out of it.
          if (/node_modules\/(react|react-dom|scheduler)\//.test(id) || id.includes('commonjsHelpers')) return 'react'
          // All of ECharts in one chunk: split across two, its classes extend each other before
          // they exist ("Class extends value undefined").
          if (id.includes('node_modules/echarts') || id.includes('node_modules/zrender')) return 'echarts'
          if (id.includes('node_modules/leaflet')) return 'leaflet'
          if (id.includes('node_modules/@dnd-kit')) return 'dndkit'
        },
      },
    },
  },
  server: {
    // No changeOrigin: the API's same-origin check compares Origin with the Host it receives.
    proxy: { '/api': 'http://127.0.0.1:8642', '/tiles': 'http://127.0.0.1:8642' },
    headers: { 'Content-Security-Policy-Report-Only': csp },
  },
})
