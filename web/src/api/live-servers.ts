// SPDX-License-Identifier: AGPL-3.0-or-later
import { useQueryClient } from '@tanstack/react-query';
import { useParams } from '@tanstack/react-router';
import { useTopic } from '@/lib/live-context';
import { applyOverview, useServers, type Server } from './servers';
import { useSession } from './session';

// useLiveServers returns the servers in scope and keeps them live over
// servers.overview (Design 2.3).
export function useLiveServers() {
  const qc = useQueryClient();
  const { data: session } = useSession();
  useTopic(session ? 'servers.overview' : undefined, (m) => {
    if (m.type === 'event') applyOverview(qc, m.data);
  });
  return useServers();
}

// useServer reads the server of the page from the live list of servers in
// scope; the route loader has made sure it is there.
export function useServer(): Server | undefined {
  const { serverId } = useParams({ strict: false });
  const servers = useLiveServers();
  return servers.data?.find((s) => s.id === serverId);
}
