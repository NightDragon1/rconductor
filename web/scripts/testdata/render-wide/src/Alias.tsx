// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
// Another function imported under the name of the cleaning one (fitness test
// data): it must be reported.
import type { components } from './api/schema';
import { rawText as plainText } from './lib/foreign';

export function Alias({ a }: { a: components['schemas']['Agent'] }) {
  return <p>{plainText(a.name)}</p>;
}
