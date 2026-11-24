import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { completedAssessment } from '../../../tests/support/assessments';
import { detail, first } from '../../../tests/support/incidents';
import { parseAssessment } from './assessment';
import { AssessmentFindings } from './AssessmentFindings';
import type { IncidentDetail } from './model';

vi.mock('./AIReview', () => ({ AIReview: () => null }));

const incident: IncidentDetail = { ...detail, status: 'submitted' };

describe('archived AI assessment indicator semantics', () => {
  it('does not reinterpret archived credentials_entered as the new question', async () => {
    const user = userEvent.setup();
    const archived = {
      ...completedAssessment,
      result: {
        ...completedAssessment.result,
        assessment: {
          ...completedAssessment.result.assessment,
          triage: {
            ...completedAssessment.result.assessment.triage,
            indicator_probabilities: {
              clicked_suspicious_link: 0.8,
              credentials_entered: 0.6,
            },
          },
        },
      },
    };
    render(
      <AssessmentFindings
        data={parseAssessment(archived, first.id)}
        version="test"
        incident={incident}
        onChanged={vi.fn()}
      />,
    );
    const newQuestion = screen.getByText('Password or authentication code entered');
    expect(within(newQuestion.parentElement!).getByText('Not scored')).toBeVisible();
    await user.click(screen.getByText('Legacy model signals'));
    expect(screen.getByText('Credentials entered into a suspicious form')).toBeVisible();
    expect(screen.getByText('60.0%')).toBeVisible();
  });

  it('shows current scores under the new questions', () => {
    render(
      <AssessmentFindings
        data={parseAssessment(completedAssessment, first.id)}
        version="current"
        incident={incident}
        onChanged={vi.fn()}
      />,
    );
    const currentQuestion = screen.getByText('Password or authentication code entered');
    expect(within(currentQuestion.parentElement!).getByText('2.0%')).toBeVisible();
    expect(screen.queryByText('Legacy model signals')).not.toBeInTheDocument();
  });
});
