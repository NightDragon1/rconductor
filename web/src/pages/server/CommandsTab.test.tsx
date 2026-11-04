// SPDX-License-Identifier: AGPL-3.0-or-later
import { fireEvent, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type { Command } from '@/lib/commandform';
import { renderPage, setLanguage, unmarked } from '@/test/render';
import { CommandCard } from './CommandsTab';

const kick: Command = {
  path: 'kick',
  description: 'Kick a player',
  params: [
    { name: 'player', type: 'player' },
    { name: 'reason', type: 'text', optional: true },
  ],
};
const weather: Command = { path: 'weather', description: '', params: [] };

describe('command cards', () => {
  it('have no text outside the language files (NFR-007)', async () => {
    setLanguage('xx');
    const { container } = await renderPage(() => (
      <>
        {[kick, weather].map((c) => (
          <CommandCard
            key={c.path}
            c={c}
            values={{ player: 'two words' }}
            players={['Steve']}
            listId="p"
            busy={false}
            isPlayer={() => true}
            onChange={() => undefined}
            onFocusPlayer={() => undefined}
            onRun={() => undefined}
          />
        ))}
      </>
    ));
    // Paths, descriptions, parameter names and the preview come from the
    // game profile or the input, not from the language files.
    const data =
      /^(kick|weather|Kick a player|player|kick \{player\} \{reason\}|weather …|aria-label=.*)$/;
    expect(unmarked(container).filter((s) => !data.test(s))).toEqual([]);
  });

  it('fold on a phone and show no fields then (Design 6.6)', async () => {
    setLanguage('en');
    const onToggle = vi.fn();
    const card = (folded: boolean) => (
      <CommandCard
        c={kick}
        values={{}}
        players={[]}
        listId="p"
        busy={false}
        isPlayer={() => true}
        onChange={() => undefined}
        onFocusPlayer={() => undefined}
        onRun={() => undefined}
        folded={folded}
        onToggle={onToggle}
      />
    );
    const closed = await renderPage(() => card(true));
    expect(screen.queryByRole('button', { name: 'Run' })).toBeNull();
    const head = screen.getByRole('button', { expanded: false });
    fireEvent.click(head);
    expect(onToggle).toHaveBeenCalledOnce();
    closed.unmount();
    await renderPage(() => card(false));
    expect(screen.getByRole('button', { name: 'Run' })).toBeTruthy();
    expect(screen.getByRole('button', { expanded: true })).toBeTruthy();
  });
});
