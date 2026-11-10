import { describe, expect, it } from 'vitest';
import { analyst, detail, first } from '../../../tests/support/incidents';
import {
  incidentListBackLink,
  parseDetail,
  parsePage,
  parseSummary,
  readFilters,
} from './model';

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

describe('incident detail contract', () => {
  it('preserves original Unicode text, whitespace and reporter-facing resolution', () => {
    const original = {
      ...detail,
      description: '  Original\n\nŽiadne údaje 🔒  ',
      resolution_note: 'Full resolution\nSecond line',
    };
    expect(parseDetail({ incident: original })).toEqual(original);
  });
  it('accepts a missing analyst account with a retained stable ID', () => {
    expect(
      parseDetail({
        incident: {
          ...detail,
          assigned_analyst_id: analyst.id,
          assigned_analyst: { id: analyst.id, username: null },
        },
      }).assigned_analyst,
    ).toEqual({ id: analyst.id, username: null });
  });
  it.each([
    { ...detail, description: null },
    { ...detail, assigned_analyst: undefined },
    { ...detail, assigned_analyst_id: analyst.id },
    { ...detail, resolution_note: 17 },
  ])('rejects incomplete detail instead of partially rendering it', (incident) => {
    expect(() => parseDetail({ incident })).toThrow('invalid_response');
  });
  it('limits back navigation to implemented incident filters', () => {
    expect(
      incidentListBackLink({ listSearch: 'status=submitted&category=phishing' }),
    ).toBe('/incidents?status=submitted&category=phishing');
    expect(incidentListBackLink({ listSearch: 'https://evil.invalid' })).toBe(
      '/incidents',
    );
    expect(incidentListBackLink(null)).toBe('/incidents');
  });
});
