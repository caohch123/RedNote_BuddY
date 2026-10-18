import type { Capability, ProviderConfig } from './types';

export interface ManagedAccount {
  id: string; username: string; display_name: string; role: 'admin' | 'member'; enabled: boolean;
  must_change_password: boolean; created_at: string; daily_request_limit: number; max_concurrent: number; daily_requests?: number;
}
export interface AdminOverview {
  users: number; enabled_users: number; requests: number; tokens: number; estimated_cny: number;
  pending_jobs: number; failed_jobs: number; unreported: number;
  per_user: { user_id: string; username: string; display_name: string; requests: number; tokens: number;
    estimated_cny: number; daily_requests: number; daily_request_limit: number }[];
}
export interface AdminJob {
  id: string; kind: string; state: string; phase: string; progress: number; message?: string; error?: string;
  created_at: string; updated_at: string; attempts: number; user_id: string; username: string;
}
export interface AuditEntry { id: string; created_at: string; actor_name: string; action: string; target: string; result: string }
export interface ModelSettings {
  providers: Record<Capability, ProviderConfig>;
  collector_url: string; ffmpeg_path: string; ffprobe_path: string; node_path: string;
}
export const modelCapabilities: { key: Capability; label: string; detail: string }[] = [
  { key: 'text', label: '文案与提示词', detail: '文案仿写、创作规划和提示词生成' },
  { key: 'vision', label: '视觉分析', detail: '图片特征和视频关键帧理解' },
  { key: 'image', label: '图片生成', detail: '图片创作与图片仿写' },
  { key: 'video', label: '视频生成', detail: '视频创作与视频仿写' },
  { key: 'ocr', label: '文字识别', detail: '提取参考图片中的文字' },
  { key: 'asr', label: '音频转写', detail: '将视频与音频内容转为文字' },
];
export const jobNames: Record<string, string> = {
  rewrite: '文案仿写', download: '素材下载', collect: '笔记采集', ocr: '文字识别', asr: '音频转写',
  image_analyze: '图片分析', image_compose: '图片提示词', image_rewrite: '图片仿写', video_script: '视频提示词',
  video_rewrite: '视频仿写', publish: '笔记发布', video_poll: '查询视频结果',
};

export function canRetryManagedJob(job: Pick<AdminJob, 'kind' | 'state'>) {
  return ['failed', 'waiting_config'].includes(job.state) && ['collect', 'ocr', 'asr', 'rewrite'].includes(job.kind);
}
export function canCancelManagedJob(job: Pick<AdminJob, 'kind' | 'state'> & { phase?: string }) {
  return job.state === 'queued' && (!job.phase || job.phase === 'queued') && ['collect', 'ocr', 'asr', 'rewrite', 'video_script', 'video_rewrite', 'image_analyze', 'image_rewrite', 'image_compose'].includes(job.kind);
}
export function modelSettingsPatch(current: ModelSettings, previous: ModelSettings, keys: Partial<Record<Capability, string>>) {
  const changes: Record<string, unknown> = {};
  for (const field of ['collector_url', 'ffmpeg_path', 'ffprobe_path', 'node_path'] as const) {
    if (current[field] !== previous[field]) changes[field] = current[field];
  }
  const providers: Partial<Record<Capability, Partial<ProviderConfig>>> = {};
  for (const { key } of modelCapabilities) {
    const patch: Partial<ProviderConfig> = {};
    for (const field of ['base_url', 'model', 'workspace_id'] as const) {
      if ((current.providers[key]?.[field] || '') !== (previous.providers[key]?.[field] || '')) patch[field] = current.providers[key]?.[field] || '';
    }
    if (keys[key]) patch.api_key = keys[key];
    if (Object.keys(patch).length) {
      patch.workspace_id = current.providers[key]?.workspace_id || '';
      providers[key] = patch;
    }
  }
  if (Object.keys(providers).length) changes.providers = providers;
  return changes;
}

export const auditActions: Record<string, string> = {
  'auth.login': '登录', 'auth.login_failed': '登录失败', 'auth.logout': '退出登录', 'auth.password': '修改密码',
  'user.create': '创建账号', 'user.update': '更新账号', 'user.reset_password': '重置密码', 'models.update': '更新模型配置',
  'job.retry': '重试任务', 'job.cancel': '取消任务', 'billing.prices_update': '更新计费单价',
};
