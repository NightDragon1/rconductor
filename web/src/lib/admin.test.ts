// SPDX-License-Identifier: AGPL-3.0-or-later
import { describe, expect, it } from 'vitest';
import { reasonText } from './admin';

const t = (k: string) => `text:${k}`;

describe('refusal texts (UI-016)', () => {
  it('shows the detail of an input refused as invalid', () => {
    expect(reasonText(t, { reason: 'invalid', detail: 'a server with this name exists' })).toBe(
      'a server with this name exists',
    );
    expect(
      reasonText(t, { id: 'x', ok: false, reason: 'invalid', detail: ' padded ' } as never),
    ).toBe('padded');
  });

  it('keeps the fixed text without a detail', () => {
    expect(reasonText(t, { reason: 'invalid' })).toBe('text:admin.reason.invalid');
    expect(reasonText(t, { reason: 'invalid', detail: '  ' })).toBe('text:admin.reason.invalid');
    expect(reasonText(t, 'invalid')).toBe('text:admin.reason.invalid');
  });

  it('never shows the detail for other reasons', () => {
    for (const reason of [
      'forbidden',
      'not_found',
      'unauthenticated',
      'conflict',
      'rate_limited',
    ]) {
      expect(reasonText(t, { reason, detail: 'server 42 belongs to someone else' })).not.toContain(
        'server 42',
      );
    }
    expect(reasonText(t, { reason: 'forbidden', detail: 'x' })).toBe('text:admin.reason.forbidden');
    expect(reasonText(t, undefined)).toBe('text:admin.reason.other');
  });
});
