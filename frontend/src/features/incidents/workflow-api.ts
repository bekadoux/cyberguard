import { ApiError, request } from '../../shared/api/client';
import {
  invalidResponse,
  isRecord,
  isUUID,
  type IncidentCategory,
  type IncidentSeverity,
  type IncidentStatus,
} from './model';

export type IncidentComment = {
  id: string;
  author_id: string;
  visibility: 'public' | 'internal';
  text: string;
  created_at: string;
};
export type AuditEvent = {
  id: string;
  actor_id: string | null;
  action: string;
  reason: string | null;
  details: Record<string, unknown>;
  created_at: string;
};
export type IncidentFields = {
  category?: IncidentCategory | null;
  severity?: IncidentSeverity | null;
  affected_system?: string | null;
  resolution_note?: string | null;
};
export type FeedbackDecision = 'accepted' | 'corrected' | 'rejected';

function endpoint(id: string, suffix: string): string {
  if (!isUUID(id)) throw new ApiError(404, 'not_found');
  return `/incidents/${encodeURIComponent(id)}/${suffix}`;
}
function parseCollection<T>(
  value: unknown,
  key: string,
  limit: number,
  convert: (entry: unknown) => T,
): T[] {
  if (!isRecord(value)) return invalidResponse();
  const items = value[key];
  if (!Array.isArray(items) || items.length > limit) return invalidResponse();
  return items.map(convert);
}
const validDate = (v: unknown): v is string =>
  typeof v === 'string' && Number.isFinite(Date.parse(v));
function parseComment(v: unknown): IncidentComment {
  if (
    !isRecord(v) ||
    !isUUID(v.id) ||
    !isUUID(v.author_id) ||
    (v.visibility !== 'public' && v.visibility !== 'internal') ||
    typeof v.text !== 'string' ||
    !validDate(v.created_at)
  )
    return invalidResponse();
  return {
    id: v.id,
    author_id: v.author_id,
    visibility: v.visibility,
    text: v.text,
    created_at: v.created_at,
  };
}
function parseEvent(v: unknown): AuditEvent {
  if (
    !isRecord(v) ||
    !isUUID(v.id) ||
    (v.actor_id !== null && !isUUID(v.actor_id)) ||
    typeof v.action !== 'string' ||
    (v.reason !== null && typeof v.reason !== 'string') ||
    !isRecord(v.details) ||
    !validDate(v.created_at)
  )
    return invalidResponse();
  return {
    id: v.id,
    actor_id: v.actor_id,
    action: v.action,
    reason: v.reason,
    details: v.details,
    created_at: v.created_at,
  };
}
export const workflowApi = {
  claim: (id: string) => request(endpoint(id, 'claim'), { method: 'POST', body: {} }),
  assign: (id: string, analyst_id: string) =>
    request(endpoint(id, 'assign'), { method: 'POST', body: { analyst_id } }),
  fields: (id: string, body: IncidentFields) =>
    request(endpoint(id, 'fields'), { method: 'PATCH', body }),
  transition: (id: string, status: IncidentStatus, reason = '') =>
    request(endpoint(id, 'transition'), { method: 'POST', body: { status, reason } }),
  archive: (id: string, reason: string) =>
    request(endpoint(id, 'archive'), { method: 'POST', body: { reason } }),
  comments: async (id: string, signal?: AbortSignal): Promise<IncidentComment[]> =>
    parseCollection(
      await request(endpoint(id, 'comments'), { signal }),
      'comments',
      500,
      parseComment,
    ),
  addComment: (id: string, text: string, visibility: 'public' | 'internal') =>
    request(endpoint(id, 'comments'), {
      method: 'POST',
      expectedStatus: 201,
      body: { text, visibility },
    }),
  history: async (id: string, signal?: AbortSignal): Promise<AuditEvent[]> =>
    parseCollection(
      await request(endpoint(id, 'history'), { signal }),
      'history',
      500,
      parseEvent,
    ),
  feedback: (
    id: string,
    body: {
      job_id: string;
      decision: FeedbackDecision;
      category: IncidentCategory | null;
      reason: string;
    },
  ) =>
    request(endpoint(id, 'ai-feedback'), { method: 'POST', expectedStatus: 201, body }),
};

export type Notification = {
  id: string;
  incident_id: string | null;
  event_type: string;
  message: string;
  created_at: string;
  read_at: string | null;
};
function parseNotification(v: unknown): Notification {
  if (
    !isRecord(v) ||
    !isUUID(v.id) ||
    (v.incident_id !== null && !isUUID(v.incident_id)) ||
    typeof v.event_type !== 'string' ||
    typeof v.message !== 'string' ||
    !validDate(v.created_at) ||
    (v.read_at !== null && !validDate(v.read_at))
  )
    return invalidResponse();
  return {
    id: v.id,
    incident_id: v.incident_id,
    event_type: v.event_type,
    message: v.message,
    created_at: v.created_at,
    read_at: v.read_at,
  };
}
export const notificationsApi = {
  list: async (signal?: AbortSignal): Promise<Notification[]> =>
    parseCollection(
      await request('/notifications', { signal }),
      'notifications',
      100,
      parseNotification,
    ),
  unread: async (id: string) => {
    if (!isUUID(id)) throw new ApiError(404, 'not_found');
    return request(`/notifications/${encodeURIComponent(id)}/unread`, { method: 'POST' });
  },
  read: async (id: string) => {
    if (!isUUID(id)) throw new ApiError(404, 'not_found');
    return request(`/notifications/${encodeURIComponent(id)}/read`, { method: 'POST' });
  },
};

export type ManagedUser = {
  id: string;
  username: string;
  role: 'employee' | 'analyst' | 'admin';
  is_blocked: boolean;
};
function parseUser(v: unknown): ManagedUser {
  if (
    !isRecord(v) ||
    !isUUID(v.id) ||
    typeof v.username !== 'string' ||
    !['employee', 'analyst', 'admin'].includes(String(v.role)) ||
    typeof v.is_blocked !== 'boolean'
  )
    return invalidResponse();
  return {
    id: v.id,
    username: v.username,
    role: v.role as ManagedUser['role'],
    is_blocked: v.is_blocked,
  };
}
export type IdentityEvent = Omit<AuditEvent, 'actor_id'> & {
  actor_id: string;
  subject_id: string;
};
function parseIdentityEvent(v: unknown): IdentityEvent {
  if (
    !isRecord(v) ||
    !isUUID(v.id) ||
    !isUUID(v.actor_id) ||
    !isUUID(v.subject_id) ||
    typeof v.action !== 'string' ||
    !isRecord(v.details) ||
    !validDate(v.created_at)
  )
    return invalidResponse();
  return {
    id: v.id,
    actor_id: v.actor_id,
    subject_id: v.subject_id,
    action: v.action,
    reason: null,
    details: v.details,
    created_at: v.created_at,
  };
}
export const adminApi = {
  users: async (signal?: AbortSignal): Promise<ManagedUser[]> =>
    parseCollection(await request('/admin/users', { signal }), 'users', 500, parseUser),
  audit: async (signal?: AbortSignal): Promise<IdentityEvent[]> =>
    parseCollection(
      await request('/admin/audit', { signal }),
      'audit',
      500,
      parseIdentityEvent,
    ),
  create: (username: string, password: string, role: ManagedUser['role']) =>
    request('/admin/users', {
      method: 'POST',
      expectedStatus: 201,
      body: { username, password, role },
    }),
  role: (id: string, role: ManagedUser['role'], reason: string) =>
    request(`/admin/users/${encodeURIComponent(id)}/role`, {
      method: 'POST',
      body: { role, reason },
    }),
  block: (id: string, blocked: boolean, reason: string) =>
    request(`/admin/users/${encodeURIComponent(id)}/block`, {
      method: 'POST',
      body: { blocked, reason },
    }),
  password: (id: string, password: string, reason: string) =>
    request(`/admin/users/${encodeURIComponent(id)}/password`, {
      method: 'POST',
      body: { password, reason },
    }),
};
