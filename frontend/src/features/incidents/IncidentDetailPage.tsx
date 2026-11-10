import { AlertCircle, ArrowLeft, FileText, LoaderCircle } from 'lucide-react';
import { useEffect } from 'react';
import { Link, useLocation, useParams } from 'react-router';
import { ApiError } from '../../shared/api/client';
import { useAuth } from '../auth/use-auth';
import { IncidentStatusBadge } from './IncidentStatusBadge';
import { incidentErrorMessage } from './messages';
import { categoryLabels, incidentListBackLink, isRecord, severityLabels } from './model';
import { useIncidentDetail } from './use-incident-detail';
import { AssessmentPanel } from './AssessmentPanel';
import './incidents.css';

const dateTime = (date: string) =>
  new Date(date).toLocaleString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
    timeZoneName: 'short',
  });

export function IncidentDetailPage() {
  const { id = '' } = useParams();
  const { user } = useAuth();
  const location = useLocation();
  const detail = useIncidentDetail(id);
  const incident = detail.incident;
  const notFound = detail.error instanceof ApiError && detail.error.status === 404;
  const professional = user?.role === 'analyst' || user?.role === 'admin';
  useEffect(() => {
    document.title = 'Incident detail · CyberGuard';
  }, []);
  return (
    <section aria-label="Incident detail">
      <Link className="back-link" to={incidentListBackLink(location.state)}>
        <ArrowLeft size={16} aria-hidden="true" />
        Back to incidents
      </Link>
      {detail.loading ? (
        <div className="panel loading-state" role="status">
          <LoaderCircle className="spinner" size={20} aria-hidden="true" />
          Loading incident…
        </div>
      ) : !incident ? (
        <div className="panel page-state">
          <AlertCircle aria-hidden="true" />
          <h1>{notFound ? 'Incident not found' : 'Unable to load incident'}</h1>
          <p role={notFound ? undefined : 'alert'}>
            {notFound
              ? 'This report does not exist or is not available to you.'
              : incidentErrorMessage(detail.error)}
          </p>
          {!notFound && (
            <button className="button button-secondary" onClick={detail.retry}>
              Try again
            </button>
          )}
        </div>
      ) : (
        <>
          {isRecord(location.state) && location.state.created === true && (
            <p className="notice submission-success" role="status">
              Incident submitted. Your report has been saved.
            </p>
          )}
          <div className="page-heading">
            <div>
              <span className="eyebrow">INCIDENT RECORD · {incident.id.slice(0, 8)}</span>
              <h1 className="text-preserved">{incident.title}</h1>
              <p>
                Reported{' '}
                <time dateTime={incident.created_at}>
                  {dateTime(incident.created_at)}
                </time>
              </p>
            </div>
            <IncidentStatusBadge status={incident.status} />
          </div>
          <div className="incident-detail-grid">
            <div className="incident-report-content">
              <section
                className="panel detail-panel"
                aria-labelledby="original-report-heading"
              >
                <h2 id="original-report-heading">
                  <FileText size={18} aria-hidden="true" />
                  Original report
                </h2>
                <p className="text-preserved incident-description">
                  {incident.description}
                </p>
                <h3>Affected system</h3>
                <p className="text-preserved">
                  {incident.affected_system ?? 'Not provided'}
                </p>
              </section>
              <section
                className="panel detail-panel"
                aria-labelledby="resolution-heading"
              >
                <h2 id="resolution-heading">Resolution</h2>
                <p
                  className={`text-preserved${incident.resolution_note === null ? ' muted' : ''}`}
                >
                  {incident.resolution_note ?? 'No resolution has been recorded.'}
                </p>
              </section>
            </div>
            <aside
              className="panel detail-panel incident-metadata"
              aria-labelledby="information-heading"
            >
              <h2 id="information-heading">Incident information</h2>
              <dl>
                <div>
                  <dt>Category</dt>
                  <dd>
                    {incident.category
                      ? categoryLabels[incident.category]
                      : 'Not assessed'}
                  </dd>
                </div>
                {professional && Object.hasOwn(incident, 'severity') && (
                  <div>
                    <dt>Severity</dt>
                    <dd>
                      {incident.severity
                        ? severityLabels[incident.severity]
                        : 'Not assessed'}
                    </dd>
                  </div>
                )}
                <div>
                  <dt>Assigned analyst</dt>
                  <dd>
                    {incident.assigned_analyst ? (
                      <>
                        <span>
                          {incident.assigned_analyst.username ??
                            'Analyst account unavailable'}
                        </span>
                        <span className="metadata-id">
                          {incident.assigned_analyst.id}
                        </span>
                      </>
                    ) : (
                      'Unassigned'
                    )}
                  </dd>
                </div>
                <div>
                  <dt>Reported by</dt>
                  <dd>
                    {incident.reporter_id === user?.id ? (
                      'You'
                    ) : (
                      <span className="metadata-id">{incident.reporter_id}</span>
                    )}
                  </dd>
                </div>
                <div>
                  <dt>Last updated</dt>
                  <dd>
                    <time dateTime={incident.updated_at}>
                      {dateTime(incident.updated_at)}
                    </time>
                  </dd>
                </div>
                <div>
                  <dt>Incident ID</dt>
                  <dd className="metadata-id">{incident.id}</dd>
                </div>
              </dl>
              <p className="metadata-hint">Times are shown in your local time zone.</p>
            </aside>
          </div>
          {professional && <AssessmentPanel incidentId={incident.id} />}
          <p className="readonly-note">
            Read-only record. Editing, comments and investigation actions are not
            available in this release.
          </p>
        </>
      )}
    </section>
  );
}
