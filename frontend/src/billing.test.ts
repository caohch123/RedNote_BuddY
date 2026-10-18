import { describe, expect, it } from 'vitest';
import { billingQuery, tokenNumber, trendPoints } from './billing';
import type { UsageGroup } from './billing';

describe('billing presentation', () => {
  it('keeps missing usage distinct from reported zero', () => {
    expect(tokenNumber(null)).toBe('—'); expect(tokenNumber(0)).toBe('0'); expect(tokenNumber(12345)).toBe('12,345');
  });
  it('uses local calendar dates and an exclusive end for time filters', () => {
    const now = new Date(2026, 9, 18, 15, 20);
    const params = new URLSearchParams(billingQuery('7d', 'model with spaces', 'vision', 2, now));
    const start = new Date(params.get('from')!); const end = new Date(params.get('to')!);
    expect(start.getDate()).toBe(12); expect(start.getHours()).toBe(0); expect(end.getDate()).toBe(19); expect(end.getHours()).toBe(0);
    expect(params.get('model')).toBe('model with spaces'); expect(params.get('page')).toBe('2');
    expect(new URLSearchParams(billingQuery('all', '', '')).has('from')).toBe(false);
    expect(new URLSearchParams(billingQuery('all', '', '', 1, now, 'member id')).get('user_id')).toBe('member id');
  });
  it('fills quiet days and does not double count cached or reasoning tokens', () => {
    const row = { key: '2026-10-18', input_tokens: 100, output_tokens: 30, total_tokens: 130, cached_tokens: 70, reasoning_tokens: 20 } as UsageGroup;
    const points = trendPoints([row], '7d', new Date(2026, 9, 18, 15));
    expect(points).toHaveLength(7); expect(points[0].total).toBe(0); expect(points[6]).toMatchObject({ input: 100, output: 30, other: 0, total: 130 });
    expect(trendPoints([{ ...row, input_tokens: 0, output_tokens: 0 }], '7d', new Date(2026, 9, 18))[6].other).toBe(130);
  });
  it('keeps separate calendar days across daylight saving changes', () => {
    const previous = process.env.TZ;
    try {
      process.env.TZ = 'America/New_York';
      const rows = Array.from({ length: 7 }, (_, i) => ({ key: `2026-03-${String(i + 7).padStart(2, '0')}`, input_tokens: 1, output_tokens: 0, total_tokens: 1 } as UsageGroup));
      const points = trendPoints(rows, '7d', new Date(2026, 2, 13, 15));
      expect(points.map(p => p.total)).toEqual([1, 1, 1, 1, 1, 1, 1]);
    } finally { if (previous == null) delete process.env.TZ; else process.env.TZ = previous; }
  });
});
