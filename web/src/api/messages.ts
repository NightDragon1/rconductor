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

// titleMax is the length of the list title, cut from the first line.
const titleMax = 120;

// messageTitle names a message in the list: a message has no subject, so
// the first line with text stands for it, cut to titleMax characters.
export function messageTitle(text: string): string {
  const line = (text.split('\n', 50).find((l) => l.trim() !== '') ?? '').trim();
  const chars = Array.from(line.slice(0, titleMax * 2));
  return chars.length > titleMax ? `${chars.slice(0, titleMax).join('')}…` : line;
}
