// SPDX-License-Identifier: AGPL-3.0-or-later
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router';
import { render } from '@testing-library/react';
import en from '@/i18n/en.json';
import { initI18n } from '@/i18n';
import { pseudo } from '@/test/pseudo';

export function setLanguage(lng: 'en' | 'xx') {
  return initI18n(lng, { xx: { translation: pseudo(en) as object } });
}

// renderPage renders a page component on its own route.
export async function renderPage(
  Page: () => React.JSX.Element | null,
  data: [readonly unknown[], unknown][] = [],
) {
  const root = createRootRoute();
  const route = createRoute({ getParentRoute: () => root, path: '/', component: Page });
  const router = createRouter({
    routeTree: root.addChildren([route]),
    history: createMemoryHistory(),
  });
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  for (const [key, value] of data) qc.setQueryData(key, value);
  const r = render(
    <QueryClientProvider client={qc}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  await router.load();
  // The query client goes along for tests that change the cache later.
  return Object.assign(r, { qc });
}

// unmarked lists visible texts and labels without the test-language mark.
export function unmarked(el: HTMLElement): string[] {
  const out: string[] = [];
  const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
  for (let n = walker.nextNode(); n; n = walker.nextNode()) {
    const s = n.textContent?.trim() ?? '';
    if (s && !s.includes('⟦')) out.push(s);
  }
  for (const e of el.querySelectorAll('[aria-label],[title],[placeholder]')) {
    for (const a of ['aria-label', 'title', 'placeholder']) {
      const s = e.getAttribute(a);
      if (s && !s.includes('⟦')) out.push(`${a}=${s}`);
    }
  }
  return out;
}
