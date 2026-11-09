import { request } from '../../shared/api/client';
import { parsePage, type IncidentFilters, type IncidentPage } from './model';

export const incidentsApi = {
  async list(
    filters: IncidentFilters,
    cursor?: string,
    signal?: AbortSignal,
  ): Promise<IncidentPage> {
    const params = new URLSearchParams({ limit: '20' });
    if (filters.status) params.set('status', filters.status);
    if (filters.category) params.set('category', filters.category);
    if (cursor) params.set('cursor', cursor);
    return parsePage(await request(`/incidents?${params}`, { signal }));
  },
};
