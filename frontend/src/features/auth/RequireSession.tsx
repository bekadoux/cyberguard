import { Navigate, Outlet } from 'react-router';
import { AuthLayout } from './AuthLayout';
import { SessionStatus } from './SessionStatus';
import { useAuth } from './use-auth';

export function RequireSession() {
  const { status } = useAuth();
  if (status === 'checking' || status === 'unavailable') {
    return (
      <AuthLayout>
        <SessionStatus />
      </AuthLayout>
    );
  }
  if (status === 'anonymous') return <Navigate to="/login" replace />;
  return <Outlet />;
}
