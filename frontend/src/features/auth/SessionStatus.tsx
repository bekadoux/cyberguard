import { AlertCircle, LoaderCircle } from 'lucide-react';
import { useAuth } from './use-auth';

export function SessionStatus() {
  const { status, refresh } = useAuth();
  const unavailable = status === 'unavailable';
  return (
    <section className="session-status" aria-labelledby="session-heading">
      {unavailable ? (
        <AlertCircle aria-hidden="true" />
      ) : (
        <LoaderCircle aria-hidden="true" className="spinner" />
      )}
      <h1 id="session-heading">
        {unavailable ? 'Unable to check your session' : 'Checking your session'}
      </h1>
      <p role="status">
        {unavailable
          ? 'The service is temporarily unavailable. This does not mean you have been signed out.'
          : 'One moment while we check your company account.'}
      </p>
      {unavailable && (
        <button
          type="button"
          className="button button-primary"
          onClick={() => void refresh()}
        >
          Try again
        </button>
      )}
    </section>
  );
}
