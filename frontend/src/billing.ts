export const billingCapabilities: Record<string, string> = {
  text: '文案与提示词', vision: '图片与视频分析', ocr: '图片文字识别', asr: '音频转写', image: '图片生成', video: '视频生成',
};
export const billingOperations: Record<string, string> = {
  rewrite: '文案仿写', image_analyze: '图片分析', image_compose: '图片提示词', image_rewrite: '图片仿写',
  video_script: '视频提示词', video_rewrite: '视频仿写', ocr: '图片文字识别', asr: '视频转写', collect: '采集与解析',
};
export type BillingPeriod = 'today' | '7d' | '30d' | 'all';
export interface UsageTotals {
  requests: number; reported: number; missing: number; failed: number; pending: number; unknown: number;
  input_tokens: number; output_tokens: number; total_tokens: number; cached_tokens: number; reasoning_tokens: number;
  input_reported: number; output_reported: number; estimated_cny: number; priced: number; images: number; video_seconds: number;
  cached_reported: number; reasoning_reported: number;
}
export interface UsageGroup extends UsageTotals { key: string }
export interface TokenPrice {
  provider: string; model: string; input_per_million: number; output_per_million: number; cached_per_million?: number;
}
export interface UsageRecord {
  id: string; created_at: string; capability: string; operation: string; provider: string; model: string;
  user_id?: string; username?: string;
  credential_id: string; job_id?: string; entity_id?: string; status: string;
  input_tokens: number | null; output_tokens: number | null; total_tokens: number | null;
  cached_tokens: number | null; reasoning_tokens: number | null; estimated_cny: number | null;
  image_count?: number; video_seconds?: number; price?: TokenPrice;
}
export interface BillingData {
  summary: UsageTotals; timeline: UsageGroup[]; models: UsageGroup[]; capabilities: UsageGroup[];
  items: UsageRecord[]; total: number; page: number; page_size: number; started_at: string;
  prices: TokenPrice[]; model_options: { model: string; provider: string }[]; hourly: boolean;
  user_options?: { id: string; username: string; display_name: string }[]; users?: UsageGroup[];
}
export const tokenNumber = (value: number | null | undefined) => value == null ? '—' : new Intl.NumberFormat('zh-CN').format(value);
export const costNumber = (value: number) => value > 0 && value < .000001 ? '¥<0.000001' : `¥${value.toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 6 })}`;

export function billingQuery(period: BillingPeriod, model: string, capability: string, page = 1, now = new Date(), userID = '') {
  const query = new URLSearchParams({ timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC', page: String(page) });
  if (period !== 'all') {
    const start = new Date(now); start.setHours(0, 0, 0, 0);
    start.setDate(start.getDate() - (period === '7d' ? 6 : period === '30d' ? 29 : 0));
    const end = new Date(now); end.setHours(0, 0, 0, 0); end.setDate(end.getDate() + 1);
    query.set('from', start.toISOString()); query.set('to', end.toISOString());
  }
  if (model) query.set('model', model);
  if (capability) query.set('capability', capability);
  if (userID) query.set('user_id', userID);
  return query.toString();
}

export interface TrendPoint { key: string; label: string; input: number; output: number; other: number; total: number }
export function trendPoints(rows: UsageGroup[], period: BillingPeriod, now = new Date()): TrendPoint[] {
  const start = new Date(now); start.setHours(0, 0, 0, 0);
  const end = new Date(start); end.setDate(end.getDate() + 1);
  if (period === 'all') {
    if (!rows.length) return [];
    start.setTime(new Date(rows[0].key + 'T00:00:00').getTime());
    end.setTime(new Date(rows[rows.length - 1].key + 'T00:00:00').getTime()); end.setDate(end.getDate() + 1);
  } else start.setDate(start.getDate() - (period === '7d' ? 6 : period === '30d' ? 29 : 0));
  const points: TrendPoint[] = [];
  const dateKey = (date: Date) => `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`;
  const dayIndexes = new Map<string, number>();
  if (period === 'today') {
    for (let time = start.getTime(); time < end.getTime(); time += 3600000) {
      const date = new Date(time);
      points.push({ key: date.toISOString(), label: `${String(date.getHours()).padStart(2, '0')}:00`, input: 0, output: 0, other: 0, total: 0 });
    }
  } else {
    const days: Date[] = [];
    for (const date = new Date(start); date < end; date.setDate(date.getDate() + 1)) days.push(new Date(date));
    const stride = Math.max(1, Math.ceil(days.length / 30));
    for (let i = 0; i < days.length; i += stride) {
      const first = days[i], last = days[Math.min(i + stride - 1, days.length - 1)];
      const label = `${first.getMonth() + 1}/${first.getDate()}${stride > 1 ? `–${last.getMonth() + 1}/${last.getDate()}` : ''}`;
      for (let j = i; j < Math.min(i + stride, days.length); j++) dayIndexes.set(dateKey(days[j]), points.length);
      points.push({ key: first.toISOString(), label, input: 0, output: 0, other: 0, total: 0 });
    }
  }
  for (const row of rows) {
    const index = period === 'today' ? Math.floor((new Date(row.key).getTime() - start.getTime()) / 3600000) : dayIndexes.get(row.key);
    const point = index == null ? undefined : points[index];
    if (!point) continue;
    point.input += row.input_tokens; point.output += row.output_tokens;
    point.other += Math.max(0, row.total_tokens - row.input_tokens - row.output_tokens);
    point.total += row.total_tokens;
  }
  return points;
}
