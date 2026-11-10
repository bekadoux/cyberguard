import { ShieldCheck } from 'lucide-react';
import { useEffect } from 'react';
import { roleLabels } from './labels';
import { useAuth } from './use-auth';

/** A real session confirmation, not a dashboard for unimplemented functionality. */
export function SessionPage() {
  const { user } = useAuth();
  useEffect(() => {
    document.title = 'Your account · CyberGuard';
  }, []);
  if (!user) return null;

  return (
    <section className="account-content workspace-account">
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
      <p>
        Access to each incident is checked by the server. Contact your administrator for
        account or password support.
      </p>
    </section>
  );
}
