// SPDX-License-Identifier: AGPL-3.0-or-later
import { useQueries, useQuery } from '@tanstack/react-query';
import { api, ApiError, must } from './client';
import type { components } from './schema';

export type User = components['schemas']['User'];
export type Invite = components['schemas']['Invite'];
export type Assignments = components['schemas']['Assignments'];
export type AssignmentsUpdate = components['schemas']['AssignmentsUpdate'];
export type UserGroup = components['schemas']['UserGroup'];
export type Role = components['schemas']['Role'];

export const managedUsersKey = ['users'] as const;
export const invitesKey = ['invites'] as const;
export const userGroupsKey = ['user-groups'] as const;
export const assignmentsKey = (id: string) => ['assignments', id] as const;

export function useManagedUsers() {
  return useQuery({
    queryKey: managedUsersKey,
    queryFn: async () => must(await api.GET('/users', { params: { query: {} } })).items,
  });
}

export function useInvites() {
  return useQuery({
    queryKey: invitesKey,
    queryFn: async () => must(await api.GET('/invites')).items,
  });
}

// User groups are for RCON admin and server admin (Design 2.2).
export function useUserGroups(enabled: boolean) {
  return useQuery({
    queryKey: userGroupsKey,
    queryFn: async () => must(await api.GET('/user-groups')).items,
    enabled,
  });
}

export async function fetchAssignments(id: string): Promise<Assignments> {
  return must(await api.GET('/users/{id}/assignments', { params: { path: { id } } }));
}

export function useAssignments(id: string | undefined) {
  return useQuery({
    queryKey: assignmentsKey(id ?? ''),
    queryFn: () => fetchAssignments(id ?? ''),
    enabled: !!id,
  });
}

// useAllAssignments loads the assignments of the listed users for the
// column of groups and scope.
export function useAllAssignments(ids: string[]) {
  return useQueries({
    queries: ids.map((id) => ({
      queryKey: assignmentsKey(id),
      queryFn: () => fetchAssignments(id),
    })),
  });
}

// Outcome is the result of one change for one user: ok, or the reason
// code of the refusal (Design 3.5).
export interface Outcome {
  id: string;
  ok: boolean;
  reason?: string;
}

export async function outcome(id: string, run: () => Promise<unknown>): Promise<Outcome> {
  try {
    await run();
    return { id, ok: true };
  } catch (e) {
    return {
      id,
      ok: false,
      reason: e instanceof ApiError ? (e.problem?.reason ?? String(e.status)) : 'error',
    };
  }
}

// Roles a caller may give: always below his own (ROLE-008).
export function rolesBelow(role: string | undefined): Role[] {
  const order: Role[] = ['rcon_admin', 'server_admin', 'moderator', 'supporter'];
  const i = order.indexOf(role as Role);
  return i < 0 ? [] : order.slice(i + 1);
}

export type CommandList = components['schemas']['CommandList'];
export type CommandListEntry = components['schemas']['CommandListEntry'];
export const commandListsKey = ['command-lists'] as const;

// Command lists are for RCON admin and server admin (Design 2.2).
export function useCommandLists(enabled: boolean) {
  return useQuery({
    queryKey: commandListsKey,
    queryFn: async () => must(await api.GET('/command-lists')).items,
    enabled,
  });
}
