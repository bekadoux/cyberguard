import { describe, expect, it } from 'vitest';
import { ApiError } from '../../shared/api/client';
import {
  countCharacters,
  submissionBody,
  submissionFailure,
  validateIncident,
} from './submission';

const valid = {
  title: 'Suspicious message',
  description: 'I received an unexpected email.',
  affected_system: '',
};
describe('incident submission validation', () => {
  it('counts Unicode code points rather than UTF-16 units or grapheme clusters', () => {
    expect(countCharacters('🔒')).toBe(1);
    expect(countCharacters('e\u0301')).toBe(2);
    expect(countCharacters('👨‍👩‍👧‍👦')).toBe(7);
    expect(
      validateIncident({
        ...valid,
        title: '🔒'.repeat(200),
        description: '界'.repeat(10000),
        affected_system: '🔒'.repeat(500),
      }),
    ).toEqual({});
  });
  it.each(['title', 'description', 'affected_system'] as const)(
    'rejects an overlong %s',
    (name) => {
      const limits = { title: 200, description: 10000, affected_system: 500 };
      expect(
        validateIncident({ ...valid, [name]: '🔒'.repeat(limits[name] + 1) })[name],
      ).toBeTruthy();
    },
  );
  it('matches Go whitespace semantics for NEL versus BOM', () => {
    expect(validateIncident({ ...valid, title: ' \u0085\u2003\t' }).title).toBeTruthy();
    expect(validateIncident({ ...valid, title: '\uFEFF' })).toEqual({});
    expect(validateIncident({ ...valid, affected_system: ' \u0085 ' })).toEqual({});
  });
  it.each(['\0', '\uD800', '\uDC00'])(
    'rejects NUL and malformed Unicode',
    (character) => {
      expect(
        validateIncident({ ...valid, description: `Content ${character}` }).description,
      ).toBeTruthy();
    },
  );
  it('does not trim or rewrite original text and whitelists fields', () => {
    const input = {
      title: '  Original title  ',
      description: '  Original\n\nSlovenský text 🔒  ',
      affected_system: '  Work email  ',
      reporter_id: 'never-submit',
      severity: 'critical',
    };
    expect(submissionBody(input)).toEqual({
      title: input.title,
      description: input.description,
      affected_system: input.affected_system,
    });
    expect(submissionBody(valid)).not.toHaveProperty('affected_system');
  });
  it.each([0, 500, 503, 201])(
    'treats an uncertain %s result as requiring review before a retry',
    (status) => {
      expect(submissionFailure(new ApiError(status, 'invalid_response')).uncertain).toBe(
        true,
      );
    },
  );
  it.each([400, 403, 413, 415])('keeps a definitive rejection %s retryable', (status) => {
    expect(submissionFailure(new ApiError(status, 'request_failed')).uncertain).toBe(
      false,
    );
  });
});
