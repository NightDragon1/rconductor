// SPDX-License-Identifier: AGPL-3.0-or-later
// Breaks the render rule on purpose (fitness test data).
import type { components } from './api/schema';

export function Bad({ a }: { a: components['schemas']['Agent'] }) {
  return <p dangerouslySetInnerHTML={{ __html: a.name }}>{a.name}</p>;
}
