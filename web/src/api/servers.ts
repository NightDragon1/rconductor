// SPDX-License-Identifier: AGPL-3.0-or-later
import { useQuery, type QueryClient } from '@tanstack/react-query';
import { api, must } from './client';
import type { components } from './schema';

export type Server = components['schemas']['Server'];
export type ServerRuntime = components['schemas']['ServerRuntime'];
export type ServerGroup = components['schemas']['ServerGroup'];
export type GameProfile = components['schemas']['GameProfile'];
export type Agent = components['schemas']['Agent'];
export type User = components['schemas']['User'];

export const serversKey = ['servers'] as const;
export const serverGroupsKey = ['server-groups'] as const;
export const profilesKey = ['profiles'] as const;
export const agentsKey = ['agents'] as const;
export const usersKey = ['users'] as const;

export function useServers() {
  return useQuery({
    queryKey: serversKey,
    queryFn: async () => must(await api.GET('/servers')).items,
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
