import { describe, expect, it } from 'vitest';
import { canCancelManagedJob, canRetryManagedJob, modelSettingsPatch } from './admin';
import type { ModelSettings } from './admin';

describe('administrator safeguards', () => {
  it('does not offer retries for publication or uncertain submissions', () => {
    expect(canRetryManagedJob({ kind: 'publish', state: 'failed' })).toBe(false);
    expect(canRetryManagedJob({ kind: 'image_rewrite', state: 'submission_unknown' })).toBe(false);
    expect(canRetryManagedJob({ kind: 'video_poll', state: 'failed' })).toBe(false);
    expect(canRetryManagedJob({ kind: 'rewrite', state: 'waiting_config' })).toBe(true);
    expect(canCancelManagedJob({ kind: 'publish', state: 'queued' })).toBe(false);
    expect(canCancelManagedJob({ kind: 'rewrite', state: 'running' })).toBe(false);
    expect(canCancelManagedJob({ kind: 'rewrite', state: 'queued', phase: 'generating' })).toBe(false);
    expect(canRetryManagedJob({ kind: 'video_rewrite', state: 'failed' })).toBe(false);
    expect(canRetryManagedJob({ kind: 'future_unknown_kind', state: 'failed' })).toBe(false);
    expect(canCancelManagedJob({ kind: 'rewrite', state: 'queued' })).toBe(true);
  });
  it('preserves saved keys when inputs are empty and sends only changed settings', () => {
    const config = { collector_url: 'http://localhost:5556', ffmpeg_path: '', ffprobe_path: '', node_path: '',
      providers: { text: { base_url: 'https://provider.example/v1', model: 'model-a', configured: true, key_mask: '***1234', workspace_id: 'team-a' } } } as ModelSettings;
    expect(modelSettingsPatch(config, config, { text: '' })).toEqual({});
    const next = { ...config, providers: { ...config.providers, text: { ...config.providers.text, model: 'model-b' } } };
    expect(modelSettingsPatch(next, config, {})).toEqual({ providers: { text: { model: 'model-b', workspace_id: 'team-a' } } });
    expect(modelSettingsPatch(config, config, { text: '__CLEAR__' })).toEqual({ providers: { text: { api_key: '__CLEAR__', workspace_id: 'team-a' } } });
    expect(JSON.stringify(modelSettingsPatch(next, config, {}))).not.toContain('key_mask');
  });
});
