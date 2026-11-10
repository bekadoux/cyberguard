import { Navigate, Outlet } from 'react-router';
import { AuthLayout } from './AuthLayout';
import { SessionStatus } from './SessionStatus';
import { useAuth } from './use-auth';

export function RequireSession() {
  const { status, user } = useAuth();
  if (status === 'checking' || status === 'unavailable') {
    return (
      <AuthLayout>
        <SessionStatus />
      </AuthLayout>
    );
  }
  if (status === 'anonymous') return <Navigate to="/login" replace />;
  // Never retain a previous account's feature state after identity/role changes.
  return <Outlet key={`${user?.id}:${user?.role}`} />;
}
