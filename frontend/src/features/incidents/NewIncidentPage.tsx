import { AlertCircle, ArrowLeft, LoaderCircle, Send, ShieldCheck } from 'lucide-react';
import { useEffect, useRef, useState, type FormEvent } from 'react';
import { Link, useNavigate } from 'react-router';
import { useAuth } from '../auth/use-auth';
import { incidentsApi } from './api';
import {
  countCharacters,
  inputLimits,
  submissionFailure,
  validateIncident,
  type IncidentInput,
} from './submission';
import './incidents.css';

const empty: IncidentInput = { title: '', description: '', affected_system: '' };

export function NewIncidentPage() {
  const { pending, runProtected } = useAuth();
  const navigate = useNavigate();
  const [input, setInput] = useState(empty);
  const [attempted, setAttempted] = useState(false);
  const [saving, setSaving] = useState(false);
  const [failure, setFailure] = useState<ReturnType<typeof submissionFailure> | null>(
    null,
  );
  const [checkedList, setCheckedList] = useState(false);
  const titleRef = useRef<HTMLInputElement>(null);
  const descriptionRef = useRef<HTMLTextAreaElement>(null);
  const systemRef = useRef<HTMLInputElement>(null);
  const alertRef = useRef<HTMLDivElement>(null);
  const locked = useRef(false);
  const mounted = useRef(true);
  const errors = validateIncident(input);
  const disabled = saving || pending;

  useEffect(() => {
    document.title = 'Report an incident · CyberGuard';
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  useEffect(() => {
    if (failure) alertRef.current?.focus();
  }, [failure]);

  function change(name: keyof IncidentInput, value: string) {
    setInput((previous) => ({ ...previous, [name]: value }));
  }
  function describedBy(name: keyof IncidentInput) {
    return `report-${name}-hint report-${name}-count${attempted && errors[name] ? ` report-${name}-error` : ''}`;
  }
  function counter(name: keyof IncidentInput) {
    const count = countCharacters(input[name]);
    return (
      <span
        id={`report-${name}-count`}
        className={`input-count${count > inputLimits[name] ? ' field-error' : ''}`}
      >
        {count.toLocaleString('en-US')} / {inputLimits[name].toLocaleString('en-US')}{' '}
        characters
      </span>
    );
  }
  function error(name: keyof IncidentInput) {
    return attempted && errors[name] ? (
      <p className="field-error" id={`report-${name}-error`}>
        {errors[name]}
      </p>
    ) : null;
  }
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (locked.current || pending || (failure?.uncertain && !checkedList)) return;
    setAttempted(true);
    if (Object.keys(errors).length) {
      (errors.title
        ? titleRef
        : errors.description
          ? descriptionRef
          : systemRef
      ).current?.focus();
      return;
    }
    locked.current = true;
    setSaving(true);
    setFailure(null);
    try {
      const incident = await runProtected(() => incidentsApi.create(input));
      if (mounted.current)
        navigate(`/incidents/${incident.id}`, {
          replace: true,
          state: { created: true },
        });
    } catch (error) {
      if (mounted.current) {
        setFailure(submissionFailure(error));
        setCheckedList(false);
      }
    } finally {
      locked.current = false;
      if (mounted.current) setSaving(false);
    }
  }

  return (
    <section aria-labelledby="report-heading">
      <Link className="back-link" to="/incidents">
        <ArrowLeft size={16} aria-hidden="true" />
        Back to incidents
      </Link>
      <div className="page-heading">
        <div>
          <span className="eyebrow">NEW REPORT</span>
          <h1 id="report-heading">Report an incident</h1>
          <p>
            Describe what you observed in your own words. You do not need to classify the
            incident. Title and description are required.
          </p>
        </div>
      </div>
      <div className="incident-detail-grid">
        <form
          className="panel report-form"
          noValidate
          onSubmit={(event) => void submit(event)}
          aria-busy={saving}
        >
          {failure && (
            <div
              className="notice notice-error submission-error"
              role="alert"
              tabIndex={-1}
              ref={alertRef}
            >
              <AlertCircle size={18} aria-hidden="true" />
              <div>
                <p>{failure.message}</p>
                {failure.uncertain && (
                  <>
                    <a href="/incidents" target="_blank" rel="noopener noreferrer">
                      Check incidents in a new tab
                    </a>
                    <label className="resubmission-confirmation">
                      <input
                        type="checkbox"
                        checked={checkedList}
                        onChange={(event) => setCheckedList(event.target.checked)}
                      />
                      I checked the list and want to submit this report again.
                    </label>
                  </>
                )}
              </div>
            </div>
          )}
          <div className="report-field">
            <label htmlFor="report-title">Title</label>
            <p className="report-hint" id="report-title-hint">
              A short summary of what happened.
            </p>
            <input
              id="report-title"
              name="title"
              ref={titleRef}
              value={input.title}
              onChange={(event) => change('title', event.target.value)}
              required
              disabled={disabled}
              autoComplete="off"
              spellCheck={false}
              aria-invalid={attempted && !!errors.title}
              aria-describedby={describedBy('title')}
            />
            {counter('title')}
            {error('title')}
          </div>
          <div className="report-field">
            <label htmlFor="report-description">Description</label>
            <p className="report-hint" id="report-description-hint">
              Include what you noticed and when. Do not include passwords or access codes.
            </p>
            <textarea
              id="report-description"
              name="description"
              ref={descriptionRef}
              rows={7}
              value={input.description}
              onChange={(event) => change('description', event.target.value)}
              required
              disabled={disabled}
              autoComplete="off"
              spellCheck={false}
              aria-invalid={attempted && !!errors.description}
              aria-describedby={describedBy('description')}
            />
            {counter('description')}
            {error('description')}
          </div>
          <div className="report-field">
            <div className="report-label">
              <label htmlFor="report-system">Affected system</label>
              <span>Optional</span>
            </div>
            <p className="report-hint" id="report-affected_system-hint">
              For example, company email, a work laptop or an internal application.
            </p>
            <input
              id="report-system"
              name="affected_system"
              ref={systemRef}
              value={input.affected_system}
              onChange={(event) => change('affected_system', event.target.value)}
              disabled={disabled}
              autoComplete="off"
              spellCheck={false}
              aria-invalid={attempted && !!errors.affected_system}
              aria-describedby={describedBy('affected_system')}
            />
            {counter('affected_system')}
            {error('affected_system')}
          </div>
          <p className="submission-review">
            Your title and description cannot be edited after submission. Review your
            report before sending.
          </p>
          <div className="report-actions">
            <button
              className="button button-primary"
              type="submit"
              disabled={disabled || (!!failure?.uncertain && !checkedList)}
            >
              {saving ? (
                <LoaderCircle size={17} className="spinner" aria-hidden="true" />
              ) : (
                <Send size={17} aria-hidden="true" />
              )}
              {saving ? 'Submitting…' : 'Submit incident'}
            </button>
            <Link className="button button-secondary" to="/incidents">
              Cancel
            </Link>
          </div>
          <p className="report-privacy">
            Unsaved reports are kept only on this page, not in browser storage.
          </p>
        </form>
        <aside
          className="panel detail-panel report-guidance"
          aria-labelledby="before-send-heading"
        >
          <h2 id="before-send-heading">
            <ShieldCheck size={18} aria-hidden="true" />
            Before you send
          </h2>
          <ul>
            <li>Describe what you saw, rather than guessing the cause.</li>
            <li>Include relevant timing and the system involved.</li>
            <li>Mention any actions you have already taken.</li>
          </ul>
          <p>
            New reports start as Submitted, without an assigned analyst or confirmed
            category.
          </p>
          <p>This release saves the incident only. No AI assessment is scheduled.</p>
        </aside>
      </div>
    </section>
  );
}
