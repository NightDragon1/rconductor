// SPDX-License-Identifier: AGPL-3.0-or-later
import { fileURLToPath, URL } from 'node:url';
import tailwindcss from '@tailwindcss/vite';
import react from '@vitejs/plugin-react';
import { defineConfig } from 'vitest/config';
import { pseudo } from './src/test/pseudo';

// In the mode language every text is marked; only the E2E test of NFR-007
// builds it, see playwright.config.ts.
const testLanguage = {
  name: 'test-language',
  enforce: 'pre' as const,
  transform(code: string, id: string) {
    return id.endsWith('/i18n/en.json') ? JSON.stringify(pseudo(JSON.parse(code))) : undefined;
  },
};

export default defineConfig(({ mode }) => ({
  plugins: [react(), tailwindcss(), ...(mode === 'language' ? [testLanguage] : [])],
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
}));
