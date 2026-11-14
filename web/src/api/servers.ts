// SPDX-License-Identifier: AGPL-3.0-or-later
import {
  queryOptions,
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
} from '@tanstack/react-query';
import { api, ApiError, must } from './client';
import type { components } from './schema';

export type Server = components['schemas']['Server'];
export type ServerRuntime = components['schemas']['ServerRuntime'];
export type ServerGroup = components['schemas']['ServerGroup'];
export type GameProfile = components['schemas']['GameProfile'];
export type EffectiveProfile = components['schemas']['EffectiveProfile'];
export type Agent = components['schemas']['Agent'];
export type User = components['schemas']['User'];

export const serversKey = ['servers'] as const;
export const serverGroupsKey = ['server-groups'] as const;
export const profilesKey = ['profiles'] as const;
export const agentsKey = ['agents'] as const;
export const usersKey = ['users'] as const;

export type HistoryEntry = components['schemas']['HistoryEntry'];
export type PlayerList = components['schemas']['PlayerList'];
export type AllowedCommands = components['schemas']['AllowedCommands'];

// serversQuery is the list of servers in scope; the server page and its
// guards read a server from it.
export const serversQuery = queryOptions({
  queryKey: serversKey,
  queryFn: async () => must(await api.GET('/servers')).items,
});

export function useServers() {
  return useQuery(serversQuery);
}

export function usePlayers(id: string) {
  return useQuery({
    queryKey: ['players', id],
    queryFn: async () => must(await api.GET('/servers/{id}/players', { params: { path: { id } } })),
    refetchInterval: 60_000,
  });
}

export function useAllowedCommands(id: string, enabled: boolean) {
  return useQuery({
    queryKey: ['allowed-commands', id],
    queryFn: async () =>
      must(await api.GET('/servers/{id}/commands', { params: { path: { id } } })),
    enabled,
  });
}

// useHistory pages through the command history, newest first; own keeps
// the caller's commands only. What a role may see the backend decides
// (ROLE-009).
export function useHistory(id: string, own = false) {
  return useInfiniteQuery({
    queryKey: ['history', id, own],
    queryFn: async ({ pageParam }) =>
      must(
        await api.GET('/servers/{id}/history', {
          params: { path: { id }, query: { limit: 50, cursor: pageParam, own: own || undefined } },
        }),
      ),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor || undefined,
  });
}

export const favoritesKey = ['favorites'] as const;

// useFavorites holds the caller's favorite servers (CMD-001), kept in the
// backend so that they follow him to every device.
export function useFavorites(enabled = true) {
  return useQuery({
    queryKey: favoritesKey,
    queryFn: async () => must(await api.GET('/me/favorites')).server_ids,
    enabled,
  });
}

export function useSetFavorites() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (ids: string[]) =>
      must(await api.PUT('/me/favorites', { body: { server_ids: ids } })).server_ids,
    onSuccess: (ids) => qc.setQueryData(favoritesKey, ids),
  });
}

// Server groups are readable for RCON admin and server admin only
// (Design 2.2); the others never ask.
export function useServerGroups(enabled: boolean) {
  return useQuery({
    queryKey: serverGroupsKey,
    queryFn: async () => must(await api.GET('/server-groups')).items,
    enabled,
  });
}

export function useProfiles() {
  return useQuery({
    queryKey: profilesKey,
    queryFn: async () => must(await api.GET('/profiles')).items,
    staleTime: 5 * 60_000,
  });
}

// useServerProfile reads the effective game profile of a server (UI-020,
// flag profile_browser). A 404 (flag off, server outside the scope) is an
// answer, not a fault: it is not asked again.
export function useServerProfile(id: string | undefined) {
  return useQuery({
    queryKey: ['server-profile', id],
    queryFn: async () =>
      must(await api.GET('/servers/{id}/profile', { params: { path: { id: id ?? '' } } })),
    enabled: id !== undefined,
    staleTime: 5 * 60_000,
    retry: (count, e) => !(e instanceof ApiError && e.status === 404) && count < 3,
  });
}

export function useAgents(enabled: boolean) {
  return useQuery({
    queryKey: agentsKey,
    queryFn: async () => must(await api.GET('/agents')).items,
    enabled,
  });
}

export function useUsers(enabled: boolean) {
  return useQuery({
    queryKey: usersKey,
    queryFn: async () => must(await api.GET('/users', { params: { query: {} } })).items,
    enabled,
  });
}

interface StatusEvent {
  kind: 'status';
  server_id: string;
  status: Server['status'];
  since: string;
}

// applyOverview puts an event of servers.overview into the server list:
// a reachability change or new runtime data (Design 2.3). Anything else is
// ignored; the data is foreign to the browser only in shape, so it is
// checked before use.
export function applyOverview(qc: QueryClient, data: unknown) {
  if (!data || typeof data !== 'object') return;
  const ev = data as Partial<StatusEvent> & Partial<ServerRuntime>;
  if (typeof ev.server_id !== 'string') return;
  qc.setQueryData<Server[]>(serversKey, (list) =>
    list?.map((s) => {
      if (s.id !== ev.server_id) return s;
      if (ev.kind === 'status' && ev.status && ev.since) {
        return { ...s, status: ev.status, status_since: ev.since };
      }
      if (ev.kind === 'runtime') return { ...s, runtime: ev as ServerRuntime };
      return s;
    }),
  );
}
