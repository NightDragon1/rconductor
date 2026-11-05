// SPDX-License-Identifier: AGPL-3.0-or-later
import { defineConfig, devices } from '@playwright/test';

// Panels from the e2e stack (internal/e2e/stack): one at the root, one
// below the base path /rcon behind a proxy that strips it (NFR-003), and
// one under the name rcon.lan over plain http, which is no secure context
// as on a LAN address (D-011): there the clipboard API is missing. The
// last one serves a build with a test language, every text marked (NFR-007).
const language = 'web/.dist-language';
const stacks = [
  { name: 'root', port: 4380, base: '/', host: '', dist: '' },
  { name: 'basepath', port: 4381, base: '/rcon', host: '', dist: '' },
  { name: 'insecure', port: 4382, base: '/', host: 'rcon.lan', dist: '' },
  { name: 'language', port: 4383, base: '/', host: '', dist: language },
];
const insecureOnly = /clipboard\.spec\.ts/;
const phoneOnly = /mobile\.spec\.ts/;
const languageOnly = /language\.spec\.ts/;

export default defineConfig<{ stack: string }>({
  testDir: 'e2e',
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  use: { trace: 'retain-on-failure', screenshot: 'only-on-failure' },
  webServer: stacks.map((s) => ({
    // From the repository root: web/ has its own go.mod as a barrier.
    command: `${s.dist ? `(cd web && npx vite build --mode language --outDir ${s.dist.replace('web/', '')}) && ` : ''}go run ./internal/e2e/stack -addr 127.0.0.1:${s.port} -base ${s.base}${s.host ? ` -host ${s.host}` : ''} -dist ${s.dist || 'internal/webui/dist'} -state web/e2e/.state-${s.name}.json -log web/e2e/.panel-${s.name}.log`,
    cwd: '..',
    url: `http://127.0.0.1:${s.port}${s.base === '/' ? '' : s.base}/`,
    reuseExistingServer: false,
    timeout: 180_000,
    stdout: 'pipe',
  })),
  projects: [
    ...stacks
      .filter((s) => !s.dist)
      .map((s) =>
        s.host
          ? {
              name: s.name,
              testMatch: insecureOnly,
              use: {
                ...devices['Desktop Chrome'],
                stack: s.name,
                launchOptions: { args: [`--host-resolver-rules=MAP ${s.host} 127.0.0.1`] },
              },
            }
          : {
              name: s.name,
              testIgnore: [insecureOnly, phoneOnly, languageOnly],
              use: { ...devices['Desktop Chrome'], stack: s.name },
            },
      ),
    {
      name: 'language',
      testMatch: languageOnly,
      use: { ...devices['Desktop Chrome'], stack: 'language' },
    },
    // The phone view at the width of the acceptance (UI-012), with touch.
    {
      name: 'mobile',
      testMatch: phoneOnly,
      use: {
        ...devices['Pixel 7'],
        viewport: { width: 390, height: 844 },
        stack: 'root',
      },
    },
  ],
});
