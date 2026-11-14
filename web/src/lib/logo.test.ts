// SPDX-License-Identifier: AGPL-3.0-or-later
/// <reference types="node" />
import { spawnSync } from 'node:child_process';
import { appendFileSync, cpSync, mkdtempSync, readFileSync, rmSync, unlinkSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterEach, describe, expect, it } from 'vitest';

// UI-023: every size and format of the logo comes from assets/logo through
// scripts/logo.sh. This test is where the CI runs its check (job web).
const script = '../scripts/logo.sh';
const generated = [
  'web/public/favicon.svg',
  'web/public/icon.svg',
  'web/public/manifest.json',
  'web/src/assets/logo/logo.svg',
  'web/src/assets/logo/logo-dark.svg',
  'web/src/assets/logo/mark.svg',
  'web/src/assets/logo/mark-light.svg',
];

function check(root?: string) {
  const r = spawnSync('sh', [script, '--check', ...(root ? [root] : [])], { encoding: 'utf8' });
  return { status: r.status, stderr: r.stderr };
}

let tmp = '';
afterEach(() => {
  if (tmp) rmSync(tmp, { recursive: true, force: true });
  tmp = '';
});

// copy puts the generated files of the work tree into a throwaway root.
function copy() {
  tmp = mkdtempSync(join(tmpdir(), 'logo-'));
  for (const f of generated) cpSync(join('..', f), join(tmp, f));
  return tmp;
}

describe('scripts/logo.sh (UI-023)', () => {
  it('the generated files of the work tree are made from assets/logo', () => {
    const r = check();
    expect(r.stderr).toBe('');
    expect(r.status).toBe(0);
  });

  it('a copy of the generated files passes', () => {
    expect(check(copy()).status).toBe(0);
  });

  it('a changed file fails and is named', () => {
    const root = copy();
    appendFileSync(join(root, 'web/src/assets/logo/logo-dark.svg'), '<!-- edited -->\n');
    const r = check(root);
    expect(r.status).not.toBe(0);
    expect(r.stderr).toContain('web/src/assets/logo/logo-dark.svg');
    expect(r.stderr).not.toContain('web/public/favicon.svg');
  });

  it('a missing file fails and is named', () => {
    const root = copy();
    unlinkSync(join(root, 'web/public/manifest.json'));
    const r = check(root);
    expect(r.status).not.toBe(0);
    expect(r.stderr).toContain('web/public/manifest.json');
  });

  it('a missing variant for the light design fails and is named', () => {
    const root = copy();
    unlinkSync(join(root, 'web/src/assets/logo/mark-light.svg'));
    const r = check(root);
    expect(r.status).not.toBe(0);
    expect(r.stderr).toContain('web/src/assets/logo/mark-light.svg');
  });

  it('the light design ends the gradient in the dark cyan, the others keep the brand colours', () => {
    const end = (f: string) =>
      [...readFileSync(join('..', f), 'utf8').matchAll(/stop-color="(#[0-9A-F]{6})"/g)].map(
        (m) => m[1],
      );
    for (const f of ['web/src/assets/logo/logo.svg', 'web/src/assets/logo/mark-light.svg']) {
      expect(end(f), f).not.toContain('#22D3EE');
      expect(end(f), f).toContain('#0E7490');
    }
    for (const f of [
      'web/src/assets/logo/logo-dark.svg',
      'web/src/assets/logo/mark.svg',
      'web/public/favicon.svg',
      'web/public/icon.svg',
    ]) {
      expect(end(f), f).toContain('#22D3EE');
      expect(end(f), f).not.toContain('#0E7490');
    }
  });

  it('refuses unknown arguments', () => {
    const r = spawnSync('sh', [script, '--write'], { encoding: 'utf8' });
    expect(r.status).not.toBe(0);
    expect(r.stderr).toContain('usage');
  });
});
