import { ApiError, request } from '../../shared/api/client';
import {
  categoryLabels,
  invalidResponse,
  isRecord,
  isUUID,
  type IncidentCategory,
} from './model';

export const jobLabels = {
  queued: 'Queued',
  running: 'Running',
  completed: 'Completed',
  failed: 'Failed',
} as const;
export const indicatorLabels = {
  clicked_suspicious_link: 'Suspicious link clicked',
  credentials_entered: 'Credentials entered',
  files_encrypted: 'Files encrypted',
  unauthorized_access: 'Unauthorized access reported',
} as const;
type JobStatus = keyof typeof jobLabels;
type Probabilities = Record<string, number>;
export type Assessment = {
  job: {
    id: string;
    incident_id: string;
    status: JobStatus;
    incident_revision: number;
    attempt_count: number;
    created_at: string;
    updated_at: string;
    finished_at: string | null;
    error_code: string | null;
  };
  result: {
    source_kind: 'fake' | 'kev_llm';
    created_at: string;
    assessment: {
      schema_version: '1.0';
      is_fake: boolean;
      disclaimer: string;
      triage: {
        model: string;
        suggested_category: IncidentCategory | null;
        potential_impact: number | null;
        needs_human_review: boolean;
        raw_model_confidence: number | null;
        category_probabilities: Probabilities;
        indicator_probabilities: Probabilities;
      };
      brief: {
        summary: string;
        reported_facts: string[];
        unknowns: string[];
        follow_up_questions: string[];
        suggested_steps: string[];
      };
    };
  } | null;
};

const text = (value: unknown): value is string => typeof value === 'string';
const date = (value: unknown): value is string =>
  text(value) && Number.isFinite(Date.parse(value));
const integer = (value: unknown): value is number =>
  typeof value === 'number' && Number.isSafeInteger(value) && value >= 0;
function strings(value: unknown): string[] {
  if (!Array.isArray(value) || value.length > 5 || !value.every(text))
    return invalidResponse();
  return [...value];
}
function probabilities(value: unknown, labels: object): Probabilities {
  if (!isRecord(value)) return invalidResponse();
  const result: Probabilities = {};
  for (const [key, probability] of Object.entries(value)) {
    if (
      !Object.hasOwn(labels, key) ||
      typeof probability !== 'number' ||
      !Number.isFinite(probability) ||
      probability < 0 ||
      probability > 1
    )
      return invalidResponse();
    result[key] = probability;
  }
  return result;
}

/** Project stored, advisory output only; never merge suggestions into incident fields. */
export function parseAssessment(data: unknown, incidentId: string): Assessment {
  if (!isRecord(data) || !isRecord(data.job)) return invalidResponse();
  const job = data.job;
  if (
    !isUUID(job.id) ||
    job.incident_id !== incidentId ||
    job.kind !== 'initial_assessment' ||
    !text(job.status) ||
    !Object.hasOwn(jobLabels, job.status) ||
    !integer(job.incident_revision) ||
    job.incident_revision < 1 ||
    !integer(job.attempt_count) ||
    !date(job.created_at) ||
    !date(job.updated_at) ||
    (job.finished_at !== null && !date(job.finished_at)) ||
    (job.error_code !== null &&
      (!text(job.error_code) || !/^[a-z_]{1,50}$/.test(job.error_code)))
  )
    return invalidResponse();
  let result: Assessment['result'] = null;
  if (job.status === 'completed') {
    const row = data.result;
    if (
      !isRecord(row) ||
      (row.source_kind !== 'fake' && row.source_kind !== 'kev_llm') ||
      !date(row.created_at) ||
      !isRecord(row.assessment)
    )
      return invalidResponse();
    const assessment = row.assessment;
    if (
      assessment.schema_version !== '1.0' ||
      typeof assessment.is_fake !== 'boolean' ||
      assessment.is_fake !== (row.source_kind === 'fake') ||
      !text(assessment.disclaimer) ||
      !isRecord(assessment.triage) ||
      !isRecord(assessment.brief)
    )
      return invalidResponse();
    const triage = assessment.triage;
    const brief = assessment.brief;
    if (
      !text(triage.model) ||
      !triage.model ||
      (triage.suggested_category !== null &&
        (!text(triage.suggested_category) ||
          !Object.hasOwn(categoryLabels, triage.suggested_category))) ||
      (triage.potential_impact !== null &&
        (typeof triage.potential_impact !== 'number' ||
          !Number.isFinite(triage.potential_impact) ||
          triage.potential_impact < 0 ||
          triage.potential_impact > 3)) ||
      typeof triage.needs_human_review !== 'boolean' ||
      (triage.raw_model_confidence !== null &&
        (typeof triage.raw_model_confidence !== 'number' ||
          !Number.isFinite(triage.raw_model_confidence))) ||
      !text(brief.summary) ||
      !brief.summary ||
      Array.from(brief.summary).length > 900
    )
      return invalidResponse();
    result = {
      source_kind: row.source_kind as 'fake' | 'kev_llm',
      created_at: row.created_at,
      assessment: {
        schema_version: '1.0',
        is_fake: assessment.is_fake,
        disclaimer: assessment.disclaimer,
        triage: {
          model: triage.model,
          suggested_category: triage.suggested_category as IncidentCategory | null,
          potential_impact: triage.potential_impact as number | null,
          needs_human_review: triage.needs_human_review,
          raw_model_confidence: triage.raw_model_confidence as number | null,
          category_probabilities: probabilities(
            triage.category_probabilities,
            categoryLabels,
          ),
          indicator_probabilities: probabilities(
            triage.indicator_probabilities,
            indicatorLabels,
          ),
        },
        brief: {
          summary: brief.summary,
          reported_facts: strings(brief.reported_facts),
          unknowns: strings(brief.unknowns),
          follow_up_questions: strings(brief.follow_up_questions),
          suggested_steps: strings(brief.suggested_steps),
        },
      },
    };
  } else if (data.result !== null) return invalidResponse();
  return {
    job: {
      id: job.id,
      incident_id: incidentId,
      status: job.status as JobStatus,
      incident_revision: job.incident_revision,
      attempt_count: job.attempt_count,
      created_at: job.created_at,
      updated_at: job.updated_at,
      finished_at: job.finished_at as string | null,
      error_code: job.error_code as string | null,
    },
    result,
  };
}

export async function getAssessment(
  id: string,
  signal: AbortSignal,
): Promise<Assessment> {
  if (!isUUID(id)) throw new ApiError(404, 'not_found');
  return parseAssessment(
    await request(`/incidents/${encodeURIComponent(id)}/assessment`, {
      service: 'ai',
      expectedStatus: 200,
      signal,
    }),
    id,
  );
}

export function assessmentError(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.status === 404)
      return 'No assessment is available for this report. Older reports may not have an assessment job.';
    if (error.status === 403) return 'You do not have access to this assessment.';
    if (error.status === 503 || error.status === 502)
      return 'AI assessment is temporarily unavailable or is not enabled in this environment.';
    if (error.code === 'invalid_response')
      return 'The assessment service returned an unexpected response. No result has been displayed.';
  }
  return 'Unable to load the assessment. Check your connection and try again.';
}

export function assessmentFailure(code: string | null): string {
  if (code === 'stale_context')
    return 'The report changed after this assessment was scheduled.';
  if (code === 'go_context_unavailable')
    return 'The worker could not retrieve the incident context.';
  return 'The assessment could not be completed. No verified AI result is available.';
}
