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

// VersionedPath lists the writes that carry x-rcd-required-by-server in
// api/openapi.yaml: the panel refuses them without If-Match (428). The API
// file declares the header optional, so that role and scope are checked
// before it (D-051); patchVersioned takes the version as an argument of its
// own, so no call can leave it out. A Go test keeps this list equal to the
// API file.
export type VersionedPath =
  | '/servers/{id}'
  | '/server-groups/{id}'
  | '/user-groups/{id}'
  | '/command-lists/{id}'
  | '/servers/{id}/templates/{template_id}'
  | '/settings'
  | '/settings/smtp';

// PatchPath are the versioned writes by PATCH; /settings/smtp is a PUT
// (putVersioned).
type PatchPath = Exclude<VersionedPath, '/settings/smtp'>;

type Patch<P extends PatchPath> = paths[P]['patch'];
type PathParams<P extends PatchPath> = Patch<P>['parameters']['path'];
type PatchBody<P extends PatchPath> = Patch<P>['requestBody']['content']['application/json'];
type PatchData<P extends PatchPath> = Patch<P>['responses'][200]['content']['application/json'];
type PatchInit<P extends PatchPath> = { body: PatchBody<P> } & ([PathParams<P>] extends [undefined]
  ? { path?: never }
  : { path: NonNullable<PathParams<P>> });

// patchVersioned sends a versioned write with If-Match set to version.
export function patchVersioned<P extends PatchPath>(
  path: P,
  version: string,
  init: PatchInit<P>,
): Promise<{ data?: PatchData<P>; error?: unknown; response: Response }> {
  const send = api.PATCH as unknown as (
    url: string,
    init: { params: { path?: unknown; header: { 'If-Match': string } }; body: unknown },
  ) => Promise<{ data?: PatchData<P>; error?: unknown; response: Response }>;
  return send(path, {
    params: { path: init.path, header: { 'If-Match': version } },
    body: init.body,
  });
}

type SMTPPut = paths['/settings/smtp']['put'];

// putVersioned sends the SMTP setup with If-Match set to version
// (NFR-017).
export function putVersioned(
  path: '/settings/smtp',
  version: string,
  body: SMTPPut['requestBody']['content']['application/json'],
) {
  return api.PUT(path, { params: { header: { 'If-Match': version } }, body });
}

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
