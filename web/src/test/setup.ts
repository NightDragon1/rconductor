// SPDX-License-Identifier: AGPL-3.0-or-later
import { cleanup } from '@testing-library/react';
import { afterEach } from 'vitest';

afterEach(() => cleanup());

// jsdom has no scrolling; the router restores scroll positions.
window.scrollTo = () => {};
