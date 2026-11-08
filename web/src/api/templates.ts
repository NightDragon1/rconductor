// SPDX-License-Identifier: AGPL-3.0-or-later
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { Template } from '@/lib/templates';
import { api, must, patchVersioned } from './client';

export const templatesKey = (id: string) => ['templates', id] as const;

// useTemplates lists the command templates of a server the caller sees
// (CMD-008); which ones the backend decides.
export function useTemplates(id: string, enabled = true) {
  return useQuery({
    queryKey: templatesKey(id),
    queryFn: async () =>
      must(await api.GET('/servers/{id}/templates', { params: { path: { id } } })).items,
    enabled: enabled && !!id,
  });
}

// useSaveTemplate creates a template or, with a template to change,
// changes it with its version (If-Match).
export function useSaveTemplate(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (v: { from?: Template; name: string; template: string }) =>
      v.from
        ? must(
            await patchVersioned('/servers/{id}/templates/{template_id}', v.from.version, {
              path: { id, template_id: v.from.id },
              body: { name: v.name, template: v.template },
            }),
          )
        : must(
            await api.POST('/servers/{id}/templates', {
              params: { path: { id } },
              body: { name: v.name, template: v.template },
            }),
          ),
    onSettled: () => qc.invalidateQueries({ queryKey: templatesKey(id) }),
  });
}

export function useDeleteTemplate(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (templateId: string) =>
      must(
        await api.DELETE('/servers/{id}/templates/{template_id}', {
          params: { path: { id, template_id: templateId } },
        }),
      ),
    onSettled: () => qc.invalidateQueries({ queryKey: templatesKey(id) }),
  });
}
