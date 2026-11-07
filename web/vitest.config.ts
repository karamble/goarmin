// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

// Logic (*.test.ts) runs in node; components (*.test.tsx) in jsdom.
export default defineConfig({
  test: {
    clearMocks: true,
    projects: [
      { test: { name: 'node', include: ['src/**/*.test.ts'], environment: 'node' } },
      {
        plugins: [react()],
        // Longer than Testing Library's 5 s wait, so a missing element is reported as such.
        test: { name: 'dom', include: ['src/**/*.test.tsx'], environment: 'jsdom', setupFiles: ['src/test/setup.ts'], testTimeout: 15_000 },
      },
    ],
  },
})
