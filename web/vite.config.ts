// SPDX-License-Identifier: AGPL-3.0-or-later
import { fileURLToPath, URL } from 'node:url';
import tailwindcss from '@tailwindcss/vite';
import react from '@vitejs/plugin-react';
import { defineConfig } from 'vitest/config';

export default defineConfig({
  plugins: [react(), tailwindcss()],
  // Relative asset URLs; the panel sets <base href> from RCD_BASE_PATH.
  base: './',
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  build: {
    // Embedded into the panel binary by internal/webui.
    outDir: '../internal/webui/dist',
    emptyOutDir: true,
    // No inline scripts or data URLs for modules (SEC-009).
    assetsInlineLimit: 0,
    modulePreload: { polyfill: false },
  },
  server: {
    proxy: {
      '/api': { target: 'http://localhost:8080', ws: true },
    },
  },
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.{ts,tsx}'],
    setupFiles: ['src/test/setup.ts'],
  },
});
