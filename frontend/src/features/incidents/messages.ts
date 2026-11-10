import { ApiError } from '../../shared/api/client';

export function incidentErrorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.status === 403)
      return 'This request was not allowed. Check your access or contact your administrator.';
    if (error.status === 400)
      return 'The incident request could not be accepted. Reset your filters and try again.';
    if (error.code === 'invalid_response')
      return 'The service returned an unexpected response. Please try again.';
    if (error.status === 503)
      return 'Incidents are temporarily unavailable. Please try again.';
  }
  return 'Unable to load incidents. Check your connection and try again.';
}

export const isAborted = (error: unknown) =>
  error instanceof DOMException && error.name === 'AbortError';
