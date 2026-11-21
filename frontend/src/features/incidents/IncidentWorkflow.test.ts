import { describe, expect, it } from 'vitest';
import { detail } from '../../../tests/support/incidents';
import { availableTransitions } from './workflow-ui';

const sample = { ...detail, assigned_analyst_id: detail.reporter_id };
describe('workflow role and ownership hints', () => {
  it('offers only assigned analyst triage transitions', () => {
    expect(
      availableTransitions(
        { ...sample, status: 'triage' },
        'analyst',
        sample.reporter_id,
      ),
    ).toEqual(['investigating', 'false_positive']);
    expect(
      availableTransitions({ ...sample, status: 'triage' }, 'analyst', 'someone-else'),
    ).toEqual([]);
  });
  it('lets reporter review a resolution, but not change triage', () => {
    expect(
      availableTransitions(
        { ...sample, status: 'resolved' },
        'employee',
        sample.reporter_id,
      ),
    ).toEqual(['closed', 'investigating']);
    expect(
      availableTransitions(
        { ...sample, status: 'triage' },
        'employee',
        sample.reporter_id,
      ),
    ).toEqual([]);
  });
  it('lets admin reopen terminal incidents', () => {
    expect(
      availableTransitions({ ...sample, status: 'false_positive' }, 'admin', 'admin'),
    ).toEqual(['investigating']);
    expect(
      availableTransitions(
        { ...sample, status: 'closed' },
        'analyst',
        sample.reporter_id,
      ),
    ).toEqual([]);
  });
});
