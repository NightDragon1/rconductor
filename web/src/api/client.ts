// SPDX-License-Identifier: AGPL-3.0-or-later
import createClient from 'openapi-fetch';
import type { components, paths } from './schema';

export type Problem = components['schemas']['Problem'];
export type Session = components['schemas']['Session'];

// The API lives below the base path the panel names in <base href>.
export function apiBase(): string {
  return new URL('api/v1', document.baseURI).toString();
}

export const api = createClient<paths>({ baseUrl: apiBase(), credentials: 'same-origin' });

// ApiError carries the Problem Details of a failed call (RFC 9457).
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly problem?: Problem,
    // Seconds from Retry-After (429, 503), if the panel sent it.
    readonly retryAfter?: number,
  ) {
    super(problem?.title ?? `HTTP ${status}`);
  }
}

// must unwraps an openapi-fetch result or throws ApiError.
export function must<T>(r: { data?: T; error?: unknown; response: Response }): T {
  if (r.error !== undefined || !r.response.ok) {
    const retry = Number(r.response.headers.get('Retry-After'));
    throw new ApiError(
      r.response.status,
      r.error as Problem | undefined,
      Number.isFinite(retry) && retry > 0 ? retry : undefined,
    );
  }
  return r.data as T;
}
