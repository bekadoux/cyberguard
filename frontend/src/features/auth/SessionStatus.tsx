import { AlertCircle, LoaderCircle, LogOut } from 'lucide-react';
import { useState } from 'react';
import { authErrorMessage } from './messages';
import { useAuth } from './use-auth';

export function SessionStatus() {
  const { status, user, pending, refresh, logout } = useAuth();
  const [signOutError, setSignOutError] = useState<string | null>(null);
  const unavailable = status === 'unavailable';

  const signOut = async () => {
    setSignOutError(null);
    try {
      await logout();
    } catch (error) {
      setSignOutError(authErrorMessage(error, 'logout'));
    }
  };

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
      {signOutError && (
        <p className="notice notice-error" role="alert">
          {signOutError}
        </p>
      )}
      <div className="session-actions">
        {unavailable && (
          <button
            type="button"
            className="button button-primary"
            disabled={pending}
            onClick={() => void refresh()}
          >
            Try again
          </button>
        )}
        {(unavailable || user) && (
          <button
            type="button"
            className="button button-secondary"
            disabled={pending}
            onClick={() => void signOut()}
          >
            <LogOut size={17} aria-hidden="true" />
            {pending ? 'Signing out…' : 'Sign out'}
          </button>
        )}
      </div>
    </section>
  );
}
