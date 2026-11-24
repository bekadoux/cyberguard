import {
  AlertCircle,
  ArrowLeft,
  ArrowRight,
  CheckCircle2,
  Eye,
  EyeOff,
  LoaderCircle,
  ShieldCheck,
  UserRound,
  KeyRound,
} from 'lucide-react';
import { useEffect, useRef, useState, type FormEvent } from 'react';
import { Link, Navigate } from 'react-router';
import { ApiError } from '../../shared/api/client';
import { AuthLayout } from './AuthLayout';
import { authApi } from './api';
import { SessionStatus } from './SessionStatus';
import { useAuth } from './use-auth';
import { validateSignup } from './signup-validation';

function signupError(error: ApiError): string {
  if (error.status === 404)
    return 'Self-registration is not enabled here. Contact your company administrator for an account.';
  if (error.status === 409)
    return 'This username is unavailable. Choose a different one.';
  if (error.status === 400)
    return 'The account details do not meet registration requirements. Check the username and password.';
  if (error.status === 429)
    return 'Too many registration attempts. Please try again later.';
  if (error.status === 403)
    return 'Registration was not permitted from this site. Check the address or contact an administrator.';
  if (error.status === 503 || error.code === 'auth_busy')
    return 'Registration is temporarily unavailable. Please try again later.';
  if (error.status === 0)
    return 'Unable to connect. Check your connection and try again.';
  return 'Unable to complete registration. Please contact your administrator.';
}

export function SignupPage() {
  const { status } = useAuth();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [errors, setErrors] = useState<ReturnType<typeof validateSignup>>({});
  const [failure, setFailure] = useState<ApiError | null>(null);
  const [retryIn, setRetryIn] = useState(0);
  const [busy, setBusy] = useState(false);
  const [created, setCreated] = useState(false);
  const alertRef = useRef<HTMLDivElement>(null);
  const usernameRef = useRef<HTMLInputElement>(null);
  const passwordRef = useRef<HTMLInputElement>(null);
  const confirmRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    document.title = 'Create account · CyberGuard';
  }, []);
  useEffect(() => {
    if (failure) alertRef.current?.focus();
    const update = () =>
      setRetryIn(Math.max(0, Math.ceil(((failure?.retryAt ?? 0) - Date.now()) / 1000)));
    update();
    if (!failure?.retryAt) return;
    const interval = window.setInterval(update, 1000);
    return () => window.clearInterval(interval);
  }, [failure]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || retryIn > 0 || Date.now() < (failure?.retryAt ?? 0)) return;
    const nextErrors = validateSignup({ username, password, confirmPassword });
    setErrors(nextErrors);
    if (Object.keys(nextErrors).length) {
      (nextErrors.username
        ? usernameRef
        : nextErrors.password
          ? passwordRef
          : confirmRef
      ).current?.focus();
      return;
    }
    setFailure(null);
    setBusy(true);
    try {
      await authApi.signup({ username, password });
      // A successful signup is not a login. No session is established or cached.
      setCreated(true);
    } catch (error) {
      setFailure(
        error instanceof ApiError ? error : new ApiError(0, 'network_unavailable'),
      );
    } finally {
      setBusy(false);
      setPassword('');
      setConfirmPassword('');
      setShowPassword(false);
    }
  }

  if (status === 'authenticated') return <Navigate to="/" replace />;
  return (
    <AuthLayout>
      {status === 'checking' ? (
        <SessionStatus />
      ) : created ? (
        <section
          className="login-panel signup-success"
          aria-labelledby="signup-success-heading"
          role="status"
        >
          <span className="section-marker" aria-hidden="true">
            <CheckCircle2 size={22} />
          </span>
          <h1 id="signup-success-heading">Account created</h1>
          <p className="login-intro">
            Your employee account is ready. Sign in to access CyberGuard. Registration
            does not sign you in automatically.
          </p>
          <Link className="button button-primary sign-in-button" to="/login">
            Continue to sign in <ArrowRight size={18} aria-hidden="true" />
          </Link>
        </section>
      ) : (
        <section className="login-panel signup-panel" aria-labelledby="signup-heading">
          <Link className="auth-back" to="/login">
            <ArrowLeft size={16} aria-hidden="true" /> Back to sign in
          </Link>
          <span className="section-marker" aria-hidden="true">
            <ShieldCheck size={21} strokeWidth={1.7} />
          </span>
          <h1 id="signup-heading">Create your account</h1>
          <p className="login-intro">
            Register as an employee to report security incidents. Account creation may be
            disabled by your organization.
          </p>
          <form noValidate aria-busy={busy} onSubmit={(event) => void submit(event)}>
            {failure && (
              <div
                className="notice notice-error"
                role="alert"
                tabIndex={-1}
                ref={alertRef}
              >
                <AlertCircle size={18} aria-hidden="true" />
                <div>
                  {signupError(failure)}
                  {retryIn > 0 && (
                    <span className="retry-message" role="status">
                      Try again in {retryIn}s.
                    </span>
                  )}
                </div>
              </div>
            )}
            <div className="form-field">
              <label htmlFor="signup-username">Username</label>
              <div className="input-wrap">
                <UserRound size={18} aria-hidden="true" />
                <input
                  id="signup-username"
                  name="username"
                  ref={usernameRef}
                  value={username}
                  onChange={(event) => setUsername(event.target.value)}
                  autoComplete="username"
                  autoCapitalize="none"
                  autoCorrect="off"
                  spellCheck={false}
                  placeholder="Choose a username"
                  required
                  disabled={busy}
                  aria-invalid={!!errors.username}
                  aria-describedby={
                    errors.username ? 'signup-username-error' : 'signup-username-hint'
                  }
                />
              </div>
              {errors.username ? (
                <p id="signup-username-error" className="field-error">
                  {errors.username}
                </p>
              ) : (
                <p id="signup-username-hint" className="field-hint">
                  3–64 ASCII letters, digits, dots, underscores or hyphens after
                  normalization.
                </p>
              )}
            </div>
            <div className="form-field">
              <label htmlFor="signup-password">Password</label>
              <div className="input-wrap">
                <KeyRound size={18} aria-hidden="true" />
                <input
                  id="signup-password"
                  name="password"
                  ref={passwordRef}
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  type={showPassword ? 'text' : 'password'}
                  autoComplete="new-password"
                  placeholder="At least 12 characters"
                  required
                  disabled={busy}
                  aria-invalid={!!errors.password}
                  aria-describedby={
                    errors.password ? 'signup-password-error' : 'signup-password-hint'
                  }
                />
                <button
                  type="button"
                  className="password-toggle"
                  disabled={busy}
                  aria-label={showPassword ? 'Hide password' : 'Show password'}
                  aria-pressed={showPassword}
                  onClick={() => setShowPassword(!showPassword)}
                >
                  {showPassword ? (
                    <EyeOff size={18} aria-hidden="true" />
                  ) : (
                    <Eye size={18} aria-hidden="true" />
                  )}
                </button>
              </div>
              {errors.password ? (
                <p id="signup-password-error" className="field-error">
                  {errors.password}
                </p>
              ) : (
                <p id="signup-password-hint" className="field-hint">
                  12–128 UTF-8 bytes. Do not reuse another account’s password.
                </p>
              )}
            </div>
            <div className="form-field">
              <label htmlFor="signup-confirm">Confirm password</label>
              <div className="input-wrap">
                <KeyRound size={18} aria-hidden="true" />
                <input
                  id="signup-confirm"
                  name="confirmPassword"
                  ref={confirmRef}
                  value={confirmPassword}
                  onChange={(event) => setConfirmPassword(event.target.value)}
                  type="password"
                  autoComplete="new-password"
                  placeholder="Repeat your password"
                  required
                  disabled={busy}
                  aria-invalid={!!errors.confirmPassword}
                  aria-describedby={
                    errors.confirmPassword ? 'signup-confirm-error' : undefined
                  }
                />
              </div>
              {errors.confirmPassword && (
                <p id="signup-confirm-error" className="field-error">
                  {errors.confirmPassword}
                </p>
              )}
            </div>
            <button
              className="button button-primary sign-in-button"
              type="submit"
              disabled={busy || retryIn > 0}
            >
              {busy ? (
                <>
                  <LoaderCircle className="spinner" size={18} aria-hidden="true" />{' '}
                  Creating account…
                </>
              ) : (
                <>
                  Create employee account <ArrowRight size={18} aria-hidden="true" />
                </>
              )}
            </button>
          </form>
          <p className="signup-note">
            <ShieldCheck size={16} aria-hidden="true" /> Accounts are created with
            employee access only. Higher permissions are assigned by administrators.
          </p>
        </section>
      )}
    </AuthLayout>
  );
}
