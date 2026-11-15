// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { useQuery } from '@tanstack/react-query';
import { api, must } from './client';
import type { components } from './schema';

export type About = components['schemas']['About'];
export type AboutAgent = components['schemas']['AboutAgent'];

// The answer depends on the caller (UI-024): the panel leaves out what a
// role may not see, so each user has an entry of their own and an
// anonymous caller the empty one.
export function aboutKey(user?: string) {
  return ['about', user ?? ''] as const;
}

export function fetchAbout() {
  return api.GET('/about').then(must);
}

export function useAbout(user?: string, enabled = true) {
  return useQuery({ queryKey: aboutKey(user), queryFn: fetchAbout, staleTime: 60_000, enabled });
}

// The license text in the repository; the third-party licenses lie next to
// the interface, so the link stays below the base path.
export const licenseUrl = 'https://github.com/NightDragon1/rconductor/blob/main/LICENSE';
export const thirdPartyUrl = 'third-party-licenses.txt';

// webUrl lets only an http(s) address become a link.
export function webUrl(url: string): string | undefined {
  return /^https?:\/\//i.test(url) ? url : undefined;
}
