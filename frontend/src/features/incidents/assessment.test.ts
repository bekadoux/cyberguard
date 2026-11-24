import { describe, expect, it, vi, afterEach } from 'vitest';
import { completedAssessment } from '../../../tests/support/assessments';
import { first } from '../../../tests/support/incidents';
import { getAssessment, indicatorRows, parseAssessment } from './assessment';

afterEach(() => vi.unstubAllGlobals());
describe('assessment contract', () => {
  it('accepts the eight-indicator fixture', () => {
    const assessment = parseAssessment(completedAssessment, first.id);
    expect(
      Object.keys(assessment.result!.assessment.triage.indicator_probabilities),
    ).toHaveLength(8);
  });

  it('keeps archived four-signal results readable without inventing missing rubric scores', () => {
    const oldAssessment = {
      ...completedAssessment,
      result: {
        ...completedAssessment.result,
        assessment: {
          ...completedAssessment.result.assessment,
          triage: {
            ...completedAssessment.result.assessment.triage,
            indicator_probabilities: {
              clicked_suspicious_link: 0.72,
              credentials_entered: 0.04,
              files_encrypted: 0.02,
              unauthorized_access: 0.12,
            },
          },
        },
      },
    };
    const parsed = parseAssessment(oldAssessment, first.id);
    expect(parsed.result?.assessment.triage.indicator_rubric).toBe('legacy');
    expect(
      parsed.result?.assessment.triage.indicator_probabilities.clicked_suspicious_link,
    ).toBe(0.72);
    expect(
      parsed.result?.assessment.triage.indicator_probabilities.attachment_opened,
    ).toBeUndefined();
  });

  it('projects a separate advisory result, not incident classification', () => {
    const parsed = parseAssessment(
      {
        ...completedAssessment,
        secret: 'ignored',
        job: { ...completedAssessment.job, claim_token: 'ignored' },
      },
      first.id,
    );
    expect(parsed.result?.assessment.triage.suggested_category).toBe('phishing');
    expect(parsed.job).not.toHaveProperty('claim_token');
    expect(parsed).not.toHaveProperty('category');
  });
  it('accepts the eight-indicator rubric while keeping schema 1.0', () => {
    const scores = {
      clicked_link: 0.1,
      credentials_entered: 0.2,
      attachment_opened: 0.3,
      payment_made: 0.4,
      data_sent_out: 0.5,
      device_abnormal: 0.6,
      multiple_affected: 0.7,
      ongoing: 0.8,
    };
    const data = {
      ...completedAssessment,
      result: {
        ...completedAssessment.result,
        assessment: {
          ...completedAssessment.result.assessment,
          triage: {
            ...completedAssessment.result.assessment.triage,
            indicator_probabilities: scores,
          },
        },
      },
    };
    const parsed = parseAssessment(data, first.id);
    expect(parsed.result?.assessment.triage.indicator_rubric).toBe('current');
    expect(parsed.result?.assessment.triage.indicator_probabilities).toEqual(scores);
    expect(indicatorRows(scores, 'current')).toContainEqual({
      key: 'credentials_entered',
      label: 'Password or authentication code entered',
      probability: 0.2,
    });
  });
  it('preserves the historical credentials question and marks the new one unscored', () => {
    const data = {
      ...completedAssessment,
      result: {
        ...completedAssessment.result,
        assessment: {
          ...completedAssessment.result.assessment,
          triage: {
            ...completedAssessment.result.assessment.triage,
            indicator_probabilities: { credentials_entered: 0.6 },
          },
        },
      },
    };
    const parsed = parseAssessment(data, first.id);
    const triage = parsed.result?.assessment.triage;
    expect(triage?.indicator_rubric).toBe('legacy');
    const rows = indicatorRows(triage!.indicator_probabilities, triage!.indicator_rubric);
    expect(rows).toContainEqual({
      key: 'credentials_entered',
      label: 'Credentials entered into a suspicious form',
      probability: 0.6,
    });
    expect(rows).toContainEqual({
      key: 'current_credentials_entered',
      label: 'Password or authentication code entered',
      probability: null,
    });
  });
  it('rejects a mixed legacy and current indicator rubric', () => {
    const data = {
      ...completedAssessment,
      result: {
        ...completedAssessment.result,
        assessment: {
          ...completedAssessment.result.assessment,
          triage: {
            ...completedAssessment.result.assessment.triage,
            indicator_probabilities: { clicked_suspicious_link: 0.1, clicked_link: 0.2 },
          },
        },
      },
    };
    expect(() => parseAssessment(data, first.id)).toThrow('invalid_response');
  });
  it.each(['queued', 'running', 'failed'])(
    'accepts a real %s state with no result',
    (status) => {
      expect(
        parseAssessment(
          { job: { ...completedAssessment.job, status }, result: null },
          first.id,
        ).job.status,
      ).toBe(status);
    },
  );
  it.each([
    { ...completedAssessment, result: null },
    {
      ...completedAssessment,
      result: { ...completedAssessment.result, source_kind: ['kev_llm'] },
    },
    { ...completedAssessment, job: { ...completedAssessment.job, incident_id: 'other' } },
    { ...completedAssessment, job: { ...completedAssessment.job, status: 'queued' } },
    { ...completedAssessment, job: { ...completedAssessment.job, status: 'invented' } },
    {
      ...completedAssessment,
      result: { ...completedAssessment.result, source_kind: 'fake' },
    },
    {
      ...completedAssessment,
      result: {
        ...completedAssessment.result,
        assessment: { ...completedAssessment.result.assessment, schema_version: '2.0' },
      },
    },
    {
      ...completedAssessment,
      result: {
        ...completedAssessment.result,
        assessment: {
          ...completedAssessment.result.assessment,
          triage: {
            ...completedAssessment.result.assessment.triage,
            category_probabilities: { phishing: NaN },
          },
        },
      },
    },
    {
      ...completedAssessment,
      result: {
        ...completedAssessment.result,
        assessment: {
          ...completedAssessment.result.assessment,
          triage: {
            ...completedAssessment.result.assessment.triage,
            indicator_probabilities: { unknown: 0.5 },
          },
        },
      },
    },
  ])('rejects malformed, mismatched or misleading provenance', (data) => {
    expect(() => parseAssessment(data, first.id)).toThrow('invalid_response');
  });
  it('uses the same-origin AI prefix, cookies and abort signal', async () => {
    const fetch = vi
      .fn()
      .mockResolvedValue(new Response(JSON.stringify(completedAssessment)));
    vi.stubGlobal('fetch', fetch);
    await getAssessment(first.id, new AbortController().signal);
    expect(fetch).toHaveBeenCalledWith(
      `/ai/incidents/${first.id}/assessment`,
      expect.objectContaining({
        credentials: 'include',
        cache: 'no-store',
        method: 'GET',
        signal: expect.any(AbortSignal),
      }),
    );
  });
});
