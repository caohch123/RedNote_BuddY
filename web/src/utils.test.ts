import { describe, expect, it } from 'vitest';
import { applyImageToDraft, fullCopy, number, ratio, toDraft } from './utils';
import type { Version } from './types';

describe('content presentation', () => {
  it('keeps absent engagement data distinct from zero and avoids division by zero', () => {
    expect(number(null)).toBe('—'); expect(number(0)).toBe('0');
    expect(ratio(100, 0)).toBe('—'); expect(ratio(null, 8)).toBe('—');
    expect(ratio(0, 2)).toBe('0.00'); expect(ratio(100, 25)).toBe('4.00');
  });
  it('creates a publication copy including all topics without duplicate hash signs', () => {
    expect(fullCopy({ title: '标题', body: '完整正文', topics: ['生活', '#学习'] }))
      .toBe('标题\n\n完整正文\n\n#生活 #学习');
  });
  it('applying an image preserves pending copy edits, deselections and the order of other images', () => {
    const before: Version = { id: 'v1', number: 1, title: '原题', body: '原文', topics: ['原话题'], script: '原口播', asset_ids: ['a', 'b', 'c'], confirmed: false, created_at: '' };
    const next = { ...before, id: 'v2', asset_ids: ['result', 'b', 'c'] };
    const pending = { ...toDraft(before), title: '尚未保存的标题', body: '尚未保存的正文', topics: ['新话题'], asset_ids: ['c', 'a'] };
    const merged = applyImageToDraft(pending, before, next, ['a'], 'result');
    expect(merged).toEqual({ ...pending, asset_ids: ['c', 'result'] });
    expect(applyImageToDraft(merged, next, next, ['a'], 'result').asset_ids).toEqual(['c', 'result']);
    expect(applyImageToDraft(toDraft(before), before, next, ['a'], 'result')).toEqual(toDraft(next));
    expect(applyImageToDraft({ ...pending, asset_ids: ['c'] }, before, next, ['a'], 'result').asset_ids).toEqual(['c']);
    expect(before.asset_ids).toEqual(['a', 'b', 'c']);
  });
});
