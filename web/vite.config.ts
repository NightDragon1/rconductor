// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { fileURLToPath, URL } from 'node:url';
import tailwindcss from '@tailwindcss/vite';
import react from '@vitejs/plugin-react';
import license from 'rollup-plugin-license';
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

// Licenses of the bundled dependencies (NFR-021): one text file next to the
// UI, which the image also copies to /licenses.
const thirdParty = license({
  thirdParty: {
    includePrivate: false,
    output: {
      file: fileURLToPath(
        new URL('../internal/webui/dist/third-party-licenses.txt', import.meta.url),
      ),
      template: (deps) =>
        [...deps]
          .sort((a, b) => ((a.name ?? '') < (b.name ?? '') ? -1 : 1))
          .map((d) => `== ${d.name} ${d.version} (${d.license})\n${d.licenseText ?? ''}\n`)
          .join('\n'),
    },
  },
});

export default defineConfig(({ mode }) => ({
  plugins: [react(), tailwindcss(), thirdParty, ...(mode === 'language' ? [testLanguage] : [])],
  // Relative asset URLs; the panel sets <base href> from RCD_BASE_PATH.
  base: './',
  // No lookup of .env or postcss files beside this config: what the build
  // reads is fixed here (PRJ-005).
  envDir: false,
  css: { postcss: {} },
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
