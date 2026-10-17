import { describe, expect, it } from 'vitest';
import { healthRows, runtimeDependencyRows, runtimePathChanged } from './runtimeDependencies';
import type { Settings } from './types';

describe('local dependency decisions', () => {
  const report = { dependencies: { ffmpeg: { ok: true, path: 'C:/ffmpeg.exe', version: 'ffmpeg version 9' }, ffprobe: { ok: true }, node: { ok: false, message: '需要 Node.js 22 或更高版本' }, collector: { ok: true }, database: { ok: true } } };
  it('shows each editable tool once and keeps service status in the same list', () => {
    const rows = runtimeDependencyRows(report);
    expect(rows.map(row => row.name)).toEqual(['ffmpeg', 'ffprobe', 'node', 'collector', 'database']);
    expect(rows.filter(row => row.field).map(row => row.field)).toEqual(['ffmpeg_path', 'ffprobe_path', 'node_path']);
    expect(rows.find(row => row.name === 'node')).toMatchObject({ ok: false, checked: true, message: '需要 Node.js 22 或更高版本' });
    expect(rows.find(row => row.name === 'ffmpeg')?.version).toBe('ffmpeg version 9');
  });
  it('distinguishes missing checks and unsaved path edits from ready tools', () => {
    expect(runtimeDependencyRows(undefined).every(row => !row.checked)).toBe(true);
    const saved = { node_path: 'node', ffmpeg_path: '' } as Settings;
    expect(runtimePathChanged('node_path', { ...saved, node_path: 'C:/custom/node.exe' }, saved)).toBe(true);
    expect(runtimePathChanged('node_path', { ...saved, node_path: '' }, saved)).toBe(true);
    expect(runtimePathChanged('node_path', saved, saved)).toBe(false);
    expect(healthRows({ checks: { database: false } })).toEqual([{ name: 'database', ok: false }]);
  });
});
