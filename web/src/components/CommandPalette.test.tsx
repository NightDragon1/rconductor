// SPDX-License-Identifier: AGPL-3.0-or-later
import { act, fireEvent, screen, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { favoritesKey, serversKey } from '@/api/servers';
import { templatesKey } from '@/api/templates';
import { MAX_RESULTS, paletteEntries, searchPalette } from '@/lib/palette';
import { renderPage, setLanguage, unmarked } from '@/test/render';
import { CommandPalette } from './CommandPalette';

const ui = vi.hoisted(() => ({ features: [] as string[], shortcuts: false }));
vi.mock('@/api/session', async (original) => ({
  ...(await original<typeof import('@/api/session')>()),
  useSession: () => ({
    data: {
      user: { id: 'boss', pseudonym: 'boss', role: 'server_admin' },
      ui: {
        features: ui.features,
        shortcuts_enabled: ui.shortcuts,
        supporters_enabled: false,
        supporters_allowed: false,
      },
    },
  }),
}));

const cmd = (path: string, description = '') => ({ path, description, params: [] });
const allowed = (commands: ReturnType<typeof cmd>[]) =>
  [['allowed-commands', 's1'], { free_input: false, commands }] as [readonly unknown[], unknown];
const template = {
  id: 't1',
  server_id: 's1',
  name: 'Greet',
  template: 'say hello {player}',
  placeholders: ['player'],
  version: '1',
};
const data = (commands = [cmd('list', 'Players online'), cmd('kick', 'Kick a player')]) => [
  allowed(commands),
  [templatesKey('s1'), [template]] as [readonly unknown[], unknown],
  [favoritesKey, ['s1', 's2']] as [readonly unknown[], unknown],
  [
    serversKey,
    [
      { id: 's1', name: 'alpha' },
      { id: 's2', name: 'beta' },
    ],
  ] as [readonly unknown[], unknown],
];

async function show(onUse = vi.fn(), commands?: ReturnType<typeof cmd>[]) {
  const r = await renderPage(() => <CommandPalette serverId="s1" onUse={onUse} />, data(commands));
  return Object.assign(r, { onUse });
}
const ctrlK = () => act(() => void fireEvent.keyDown(window, { key: 'k', ctrlKey: true }));
const options = () => within(screen.getByRole('listbox')).queryAllByRole('option');

beforeEach(async () => {
  ui.features = ['command_palette'];
  ui.shortcuts = false;
  await setLanguage('en');
});

describe('command palette (CMD-016)', () => {
  it('has no button and ignores Ctrl+K while its flag is off', async () => {
    ui.features = [];
    ui.shortcuts = true;
    await show();
    expect(screen.queryByRole('button', { name: 'Command palette' })).toBeNull();
    ctrlK();
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('opens by the button; Ctrl+K has no effect while shortcuts are off', async () => {
    await show();
    ctrlK();
    expect(screen.queryByRole('dialog')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Command palette' }));
    expect(screen.getByRole('dialog')).toBeTruthy();
  });

  it('opens on Ctrl+K and Cmd+K while shortcuts are on, also from a field', async () => {
    ui.shortcuts = true;
    await show();
    ctrlK();
    expect(screen.getByRole('dialog')).toBeTruthy();
    fireEvent.keyDown(screen.getByRole('combobox'), { key: 'Escape' });
    expect(screen.queryByRole('dialog')).toBeNull();
    act(() => void fireEvent.keyDown(window, { key: 'K', metaKey: true }));
    expect(screen.getByRole('dialog')).toBeTruthy();
  });

  it('lists exactly the commands of the backend list, the templates and other favorites', async () => {
    await show();
    fireEvent.click(screen.getByRole('button', { name: 'Command palette' }));
    const texts = options().map((o) => o.textContent);
    expect(texts).toEqual([
      'Commandlist' + 'Players online',
      'Commandkick' + 'Kick a player',
      'TemplateGreet' + 'say hello {player}',
      'Favoritebeta',
    ]);
  });

  it('filters by every word and puts a command into the input line', async () => {
    const { onUse } = await show();
    fireEvent.click(screen.getByRole('button', { name: 'Command palette' }));
    const box = screen.getByRole('combobox');
    fireEvent.change(box, { target: { value: 'KICK player' } });
    expect(options()).toHaveLength(1);
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(onUse).toHaveBeenCalledWith('kick ');
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('walks the results with the arrow keys and takes a template as it is', async () => {
    const { onUse } = await show();
    fireEvent.click(screen.getByRole('button', { name: 'Command palette' }));
    const box = screen.getByRole('combobox');
    fireEvent.keyDown(box, { key: 'ArrowDown' });
    fireEvent.keyDown(box, { key: 'ArrowDown' });
    expect(options()[2]?.getAttribute('aria-selected')).toBe('true');
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(onUse).toHaveBeenCalledWith('say hello {player}');
  });

  it('says so when nothing matches', async () => {
    await show();
    fireEvent.click(screen.getByRole('button', { name: 'Command palette' }));
    fireEvent.change(screen.getByRole('combobox'), { target: { value: 'nothing-like-this' } });
    expect(options()).toHaveLength(0);
    expect(screen.getByText('Nothing matches.')).toBeTruthy();
  });

  it('stays usable with 10 000 commands: few rows, search narrows down', async () => {
    const many = Array.from({ length: 10_000 }, (_, i) => cmd(`cmd-${i}`, `number ${i}`));
    const { onUse } = await show(vi.fn(), many);
    const t0 = performance.now();
    fireEvent.click(screen.getByRole('button', { name: 'Command palette' }));
    expect(options()).toHaveLength(MAX_RESULTS);
    expect(screen.getByRole('status').textContent).toBe(
      `Showing ${MAX_RESULTS} of 10002. Type more to narrow it down.`,
    );
    const box = screen.getByRole('combobox');
    fireEvent.change(box, { target: { value: 'cmd-9999' } });
    expect(options()).toHaveLength(1);
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(onUse).toHaveBeenCalledWith('cmd-9999 ');
    // Generous bound against a slow test machine; a full list of 10 000
    // rows would take far longer in jsdom.
    expect(performance.now() - t0).toBeLessThan(5000);
  });

  it('shows the bidi controls of foreign command texts', async () => {
    await show(vi.fn(), [cmd('evil‮cmd')]);
    fireEvent.click(screen.getByRole('button', { name: 'Command palette' }));
    expect(screen.getByRole('listbox').querySelector('[data-code]')).not.toBeNull();
  });

  it('has no text outside the language files (NFR-007)', async () => {
    await setLanguage('xx');
    await show();
    fireEvent.click(screen.getByRole('button'));
    const data = ['list', 'Players online', 'kick', 'Kick a player', 'Greet'];
    const rest = unmarked(document.body).filter(
      (s) => !data.includes(s) && s !== 'say hello {player}' && s !== 'beta',
    );
    expect(rest).toEqual([]);
  });
});

describe('palette search', () => {
  it('keeps the order, counts all matches and caps the shown ones', () => {
    const e = paletteEntries(
      [cmd('a1'), cmd('b'), cmd('a2'), cmd('a3')],
      [],
      [{ id: 'x', name: 'a-server' }],
    );
    const r = searchPalette(e, 'a', 2);
    expect(r.total).toBe(4);
    expect(r.shown.map((x) => x.key)).toEqual(['c:a1', 'c:a2']);
    expect(searchPalette(e, '   ').total).toBe(5);
  });
});
