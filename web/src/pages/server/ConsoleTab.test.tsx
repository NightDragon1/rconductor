// SPDX-License-Identifier: AGPL-3.0-or-later
import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { missingSlash } from '@/lib/console';
import { renderPage, setLanguage, unmarked } from '@/test/render';
import { SlashHint } from './ConsoleTab';

const factorio = ['/players', '/kick', '/server-save'];
const minecraft = ['list', 'kick', 'time set'];

describe('slash hint (O9)', () => {
  it('shows when every command starts with / and the input does not (CAT-005)', () => {
    expect(missingSlash(factorio, 'players online')).toBe(true);
    expect(missingSlash(factorio, '  players')).toBe(true);
    expect(missingSlash(factorio, '/players online')).toBe(false);
    expect(missingSlash(factorio, ' /players')).toBe(false);
    expect(missingSlash(factorio, '')).toBe(false);
    expect(missingSlash(factorio, '   ')).toBe(false);
    // Games that take commands without the slash, mixed lists and no
    // commands at all give no hint.
    expect(missingSlash(minecraft, 'list')).toBe(false);
    expect(missingSlash([...minecraft, '/mymod reload'], 'list')).toBe(false);
    expect(missingSlash([], 'players')).toBe(false);
  });

  it('is a hint in the language files and leaves the input alone (NFR-007)', async () => {
    setLanguage('en');
    const input = 'players online';
    const r = await renderPage(() => <SlashHint paths={factorio} input={input} />);
    expect(screen.getByRole('status').textContent).toBe(
      'Commands on this server start with /. Without it, the game may take the input as chat (Factorio does).',
    );
    expect(input).toBe('players online');
    r.unmount();
    setLanguage('xx');
    const { container } = await renderPage(() => <SlashHint paths={factorio} input={input} />);
    expect(unmarked(container)).toEqual([]);
  });

  it('stays away for a command with the slash and for other games', async () => {
    setLanguage('en');
    const a = await renderPage(() => <SlashHint paths={factorio} input="/players" />);
    expect(screen.queryByRole('status')).toBeNull();
    a.unmount();
    await renderPage(() => <SlashHint paths={minecraft} input="list" />);
    expect(screen.queryByRole('status')).toBeNull();
  });
});
