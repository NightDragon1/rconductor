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

// pseudo turns every text of the English file into a marked test text, so
// a test can find text that does not come from a language file (NFR-007).
export function pseudo(v: unknown): unknown {
  if (typeof v === 'string') return `⟦${v.replace(/\{\{(\w+)\}\}/g, '{{$1}}')}⟧`;
  if (v && typeof v === 'object') {
    return Object.fromEntries(Object.entries(v).map(([k, x]) => [k, pseudo(x)]));
  }
  return v;
}

export function setLanguage(lng: 'en' | 'xx') {
  return initI18n(lng, { xx: { translation: pseudo(en) as object } });
}

// renderPage renders a page component on its own route.
export async function renderPage(Page: () => React.JSX.Element) {
  const root = createRootRoute();
  const route = createRoute({ getParentRoute: () => root, path: '/', component: Page });
  const router = createRouter({
    routeTree: root.addChildren([route]),
    history: createMemoryHistory(),
  });
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const r = render(
    <QueryClientProvider client={qc}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  await router.load();
  return r;
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
