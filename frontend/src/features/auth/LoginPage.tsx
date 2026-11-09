import {
  AlertCircle,
  ArrowRight,
  Eye,
  EyeOff,
  KeyRound,
  LoaderCircle,
  UserRound,
} from 'lucide-react';
import { useEffect, useRef, useState, type FormEvent } from 'react';
import { Navigate } from 'react-router';
import { ApiError } from '../../shared/api/client';
import { AuthLayout } from './AuthLayout';
import { authErrorMessage } from './messages';
import { SessionStatus } from './SessionStatus';
import { useAuth } from './use-auth';
import { validateCredentials } from './validation';

export function LoginPage() {
  const { status, pending, login } = useAuth();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [capsLock, setCapsLock] = useState(false);
  const [errors, setErrors] = useState<ReturnType<typeof validateCredentials>>({});
  const [failure, setFailure] = useState<ApiError | null>(null);
  const [retryIn, setRetryIn] = useState(0);
  const alertRef = useRef<HTMLDivElement>(null);
  const usernameRef = useRef<HTMLInputElement>(null);
  const passwordRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    document.title = 'Sign in · CyberGuard';
  }, []);

  useEffect(() => {
    if (failure) alertRef.current?.focus();
    const update = () =>
      setRetryIn(Math.max(0, Math.ceil(((failure?.retryAt ?? 0) - Date.now()) / 1000)));
    update();
    if (!failure?.retryAt) return;
    const timer = window.setInterval(update, 1000);
    return () => window.clearInterval(timer);
  }, [failure]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (pending || retryIn > 0 || Date.now() < (failure?.retryAt ?? 0)) return;
    const nextErrors = validateCredentials({ username, password });
    setErrors(nextErrors);
    if (Object.keys(nextErrors).length > 0) {
      (nextErrors.username ? usernameRef : passwordRef).current?.focus();
      return;
    }
    setFailure(null);
    try {
      await login({ username, password });
    } catch (error) {
      setFailure(
        error instanceof ApiError ? error : new ApiError(0, 'network_unavailable'),
      );
      setPassword('');
      setShowPassword(false);
    }
  }

  if (status === 'authenticated') return <Navigate to="/" replace />;

  return (
    <AuthLayout>
      {status === 'checking' || status === 'unavailable' ? (
        <SessionStatus />
      ) : (
        <section className="login-panel" aria-labelledby="login-heading">
          <span className="section-marker" aria-hidden="true">
            <KeyRound size={21} strokeWidth={1.7} />
          </span>
          <h1 id="login-heading">Welcome back</h1>
          <p className="login-intro">Sign in with your company account to continue.</p>
          <form noValidate onSubmit={(event) => void submit(event)} aria-busy={pending}>
            {failure && (
              <div
                className="notice notice-error"
                role="alert"
                tabIndex={-1}
                ref={alertRef}
              >
                <AlertCircle size={18} aria-hidden="true" />
                <div>
                  {authErrorMessage(failure)}
                  {retryIn > 0 && (
                    <span className="retry-message" role="status">
                      Try again in {retryIn}s.
                    </span>
                  )}
                </div>
              </div>
            )}
            <div className="form-field">
              <label htmlFor="username">Username</label>
              <div className="input-wrap">
                <UserRound size={18} aria-hidden="true" />
                <input
                  id="username"
                  name="username"
                  ref={usernameRef}
                  value={username}
                  onChange={(event) => setUsername(event.target.value)}
                  autoComplete="username"
                  autoCapitalize="none"
                  autoCorrect="off"
                  spellCheck={false}
                  placeholder="Your company username"
                  required
                  disabled={pending}
                  aria-invalid={!!errors.username}
                  aria-describedby={errors.username ? 'username-error' : undefined}
                />
              </div>
              {errors.username && (
                <p id="username-error" className="field-error">
                  {errors.username}
                </p>
              )}
            </div>
            <div className="form-field">
              <label htmlFor="password">Password</label>
              <div className="input-wrap">
                <KeyRound size={18} aria-hidden="true" />
                <input
                  id="password"
                  name="password"
                  ref={passwordRef}
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  type={showPassword ? 'text' : 'password'}
                  autoComplete="current-password"
                  placeholder="Enter your password"
                  required
                  disabled={pending}
                  onKeyUp={(event) => setCapsLock(event.getModifierState('CapsLock'))}
                  onKeyDown={(event) => setCapsLock(event.getModifierState('CapsLock'))}
                  onBlur={() => setCapsLock(false)}
                  aria-invalid={!!errors.password}
                  aria-describedby={
                    [errors.password ? 'password-error' : '', capsLock ? 'caps-lock' : '']
                      .filter(Boolean)
                      .join(' ') || undefined
                  }
                />
                <button
                  type="button"
                  className="password-toggle"
                  aria-label={showPassword ? 'Hide password' : 'Show password'}
                  aria-pressed={showPassword}
                  disabled={pending}
                  onClick={() => setShowPassword(!showPassword)}
                >
                  {showPassword ? (
                    <EyeOff size={18} aria-hidden="true" />
                  ) : (
                    <Eye size={18} aria-hidden="true" />
                  )}
                </button>
              </div>
              {errors.password && (
                <p id="password-error" className="field-error">
                  {errors.password}
                </p>
              )}
              {capsLock && (
                <p id="caps-lock" className="field-hint" role="status">
                  Caps Lock is on.
                </p>
              )}
            </div>
            <button
              type="submit"
              className="button button-primary sign-in-button"
              disabled={pending || retryIn > 0}
            >
              {pending ? (
                <>
                  <LoaderCircle size={18} className="spinner" aria-hidden="true" />{' '}
                  Signing in…
                </>
              ) : (
                <>
                  Sign in <ArrowRight size={18} aria-hidden="true" />
                </>
              )}
            </button>
          </form>
          <div className="access-help">
            <strong>Need help signing in?</strong>
            <p>
              Contact your company administrator for account access or password support.
            </p>
          </div>
        </section>
      )}
    </AuthLayout>
  );
}
