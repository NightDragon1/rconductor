// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
// Breaks the HTML rule behind an eslint-disable comment (fitness test data).
import type { components } from './api/schema';

export function Bad({ a }: { a: components['schemas']['Agent'] }) {
  // eslint-disable-next-line no-restricted-syntax
  return <p dangerouslySetInnerHTML={{ __html: a.name }}>{a.name}</p>;
}
