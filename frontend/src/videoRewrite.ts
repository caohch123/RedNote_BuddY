import type { Asset, Draft, Version, VideoGenerationSnapshot, VideoProject, VideoPurpose, VideoRewriteState } from './types';
import { applyImageToDraft } from './utils';

export type VideoRewriteForm = Pick<VideoRewriteState, 'primary_asset_id' | 'reference_video_id' | 'reference_image_id' | 'purpose_sources' | 'requirements' | 'output' | 'prompt'>;
export const videoPurposes: { key: VideoPurpose; label: string }[] = [
  { key: 'content', label: '内容结构' }, { key: 'subject', label: '主体形象' }, { key: 'action', label: '动作' },
  { key: 'camera', label: '运镜节奏' }, { key: 'style', label: '视觉风格' }, { key: 'audio', label: '声音氛围' },
];
// Source lineage is immutable even when users replace or remove the current main video.
export function sortVideoRewriteProjects(projects: VideoProject[], sourceAssets: Pick<Asset, 'id' | 'position'>[]): VideoProject[] {
  const positions = new Map(sourceAssets.map(asset => [asset.id, asset.position]));
  return [...projects].sort((left, right) => {
    const leftSource = left.video_rewrite?.initial_source_asset_id || '';
    const rightSource = right.video_rewrite?.initial_source_asset_id || '';
    if (!!leftSource !== !!rightSource) return leftSource ? -1 : 1;
    const leftPosition = positions.get(leftSource) ?? Number.POSITIVE_INFINITY;
    const rightPosition = positions.get(rightSource) ?? Number.POSITIVE_INFINITY;
    if (leftPosition !== rightPosition) return leftPosition - rightPosition;
    return left.created_at.localeCompare(right.created_at) || left.id.localeCompare(right.id);
  });
}
export function videoForm(state?: VideoRewriteState): VideoRewriteForm {
  return { primary_asset_id: state?.primary_asset_id || '', reference_video_id: state?.reference_video_id || '',
    reference_image_id: state?.reference_image_id || '', purpose_sources: state?.purpose_sources || {},
    requirements: state?.requirements || '', output: state?.output || { duration: 10, resolution: '720p', ratio: '9:16', generate_audio: true },
    prompt: state?.prompt || '' };
}
// Editing the final prompt keeps its input binding; material/output edits stale it.
export function videoInputSignature(form: VideoRewriteForm) {
  const ids = [form.primary_asset_id, form.reference_video_id, form.reference_image_id].filter(Boolean);
  return JSON.stringify([ids, Object.entries(form.purpose_sources).filter(([, id]) => !!id).sort(), form.requirements.trim(), form.output]);
}
export function videoSnapshotLabels(snapshot: Pick<VideoGenerationSnapshot, 'input_asset_ids' | 'video_urls' | 'provider'>) {
  const labels: Record<string, string> = {}; let videos = 0; let images = 0;
  for (const id of snapshot.input_asset_ids) labels[id] = Object.hasOwn(snapshot.video_urls || {}, id) ? `${snapshot.provider === 'bailian_wan3' ? '' : '@'}视频${++videos}` : `${snapshot.provider === 'bailian_wan3' ? '图' : '@图像'}${++images}`;
  return labels;
}
export function replaceVideoMaterial(form: VideoRewriteForm, slot: 'primary_asset_id' | 'reference_video_id' | 'reference_image_id', id: string): VideoRewriteForm {
  const previous = form[slot]; if (previous === id) return form;
  const purpose_sources = { ...form.purpose_sources };
  for (const purpose of videoPurposes) if (purpose_sources[purpose.key] === previous) delete purpose_sources[purpose.key];
  if (slot === 'primary_asset_id' && id) for (const key of ['content', 'subject', 'action'] as const) if (!purpose_sources[key]) purpose_sources[key] = id;
  return { ...form, [slot]: id, purpose_sources };
}
export function applyVideoToDraft(draft: Draft, previous: Version, next: Version, sources: string[], assetID: string): Draft {
  const merged = applyImageToDraft(draft, previous, next, sources, assetID);
  return sources.length ? merged : { ...merged, asset_ids: [...new Set([...merged.asset_ids, assetID])] };
}

export function videoTiming(form: VideoRewriteForm, assets: Pick<Asset, 'id' | 'duration'>[]) {
  const selected = new Set(Object.values(form.purpose_sources).filter(Boolean));
  const videos = [form.primary_asset_id, form.reference_video_id].filter(id => !!id && selected.has(id));
  const durations = [...new Set(videos)].map(id => assets.find(asset => asset.id === id)?.duration || 0);
  const inputDuration = durations.reduce((sum, duration) => sum + duration, 0);
  const maxDuration = Math.min(30, Math.floor(30 - inputDuration + 0.001));
  const error = durations.some(duration => duration > 15.001) ? '百炼参考视频单条不能超过 15 秒，请更换短视频'
    : inputDuration > 15.001 ? '参与生成的参考视频合计不能超过 15 秒'
    : form.output.duration > maxDuration ? `当前参考视频合计 ${inputDuration.toFixed(1)} 秒，输出视频最多 ${maxDuration} 秒` : '';
  return { inputDuration, maxDuration, error };
}
