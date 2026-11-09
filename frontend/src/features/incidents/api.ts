import { ApiError, request } from '../../shared/api/client';
import {
  isUUID,
  parseDetail,
  parsePage,
  type IncidentDetail,
  type IncidentFilters,
  type IncidentPage,
} from './model';

export const incidentsApi = {
  async get(id: string, signal?: AbortSignal): Promise<IncidentDetail> {
    if (!isUUID(id)) throw new ApiError(404, 'not_found');
    return parseDetail(await request(`/incidents/${encodeURIComponent(id)}`, { signal }));
  },
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
