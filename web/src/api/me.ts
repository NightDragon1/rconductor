// SPDX-License-Identifier: AGPL-3.0-or-later
import { useQuery } from '@tanstack/react-query';
import { api, must } from './client';

export const meKey = ['me'] as const;

export function fetchMe() {
  return api.GET('/me').then(must);
}

export function useMe() {
  return useQuery({ queryKey: meKey, queryFn: fetchMe, staleTime: 60_000 });
}
