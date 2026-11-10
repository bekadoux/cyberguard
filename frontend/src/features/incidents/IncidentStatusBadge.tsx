import { statusLabels, type IncidentStatus } from './model';

export function IncidentStatusBadge({ status }: { status: IncidentStatus }) {
  return (
    <span className={`incident-status status-${status}`}>{statusLabels[status]}</span>
  );
}
