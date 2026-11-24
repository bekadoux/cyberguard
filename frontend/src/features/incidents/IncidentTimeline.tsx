import { useCallback, useEffect, useRef, useState } from 'react';
import { useAuth } from '../auth/use-auth';
import { incidentErrorMessage } from './messages';
import { invalidResponse, statusLabels } from './model';
import { getTimeline, type TimelineEvent } from './timeline';

type State = {
  events: TimelineEvent[];
  cursor: string | null;
  loading: boolean;
  error: { cause: unknown; cursor: string | null } | null;
};
const initial: State = { events: [], cursor: null, loading: true, error: null };

/** Reporter-only status changes; staff action history is a separate endpoint. */
export function IncidentTimeline({
  incidentId,
  revision,
}: {
  incidentId: string;
  revision: number;
}) {
  const { runProtected, pending } = useAuth();
  const [state, setState] = useState<State>(initial);
  const { error } = state;
  const controllerRef = useRef<AbortController | null>(null);

  const load = useCallback(
    (cursor: string | null) => {
      if (controllerRef.current) return;
      const controller = new AbortController();
      controllerRef.current = controller;
      setState((previous) => ({ ...previous, loading: true, error: null }));
      void runProtected(() => getTimeline(incidentId, cursor, controller.signal))
        .then((page) => {
          if (controller.signal.aborted) return;
          if (cursor !== null && page.next_cursor === cursor) return invalidResponse();
          setState((previous) => ({
            events: cursor === null ? page.events : [...previous.events, ...page.events],
            cursor: page.next_cursor,
            loading: false,
            error: null,
          }));
        })
        .catch((cause: unknown) => {
          if (!controller.signal.aborted) {
            setState((previous) => ({
              ...previous,
              loading: false,
              error: { cause, cursor },
            }));
          }
        })
        .finally(() => {
          if (controllerRef.current === controller) controllerRef.current = null;
        });
    },
    [incidentId, runProtected],
  );

  useEffect(() => {
    if (pending) return;
    load(null);
    return () => {
      controllerRef.current?.abort();
      controllerRef.current = null;
    };
  }, [load, pending, revision]);

  return (
    <section className="panel detail-panel" aria-labelledby="reporter-timeline-heading">
      <h2 id="reporter-timeline-heading">Status history</h2>
      {state.events.length > 0 && (
        <ol>
          {state.events.map((event, index) => (
            <li key={`${index}/${event.occurred_at}/${event.status}`}>
              {statusLabels[event.status]} ·{' '}
              <time dateTime={event.occurred_at}>
                {new Date(event.occurred_at).toLocaleString()}
              </time>
            </li>
          ))}
        </ol>
      )}
      {state.loading && <p role="status">Loading status history…</p>}
      {error && <p role="alert">{incidentErrorMessage(error.cause)}</p>}
      {!state.loading && error && (
        <button
          className="button button-secondary"
          type="button"
          onClick={() => load(error.cursor)}
        >
          Try again
        </button>
      )}
      {!state.loading && !error && state.events.length === 0 && (
        <p>No status history available.</p>
      )}
      {!state.loading && !error && state.cursor && (
        <button
          className="button button-secondary"
          type="button"
          onClick={() => load(state.cursor)}
        >
          Load more
        </button>
      )}
    </section>
  );
}
