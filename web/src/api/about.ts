// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later
import { useQuery } from '@tanstack/react-query';
import { api, must } from './client';
import type { components } from './schema';

export type About = components['schemas']['About'];
export type AboutAgent = components['schemas']['AboutAgent'];

// The answer depends on the caller (UI-024): the panel leaves out what a
// role may not see, so each user has an entry of their own and an
// anonymous caller the empty one.
export function aboutKey(user?: string) {
  return ['about', user ?? ''] as const;
}

export function fetchAbout() {
  return api.GET('/about').then(must);
}

export function useAbout(user?: string, enabled = true) {
  return useQuery({ queryKey: aboutKey(user), queryFn: fetchAbout, staleTime: 60_000, enabled });
}

// The license text in the repository; the third-party licenses lie next to
// the interface, so the link stays below the base path.
export const licenseUrl = 'https://github.com/NightDragon1/rconductor/blob/main/LICENSE';
export const thirdPartyUrl = 'third-party-licenses.txt';

// webUrl lets only an http(s) address become a link.
export function webUrl(url: string): string | undefined {
  return /^https?:\/\//i.test(url) ? url : undefined;
}

// The license list of the Go modules linked into the panel; the image build
// puts it next to the UI (Dockerfile). A build without it shows only the
// list of the UI.
export const thirdPartyGoUrl = 'third-party-go.txt';

// ThirdParty is one component of the license lists (UI-024, NFR-021).
export type ThirdParty = {
  name: string;
  version: string;
  license: string;
  // The copyright lines of the license text; empty when it has none.
  copyright: string[];
  text: string;
};

// Both lists start each component with "== <name> <version> (<license>)",
// the license text follows up to the next header.
const header = /^== (.+?) (\S+) \((.*)\)$/;
// A copyright line, but not the placeholder of a license template such as
// the appendix of the Apache license.
const copyrightLine = /^(Copyright\b|COPYRIGHT\b|\([cC]\)|©)/;
const placeholder = /[[{<]yyyy[\]}>]|\[year\]|<year>/i;

// parseThirdParty reads one license list; text before the first header is
// no component.
export function parseThirdParty(list: string): ThirdParty[] {
  const out: ThirdParty[] = [];
  let lines: string[] = [];
  let cur: Omit<ThirdParty, 'copyright' | 'text'> | undefined;
  const close = () => {
    if (!cur) return;
    const text = lines.join('\n').trim();
    const copyright = [
      ...new Set(
        lines.map((l) => l.trim()).filter((l) => copyrightLine.test(l) && !placeholder.test(l)),
      ),
    ];
    out.push({ ...cur, copyright, text });
  };
  for (const line of list.split('\n')) {
    const m = header.exec(line.trimEnd());
    if (m) {
      close();
      cur = { name: m[1] ?? '', version: m[2] ?? '', license: m[3] ?? '' };
      lines = [];
    } else if (cur) {
      lines.push(line);
    }
  }
  close();
  return out;
}

// fetchList reads one list below the base path; a missing list is empty.
async function fetchList(url: string): Promise<string> {
  try {
    const r = await fetch(url, { cache: 'no-cache' });
    return r.ok ? await r.text() : '';
  } catch {
    return '';
  }
}

// useThirdParty reads the lists of the Go modules and of the UI once.
export function useThirdParty(enabled: boolean) {
  return useQuery({
    queryKey: ['third-party'],
    queryFn: async () => {
      const [go, ui] = await Promise.all([fetchList(thirdPartyGoUrl), fetchList(thirdPartyUrl)]);
      return [...parseThirdParty(go), ...parseThirdParty(ui)];
    },
    staleTime: Infinity,
    enabled,
  });
}
