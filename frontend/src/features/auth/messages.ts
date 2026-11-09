import { ApiError } from '../../shared/api/client';

export function authErrorMessage(error: unknown, action: 'login' | 'logout' = 'login') {
  if (!(error instanceof ApiError)) return 'Unable to connect. Please try again.';
  if (action === 'logout') {
    return 'We could not confirm sign-out. Your session may still be active. Please try again.';
  }
  if (error.status === 401) return 'Invalid username or password.';
  if (error.status === 403) {
    return 'This sign-in request was not allowed. Check the site address or contact your administrator.';
  }
  if (error.status === 429)
    return 'Too many sign-in attempts. Please wait before trying again.';
  if (error.code === 'auth_busy')
    return 'Sign-in is busy right now. Please try again shortly.';
  if (error.status === 503)
    return 'Sign-in is temporarily unavailable. Please try again shortly.';
  if (error.status === 0)
    return 'Unable to connect. Check your connection and try again.';
  return 'We could not complete sign-in. Please try again or contact your administrator.';
}
