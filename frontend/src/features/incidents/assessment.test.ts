import { describe, expect, it, vi, afterEach } from 'vitest';
import { completedAssessment } from '../../../tests/support/assessments';
import { first } from '../../../tests/support/incidents';
import { getAssessment, parseAssessment } from './assessment';

afterEach(() => vi.unstubAllGlobals());
describe('assessment contract', () => {
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
