import { ApiError, request } from '../../shared/api/client';
import {
  invalidResponse,
  isRecord,
  isUUID,
  statusLabels,
  type IncidentStatus,
} from './model';

/** UI guard only. The Go endpoint enforces ownership on every request. */
export function isReporter(reporterId: string, userId: string | undefined): boolean {
  return userId !== undefined && reporterId === userId;
}

export type TimelineEvent = { status: IncidentStatus; occurred_at: string };
export type TimelinePage = { events: TimelineEvent[]; next_cursor: string | null };

export function parseTimeline(data: unknown): TimelinePage {
  if (
    !isRecord(data) ||
    !Array.isArray(data.events) ||
    data.events.length > 100 ||
    (data.next_cursor !== null &&
      (typeof data.next_cursor !== 'string' ||
        !data.next_cursor ||
        data.next_cursor.length > 2048))
  )
    return invalidResponse();
  const events: TimelineEvent[] = data.events.map((item: unknown) => {
    if (
      !isRecord(item) ||
      typeof item.status !== 'string' ||
      !Object.hasOwn(statusLabels, item.status) ||
      typeof item.occurred_at !== 'string' ||
      !Number.isFinite(Date.parse(item.occurred_at))
    )
      return invalidResponse();
    return { status: item.status as IncidentStatus, occurred_at: item.occurred_at };
  });
  return { events, next_cursor: data.next_cursor as string | null };
}

export async function getTimeline(
  id: string,
  cursor: string | null,
  signal: AbortSignal,
): Promise<TimelinePage> {
  if (!isUUID(id)) throw new ApiError(404, 'not_found');
  const params = new URLSearchParams({ limit: '20' });
  if (cursor) params.set('cursor', cursor);
  return parseTimeline(
    await request(`/incidents/${encodeURIComponent(id)}/timeline?${params}`, { signal }),
  );
}
