// SPDX-License-Identifier: AGPL-3.0-or-later
import { useMutation } from '@tanstack/react-query';
import { api, must } from './client';
import type { components } from './schema';

export type MultiResult = components['schemas']['MultiCommandResult'];

// useMultiCommand sends one command to several servers (CMD-010); every
// server gets its own run and its own result.
export function useMultiCommand() {
  return useMutation({
    mutationFn: async (v: { serverIds: string[]; command: string }) =>
      must(
        await api.POST('/commands/multi', {
          body: { server_ids: v.serverIds, command: v.command },
        }),
      ),
  });
}
