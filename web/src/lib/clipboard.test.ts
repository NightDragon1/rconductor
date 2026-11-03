// SPDX-License-Identifier: AGPL-3.0-or-later
import { afterEach, describe, expect, it, vi } from 'vitest';
import { copyText } from './clipboard';

afterEach(() => vi.unstubAllGlobals());

describe('copyText', () => {
  it('uses the clipboard API in a secure context', async () => {
    const writeText = vi.fn(() => Promise.resolve());
    vi.stubGlobal('isSecureContext', true);
    vi.stubGlobal('navigator', { clipboard: { writeText } });
    expect(await copyText('abc', null)).toBe('copied');
    expect(writeText).toHaveBeenCalledWith('abc');
  });

  it('selects the text outside a secure context (D-011)', async () => {
    vi.stubGlobal('isSecureContext', false);
    const el = document.createElement('pre');
    el.textContent = 'answer';
    document.body.append(el);
    expect(await copyText('answer', el)).toBe('selected');
    expect(window.getSelection()?.toString()).toBe('answer');
    el.remove();
  });
});
