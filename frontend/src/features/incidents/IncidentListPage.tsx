import {
  AlertCircle,
  ClipboardList,
  LoaderCircle,
  Plus,
  RefreshCw,
  SlidersHorizontal,
} from 'lucide-react';
import { useEffect } from 'react';
import { Link, useSearchParams } from 'react-router';
import { useAuth } from '../auth/use-auth';
import { IncidentStatusBadge } from './IncidentStatusBadge';
import { incidentErrorMessage } from './messages';
import { categoryLabels, readFilters, severityLabels, statusLabels } from './model';
import { useIncidentList } from './use-incident-list';
import './incidents.css';

export function IncidentListPage() {
  const { user } = useAuth();
  const [params, setParams] = useSearchParams();
  const filters = readFilters(params);
  const list = useIncidentList(filters);
  const professional = user?.role !== 'employee';
  const title = professional ? 'Incident queue' : 'My incidents';
  const filtered = !!(filters?.status || filters?.category);
  useEffect(() => {
    document.title = `${title} · CyberGuard`;
  }, [title]);

  function filter(name: 'status' | 'category', value: string) {
    const next = new URLSearchParams(params);
    if (value) next.set(name, value);
    else next.delete(name);
    setParams(next);
  }

  return (
    <section aria-labelledby="incidents-heading">
      <div className="page-heading">
        <div>
          <span className="eyebrow">INCIDENTS</span>
          <h1 id="incidents-heading">{title}</h1>
          <p>
            {professional
              ? 'Review reported security incidents across your company. Investigation actions are not available in this release.'
              : 'Your reported security incidents, with the latest information available from your security team.'}
          </p>
        </div>
        <Link className="button button-primary report-incident-link" to="/incidents/new">
          <Plus size={18} aria-hidden="true" />
          Report an incident
        </Link>
      </div>
      <div className="panel incident-list-panel">
        <div className="incident-filters" role="group" aria-label="Filter incidents">
          <span className="filter-label">
            <SlidersHorizontal size={16} aria-hidden="true" />
            Filters
          </span>
          <div className="incident-filter">
            <label htmlFor="filter-status">Status</label>
            <select
              id="filter-status"
              value={filters?.status ?? ''}
              onChange={(event) => filter('status', event.target.value)}
            >
              <option value="">All statuses</option>
              {Object.entries(statusLabels).map(([value, label]) => (
                <option key={value} value={value}>
                  {label}
                </option>
              ))}
            </select>
          </div>
          <div className="incident-filter">
            <label htmlFor="filter-category">Category</label>
            <select
              id="filter-category"
              value={filters?.category ?? ''}
              onChange={(event) => filter('category', event.target.value)}
            >
              <option value="">All categories</option>
              {Object.entries(categoryLabels).map(([value, label]) => (
                <option key={value} value={value}>
                  {label}
                </option>
              ))}
            </select>
          </div>
          {(filtered || !filters) && (
            <button className="button button-secondary" onClick={() => setParams({})}>
              Clear filters
            </button>
          )}
          <button
            type="button"
            className="button button-secondary refresh-incidents"
            disabled={!filters || list.phase === 'loading' || list.phase === 'more'}
            onClick={list.refresh}
          >
            <RefreshCw size={15} aria-hidden="true" />
            Refresh list
          </button>
        </div>
        {!filters ? (
          <div className="page-state">
            <AlertCircle aria-hidden="true" />
            <h2>Unsupported filters</h2>
            <p>
              Use the status and category controls, or clear the filters to start again.
            </p>
          </div>
        ) : list.phase === 'loading' ? (
          <div className="loading-state" role="status">
            <LoaderCircle className="spinner" size={20} aria-hidden="true" />
            Loading incidents…
          </div>
        ) : list.phase === 'error' ? (
          <div className="page-state">
            <AlertCircle aria-hidden="true" />
            <h2>Unable to load incidents</h2>
            <p role="alert">{incidentErrorMessage(list.error)}</p>
            <button className="button button-secondary" onClick={list.refresh}>
              Try again
            </button>
          </div>
        ) : (
          <>
            {list.incidents.length === 0 ? (
              <div className="page-state">
                <ClipboardList size={30} aria-hidden="true" />
                <h2>
                  {filtered
                    ? 'No matching incidents'
                    : professional
                      ? 'The queue is empty'
                      : 'No reports yet'}
                </h2>
                <p>
                  {filtered
                    ? 'Try a different status or category, or clear your filters.'
                    : 'Reported incidents will appear here.'}
                </p>
              </div>
            ) : (
              <div className="incident-table-wrap">
                <table className="incident-table">
                  <caption className="sr-only">{title}, newest first</caption>
                  <thead>
                    <tr>
                      <th scope="col">Incident</th>
                      <th scope="col">Status</th>
                      <th scope="col">Category</th>
                      {professional && <th scope="col">Severity</th>}
                      <th scope="col">Reported</th>
                    </tr>
                  </thead>
                  <tbody>
                    {list.incidents.map((incident) => (
                      <tr key={incident.id}>
                        <td>
                          <Link
                            className="incident-title text-preserved"
                            to={`/incidents/${incident.id}`}
                            state={{ listSearch: params.toString() }}
                          >
                            {incident.title}
                          </Link>
                          <span className="incident-reference">
                            {incident.id.slice(0, 8)}
                            {professional && incident.reporter_id === user?.id
                              ? ' · Reported by you'
                              : ''}
                          </span>
                        </td>
                        <td>
                          <IncidentStatusBadge status={incident.status} />
                        </td>
                        <td>
                          {incident.category ? (
                            categoryLabels[incident.category]
                          ) : (
                            <span className="muted">Not assessed</span>
                          )}
                        </td>
                        {professional && (
                          <td>
                            {incident.severity ? (
                              severityLabels[incident.severity]
                            ) : (
                              <span className="muted">Not assessed</span>
                            )}
                          </td>
                        )}
                        <td>
                          <time dateTime={incident.created_at}>
                            {new Date(incident.created_at).toLocaleDateString(undefined, {
                              year: 'numeric',
                              month: 'short',
                              day: 'numeric',
                            })}
                          </time>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
            <div className="incident-list-footer">
              <span role="status">
                {list.incidents.length}{' '}
                {list.incidents.length === 1 ? 'incident' : 'incidents'} loaded · Newest
                first
              </span>
              {list.cursor && (
                <button
                  type="button"
                  className="button button-secondary"
                  onClick={() => void list.loadMore()}
                  disabled={list.phase === 'more'}
                >
                  {list.phase === 'more' ? 'Loading more…' : 'Load more'}
                </button>
              )}
            </div>
            {list.error !== null && (
              <div className="notice notice-error incident-more-error" role="alert">
                {incidentErrorMessage(list.error)} Use “Load more” to retry.
              </div>
            )}
          </>
        )}
      </div>
    </section>
  );
}
