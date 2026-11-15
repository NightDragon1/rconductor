// SPDX-License-Identifier: AGPL-3.0-or-later
import { render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { ForeignText } from './ForeignText';

describe('ForeignText with formatting (CMD-017, SEC-006)', () => {
  it('shows codes as styles, markup as text, bidi controls as stand-ins', () => {
    const { container } = render(
      <pre>
        <ForeignText
          text={'§a<img src=x onerror="window.__x=1">\n§lfile‮txt.exe §zkept'}
          formatting="minecraft"
        />
      </pre>,
    );
    expect(container.querySelector('img')).toBeNull();
    expect(container.textContent).toBe('<img src=x onerror="window.__x=1">\nfile‮txt.exe §zkept');
    const green = container.querySelector('span.text-mc-a');
    expect(green?.textContent).toBe('<img src=x onerror="window.__x=1">');
    const bold = container.querySelector('span.font-bold');
    expect(bold?.querySelector('[data-code]')?.getAttribute('data-code')).toBe('⟨U+202E⟩');
    expect(container.querySelectorAll('[style]')).toHaveLength(0);
    for (const el of container.querySelectorAll('span')) {
      expect([...el.attributes].map((a) => a.name)).toEqual(['class']);
    }
  });

  it('shows the text as given without formatting', () => {
    const { container } = render(<ForeignText text="§ahello" />);
    expect(container.textContent).toBe('§ahello');
    expect(container.querySelectorAll('span')).toHaveLength(0);
  });
});
