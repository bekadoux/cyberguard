import { LockKeyhole, ShieldCheck } from 'lucide-react';
import type { ReactNode } from 'react';
import { Brand } from '../../shared/ui/Brand';
import { ThemeSelect } from '../../shared/ui/ThemeSelect';
import './auth.css';

export function AuthLayout({ children }: { children: ReactNode }) {
  return (
    <div className="auth-layout">
      <a className="skip-link" href="#main">
        Skip to content
      </a>
      <aside className="auth-story" aria-label="About CyberGuard">
        <Brand />
        <div className="story-content">
          <span className="eyebrow">YOUR SECURITY WORKSPACE</span>
          <h2>
            A clearer view.
            <br />A safer workplace.
          </h2>
          <p>
            A shared starting point for reporting and understanding security incidents.
          </p>
          <div className="story-note">
            <span className="story-note-icon">
              <ShieldCheck size={22} aria-hidden="true" />
            </span>
            <div>
              <strong>Security starts with awareness.</strong>
              <span>
                Your perspective helps your security team see the bigger picture.
              </span>
            </div>
          </div>
        </div>
        <div className="story-footer">
          <LockKeyhole size={15} aria-hidden="true" /> For authorized company accounts
        </div>
      </aside>
      <div className="auth-main">
        <header className="auth-header">
          <span className="company-label">Company access</span>
          <ThemeSelect />
        </header>
        <main id="main" className="auth-content">
          {children}
        </main>
        <footer className="auth-footer">
          CyberGuard <span aria-hidden="true">·</span> Company security, connected.
        </footer>
      </div>
    </div>
  );
}
