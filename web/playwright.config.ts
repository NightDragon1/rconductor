// SPDX-License-Identifier: AGPL-3.0-or-later
import { defineConfig, devices } from '@playwright/test';

// Panels from the e2e stack (internal/e2e/stack): one at the root, one
// below the base path /rcon behind a proxy that strips it (NFR-003), and
// one under the name rcon.lan over plain http, which is no secure context
// as on a LAN address (D-011): there the clipboard API is missing.
const stacks = [
  { name: 'root', port: 4380, base: '/', host: '' },
  { name: 'basepath', port: 4381, base: '/rcon', host: '' },
  { name: 'insecure', port: 4382, base: '/', host: 'rcon.lan' },
];
const insecureOnly = /clipboard\.spec\.ts/;

export default defineConfig({
  testDir: 'e2e',
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  use: { trace: 'retain-on-failure', screenshot: 'only-on-failure' },
  webServer: stacks.map((s) => ({
    // From the repository root: web/ has its own go.mod as a barrier.
    command: `go run ./internal/e2e/stack -addr 127.0.0.1:${s.port} -base ${s.base}${s.host ? ` -host ${s.host}` : ''} -dist internal/webui/dist -state web/e2e/.state-${s.name}.json`,
    cwd: '..',
    url: `http://127.0.0.1:${s.port}${s.base === '/' ? '' : s.base}/`,
    reuseExistingServer: false,
    timeout: 180_000,
    stdout: 'pipe',
  })),
  projects: stacks.map((s) =>
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
          testIgnore: insecureOnly,
          use: { ...devices['Desktop Chrome'], stack: s.name },
        },
  ),
});
