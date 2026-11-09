import { first } from './incidents.ts';

export const completedAssessment = {
  job: {
    id: 'dddddddd-dddd-4ddd-8ddd-dddddddddddd',
    incident_id: first.id,
    kind: 'initial_assessment',
    status: 'completed',
    incident_revision: 1,
    attempt_count: 1,
    created_at: first.created_at,
    updated_at: first.updated_at,
    finished_at: first.updated_at,
    error_code: null,
  },
  result: {
    source_kind: 'kev_llm',
    created_at: first.updated_at,
    assessment: {
      schema_version: '1.0',
      is_fake: false,
      triage: {
        suggested_category: 'phishing',
        category_probabilities: { phishing: 0.8, other: 0.2 },
        potential_impact: 1.5,
        indicator_probabilities: { clicked_suspicious_link: 0.3 },
        needs_human_review: true,
        model: 'synthetic-model',
        raw_model_confidence: 0.9,
      },
      brief: {
        summary: 'Synthetic draft summary.\nNeeds analyst review.',
        reported_facts: ['A suspicious message was reported.'],
        unknowns: ['Whether the link was opened.'],
        follow_up_questions: ['When did the message arrive?'],
        suggested_steps: ['Review the message without opening its links.'],
      },
      disclaimer: 'AI-generated draft, not verified evidence.',
    },
  },
};
