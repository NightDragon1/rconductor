// SPDX-License-Identifier: AGPL-3.0-or-later
// Outside src/, imported from it (fitness test data).
import type { components } from '../src/api/schema';

export function Away({ a }: { a: components['schemas']['Agent'] }) {
  return <p dangerouslySetInnerHTML={{ __html: a.name }}>{a.name}</p>;
}
