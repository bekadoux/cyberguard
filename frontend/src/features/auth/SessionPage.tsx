import { ArrowUpRight, LogOut, ShieldCheck } from 'lucide-react';
import { useEffect, useState } from 'react';
import { Brand } from '../../shared/ui/Brand';
import { ThemeSelect } from '../../shared/ui/ThemeSelect';
import { authErrorMessage } from './messages';
import { useAuth } from './use-auth';

const roleLabels = {
  employee: 'Employee',
  analyst: 'Security analyst',
  admin: 'Administrator',
};

/** A real session confirmation, not a dashboard for unimplemented functionality. */
export function SessionPage() {
  const { user, pending, logout } = useAuth();
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    document.title = 'Your account · CyberGuard';
  }, []);
  if (!user) return null;

  const signOut = async () => {
    setError(null);
    try {
      await logout();
    } catch (error) {
      setError(authErrorMessage(error, 'logout'));
    }
  };

  return (
    <div className="account-page">
      <header className="account-header">
        <Brand />
        <ThemeSelect />
      </header>
      <main id="main" className="account-content">
        <span className="section-marker">
          <ShieldCheck aria-hidden="true" />
        </span>
        <span className="eyebrow">COMPANY ACCOUNT</span>
        <h1>You’re signed in.</h1>
        <p>
          Welcome, <strong>{user.username}</strong>. Your company session is active.
        </p>
        <dl className="account-details">
          <div>
            <dt>Username</dt>
            <dd>{user.username}</dd>
          </div>
          <div>
            <dt>Role</dt>
            <dd>{roleLabels[user.role]}</dd>
          </div>
        </dl>
        <div className="scope-note">
          <ArrowUpRight size={18} aria-hidden="true" />
          <p>
            This release includes sign-in and account access. Incident screens will be
            added in a later update.
          </p>
        </div>
        {error && (
          <p className="notice notice-error" role="alert">
            {error}
          </p>
        )}
        <button
          className="button button-secondary"
          disabled={pending}
          onClick={() => void signOut()}
        >
          <LogOut size={17} aria-hidden="true" />
          {pending ? 'Signing out…' : 'Sign out'}
        </button>
      </main>
    </div>
  );
}
