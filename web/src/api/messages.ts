// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { useQuery } from '@tanstack/react-query';
import { api, must } from './client';
import type { components } from './schema';

export type Message = components['schemas']['Message'];
export type MessageComment = components['schemas']['MessageComment'];
export type MessageAudience = components['schemas']['MessageAudience'];

// MESSAGES_FLAG is the flag name the backend reports in ui.features
// (MSG-001 to MSG-003, NFR-019).
export const MESSAGES_FLAG = 'messages';

// Every query of the messages sits below this key: a change reads them all again.
export const messagesKey = ['messages'] as const;

// useUnreadMessages reads the number of unread messages to me for the
// menu; no request goes out while the flag is off.
export function useUnreadMessages(enabled: boolean) {
  return useQuery({
    queryKey: [...messagesKey, 'unread'],
    queryFn: async () =>
      must(await api.GET('/messages', { params: { query: { limit: 1 } } })).unread,
    enabled,
  });
}
