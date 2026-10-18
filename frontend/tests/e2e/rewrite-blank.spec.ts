import { memberAuthResponse } from './member-auth';
import { expect, test, type Page } from '@playwright/test';

async function fixture(page: Page, failOnce = false) {
  const note = { id: 'old-note', platform: 'xiaohongshu', title: '已有来源标题', body: '已有来源正文', topics: ['旧话题'], asset_ids: ['old-image'], media_status: 'succeeded' };
  const old = { id: 'old-project', note_id: note.id, title: note.title, updated_at: '2026-10-16T00:00:00Z', versions: [{ id: 'old-version', number: 1, title: note.title, body: note.body, topics: note.topics, asset_ids: note.asset_ids }], image_rewrites: [], options: {} };
  const image: any = { id: 'blank-image-scheme', workflow_version: 2, edit_intent: { mode: 'flexible' }, version_id: 'blank-version', primary_asset_id: '', references: [], source_asset_ids: [], feature_sources: {}, feature_controls: Object.fromEntries(['subject', 'composition', 'action', 'environment', 'style', 'lighting', 'color', 'text'].map(key => [key, { mode: 'auto' }])), proposals: [], analyses: [], batches: [], results: [], prompt: '', revision: 1, status: 'draft', output: { ratio: '3:4', clarity: 'standard', count: 1 } };
  const blank: any = { id: 'blank-project', blank: true, note_id: '', title: '未命名项目', updated_at: '2026-10-17T00:00:00Z', versions: [{ id: 'blank-version', number: 1, title: '', body: '', script: '', topics: [], asset_ids: [] }], image_rewrites: [image], options: { audience: '', tone: '自然分享', length: '适中', instructions: '', include_body: true, extraction_asset_ids: [] } };
  const video: any = { id: 'blank-video', rewrite_id: blank.id, version_id: 'blank-version', workflow: 'video_rewrite_v2', revision: 1, status: 'draft', video_rewrite: { primary_asset_id: '', reference_video_id: '', reference_image_id: '', purpose_sources: {}, requirements: '', prompt: '', output: { duration: 10, resolution: '720p', ratio: '9:16', generate_audio: true }, analyses: [], submissions: [], results: [], script: [], initialization_complete: true } };
  const emptyNote = { id: '', platform: 'xiaohongshu', title: '', body: '', topics: [], asset_ids: [], media_status: 'succeeded', metadata_status: 'succeeded', author: {} };
  const projects: any[] = [old], assets: any[] = [];
  const writes: { method: string; path: string; body: any }[] = [];
  await page.addInitScript(() => {
    localStorage.setItem('workbench.workspace', 'xiaohongshu');
    localStorage.setItem('workbench.xiaohongshu.page', 'rewrites');
    localStorage.setItem('workbench.xiaohongshu.sidebarCollapsed', 'true');
  });
  await page.route('**/api/v1/**', async route => {
    const authResponse = memberAuthResponse(route); if (authResponse) return authResponse;
    const req = route.request(), url = new URL(req.url()), path = url.pathname.replace('/api/v1', '');
    const json = (value: unknown, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(value) });
    if (req.method() !== 'GET') {
      expect(req.headers()['x-csrf-token']).toBe('blank-csrf'); expect(url.searchParams.get('platform')).toBe('xiaohongshu');
      writes.push({ method: req.method(), path, body: path === '/assets' ? req.postData() : req.postDataJSON() });
    }
    if (path === '/session') return json({ csrf_token: 'blank-csrf' });
    if (path === '/workspaces') return json({ items: [{ platform: 'xiaohongshu' }], total: 1 });
    if (path === '/rewrites') {
      if (req.method() === 'POST') {
        expect(req.postDataJSON()).toEqual({ blank: true });
        if (failOnce) { failOnce = false; return json({ error: '暂时无法创建空白项目' }, 503); }
        await new Promise(resolve => setTimeout(resolve, 100)); projects.unshift(blank); return json(blank, 201);
      }
      return json({ items: projects, total: projects.length });
    }
    if (path === '/rewrites/old-project') return json({ rewrite: old, note, assets: [], videos: [] });
    if (path === '/rewrites/blank-project') return json({ rewrite: blank, note: emptyNote, assets, videos: [video] });
    if (path === '/rewrites/blank-project/versions') {
      const next = { ...req.postDataJSON(), id: 'saved-version', number: 2, confirmed: false };
      blank.versions.push(next); blank.title = next.title; return json(next, 201);
    }
    if (path === '/rewrites/blank-project/confirm') { blank.confirmed_version_id = req.postDataJSON().version_id; return json(blank); }
    if (path === '/rewrites/blank-project/images/initialize') return json([image]);
    if (path === '/rewrites/blank-project/videos/initialize') return json([video]);
    if (path === '/video-projects/blank-video') return json(video);
    if (path === '/image-capabilities') return json({ sizes: [], vision_configured: true, text_configured: true, image_configured: true, prompt_rules_version: 6 });
    if (path === '/video-capabilities') return json({ text_configured: true, vision_configured: true, video_configured: true, supported: true, rules_version: 2 });
    if (path === '/assets' && req.method() === 'POST') {
      expect(req.postData()).toContain('blank-project');
      const asset = { id: 'new-upload', owner_id: blank.id, kind: 'image', mime: 'image/png', status: 'ready', width: 320, height: 240, name: '自己的参考图.png' }; assets.push(asset); return json(asset, 201);
    }
    if (path === '/rewrites/blank-project/images/blank-image-scheme') { Object.assign(image, req.postDataJSON()); image.revision++; return json(image); }
    if (path.startsWith('/assets/')) return route.fulfill({ contentType: 'image/svg+xml', body: '<svg xmlns="http://www.w3.org/2000/svg" width="320" height="240"><rect width="320" height="240" fill="#edf2ff"/></svg>' });
    return json({ items: [], total: 0 });
  });
  await page.goto('/'); await expect(page.locator('.rewrite-list-item.selected')).toContainText(old.title);
  return { old, blank, image, writes };
}

test('新增空白项目清空来源并可保存确认、独立上传图片，刷新保持内容', async ({ page }) => {
  await page.setViewportSize({ width: 1157, height: 763 });
  const { old, image, writes } = await fixture(page);
  await page.getByRole('textbox', { name: '仿写正文', exact: true }).fill('旧项目尚未保存的内容');
  await page.getByPlaceholder('搜索仿写项目').fill('已有');
  await page.getByRole('button', { name: '新增空白项目', exact: true }).dblclick();
  await expect(page.locator('.rewrite-list-item.selected')).toContainText('未命名项目');
  await expect(page.getByPlaceholder('搜索仿写项目')).toHaveValue('');
  await expect(page.getByRole('textbox', { name: '仿写标题', exact: true })).toHaveValue('');
  await expect(page.getByRole('textbox', { name: '仿写正文', exact: true })).toHaveValue('');
  await expect(page.getByRole('combobox', { name: '仿写话题', exact: true }).locator('..').locator('.ant-select-selection-item')).toHaveCount(0);
  await expect(page.getByRole('tab', { name: '发布素材 (0)', exact: true })).toBeVisible();
  await page.getByRole('textbox', { name: '仿写标题', exact: true }).fill('自己的原创笔记');
  await page.getByRole('textbox', { name: '仿写正文', exact: true }).fill('从空白开始填写的完整正文。');
  await page.getByRole('button', { name: '保存新版本', exact: true }).click();
  await expect(page.getByText(/当前版本 2 ·/)).toBeVisible();
  await page.getByRole('button', { name: '确认此版本', exact: true }).click();
  await expect(page.locator('.rewrite-tab-actions')).toContainText('已确认');
  await page.reload(); await expect(page.getByRole('textbox', { name: '仿写正文', exact: true })).toHaveValue('从空白开始填写的完整正文。');
  await page.getByRole('tab', { name: '图片仿写', exact: true }).click();
  await expect(page.locator('.reference-slot-card')).toHaveCount(3); await expect(page.locator('.reference-slot-card.has-media')).toHaveCount(0);
  const primary = page.locator('[data-reference-title="被仿写图"]');
  await primary.getByRole('button', { name: '被仿写图菜单', exact: true }).click();
  const chooser = page.waitForEvent('filechooser'); await page.locator('.reference-slot-menu').getByRole('menuitem', { name: '上传图片', exact: true }).click();
  await (await chooser).setFiles({ name: '自己的参考图.png', mimeType: 'image/png', buffer: Buffer.from('fixture-image') });
  await expect.poll(() => image.primary_asset_id).toBe('new-upload'); await expect(primary).toHaveClass(/has-media/);
  await page.getByRole('tab', { name: '视频仿写', exact: true }).click();
  await expect(page.getByRole('tabpanel', { name: '视频仿写', exact: true }).locator('.reference-slot-card')).toHaveCount(3);
  await expect(page.getByRole('tabpanel', { name: '视频仿写', exact: true }).locator('.reference-slot-card.has-media')).toHaveCount(0);
  expect(writes.filter(item => item.path === '/rewrites')).toHaveLength(1);
  expect(writes.filter(item => /generate|analyze|compose/.test(item.path))).toHaveLength(0);
  expect(old.versions).toHaveLength(1); expect(old.versions[0].body).toBe('已有来源正文');
});

for (const width of [1157, 644, 360]) test(`新增按钮与搜索框同行，三个文案选项在 ${width}px 保持一行且不溢出`, async ({ page }) => {
  await page.setViewportSize({ width, height: 763 }); await fixture(page);
  const search = (await page.getByPlaceholder('搜索仿写项目').boundingBox())!;
  const create = (await page.getByRole('button', { name: '新增空白项目', exact: true }).boundingBox())!;
  expect(create.x).toBeGreaterThan(search.x + search.width); expect(Math.abs(create.y + create.height / 2 - search.y - search.height / 2)).toBeLessThan(2);
  const fields = await Promise.all(['目标读者', '表达语气', '正文长度'].map(name => page.getByRole(name === '目标读者' ? 'textbox' : 'combobox', { name, exact: true }).evaluate(node => {
    const box = (node.closest('.ant-select') || node).getBoundingClientRect();
    return { x: box.x, y: box.y, width: box.width, height: box.height };
  })));
  for (let i = 1; i < fields.length; i++) {
    expect(Math.abs(fields[i]!.y - fields[0]!.y)).toBeLessThan(2);
    expect(fields[i]!.x - fields[i - 1]!.x - fields[i - 1]!.width).toBeGreaterThanOrEqual(15);
  }
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
  await page.getByRole('combobox', { name: '表达语气', exact: true }).click(); await page.getByTitle('专业实用', { exact: true }).click();
  await page.getByRole('combobox', { name: '正文长度', exact: true }).click(); await page.getByTitle('精简（约 200 字）', { exact: true }).click();
  await page.locator('.rewrite-layout').screenshot({ path: `../data/verification/rewrite-blank-options-${width}-20261017.png` });
});

test('创建空白项目失败保留原文案，可再次点击重试', async ({ page }) => {
  const { writes } = await fixture(page, true);
  const body = page.getByRole('textbox', { name: '仿写正文', exact: true }); await body.fill('尚未保存的编辑');
  await page.getByRole('button', { name: '新增空白项目', exact: true }).click();
  await expect(page.getByText('暂时无法创建空白项目', { exact: true })).toBeVisible();
  await expect(body).toHaveValue('尚未保存的编辑'); await expect(page.locator('.rewrite-list-item.selected')).toContainText('已有来源标题');
  await page.getByRole('button', { name: '新增空白项目', exact: true }).click();
  await expect(page.locator('.rewrite-list-item.selected')).toContainText('未命名项目');
  await expect(body).toHaveValue(''); expect(writes.filter(item => item.path === '/rewrites')).toHaveLength(2);
});
