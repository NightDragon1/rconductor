// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { fireEvent, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type { Command } from '@/lib/commandform';
import { renderPage, setLanguage, unmarked } from '@/test/render';
import { CommandCard, HistoryItem, TemplateCard } from './CommandsTab';

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
      /^(kick|weather|Kick a player|player|kick two words|kick \{player\} \{reason\}|weather …|aria-label=.*)$/;
    expect(unmarked(container).filter((s) => !data.test(s))).toEqual([]);
  });

  it('show a player name in the preview as foreign text (SEC-006)', async () => {
    setLanguage('en');
    const { container } = await renderPage(() => (
      <CommandCard
        c={kick}
        values={{ player: 'evil\u202Egnp.exe' }}
        players={[]}
        listId="p"
        busy={false}
        isPlayer={() => true}
        onChange={() => undefined}
        onFocusPlayer={() => undefined}
        onRun={() => undefined}
      />
    ));
    const preview = container.querySelector('p[aria-live="polite"]');
    expect(preview?.textContent).toContain('›');
    // The bidi control is isolated and named, not applied.
    expect(preview?.querySelector('bdi[data-code]')).toBeTruthy();
  });

  it('show a player name in the preview of a template as foreign text (SEC-006)', async () => {
    setLanguage('en');
    const tmpl = {
      id: '00000000-0000-4000-8000-000000000001',
      name: 'kick one',
      template: 'kick {player}',
      placeholders: ['player'],
    } as never;
    const { container } = await renderPage(() => (
      <TemplateCard
        tmpl={tmpl}
        values={{ player: 'evil\u202Egnp.exe' }}
        players={[]}
        listId="p"
        busy={false}
        isPlayer={() => true}
        action="Run"
        onChange={() => undefined}
        onFocusPlayer={() => undefined}
        onRun={() => undefined}
      />
    ));
    const preview = container.querySelector('p[aria-live="polite"]');
    expect(preview?.querySelector('bdi[data-code]')).toBeTruthy();
  });

  it('keep a player field as free text when the game gives no names (PROT-003)', async () => {
    setLanguage('en');
    const { container } = await renderPage(() => (
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
      />
    ));
    const input = container.querySelector('input');
    expect(input).toBeTruthy();
    expect(input?.getAttribute('list')).toBeNull();
    expect(container.querySelector('datalist')).toBeNull();
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

describe('history of the commands', () => {
  it('C237: names a say refused as too long with its own text (CMD-020)', async () => {
    setLanguage('en');
    await renderPage(() => (
      <ul>
        <HistoryItem
          b={{
            kind: 'cmd',
            id: 'r1',
            ts: '2026-11-22T12:00:00Z',
            command: 'say hello',
            result: 'denied',
            reason: 'message_too_long',
          }}
        />
      </ul>
    ));
    expect(
      screen.getByText('The message is too long for the server with your name and the prefix'),
    ).toBeTruthy();
  });
});
