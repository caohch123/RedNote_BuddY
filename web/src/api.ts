import type { AsyncResult, Platform } from './types';
import { workspacePath } from './platform';

const base = '/api/v1';
let token: Promise<string> | undefined;

export class ApiError extends Error {
  status: number;
  code: string;
  constructor(message: string, status: number, code = '') {
    super(message); this.name = 'ApiError'; this.status = status; this.code = code;
  }
}

async function csrf(): Promise<string> {
  if (!token) token = fetch(`${base}/session`, { credentials: 'same-origin' })
    .then(async r => {
      if (!r.ok) throw new Error('无法连接本地服务，请检查工作台是否已启动');
      const data = await r.json();
      if (!data.csrf_token) throw new Error('会话初始化失败');
      return data.csrf_token as string;
    }).catch(e => { token = undefined; throw e; });
  return token;
}

async function checked(response: Response) {
  if (!response.ok) {
    const data = await response.json().catch(() => ({}));
    if (response.status === 403) token = undefined;
    throw new ApiError(data.error || `请求失败 (${response.status})`, response.status, data.code);
  }
  return response;
}

export async function api<T>(path: string, method = 'GET', data?: unknown): Promise<T> {
  const headers: Record<string, string> = {};
  if (method !== 'GET') headers['X-CSRF-Token'] = await csrf();
  if (data !== undefined) headers['Content-Type'] = 'application/json';
  let response: Response;
  try { response = await fetch(`${base}${path}`, {
    method, headers, credentials: 'same-origin', body: data === undefined ? undefined : JSON.stringify(data),
  }); } catch { throw new Error('无法连接本地服务，请检查工作台是否已启动'); }
  await checked(response);
  if (response.status === 204) return undefined as T;
  return response.json();
}

export async function upload(file: File, ownerID: string, path = '/assets') {
  const data = new FormData(); data.append('file', file); data.append('owner_id', ownerID);
  const r = await fetch(`${base}${path}`, {
    method: 'POST', headers: { 'X-CSRF-Token': await csrf() }, body: data, credentials: 'same-origin',
  });
  await checked(r); return r.json();
}

export function assetURL(id: string, platform?: Platform) {
  const path = `/assets/${encodeURIComponent(id)}/file`;
  return `${base}${platform ? workspacePath(path, platform) : path}`;
}

export async function download(path: string, fallback: string, changesState = false) {
  const headers: Record<string, string> = {};
  if (changesState) headers['X-CSRF-Token'] = await csrf();
  const response = await checked(await fetch(`${base}${path}`, { credentials: 'same-origin', headers }));
  const disposition = response.headers.get('Content-Disposition') || '';
  const encoded = disposition.match(/filename\*=UTF-8''([^;]+)/i);
  const plain = disposition.match(/filename="?([^";]+)"?/i);
  let filename = fallback;
  try { filename = encoded ? decodeURIComponent(encoded[1]) : plain ? plain[1] : fallback; } catch { /* fallback */ }
  const blob = await response.blob(); const url = URL.createObjectURL(blob);
  const a = document.createElement('a'); a.href = url; a.download = filename; a.click();
  setTimeout(() => URL.revokeObjectURL(url), 10_000);
}

export function jobIDs(result: AsyncResult) {
  return result.job_ids?.length ? result.job_ids : result.job_id ? [result.job_id] : [];
}

export function errorText(error: unknown) { return error instanceof Error ? error.message : '操作失败，请稍后重试'; }
