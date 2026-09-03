// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 3000,
    proxy: {
      '/api/v1/workflows': { target: 'http://localhost:8001', changeOrigin: true },
      '/api/v1/runs': { target: 'http://localhost:8002', changeOrigin: true },
      '/api/v1/decisions': { target: 'http://localhost:8002', changeOrigin: true },
      '/api/v1/connectors': { target: 'http://localhost:8002', changeOrigin: true },
      '/api/v1/stats': { target: 'http://localhost:8002', changeOrigin: true },
      '/api/v1/tasks': { target: 'http://localhost:8004', changeOrigin: true },
      '/api/v1/agents': { target: 'http://localhost:8005', changeOrigin: true },
      '/internal/v1/task-specs': { target: 'http://localhost:8003', changeOrigin: true },
      '/internal/v1': { target: 'http://localhost:8002', changeOrigin: true },
    }
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.js'],
    include: ['src/**/*.{test,spec}.{js,jsx}'],
  }
})
