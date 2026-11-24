import { ChevronDown, ChevronUp, ShieldAlert } from 'lucide-react';
import { useState } from 'react';
import type { Assessment } from './assessment';
import {
  currentIndicatorLabels,
  domainIndicatorKeys,
  legacyIndicatorLabels,
} from './assessment';
import { AIReview } from './AIReview';
import { categoryLabels, type IncidentDetail, type IncidentCategory } from './model';

type Props = {
  data: Assessment;
  version: string;
  incident: IncidentDetail;
  onChanged: () => void;
};

type ToggleState = { assessment: string; expanded: boolean } | null;
const percent = (value: number) => `${(value * 100).toFixed(1)}%`;

/** Renders persisted, unconfirmed model output. Never patches human-owned fields. */
export function AssessmentFindings({ data, version, incident, onChanged }: Props) {
  const [chartToggle, setChartToggle] = useState<ToggleState>(null);
  const [briefToggle, setBriefToggle] = useState<ToggleState>(null);
  if (!data.result) return null;
  const result = data.result;
  const { triage, brief } = result.assessment;
  const assessmentKey = `${data.job.id}/${result.created_at}`;
  const categoryScores = Object.entries(triage.category_probabilities).sort(
    (a, b) => b[1] - a[1],
  );
  const top = categoryScores[0];
  const abstains = triage.suggested_category === null;
  const chartOpen =
    chartToggle?.assessment === assessmentKey ? chartToggle.expanded : abstains;
  const briefOpen =
    briefToggle?.assessment === assessmentKey ? briefToggle.expanded : true;
  const abstentionReason = !top
    ? 'The model did not provide category scores.'
    : top[0] === 'other'
      ? '“Other / unclear” has the highest model score. It is not an actionable recommendation.'
      : top[1] < 0.6
        ? 'The highest score is below the provisional 60% suggestion threshold.'
        : 'No category recommendation was returned; a human decision is required.';
  // The shared credentials_entered key changed meaning in the new rubric.
  // Never render an archived value under the new question.
  const legacySignals =
    triage.indicator_rubric === 'legacy'
      ? Object.entries(triage.indicator_probabilities)
      : [];

  return (
    <div className="assessment-findings">
      {result.source_kind === 'fake' && (
        <p className="notice assessment-fixture" role="note">
          <strong>Test fixture — not AI output.</strong>
          Synthetic integration data must not be used for security decisions.
        </p>
      )}
      <section
        className="assessment-recommendation"
        aria-label="Kev category recommendation"
      >
        <div className="assessment-eyebrow">
          <ShieldAlert size={16} aria-hidden="true" /> KEV CATEGORY ADVICE
        </div>
        <div className="assessment-recommendation-main">
          <div>
            <h3>
              {triage.suggested_category
                ? categoryLabels[triage.suggested_category]
                : 'No recommendation'}
            </h3>
            {abstains ? (
              <p>{abstentionReason}</p>
            ) : (
              <p>Suggested only. An analyst must confirm or correct this category.</p>
            )}
          </div>
          {triage.suggested_category && (
            <span className="assessment-score">
              {percent(triage.category_probabilities[triage.suggested_category] ?? 0)}
              <small>model score</small>
            </span>
          )}
        </div>
        {abstains && top && (
          <p className="assessment-top-result">
            Highest-scoring result (not a recommendation):{' '}
            <strong>{categoryLabels[top[0] as IncidentCategory]}</strong> —{' '}
            {percent(top[1])}
          </p>
        )}
      </section>
      <section
        className="assessment-subsection"
        aria-labelledby="category-scores-heading"
      >
        <button
          type="button"
          className="assessment-toggle"
          aria-expanded={chartOpen}
          aria-controls="assessment-category-chart"
          onClick={() =>
            setChartToggle({ assessment: assessmentKey, expanded: !chartOpen })
          }
        >
          <span id="category-scores-heading">Category scores</span>
          {chartOpen ? (
            <ChevronUp size={18} aria-hidden="true" />
          ) : (
            <ChevronDown size={18} aria-hidden="true" />
          )}
        </button>
        {chartOpen && (
          <div id="assessment-category-chart" className="assessment-category-chart">
            <p className="muted">
              Ranked model scores, not calibrated probabilities of real-world harm.
            </p>
            {categoryScores.length ? (
              categoryScores.map(([key, value]) => (
                <div key={key} className="assessment-bar-row">
                  <span>{categoryLabels[key as IncidentCategory]}</span>
                  <div
                    className="assessment-bar-track"
                    role="meter"
                    aria-label={categoryLabels[key as IncidentCategory]}
                    aria-valuemin={0}
                    aria-valuemax={100}
                    aria-valuenow={Number((value * 100).toFixed(1))}
                  >
                    <div
                      className="assessment-bar-fill"
                      style={{ width: percent(value) }}
                    />
                  </div>
                  <strong>{percent(value)}</strong>
                </div>
              ))
            ) : (
              <p>No scores supplied.</p>
            )}
          </div>
        )}
      </section>
      <section
        className="assessment-subsection assessment-indicators"
        aria-labelledby="indicator-heading"
      >
        <h3 id="indicator-heading">Reported indicators</h3>
        <p className="muted">
          Eight questions from the incident rubric. Scores represent what the report
          explicitly mentions; a low score does not prove an event did not occur. No
          indicator changes severity automatically.
        </p>
        <div className="assessment-indicator-grid">
          {domainIndicatorKeys.map((key) => {
            const value =
              triage.indicator_rubric === 'current'
                ? triage.indicator_probabilities[key]
                : undefined;
            return (
              <div key={key} className="assessment-indicator">
                <span>{currentIndicatorLabels[key]}</span>
                {value === undefined ? (
                  <strong className="muted">Not scored</strong>
                ) : (
                  <>
                    <strong>{percent(value)}</strong>
                    <div className="assessment-indicator-track" aria-hidden="true">
                      <div style={{ width: percent(value) }} />
                    </div>
                  </>
                )}
              </div>
            );
          })}
        </div>
        {legacySignals.length > 0 && (
          <details className="assessment-legacy">
            <summary>Legacy model signals</summary>
            <dl className="assessment-signals">
              {legacySignals.map(([key, value]) => (
                <div key={key}>
                  <dt>
                    {legacyIndicatorLabels[key as keyof typeof legacyIndicatorLabels]}
                  </dt>
                  <dd>{percent(value)}</dd>
                </div>
              ))}
            </dl>
          </details>
        )}
      </section>
      <section className="assessment-subsection" aria-labelledby="draft-brief-heading">
        <button
          type="button"
          className="assessment-toggle"
          aria-expanded={briefOpen}
          aria-controls="assessment-brief"
          onClick={() =>
            setBriefToggle({ assessment: assessmentKey, expanded: !briefOpen })
          }
        >
          <span id="draft-brief-heading">AI draft brief</span>
          {briefOpen ? (
            <ChevronUp size={18} aria-hidden="true" />
          ) : (
            <ChevronDown size={18} aria-hidden="true" />
          )}
        </button>
        {briefOpen && (
          <div id="assessment-brief" className="assessment-draft">
            <h3>Draft summary</h3>
            <p className="text-preserved">{brief.summary}</p>
            {(
              [
                ['Reported facts (model-extracted)', brief.reported_facts],
                ['Unknowns', brief.unknowns],
                ['Follow-up questions', brief.follow_up_questions],
              ] as const
            ).map(([heading, items]) => (
              <div key={heading}>
                <h3>{heading}</h3>
                {items.length ? (
                  <ul>
                    {items.map((item, index) => (
                      <li className="text-preserved" key={index}>
                        {item}
                      </li>
                    ))}
                  </ul>
                ) : (
                  <p className="muted">None supplied.</p>
                )}
              </div>
            ))}
          </div>
        )}
      </section>
      <section
        className="assessment-subsection assessment-playbook"
        aria-labelledby="playbook-heading"
      >
        <h3 id="playbook-heading">Approved playbook steps</h3>
        <p className="muted">
          Selected from checked-in playbooks, not generated actions.
        </p>
        {brief.suggested_steps.length ? (
          <ol>
            {brief.suggested_steps.map((step, index) => (
              <li key={index} className="text-preserved">
                {step}
              </li>
            ))}
          </ol>
        ) : (
          <p className="muted">No steps supplied.</p>
        )}
      </section>
      <dl className="assessment-signals assessment-metadata">
        <div>
          <dt>Potential impact score (0–3)</dt>
          <dd>{triage.potential_impact ?? 'Not supplied'} · not confirmed severity</dd>
        </div>
        <div>
          <dt>Model</dt>
          <dd className="text-preserved">{triage.model}</dd>
        </div>
      </dl>
      <AIReview
        key={data.job.id}
        assessment={data}
        version={version}
        incident={incident}
        onChanged={onChanged}
      />
      <p className="text-preserved assessment-disclaimer">
        {result.assessment.disclaimer}
      </p>
      <p className="assessment-time">
        Saved{' '}
        <time dateTime={result.created_at}>
          {new Date(result.created_at).toLocaleString()}
        </time>{' '}
        · {result.source_kind === 'fake' ? 'Synthetic fixture' : 'Kev / LLM'}
      </p>
    </div>
  );
}
