import { ApiError } from '../../shared/api/client';

export const statusLabels = {
  submitted: 'Submitted',
  triage: 'Triage',
  investigating: 'Investigating',
  awaiting_reporter: 'Awaiting reporter',
  resolved: 'Resolved',
  closed: 'Closed',
  false_positive: 'False positive',
} as const;
export const categoryLabels = {
  phishing: 'Phishing / social engineering',
  malware: 'Malicious software',
  account_compromise: 'Account compromise',
  data_exposure: 'Data exposure',
  lost_device: 'Lost or stolen device',
  policy_violation: 'Policy violation',
  service_disruption: 'Service disruption',
  other: 'Other',
} as const;
export const severityLabels = {
  low: 'Low',
  medium: 'Medium',
  high: 'High',
  critical: 'Critical',
} as const;
export type IncidentStatus = keyof typeof statusLabels;
export type IncidentCategory = keyof typeof categoryLabels;
export type IncidentSeverity = keyof typeof severityLabels;
export type IncidentSummary = {
  id: string;
  reporter_id: string;
  title: string;
  status: IncidentStatus;
  category: IncidentCategory | null;
  assigned_analyst_id: string | null;
  created_at: string;
  updated_at: string;
  severity?: IncidentSeverity | null;
};
export type IncidentPage = { incidents: IncidentSummary[]; next_cursor: string | null };
export type IncidentDetail = IncidentSummary & {
  description: string;
  affected_system: string | null;
  resolution_note: string | null;
  assigned_analyst: { id: string; username: string | null } | null;
};
export type IncidentFilters = {
  status: IncidentStatus | '';
  category: IncidentCategory | '';
};

export const isUUID = (value: unknown): value is string =>
  typeof value === 'string' &&
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(value);
export const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null && !Array.isArray(value);
export function invalidResponse(): never {
  throw new ApiError(200, 'invalid_response');
}
function hasLabel<T extends object>(labels: T, value: unknown): value is keyof T {
  return typeof value === 'string' && Object.hasOwn(labels, value);
}

export function parseSummary(data: unknown): IncidentSummary {
  if (
    !isRecord(data) ||
    !isUUID(data.id) ||
    !isUUID(data.reporter_id) ||
    typeof data.title !== 'string' ||
    !hasLabel(statusLabels, data.status) ||
    (data.category !== null && !hasLabel(categoryLabels, data.category)) ||
    (data.assigned_analyst_id !== null && !isUUID(data.assigned_analyst_id)) ||
    typeof data.created_at !== 'string' ||
    !Number.isFinite(Date.parse(data.created_at)) ||
    typeof data.updated_at !== 'string' ||
    !Number.isFinite(Date.parse(data.updated_at)) ||
    ('severity' in data &&
      data.severity !== null &&
      !hasLabel(severityLabels, data.severity))
  ) {
    return invalidResponse();
  }
  return {
    id: data.id,
    reporter_id: data.reporter_id,
    title: data.title,
    status: data.status,
    category: data.category as IncidentCategory | null,
    assigned_analyst_id: data.assigned_analyst_id,
    created_at: data.created_at,
    updated_at: data.updated_at,
    ...('severity' in data ? { severity: data.severity as IncidentSeverity | null } : {}),
  };
}

export function parsePage(data: unknown): IncidentPage {
  if (
    !isRecord(data) ||
    !Array.isArray(data.incidents) ||
    data.incidents.length > 100 ||
    (data.next_cursor !== null &&
      (typeof data.next_cursor !== 'string' ||
        !data.next_cursor ||
        data.next_cursor.length > 256))
  )
    return invalidResponse();
  return {
    incidents: data.incidents.map(parseSummary),
    next_cursor: data.next_cursor as string | null,
  };
}

export function parseDetail(data: unknown): IncidentDetail {
  if (!isRecord(data) || !isRecord(data.incident)) return invalidResponse();
  const row = data.incident;
  const summary = parseSummary(row);
  if (
    typeof row.description !== 'string' ||
    (row.affected_system !== null && typeof row.affected_system !== 'string') ||
    (row.resolution_note !== null && typeof row.resolution_note !== 'string')
  )
    return invalidResponse();
  let assigned: IncidentDetail['assigned_analyst'] = null;
  if (row.assigned_analyst !== null) {
    if (
      !isRecord(row.assigned_analyst) ||
      !isUUID(row.assigned_analyst.id) ||
      (row.assigned_analyst.username !== null &&
        typeof row.assigned_analyst.username !== 'string')
    )
      return invalidResponse();
    assigned = { id: row.assigned_analyst.id, username: row.assigned_analyst.username };
  }
  if ((assigned?.id ?? null) !== summary.assigned_analyst_id) return invalidResponse();
  return {
    ...summary,
    description: row.description,
    affected_system: row.affected_system,
    resolution_note: row.resolution_note,
    assigned_analyst: assigned,
  };
}

export function incidentListBackLink(state: unknown): string {
  const search =
    isRecord(state) && typeof state.listSearch === 'string' ? state.listSearch : '';
  return `/incidents${search && readFilters(new URLSearchParams(search)) ? `?${search}` : ''}`;
}

export function readFilters(params: URLSearchParams): IncidentFilters | null {
  for (const [key, value] of params) {
    if (
      params.getAll(key).length !== 1 ||
      !value ||
      (key === 'status'
        ? !hasLabel(statusLabels, value)
        : key === 'category'
          ? !hasLabel(categoryLabels, value)
          : true)
    )
      return null;
  }
  return {
    status: (params.get('status') ?? '') as IncidentFilters['status'],
    category: (params.get('category') ?? '') as IncidentFilters['category'],
  };
}
