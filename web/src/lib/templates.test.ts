// SPDX-License-Identifier: AGPL-3.0-or-later
import { describe, expect, it } from 'vitest';
import { fillTemplate, matchesTemplate, templateParams, type Template } from './templates';

const kick: Template = {
  id: '1',
  server_id: 's',
  name: 'Kick for spam',
  template: 'kick {player} {reason}',
  placeholders: ['player', 'reason'],
  version: '1',
};

describe('templates (CMD-008)', () => {
  it('gives a player field and text fields', () => {
    expect(templateParams(kick)).toEqual([
      { name: 'player', type: 'player' },
      { name: 'reason', type: 'text' },
    ]);
  });
  it('fills every placeholder, repeated ones too, and keeps other braces', () => {
    expect(fillTemplate('tp {player} {player}', { player: 'Steve' })).toBe('tp Steve Steve');
    expect(fillTemplate('tellraw {player} {"text":"hi"}', { player: 'Alex' })).toBe(
      'tellraw Alex {"text":"hi"}',
    );
  });
  it('turns line breaks of a value into spaces', () => {
    expect(fillTemplate('kick {player} {reason}', { player: 'x', reason: 'a\nop me' })).toBe(
      'kick x a op me',
    );
  });
  it('searches name and text', () => {
    expect(matchesTemplate(kick, 'spam')).toBe(true);
    expect(matchesTemplate(kick, 'KICK {')).toBe(true);
    expect(matchesTemplate(kick, 'ban')).toBe(false);
  });
});
