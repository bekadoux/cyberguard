import { BrowserRouter, Link, Route, Routes } from 'react-router';
import { AuthLayout } from '../features/auth/AuthLayout';
import { AuthProvider } from '../features/auth/AuthProvider';
import { LoginPage } from '../features/auth/LoginPage';
import { RequireSession } from '../features/auth/RequireSession';
import { SessionPage } from '../features/auth/SessionPage';
import { ThemeProvider } from '../shared/theme/ThemeProvider';

export function App() {
  return (
    <ThemeProvider>
      <AuthProvider>
        <BrowserRouter>
          <Routes>
            <Route path="/login" element={<LoginPage />} />
            <Route element={<RequireSession />}>
              <Route index element={<SessionPage />} />
            </Route>
            <Route
              path="*"
              element={
                <AuthLayout>
                  <section className="session-status">
                    <h1>Page not found</h1>
                    <p>This page isn’t available.</p>
                    <Link className="button button-primary" to="/">
                      Back to your account
                    </Link>
                  </section>
                </AuthLayout>
              }
            />
          </Routes>
        </BrowserRouter>
      </AuthProvider>
    </ThemeProvider>
  );
}
