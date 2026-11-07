// SPDX-License-Identifier: AGPL-3.0-or-later
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { RouterProvider } from '@tanstack/react-router';
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { setNonce } from 'get-nonce';
import { initI18n } from '@/i18n';
import { startTheme } from '@/lib/theme';
import { makeRouter } from '@/router';
import './index.css';

// The panel gives each page a nonce for style elements (SEC-009); dialogs
// need one to lock scrolling.
const nonce = document.querySelector<HTMLMetaElement>('meta[name="rcd-style-nonce"]')?.content;
if (nonce) setNonce(nonce);
startTheme();
initI18n();
const queryClient = new QueryClient({
  defaultOptions: { queries: { refetchOnWindowFocus: false } },
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
