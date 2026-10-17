import type { Draft, Publication, Version } from './types';

export const characterCount = (value: string) => Array.from(value).length;
export const truncateCharacters = (value: string, max: number) => Array.from(value).slice(0, max).join('');

export const statusLabels: Record<string, string> = {
  queued: '排队中', running: '处理中', pending: '待处理', succeeded: '已完成', success: '已完成',
  ready: '已就绪', rendered: '已合成', collecting: '采集中', waiting_config: '待配置', completed: '已完成', failed: '失败', partial: '部分完成', downloading: '下载中',
  blocked: '待配置', not_configured: '待配置', paused: '已暂停', preparing: '待准备',
  exported: '已导出', published: '已发布', draft: '草稿', stale: '需重新制作',
  missing: '未就绪', unavailable: '不可用', skipped: '未提取', unknown: '结果待核实',
  submission_unknown: '提交结果待核实', awaiting_confirmation: '等待确认', processing: '处理中',
  preparing_publish: '准备发布中', submitted: '已提交', reviewing: '审核中', rejected: '审核未通过', verification_required: '需本人验证',
};
export function formatDate(value?: string) {
  if (!value) return '—'; const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit',
    timeZone: 'Asia/Shanghai', hour12: false,
  }).format(date);
}
export function number(value: number | null | undefined) {
  return value === null || value === undefined ? '—' : value.toLocaleString('zh-CN');
}
export function ratio(a: number | null | undefined, b: number | null | undefined) {
  return a == null || b == null || b === 0 ? '—' : (a / b).toFixed(2);
}
export function toDraft(version?: Version): Draft {
  return { title: version?.title || '', body: version?.body || '', topics: version?.topics || [],
    script: version?.script || '', asset_ids: [...new Set(version?.asset_ids || [])] };
}
// A media operation advances the saved snapshot without discarding edits in other modules.
export function applyImageToDraft(draft: Draft, previous: Version, next: Version, sources: string[], assetID: string): Draft {
  const before = toDraft(previous); const saved = toDraft(next);
  const ids: string[] = []; let inserted = false;
  for (const id of draft.asset_ids) {
    if (sources.includes(id) || id === assetID) {
      if (!inserted) { ids.push(assetID); inserted = true; }
    } else ids.push(id);
  }
  return { ...saved,
    title: draft.title === before.title ? saved.title : draft.title,
    body: draft.body === before.body ? saved.body : draft.body,
    topics: JSON.stringify(draft.topics) === JSON.stringify(before.topics) ? saved.topics : draft.topics,
    script: draft.script === before.script ? saved.script : draft.script,
    asset_ids: [...new Set(ids)],
  };
}
export function fullCopy(item: Pick<Publication, 'title' | 'body' | 'topics'>) {
  const topics = (item.topics || []).map(t => t.startsWith('#') ? t : `#${t}`).join(' ');
  return [item.title, item.body, topics].filter(Boolean).join('\n\n');
}
export function move<T>(items: T[], index: number, direction: -1 | 1) {
  const next = [...items]; const target = index + direction;
  if (target < 0 || target >= items.length) return next;
  [next[index], next[target]] = [next[target], next[index]]; return next;
}
