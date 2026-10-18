import { memberAuthResponse } from './member-auth';
import { expect, test, type Page } from '@playwright/test';

async function fixtures(page: Page) {
  const note = { id: 'n1', platform: 'xhs', note_id: 'fixture-note', url: 'https://www.xiaohongshu.com/explore/fixture-note',
    title: '接口测试笔记', body: '用于自动化测试的原始正文。', topics: ['测试'], author: { id: 'a1', name: '测试博主' },
    type: 'normal', likes: 0, collects: null, comments: 3, collected_at: '2026-10-06T14:00:00Z', published_at: '',
    tags: [], status: 'succeeded', metadata_status: 'succeeded', media_status: 'succeeded', extraction_status: 'skipped', asset_ids: [] };
  let notes: typeof note[] = []; let rewrites: any[] = []; let publications: any[] = []; let jobs: any[] = [];
  const mutations: { path: string; body: any; csrf: string | undefined }[] = [];
  await page.route('**/api/v1/**', async route => {
    const authResponse = memberAuthResponse(route); if (authResponse) return authResponse;
    const req = route.request(); const path = new URL(req.url()).pathname.replace('/api/v1', ''); const method = req.method();
    const body = req.postDataJSON(); const csrf = req.headers()['x-csrf-token'];
    if (method !== 'GET' || path.endsWith('/export')) mutations.push({ path, body, csrf });
    const json = (data: unknown, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(data) });
    if (path === '/workspaces') return json({ items: [{ id: 'xiaohongshu', platform: 'xiaohongshu', name: '小红书' }], total: 1 });
    if (path === '/session') return json({ csrf_token: 'csrf-browser-fixture' });
    if (method !== 'GET' && csrf !== 'csrf-browser-fixture') return json({ error: '缺少 CSRF' }, 403);
    if (path === '/jobs') return json({ items: jobs, total: jobs.length });
    if (path === '/assets') return json({ items: [], total: 0 });
    if (path === '/health') return json({ status: 'degraded', dependencies: [{ name: 'FFmpeg', ok: true, path: 'ffmpeg' }] });
    if (path === '/collections' && method === 'POST') {
      if (!body.text.includes('xiaohongshu.com')) return json({ error: '未找到有效的小红书链接' }, 400);
      notes = [note]; jobs = [{ id: 'j1', kind: 'collect', state: 'succeeded', progress: 100, phase: 'metadata', created_at: '2026-10-06T14:00:00Z' }];
      return json({ job_id: 'j1' }, 202);
    }
    if (path === '/collections') return json({ items: notes, total: notes.length });
    if (path === '/collections/n1') return json({ note, assets: [] });
    if (path === '/rewrites' && method === 'POST') {
      const rewrite = { id: 'r1', note_id: 'n1', title: note.title, versions: [], confirmed_version_id: '',
        created_at: '2026-10-06T14:00:00Z', updated_at: '2026-10-06T14:00:00Z', options: { audience: '普通读者', tone: '自然分享', length: '中等（约 500 字）', instructions: '', include_body: true, extraction_asset_ids: [] } };
      rewrites = [rewrite]; return json(rewrite, 201);
    }
    if (path === '/rewrites') return json({ items: rewrites, total: rewrites.length });
    if (path === '/rewrites/r1') return json({ rewrite: rewrites[0], note, assets: [], videos: [] });
    if (path === '/rewrites/r1/generate') return json({ error: '请配置文字模型 API Key', code: 'not_configured' }, 409);
    if (path === '/rewrites/r1/versions') {
      const v = { ...body, id: `v${rewrites[0].versions.length + 1}`, number: rewrites[0].versions.length + 1,
        confirmed: false, created_at: '2026-10-06T14:01:00Z' };
      rewrites[0].versions.push(v); return json(v, 201);
    }
    if (path === '/rewrites/r1/confirm') { rewrites[0].confirmed_version_id = body.version_id; return json(rewrites[0]); }
    if (path === '/publications' && method === 'POST') {
      const v = rewrites[0].versions.find((v: any) => v.id === body.version_id);
      const p = { ...v, id: 'p1', rewrite_id: body.rewrite_id, version_id: body.version_id, status: 'ready' };
      publications.push(p); return json(p, 201);
    }
    if (path === '/publications') return json({ items: publications, total: publications.length });
    if (path === '/publications/p1/export') {
      if (csrf !== 'csrf-browser-fixture') return json({ error: '缺少 CSRF' }, 403);
      publications[0].status = 'exported';
      return route.fulfill({ contentType: 'application/zip', headers: { 'Content-Disposition': 'attachment; filename="fixture.zip"' }, body: Buffer.from('PK-test') });
    }
    if (path === '/publications/p1' && method === 'PATCH') { Object.assign(publications[0], body); return json(publications[0]); }
    if (path === '/publications/p1') return json(publications[0]);
    if (path === '/settings') return json({ collector_url: 'http://127.0.0.1:5556', cookie_configured: false,
      ffmpeg_path: 'ffmpeg', ffprobe_path: 'ffprobe', node_path: 'node',
      providers: Object.fromEntries(['text', 'ocr', 'asr', 'video'].map(key => [key, { base_url: 'https://example.com', model: key === 'video' ? 'doubao-seedance-2-5-260628' : 'test-model', configured: false }])) });
    if (path === '/settings/test') return json({ ok: false, message: '模型未配置密钥' });
    return json({ error: `未模拟接口 ${method} ${path}` }, 404);
  });
  return { mutations, publications: () => publications, rewrites: () => rewrites };
}

test('collect → manually rewrite → confirm → export → record actual publication', async ({ page }) => {
  const state = await fixtures(page); const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.goto('/');
  await expect(page.getByText('粘贴第一条链接，开始建立你的素材库')).toBeVisible();
  await page.getByRole('textbox', { name: '小红书链接' }).fill('分享文字 https://www.xiaohongshu.com/explore/fixture-note');
  await page.getByRole('button', { name: '开始采集' }).click();
  await expect(page.getByRole('button', { name: '接口测试笔记' })).toBeVisible();
  await page.getByRole('button', { name: '加入仿写库' }).click();
  await expect(page.getByRole('textbox', { name: '仿写标题' })).toHaveValue('接口测试笔记', { timeout: 15000 });
  await page.getByRole('button', { name: 'AI 生成新版本' }).click();
  await expect(page.getByText('请配置文字模型 API Key')).toBeVisible();
  expect(state.rewrites()[0].versions).toHaveLength(0);
  await page.getByRole('textbox', { name: '仿写标题' }).fill('我的发布标题');
  await page.getByRole('textbox', { name: '仿写正文' }).fill('人工整理并确认的发布正文。');
  await page.getByRole('button', { name: '保存新版本' }).click();
  await expect(page.getByRole('button', { name: '确认此版本' })).toBeEnabled();
  await page.getByRole('button', { name: '确认此版本' }).click();
  await page.getByRole('tab', { name: /发布素材/ }).click();
  await expect(page.getByRole('button', { name: '创建发布稿' })).toBeEnabled();
  await page.getByRole('button', { name: '创建发布稿' }).click();
  await page.getByRole('dialog').getByRole('button', { name: '创建发布稿' }).click();
  await expect(page.getByRole('heading', { name: '我的发布标题' })).toBeVisible();
  const downloadEvent = page.waitForEvent('download');
  await page.getByRole('dialog').getByRole('button', { name: '导出发布包' }).click();
  expect((await downloadEvent).suggestedFilename()).toBe('fixture.zip');
  await expect(page.getByRole('dialog').getByText('已导出', { exact: true })).toBeVisible();
  expect(state.publications()[0].status).toBe('exported');
  expect(state.publications()[0].published_url).toBeUndefined();
  await page.getByRole('dialog').getByRole('button', { name: '记录已发布' }).click();
  const modal = page.getByRole('dialog', { name: '记录发布结果' });
  await modal.getByRole('textbox', { name: '已发布作品链接' }).fill('https://www.xiaohongshu.com/explore/published-fixture');
  await modal.getByRole('button', { name: '保存发布记录' }).click();
  await expect(page.getByRole('dialog').getByText('已发布', { exact: true })).toBeVisible();
  expect(state.publications()[0].published_url).toContain('published-fixture');
  expect(state.mutations.every(m => m.csrf === 'csrf-browser-fixture')).toBe(true);
  expect(errors).toEqual([]);
  await page.screenshot({ path: 'test-results/publication-workflow.png', fullPage: true });
});

test('invalid collection input remains editable and settings contain no stored credentials', async ({ page }) => {
  await fixtures(page); await page.goto('/');
  await page.getByRole('textbox', { name: '小红书链接' }).fill('不是有效链接');
  await page.getByRole('button', { name: '开始采集' }).click();
  await expect(page.getByText('未找到有效的小红书链接')).toBeVisible();
  await expect(page.getByRole('textbox', { name: '小红书链接' })).toHaveValue('不是有效链接');
  await page.getByRole('menuitem', { name: '设置' }).click();
  await page.getByRole('tab', { name: '共用 AI 服务' }).click();
  await expect(page.getByRole('textbox', { name: '图片生成与仿写 模型', exact: true })).toHaveValue('');
  await expect(page.getByRole('textbox', { name: 'Seedance 2.5 模型' })).not.toBeVisible();
  await page.getByLabel('仿写文案 API Key', { exact: true }).fill('temporary-test-key');
  expect(await page.evaluate(() => JSON.stringify(localStorage))).not.toContain('temporary-test-key');
  expect(await page.evaluate(() => JSON.stringify(sessionStorage))).not.toContain('temporary-test-key');
  await expect(page.getByRole('button', { name: '检测连接' }).first()).toBeDisabled();
  await page.screenshot({ path: 'test-results/settings.png', fullPage: true });
});
