import { describe, expect, it } from 'vitest';
import { first } from '../../../tests/support/incidents';
import { parsePage, parseSummary, readFilters } from './model';

describe('incident summary contract', () => {
  it('keeps absent severity distinct from unassessed professional severity', () => {
    expect(parseSummary(first)).not.toHaveProperty('severity');
    expect(parseSummary({ ...first, severity: null })).toHaveProperty('severity', null);
  });
  it('projects safe fields only', () => {
    expect(
      parseSummary({ ...first, ai_result: 'private', archive_reason: 'private' }),
    ).toEqual(first);
  });
  it.each([
    { ...first, id: 'bad' },
    { ...first, status: 'queued' },
    { ...first, category: 'ransomware' },
    { ...first, severity: 'urgent' },
    { ...first, created_at: 'invalid' },
    null,
  ])('rejects malformed summaries', (data) => {
    expect(() => parseSummary(data)).toThrow('invalid_response');
  });
  it('requires a bounded page and cursor', () => {
    expect(parsePage({ incidents: [], next_cursor: null })).toEqual({
      incidents: [],
      next_cursor: null,
    });
    for (const data of [
      { incidents: null, next_cursor: null },
      { incidents: [], next_cursor: '' },
      { incidents: Array(101).fill(first), next_cursor: null },
      { incidents: [], next_cursor: 'a'.repeat(257) },
    ])
      expect(() => parsePage(data)).toThrow('invalid_response');
  });
  it.each([
    'status=',
    'status=submitted&status=resolved',
    'severity=high',
    'category=invalid',
    'sort=title',
  ])('rejects unsupported URL filters %s', (query) => {
    expect(readFilters(new URLSearchParams(query))).toBeNull();
  });
  it('accepts only implemented filters', () => {
    expect(
      readFilters(new URLSearchParams('status=submitted&category=phishing')),
    ).toEqual({ status: 'submitted', category: 'phishing' });
  });
});
