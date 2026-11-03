// SPDX-License-Identifier: AGPL-3.0-or-later
import { describe, expect, it } from 'vitest';
import { buildCommand, FREE, formErrors, pattern, type Command } from './commandform';

const kick: Command = {
  path: 'kick',
  description: 'Kick a player',
  params: [
    { name: 'player', type: 'player' },
    { name: 'reason', type: 'text', optional: true },
  ],
};
const time: Command = {
  path: 'time set',
  description: 'Set the time',
  params: [{ name: 'time', type: 'choice', values: ['day', 'night'] }],
};
const count: Command = {
  path: 'xp add',
  description: 'Give xp',
  params: [
    { name: 'player', type: 'player' },
    { name: 'amount', type: 'number', min: 1, max: 100 },
  ],
};
const free: Command = { path: 'weather', description: '', params: [] };

describe('command form (CMD-003)', () => {
  it('shows the pattern of the parameters', () => {
    expect(pattern(kick)).toBe('kick {player} {reason}');
    expect(pattern(free)).toBe('weather …');
  });

  it('builds the command in parameter order', () => {
    expect(buildCommand(kick, { player: 'kiwi', reason: 'Spam im Chat' })).toBe(
      'kick kiwi Spam im Chat',
    );
    expect(buildCommand(kick, { player: 'kiwi' })).toBe('kick kiwi');
    expect(buildCommand(free, { [FREE]: ' clear 600 ' })).toBe('weather clear 600');
    expect(buildCommand(free, {})).toBe('weather');
  });

  it('checks the fields like the backend (revision 2.4 candidate 19)', () => {
    expect(formErrors(kick, {})).toEqual({ player: 'form.required' });
    expect(formErrors(kick, { player: 'two words' })).toEqual({ player: 'form.oneWord' });
    expect(formErrors(kick, { player: 'kiwi' })).toEqual({});
    expect(formErrors(time, { time: 'dusk' })).toEqual({ time: 'form.choice' });
    expect(formErrors(time, { time: 'day' })).toEqual({});
    expect(formErrors(count, { player: 'a', amount: '2.5' })).toEqual({ amount: 'form.whole' });
    expect(formErrors(count, { player: 'a', amount: '101' })).toEqual({ amount: 'form.range' });
    expect(formErrors(count, { player: 'a', amount: '7' })).toEqual({});
    expect(formErrors(free, {})).toEqual({});
  });
});
