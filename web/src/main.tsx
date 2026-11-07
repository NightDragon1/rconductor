// SPDX-License-Identifier: AGPL-3.0-or-later
import { QueryCache, QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { RouterProvider } from '@tanstack/react-router';
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { setNonce } from 'get-nonce';
import { isUnauthenticated } from '@/api/session';
import { initI18n } from '@/i18n';
import { bufferOwner } from '@/lib/console';
import { startTheme } from '@/lib/theme';
import { makeRouter } from '@/router';
import './index.css';

// The panel gives each page a nonce for style elements (SEC-009); dialogs
// need one to lock scrolling.
const nonce = document.querySelector<HTMLMetaElement>('meta[name="rcd-style-nonce"]')?.content;
if (nonce) setNonce(nonce);
startTheme();
initI18n();
// A session that ended leads to the sign-in, and nothing of it stays for the
// next user (#160, #161).
const queryClient: QueryClient = new QueryClient({
  defaultOptions: {
    queries: {
      refetchOnWindowFocus: false,
      retry: (count, e) => !isUnauthenticated(e) && count < 3,
    },
  },
  queryCache: new QueryCache({
    onError: (e) => {
      if (!isUnauthenticated(e)) return;
      queryClient.clear();
      bufferOwner(undefined);
      void router.navigate({ to: '/login' });
    },
  }),
});
const router = makeRouter(queryClient);

const root = document.getElementById('root');
if (root) {
  createRoot(root).render(
    <StrictMode>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </StrictMode>,
  );
}
