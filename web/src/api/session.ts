// SPDX-License-Identifier: AGPL-3.0-or-later
import { useQuery } from '@tanstack/react-query';
import { api, ApiError, must, type Session } from './client';

export const sessionKey = ['session'] as const;

// fetchSession returns the session, or null without one (401).
export async function fetchSession(): Promise<Session | null> {
  const r = await api.GET('/auth/session');
  if (r.response.status === 401) {
    return null;
  }
  return must(r);
}

export function useSession() {
  return useQuery({ queryKey: sessionKey, queryFn: fetchSession, staleTime: 60_000, retry: false });
}

export function isUnauthenticated(e: unknown): boolean {
  return e instanceof ApiError && e.status === 401;
}
