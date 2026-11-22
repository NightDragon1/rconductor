// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
// Each line marked bad shows a foreign field in another form (fitness test
// data); the lines marked ok must not be reported.
import type { components } from './api/schema';
import { plainText } from './lib/foreign';

type Agent = components['schemas']['Agent'];
const upper = (s: string) => s.toUpperCase();

export function Wide({ a, list }: { a: Agent; list: Agent[] }) {
  const { name } = a;
  const shown = a.name;
  return (
    <div>
      <p>{upper(a.name)}</p>
      {/* bad: call */}
      <p>{a['name']}</p>
      {/* bad: element access */}
      <p>{`by ${a.name}`}</p>
      {/* bad: template literal */}
      <p>{name}</p>
      {/* bad: destructured */}
      <p>{shown}</p>
      {/* bad: variable */}
      <p>{a.id ? a.name : ''}</p>
      {/* bad: condition */}
      <ul>
        {a.names.map((n) => (
          <li key={n}>{n}</li>
        ))}
      </ul>
      {/* bad: element of a foreign list */}
      <p title={a.name}>x</p>
      {/* bad: attribute */}
      <p>{list.map((x) => x.name).join(', ')}</p>
      {/* bad: mapped value */}
      <p>{a.id}</p>
      {/* ok: not foreign */}
      <p title={plainText(a.name)}>{a.name && 'named'}</p>
      {/* ok: cleaned attribute, condition only */}
      <p key={a.name}>{list.filter((x) => x.name !== '').length}</p>
      {/* ok: key, predicate */}
    </div>
  );
}
