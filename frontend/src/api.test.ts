import { afterEach, describe, expect, it, vi } from 'vitest';

afterEach(() => { vi.unstubAllGlobals(); vi.unstubAllEnvs(); vi.restoreAllMocks(); vi.resetModules(); });

describe('HTTP boundary', () => {
  it('obtains CSRF once and includes it in every mutation, with same-origin credentials', async () => {
    const calls: { url: string; init?: RequestInit }[] = [];
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      calls.push({ url, init });
      return Response.json(url.endsWith('/session') ? { csrf_token: 'session-test-token' } : { job_id: 'job-1' });
    }));
    const { api } = await import('./api');
    await api('/collections', 'POST', { text: 'https://www.xiaohongshu.com/explore/fixture' });
    await api('/settings', 'PATCH', { collector_url: 'http://127.0.0.1:5556' });
    expect(calls.map(c => c.url)).toEqual(['/api/v1/session', '/api/v1/collections', '/api/v1/settings']);
    expect(calls[1].init?.headers).toMatchObject({ 'X-CSRF-Token': 'session-test-token', 'Content-Type': 'application/json' });
    expect(calls[2].init?.credentials).toBe('same-origin');
  });

  it('does not submit an operation when session setup fails', async () => {
    const fetcher = vi.fn(async () => Response.json({ error: 'unavailable' }, { status: 503 }));
    vi.stubGlobal('fetch', fetcher);
    const { api } = await import('./api');
    await expect(api('/video-projects/v1/generate', 'POST', {})).rejects.toThrow('无法连接工作台服务');
    expect(fetcher).toHaveBeenCalledTimes(1);
  });

  it('uses the independently configured API for session, writes and media', async () => {
    vi.stubEnv('VITE_API_BASE_URL', 'http://127.0.0.1:8082/api/v1/');
    const calls: { url: string; init?: RequestInit }[] = [];
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      calls.push({ url, init }); return Response.json(url.endsWith('/session') ? { csrf_token: 'cross-origin-token' } : {});
    }));
    const { api, apiURL, assetURL, upload } = await import('./api');
    await api('/rewrites', 'POST', { blank: true });
    await upload(new File(['image'], 'test.png', { type: 'image/png' }), 'project');
    expect(calls.map(call => call.url)).toEqual(['http://127.0.0.1:8082/api/v1/session', 'http://127.0.0.1:8082/api/v1/rewrites', 'http://127.0.0.1:8082/api/v1/assets']);
    expect(calls[2].init?.headers).toMatchObject({ 'X-CSRF-Token': 'cross-origin-token' });
    expect(assetURL('image 1', 'xiaohongshu')).toBe('http://127.0.0.1:8082/api/v1/assets/image%201/file?platform=xiaohongshu');
    expect(apiURL('/style-library/cover/file')).toBe('http://127.0.0.1:8082/api/v1/style-library/cover/file');
  });

  it('surfaces not-configured errors without converting them to success', async () => {
    vi.stubGlobal('fetch', vi.fn(async (url: string) => url.endsWith('/session')
      ? Response.json({ csrf_token: 'test' }) : Response.json({ error: '请配置文字模型 API Key', code: 'not_configured' }, { status: 409 })));
    const { api, ApiError } = await import('./api');
    const failure = await api('/rewrites/r1/generate', 'POST', {}).catch(e => e);
    expect(failure).toBeInstanceOf(ApiError);
    if (!(failure instanceof ApiError)) throw new Error('Expected API error');
    expect(failure.code).toBe('not_configured'); expect(failure.message).toBe('请配置文字模型 API Key');
  });

  it('includes CSRF on state-changing publication exports and downloads a blob', async () => {
    const click = vi.fn(); const anchor = { click, href: '', download: '' };
    vi.stubGlobal('document', { createElement: () => anchor });
    vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:test');
    vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => undefined);
    vi.stubGlobal('setTimeout', vi.fn());
    const fetcher = vi.fn(async (url: string, _init?: RequestInit) => url.endsWith('/session') ? Response.json({ csrf_token: 'test-export' })
      : new Response('ZIP', { headers: { 'Content-Disposition': "attachment; filename*=UTF-8''%E5%8F%91%E5%B8%83.zip" } }));
    vi.stubGlobal('fetch', fetcher);
    const { download } = await import('./api');
    await download('/publications/p1/export', 'fallback.zip', true);
    expect(fetcher.mock.calls[1][1]).toMatchObject({ headers: { 'X-CSRF-Token': 'test-export' } });
    expect(anchor.download).toBe('发布.zip'); expect(click).toHaveBeenCalledOnce();
  });
});
