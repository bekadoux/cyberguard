import type { IncidentDetail, IncidentStatus } from './model';
import { ApiError } from '../../shared/api/client';

export function actionError(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.status === 403)
      return 'Not authorized for this action. Refresh your session or check your assignment.';
    if (error.status === 404) return 'The record is no longer available.';
    if (error.status === 409)
      return 'The record has changed or a workflow prerequisite is missing. Refresh and try again.';
    if (error.status === 400)
      return 'Invalid input. Check required fields, reason and length limits.';
    if (error.status === 401) return 'Session expired. Sign in again.';
  }
  return 'The action could not be completed. Check your connection and try again.';
}

export const localTime = (value: string) => new Date(value).toLocaleString();

export const terminal: IncidentStatus[] = ['closed', 'false_positive'];
export function availableTransitions(
  incident: IncidentDetail,
  role: string | undefined,
  userId: string | undefined,
): IncidentStatus[] {
  const owner = incident.reporter_id === userId;
  const staff =
    role === 'admin' || (role === 'analyst' && incident.assigned_analyst_id === userId);
  if (incident.status === 'triage' && staff) return ['investigating', 'false_positive'];
  if (incident.status === 'investigating' && staff)
    return ['awaiting_reporter', 'resolved'];
  if (incident.status === 'resolved' && owner) return ['closed', 'investigating'];
  if (terminal.includes(incident.status) && role === 'admin') return ['investigating'];
  return [];
}
