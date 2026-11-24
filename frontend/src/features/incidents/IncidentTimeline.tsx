import { useCallback, useEffect, useRef, useState } from 'react';
import { useAuth } from '../auth/use-auth';
import { incidentErrorMessage } from './messages';
import { invalidResponse, statusLabels } from './model';
import { getTimeline, type TimelineEvent } from './timeline';

type State = {
  events: TimelineEvent[];
  cursor: string | null;
  loading: boolean;
  error: unknown;
};
const initial: State = { events: [], cursor: null, loading: true, error: null };

/** Reporter-only status changes; staff action history is a separate endpoint. */
export function IncidentTimeline({ incidentId }: { incidentId: string }) {
  const { runProtected, pending } = useAuth();
  const [state, setState] = useState<State>(initial);
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
        .catch((error: unknown) => {
          if (!controller.signal.aborted) {
            setState((previous) => ({ ...previous, loading: false, error }));
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
  }, [load, pending]);

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
      {state.error !== null && <p role="alert">{incidentErrorMessage(state.error)}</p>}
      {!state.loading && state.error !== null && (
        <button
          className="button button-secondary"
          type="button"
          onClick={() => load(state.events.length > 0 ? state.cursor : null)}
        >
          Try again
        </button>
      )}
      {!state.loading && state.error === null && state.events.length === 0 && (
        <p>No status history available.</p>
      )}
      {!state.loading && state.error === null && state.cursor && (
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
