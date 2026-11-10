import { ClipboardList, LogOut, UserRound } from 'lucide-react';
import { useState } from 'react';
import { NavLink, Outlet } from 'react-router';
import { authErrorMessage } from '../features/auth/messages';
import { roleLabels } from '../features/auth/labels';
import { useAuth } from '../features/auth/use-auth';
import { Brand } from '../shared/ui/Brand';
import { ThemeSelect } from '../shared/ui/ThemeSelect';
import './workspace.css';

export function WorkspaceLayout() {
  const { user, pending, logout } = useAuth();
  const [error, setError] = useState<string | null>(null);
  async function signOut() {
    setError(null);
    try {
      await logout();
    } catch (error) {
      setError(authErrorMessage(error, 'logout'));
    }
  }
  return (
    <div className="workspace">
      <a className="skip-link" href="#workspace-main">
        Skip to content
      </a>
      <header className="workspace-header">
        <Brand />
        <nav aria-label="Workspace" className="workspace-nav">
          <NavLink to="/incidents">
            <ClipboardList size={17} aria-hidden="true" />
            Incidents
          </NavLink>
          <NavLink to="/account">
            <UserRound size={17} aria-hidden="true" />
            Account
          </NavLink>
        </nav>
        <div className="workspace-tools">
          {user && (
            <div className="workspace-identity">
              <strong title={user.username}>{user.username}</strong>
              <span>{roleLabels[user.role]}</span>
            </div>
          )}
          <ThemeSelect />
          <button
            type="button"
            className="button button-secondary workspace-logout"
            aria-label={pending ? 'Signing out…' : 'Sign out'}
            disabled={pending}
            onClick={() => void signOut()}
          >
            <LogOut size={17} aria-hidden="true" />
            <span>{pending ? 'Signing out…' : 'Sign out'}</span>
          </button>
        </div>
      </header>
      <main id="workspace-main" className="workspace-main" tabIndex={-1}>
        {error && (
          <p className="notice notice-error workspace-alert" role="alert">
            {error}
          </p>
        )}
        <Outlet />
      </main>
      <footer className="workspace-footer">
        CyberGuard <span aria-hidden="true">·</span> Company security, connected.
      </footer>
    </div>
  );
}
