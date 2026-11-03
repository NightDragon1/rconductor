// SPDX-License-Identifier: AGPL-3.0-or-later
import { describe, expect, it } from 'vitest';
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
});
