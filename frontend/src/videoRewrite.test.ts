import { describe, expect, it } from 'vitest';
import { applyVideoToDraft, replaceVideoMaterial, sortVideoRewriteProjects, videoForm, videoInputSignature, videoSnapshotLabels, videoTiming } from './videoRewrite';
import { toDraft } from './utils';
import type { Version, VideoProject } from './types';

function project(id: string, source: string | undefined, created: string): VideoProject {
  return { id, rewrite_id: 'rewrite', version_id: 'version', title: '', duration: 10, revision: 1,
    status: 'draft', stale: false, created_at: created, updated_at: created, workflow: 'video_rewrite_v2',
    video_rewrite: { ...videoForm(), initial_source_asset_id: source, primary_asset_id: source || '', script: [], prompt: '',
      script_input_digest: '', script_model_signature: '', initialization_complete: true, analyses: [], submissions: [], results: [] } };
}

describe('video rewrite draft decisions', () => {
  it('opens the first original video and keeps scheme numbers stable when polling reverses update order or users replace the main video', () => {
    const first = project('first', 'source1', '2026-10-14T01:00:00Z');
    const second = project('second', 'source2', '2026-10-14T01:00:01Z');
    const sources = [{ id: 'source2', position: 4 }, { id: 'source1', position: 0 }];
    const serverOrder = [second, first];
    expect(sortVideoRewriteProjects(serverOrder, sources).map(item => item.id)).toEqual(['first', 'second']);
    expect(serverOrder.map(item => item.id)).toEqual(['second', 'first']);
    const edited = { ...first, updated_at: '2026-10-14T03:00:00Z', video_rewrite: { ...first.video_rewrite!, primary_asset_id: 'replacement' } };
    expect(sortVideoRewriteProjects([edited, second], sources).map(item => item.id)).toEqual(['first', 'second']);
    const removed = { ...edited, video_rewrite: { ...edited.video_rewrite, primary_asset_id: '' } };
    expect(sortVideoRewriteProjects([second, removed], sources)[0].id).toBe('first');
  });
  it('places manual drafts after source drafts, using creation time and ID even when lineage is absent', () => {
    const source = project('source', 'original', '2026-10-14T03:00:00Z');
    const manualA = project('manual-a', undefined, '2026-10-14T01:00:00Z');
    const manualB = project('manual-b', '', '2026-10-14T01:00:00Z');
    const manualLate = project('manual-late', '', '2026-10-14T02:00:00Z');
    const missingState = { ...project('missing-state', undefined, '2026-10-14T00:00:00Z'), video_rewrite: undefined };
    expect(sortVideoRewriteProjects([manualLate, manualB, source, manualA, missingState], [{ id: 'original', position: 0 }]).map(item => item.id))
      .toEqual(['source', 'missing-state', 'manual-a', 'manual-b', 'manual-late']);
  });
  it('uses Wan per-type numbering while preserving historical labels', () => {
    expect(videoSnapshotLabels({ provider: 'bailian_wan3', input_asset_ids: ['v', 'i'], video_urls: { v: 'https://example.com/v.mp4' } })).toEqual({ v: '视频1', i: '图1' });
  });
  it('counts only selected videos toward the Wan duration budget and leaves user choices unchanged', () => {
    const form = { ...videoForm(), primary_asset_id: 'main', reference_video_id: 'ref', purpose_sources: { subject: 'main' }, output: { ...videoForm().output, duration: 25 } };
    const assets = [{ id: 'main', duration: 6 }, { id: 'ref', duration: 12 }];
    expect(videoTiming(form, assets)).toEqual({ inputDuration: 6, maxDuration: 24, error: '当前参考视频合计 6.0 秒，输出视频最多 24 秒' });
    expect(form.output.duration).toBe(25);
    expect(videoTiming({ ...form, purpose_sources: { subject: 'main', style: 'ref' } }, assets).error).toContain('合计不能超过 15 秒');
  });
  it('allows 2–30 second text-only output and flags overlong references', () => {
    const form = videoForm();
    expect(videoTiming({ ...form, output: { ...form.output, duration: 30 } }, []).error).toBe('');
    expect(videoTiming({ ...form, primary_asset_id: 'v', purpose_sources: { action: 'v' } }, [{ id: 'v', duration: 16 }]).error).toContain('单条不能超过 15 秒');
  });
  it('removes old reference purposes when replacing a material, without selecting new reference purposes', () => {
    const original = { ...videoForm(), reference_video_id: 'ref', purpose_sources: { subject: 'ref', style: 'main' } };
    const next = replaceVideoMaterial(original, 'reference_video_id', 'new');
    expect(next.purpose_sources).toEqual({ style: 'main' });
    expect(original.purpose_sources.subject).toBe('ref');
  });
  it('defaults only the unused main-video purposes and makes settings and requirements invalidate script inputs', () => {
    const form = replaceVideoMaterial({ ...videoForm(), purpose_sources: { subject: 'ref' } }, 'primary_asset_id', 'main');
    expect(form.purpose_sources).toEqual({ content: 'main', subject: 'ref', action: 'main' });
    expect(videoInputSignature({ ...form, prompt: '保留主角，改为夜景。' })).toBe(videoInputSignature(form));
    expect(videoInputSignature({ ...form, requirements: '改为夜景' })).not.toBe(videoInputSignature(form));
    expect(videoInputSignature({ ...form, output: { ...form.output, generate_audio: false } })).not.toBe(videoInputSignature(form));
  });
  it('labels historical input order from the immutable snapshot rather than the current draft', () => {
    expect(videoSnapshotLabels({ input_asset_ids: ['old-main', 'old-ref', 'old-image'], video_urls: { 'old-main': 'https://a', 'old-ref': 'https://b' } } as Parameters<typeof videoSnapshotLabels>[0])).toEqual({ 'old-main': '@视频1', 'old-ref': '@视频2', 'old-image': '@图像1' });
  });
  it('adopts only the main video and preserves pending copy, order, and deselections; appends if there was no main', () => {
    const old: Version = { id: 'v1', number: 1, title: 'old', body: 'old body', topics: [], script: '', asset_ids: ['a', 'main', 'b'], created_at: '', confirmed: false };
    const next = { ...old, id: 'v2', asset_ids: ['a', 'result', 'b'] };
    const pending = { ...toDraft(old), title: 'unsaved', asset_ids: ['b', 'main'] };
    expect(applyVideoToDraft(pending, old, next, ['main'], 'result')).toEqual({ ...pending, asset_ids: ['b', 'result'] });
    expect(applyVideoToDraft({ ...pending, asset_ids: ['b'] }, old, next, ['main'], 'result').asset_ids).toEqual(['b']);
    expect(applyVideoToDraft(pending, old, next, [], 'result').asset_ids).toEqual(['b', 'main', 'result']);
  });
});
