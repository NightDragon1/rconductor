// SPDX-License-Identifier: AGPL-3.0-or-later
import { defineConfig, devices } from '@playwright/test';

// Two panels from the e2e stack (internal/e2e/stack): one at the root, one
// below the base path /rcon behind a proxy that strips it (NFR-003).
const stacks = [
  { name: 'root', port: 4380, base: '/' },
  { name: 'basepath', port: 4381, base: '/rcon' },
];

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
    command: `go run ./internal/e2e/stack -addr 127.0.0.1:${s.port} -base ${s.base} -dist internal/webui/dist -state web/e2e/.state-${s.name}.json`,
    cwd: '..',
    url: `http://127.0.0.1:${s.port}${s.base === '/' ? '' : s.base}/`,
    reuseExistingServer: false,
    timeout: 180_000,
    stdout: 'pipe',
  })),
  projects: stacks.map((s) => ({
    name: s.name,
    use: { ...devices['Desktop Chrome'], stack: s.name },
  })),
});
