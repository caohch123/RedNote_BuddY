import { memberAuthResponse } from './member-auth';
import { expect, test, type Page } from '@playwright/test';

const names = { xiaohongshu: '小红书' };
const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jxdoAAAAASUVORK5CYII=', 'base64');

async function fixtures(page: Page) {
  const calls: { path: string; platform: string | null; method: string; body: any }[] = [];
  let jobs: any[] = [];
  const data = Object.fromEntries((['xiaohongshu'] as const).map(platform => {
    const note = { id: `n-${platform}`, platform, note_id: `work-${platform}`, title: `${names[platform]}素材`, body: `${names[platform]}原文`,
      url: 'https://www.xiaohongshu.com/explore/123', author: { id: 'author', name: `${names[platform]}作者` },
      type: 'normal', likes: 4, collects: 2, comments: 1, topics: [], tags: ['原标签'], asset_ids: [`a-${platform}`], status: 'ready',
      metadata_status: 'succeeded', media_status: 'succeeded', extraction_status: 'succeeded', collected_at: '2026-10-08T00:00:00Z' };
    const asset = { id: `a-${platform}`, owner_id: note.id, kind: 'image', mime: 'image/png', position: 0, status: 'ready',
      width: 1, height: 1, size: png.length, duration: 0, extraction: { text: `${names[platform]}提取文字`, segments: [], status: 'succeeded', edited: false } };
    const versions = [1, 2].map(number => ({ id: `v-${platform}-${number}`, number, title: `${names[platform]}稿件${number}`, body: `${names[platform]}正文${number}`,
      script: `${names[platform]}口播`, topics: [], asset_ids: [asset.id], confirmed: number === 2, created_at: '2026-10-08T00:00:00Z' }));
    const rewrite = { id: `r-${platform}`, note_id: note.id, title: `${names[platform]}仿写项目`, versions, confirmed_version_id: versions[1].id,
      options: { include_body: true, extraction_asset_ids: [] }, updated_at: '2026-10-08T00:00:00Z' };
    const video = { id: `video-${platform}`, rewrite_id: rewrite.id, version_id: versions[1].id, title: `${names[platform]}视频`, duration: 30,
      revision: 1, status: 'ready', workflow: 'image_voice', renders: [], stale: false, scenes: [1, 2, 3].map(position => ({ id: `scene-${platform}-${position}`, position: position - 1,
        prompt: `${names[platform]}画面${position}`, narration: `${names[platform]}口播${position}`, duration: 10, status: 'ready', voices: [] })) };
    return [platform, { note, asset, rewrite, video }];
  })) as Record<'xiaohongshu', any>;
  let publications: any[] = [];
  await page.route('**/api/v1/**', async route => {
    const authResponse = memberAuthResponse(route); if (authResponse) return authResponse;
    const request = route.request(); const url = new URL(request.url()); const path = url.pathname.replace('/api/v1', '');
    const platform = url.searchParams.get('platform'); const method = request.method();
    const body = request.headers()['content-type']?.includes('application/json') ? request.postDataJSON() : undefined;
    calls.push({ path, platform, method, body });
    const json = (value: unknown, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(value) });
    if (path === '/session') return json({ csrf_token: 'workspace-fixture' });
    if (path === '/workspaces') return json({ items: [{ id: 'xiaohongshu', platform: 'xiaohongshu', name: '小红书工作区' }], total: 1 });
    if (path === '/health') return json({ status: 'ok', dependencies: [] });
    if (path === '/settings') return json({ providers: {}, cookie_configured: false });
    if (path.endsWith('/account')) return json({ status: 'logged_out', dependency_ready: true });
    if (path === '/jobs' && method === 'GET') {
      const items = jobs.filter(job => job.platform === platform);
      return json({ items, total: items.length });
    }
    if (platform !== 'xiaohongshu') return json({ error: '测试要求指定工作区' }, 400);
    const current = data['xiaohongshu'];
    if (method !== 'GET' && request.headers()['x-csrf-token'] !== 'workspace-fixture') return json({ error: '缺少 CSRF' }, 403);
    if (path === '/jobs' && method === 'DELETE') {
      const removed = jobs.filter(job => job.platform === platform);
      jobs = jobs.filter(job => job.platform !== platform);
      return json({ deleted: removed.length, ids: removed.map(job => job.id), interrupted: removed.filter(job => !['succeeded', 'failed', 'cancelled'].includes(job.state)).length, stopping: 0, remote_unstopped: 0 });
    }
    if (path === '/collections' && method === 'POST') {
      if (body.platform !== platform) return json({ error: '采集平台不一致' }, 400);
      if (body.text.includes('douyin.com')) return json({ error: '当前小红书工作区不接收其他平台链接' }, 400);
      return json({ job_id: `collect-${platform}` }, 202);
    }
    if (path === '/collections') return json({ items: [current.note], total: 1 });
    if (path === `/collections/${current.note.id}`) {
      if (method === 'PATCH') Object.assign(current.note, body);
      return json({ note: current.note, assets: [current.asset] });
    }
    if (path === `/collections/${current.note.id}/extract` && method === 'POST') return json({ job_id: `extract-${platform}` }, 202);
    if (path === `/assets/${current.asset.id}/text`) { Object.assign(current.asset.extraction, body, { edited: true }); return json(current.asset); }
    if (path.startsWith('/assets/') && path.endsWith('/file')) return route.fulfill({ status: 200, contentType: 'image/png', body: png });
    if (path === '/assets' && method === 'POST') return json({ ...current.asset, id: `upload-${platform}`, kind: 'video', mime: 'video/mp4' }, 201);
    if (path === '/assets') return json({ items: [current.asset], total: 1 });
    if (path === '/rewrites') return json({ items: [current.rewrite], total: 1 });
    if (path === `/rewrites/${current.rewrite.id}`) return json({ rewrite: current.rewrite, note: current.note, assets: [current.asset], videos: [current.video] });
    if (path === `/video-projects/${current.video.id}`) {
      if (method === 'PATCH') { Object.assign(current.video, body); current.video.revision++; }
      return json(current.video);
    }
    if (path.startsWith(`/video-projects/${current.video.id}/`)) return json({ job_id: `video-job-${platform}` }, 202);
    if (path === '/publications' && method === 'POST') {
      if (body.target_platform !== platform || body.rewrite_id !== current.rewrite.id) return json({ error: '发布工作区不一致' }, 400);
      const version = current.rewrite.versions.find((item: any) => item.id === body.version_id);
      const publication = { ...version, id: `pub-${platform}`, rewrite_id: body.rewrite_id, version_id: body.version_id,
        target_platform: platform, status: 'ready', created_at: '2026-10-08T00:00:00Z' };
      publications.push(publication); return json(publication, 201);
    }
    if (path === '/publications') return json({ items: publications.filter(item => item.target_platform === platform), total: publications.filter(item => item.target_platform === platform).length });
    if (path.startsWith('/publications/')) return json(publications.find(item => item.id === path.split('/')[2] && item.target_platform === platform));
    return json({ error: `未模拟 ${method} ${path}` }, 404);
  });
  return { calls, data, publications: () => publications, setJobs: (items: any[]) => { jobs = items; }, jobs: () => jobs };
}

async function rewrites(page: Page) { await page.getByRole('menuitem', { name: '仿写库' }).click(); }
async function collectionDetail(page: Page) {
  await page.getByRole('button', { name: '更多操作：小红书素材', exact: true }).click();
  await page.getByRole('menuitem', { name: '查看详情', exact: false }).click();
}

test('采集输入、列配置与跨平台链接拒绝', async ({ page }) => {
  const fixture = await fixtures(page); await page.goto('/');
  await page.getByRole('button', { name: '表格视图', exact: true }).click();
  await expect(page.getByRole('textbox', { name: '小红书链接', exact: true })).toBeVisible();
  await expect(page.locator('.collection-table-panel').getByRole('button', { name: '小红书素材', exact: false })).toBeVisible();
  await expect(page.getByRole('combobox', { name: '来源平台筛选' })).toHaveCount(0);
  await expect(page.getByText('小红书图文 / 视频 · 自动保存文字、封面与媒体', { exact: true })).toHaveCount(0);
  await expect(page.locator('.import-card')).toHaveCSS('border-radius', '14px');
  await expect(page.locator('.collection-table-panel')).toHaveCSS('border-radius', '14px');
  const input = page.getByRole('textbox', { name: '小红书链接', exact: true });
  await input.fill('https://www.douyin.com/video/123'); await page.getByRole('button', { name: '开始采集' }).click();
  await expect(page.getByText('当前小红书工作区不接收其他平台链接', { exact: true })).toBeVisible();
  await expect(input).toHaveValue('https://www.douyin.com/video/123');
  await input.fill('小红书未提交的分享文字');
  await page.getByRole('button', { name: '列配置' }).click(); await page.getByRole('checkbox', { name: '分享', exact: true }).check();
  await page.keyboard.press('Escape');
  await expect(page.getByRole('checkbox', { name: '分享', exact: true })).toHaveCount(0);
  await page.getByRole('menuitem', { name: /仿写库/ }).click(); await expect(page.getByPlaceholder('搜索仿写项目')).toBeVisible();
  await page.getByRole('menuitem', { name: /采集库/ }).click();
  await expect(input).toHaveValue('小红书未提交的分享文字');
  await page.getByRole('button', { name: '列配置' }).click();
  await expect(page.getByRole('checkbox', { name: '分享', exact: true })).toBeChecked();
  await page.getByRole('button', { name: '列配置' }).click();
  const submit = fixture.calls.find(call => call.path === '/collections' && call.method === 'POST');
  expect(submit?.platform).toBe('xiaohongshu'); expect(submit?.body.platform).toBe('xiaohongshu');
  expect(await page.evaluate(() => JSON.parse(localStorage.getItem('workbench.xiaohongshu.columns') || '[]'))).toContain('shares');
});

test('清空后台任务中断并移除当前工作区全部任务', async ({ page }) => {
  const fixture = await fixtures(page);
  fixture.setJobs([
    { id: 'xhs-done', platform: 'xiaohongshu', kind: 'ocr', state: 'succeeded', created_at: '2026-10-08T00:00:00Z' },
    { id: 'xhs-failed', platform: 'xiaohongshu', kind: 'asr', state: 'failed', created_at: '2026-10-08T00:00:00Z' },
    { id: 'xhs-running', platform: 'xiaohongshu', kind: 'ocr', state: 'running', created_at: '2026-10-08T00:00:00Z' },
    { id: 'xhs-queued', platform: 'xiaohongshu', kind: 'ocr', state: 'queued', created_at: '2026-10-08T00:00:00Z' },
  ]);
  await page.goto('/'); await page.getByRole('button', { name: '后台任务' }).click();
  const drawer = page.getByRole('dialog', { name: '小红书后台任务' });
  await expect(drawer).toBeVisible();
  await drawer.getByRole('button', { name: '清空全部任务', exact: true }).click();
  await page.getByRole('tooltip').getByRole('button', { name: /清\s*空/ }).click();
  await expect.poll(() => fixture.jobs().length).toBe(0);
  expect(fixture.jobs().map(job => job.id)).toEqual([]);
  expect(fixture.calls.find(call => call.path === '/jobs' && call.method === 'DELETE')?.platform).toBe('xiaohongshu');
});

test('提取文字自动保存，关闭详情再重新打开显示修改内容', async ({ page }) => {
  const fixture = await fixtures(page); await page.goto('/'); await collectionDetail(page);
  const detail = page.getByRole('dialog', { name: '笔记阅读', exact: false });
  await detail.getByRole('button', { name: 'OCR / 视频转写', exact: true }).click();
  await expect(detail.getByRole('button', { name: '提取全部文字', exact: true })).toHaveCount(0);
  await expect(detail.getByRole('button', { name: '保存文字', exact: true })).toHaveCount(0);
  await detail.getByRole('textbox', { name: '素材 1 提取文字', exact: true }).fill('尚未保存的小红书 OCR 修订');
  await expect.poll(() => fixture.calls.filter(call => call.path === '/assets/a-xiaohongshu/text' && call.method === 'PATCH').length).toBe(1);
  expect(fixture.calls.find(call => call.path === '/assets/a-xiaohongshu/text' && call.method === 'PATCH')?.platform).toBe('xiaohongshu');
  await detail.locator('.ant-drawer-close').click(); await expect(detail).toBeHidden();
  await collectionDetail(page); await expect(detail).toBeVisible();
  await detail.getByRole('button', { name: 'OCR / 视频转写', exact: true }).click();
  await expect(detail.getByRole('textbox', { name: '素材 1 提取文字', exact: true })).toHaveValue('尚未保存的小红书 OCR 修订');
});

test('进入提取文字页时自动提交未完成素材并默认处理全部素材', async ({ page }) => {
  const fixture = await fixtures(page);
  fixture.data.xiaohongshu.asset.extraction.status = 'pending';
  await page.goto('/'); await collectionDetail(page);
  const detail = page.getByRole('dialog', { name: '笔记阅读', exact: false });
  await detail.getByRole('button', { name: 'OCR / 视频转写', exact: true }).click();
  await expect.poll(() => fixture.calls.filter(call => call.path === '/collections/n-xiaohongshu/extract' && call.method === 'POST').length).toBe(1);
  expect(fixture.calls.find(call => call.path === '/collections/n-xiaohongshu/extract' && call.method === 'POST')?.body).toEqual({ asset_ids: ['a-xiaohongshu'] });
});

test('取消版本切换确认后未保存仿写编辑保留', async ({ page }) => {
  await fixtures(page); await page.goto('/'); await rewrites(page);
  await expect(page.getByRole('textbox', { name: '仿写标题', exact: true })).toHaveValue('小红书稿件2');
  await page.getByRole('textbox', { name: '仿写标题', exact: true }).fill('尚未保存的小红书稿');
  const version = page.getByRole('combobox', { name: '仿写版本', exact: true }); await version.click(); await version.press('ArrowUp'); await version.press('Enter');
  const confirm = page.getByRole('dialog', { name: '放弃尚未保存的编辑？', exact: true }); await expect(confirm).toBeVisible();
  await confirm.getByRole('button', { name: /取\s*消/ }).click(); await expect(confirm).toBeHidden();
  await expect(page.getByRole('textbox', { name: '仿写标题', exact: true })).toHaveValue('尚未保存的小红书稿');
  await page.getByRole('menuitem', { name: /采集库/ }).click(); await rewrites(page);
  await expect(page.getByRole('textbox', { name: '仿写标题', exact: true })).toHaveValue('尚未保存的小红书稿');
});

test('取消创建弹窗后创建发布稿固定当前工作区', async ({ page }) => {
  const fixture = await fixtures(page); await page.goto('/'); await rewrites(page); await page.getByRole('tab', { name: /发布素材/ }).click(); await page.getByRole('button', { name: '创建发布稿' }).click();
  const modal = page.getByRole('dialog', { name: '创建小红书发布稿', exact: true }); await expect(modal).toBeVisible();
  await expect(modal.getByRole('radio', { name: '抖音', exact: true })).toHaveCount(0);
  await modal.getByRole('button', { name: /取\s*消/ }).click(); await expect(modal).toBeHidden();
  await page.getByRole('button', { name: '创建发布稿' }).click();
  await modal.getByRole('button', { name: '创建发布稿', exact: true }).click();
  await expect.poll(() => fixture.publications().length).toBe(1);
  await page.getByRole('dialog', { name: '发布稿详情', exact: true }).locator('.ant-drawer-close').click();
  await page.getByRole('menuitem', { name: /采集库/ }).click(); await rewrites(page); await page.getByRole('tab', { name: /发布素材/ }).click(); await page.getByRole('button', { name: '创建发布稿' }).click(); await expect(modal).toBeVisible(); await modal.getByRole('button', { name: '创建发布稿', exact: true }).click();
  await expect.poll(() => fixture.publications().length).toBe(2);
  expect(fixture.publications().map(item => item.target_platform)).toEqual(['xiaohongshu', 'xiaohongshu']);
  const submits = fixture.calls.filter(call => call.path === '/publications' && call.method === 'POST');
  expect(submits.map(call => [call.platform, call.body.target_platform])).toEqual([['xiaohongshu', 'xiaohongshu'], ['xiaohongshu', 'xiaohongshu']]);
});
