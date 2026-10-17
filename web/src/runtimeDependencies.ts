import type { Settings } from './types';

export type ToolField = 'ffmpeg_path' | 'ffprobe_path' | 'node_path';
export interface Dependency { name: string; ok: boolean; path?: string; message?: string; version?: string }
export function healthRows(report: unknown): Dependency[] {
  if (!report || typeof report !== 'object') return [];
  const raw = report as Record<string, unknown>;
  const data = raw.dependencies || raw.checks || raw;
  if (Array.isArray(data)) return data.map(d => ({ ...d, name: d.name || d.key, ok: d.ok === true || d.available === true }));
  if (typeof data === 'object' && data) return Object.entries(data).filter(([key]) => !['status', 'ok', 'version'].includes(key)).map(([name, v]) => {
    if (typeof v === 'boolean') return { name, ok: v };
    if (typeof v === 'string') return { name, ok: false, message: v };
    const info = v as Record<string, unknown> | null;
    return { name, ok: info?.ok === true || info?.available === true, path: info?.path as string, message: (info?.message || info?.error) as string, version: info?.version as string };
  });
  return [];
}

const tools: { name: string; label: string; usage: string; field?: ToolField }[] = [
  { name: 'ffmpeg', label: 'FFmpeg', usage: '视频抽帧、音轨提取与播放副本', field: 'ffmpeg_path' },
  { name: 'ffprobe', label: 'FFprobe', usage: '检查视频格式、尺寸、时长与音轨', field: 'ffprobe_path' },
  { name: 'node', label: 'Node.js', usage: '小红书发布签名', field: 'node_path' },
];

export function runtimeDependencyRows(report: unknown) {
  const checks = healthRows(report);
  const definitions = [...tools,
    { name: 'collector', label: '采集服务', usage: '下载小红书笔记和原始素材' },
    { name: 'database', label: '本地数据库', usage: '保存项目、设置与任务记录' }];
  return definitions.map(item => ({ ...item, ...checks.find(check => check.name === item.name), checked: checks.some(check => check.name === item.name) }));
}

export function runtimePathChanged(field: ToolField, draft: Settings, saved?: Settings) {
  return !!saved && (draft[field] || '') !== (saved[field] || '');
}
