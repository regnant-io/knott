// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 3000,
    // Everything goes to the KNOTT server on :8002. It fronts the registry,
    // task and agent services itself, so the dev proxy is a single target —
    // which also means the console behaves identically in development and in
    // the built binary.
    proxy: {
      '/api': { target: 'http://localhost:8002', changeOrigin: true },
      '/internal': { target: 'http://localhost:8002', changeOrigin: true },
      '/metrics': { target: 'http://localhost:8002', changeOrigin: true },
    },
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
