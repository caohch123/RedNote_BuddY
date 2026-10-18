import { memberAuthResponse } from './member-auth';
import { expect, test } from '@playwright/test';

test('旧项目自动使用全部来源文字，编辑器不再被来源栏分割', async ({ page }) => {
  const fullBody = '完整原文。'.repeat(800) + '正文末尾';
  const note = { id: 'source', platform: 'xiaohongshu', title: '完整笔记', body: fullBody, author: { name: '作者' }, asset_ids: ['ocr-1', 'ocr-2', 'pending'], topics: [] };
  const version = { id: 'version', number: 1, title: '仿写版本', body: fullBody, topics: [], script: '', asset_ids: [], confirmed: false };
  const rewrite = { id: 'rewrite', note_id: note.id, title: note.title, versions: [version], options: { include_body: false, extraction_asset_ids: ['ocr-1'], audience: '读者', tone: '自然分享', length: '适中' } };
  const assets = [
    { id: 'ocr-1', kind: 'image', extraction: { text: '图片一全部 OCR', status: 'succeeded' } },
    { id: 'ocr-2', kind: 'image', extraction: { segments: [{ text: '第二份人工文字' }], edited: true } },
    { id: 'pending', kind: 'image', extraction: { status: 'running' } },
    { id: 'generated', kind: 'image', extraction: { text: '生成图片不属于原始笔记', status: 'succeeded' } },
  ];
  let submitted: any;
  await page.route('**/api/v1/**', async route => {
    const authResponse = memberAuthResponse(route); if (authResponse) return authResponse;
    const url = new URL(route.request().url()); const path = url.pathname.replace('/api/v1', '');
    const json = (value: unknown, status = 200) => route.fulfill({ contentType: 'application/json', body: JSON.stringify(value), status });
    if (path === '/workspaces') return json({ items: [{ id: 'xiaohongshu', platform: 'xiaohongshu' }], total: 1 });
    if (path === '/session') return json({ csrf_token: 'rewrite-fixture' });
    if (path === '/rewrites') return json({ items: [rewrite], total: 1 });
    if (path === '/rewrites/rewrite') return json({ rewrite, note, assets, videos: [] });
    if (path === '/rewrites/rewrite/generate') {
      expect(url.searchParams.get('platform')).toBe('xiaohongshu');
      submitted = route.request().postDataJSON(); return json({ job_id: 'generation' }, 202);
    }
    return json({ items: [], total: 0 });
  });
  await page.goto('/'); await page.getByRole('menuitem', { name: /仿写库/ }).click();
  await expect(page.getByRole('textbox', { name: '仿写正文', exact: true })).toHaveValue(fullBody);
  await expect(page.getByText('来源内容', { exact: true })).toHaveCount(0);
  await expect(page.getByRole('checkbox', { name: /使用当前采集标题/ })).toHaveCount(0);
  await expect(page.getByText(/AI 输入：/)).toHaveCount(0);
  await expect(page.getByRole('textbox', { name: '视频口播脚本' })).toHaveCount(0);
  const list = await page.locator('.rewrite-list').boundingBox();
  const editor = await page.locator('.draft-pane').boundingBox();
  expect(editor!.width).toBeGreaterThan(list!.width * 3);
  const generateButton = page.getByRole('button', { name: 'AI 生成新版本' });
  await expect(generateButton).toHaveCSS('background-image', /linear-gradient/);
  await generateButton.click();
  await expect.poll(() => submitted).toBeTruthy();
  expect(submitted.include_body).toBe(true);
  expect(submitted.extraction_asset_ids).toEqual(['ocr-1', 'ocr-2']);
  await page.setViewportSize({ width: 900, height: 900 });
  await expect.poll(async () => (await page.locator('.rewrite-list').boundingBox())!.height).toBeLessThanOrEqual(180);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(900);
});

for (const initialVersion of [true, false]) {
  test(`切换视频方案不影响文案保存和历史口播（${initialVersion ? '已有版本' : '首个版本'}）`, async ({ page }) => {
    const note = { id: 'source', platform: 'xiaohongshu', title: '素材标题', body: '完整正文', asset_ids: [], topics: [] };
    const version = { id: 'version', number: 1, title: '素材标题', body: '完整正文', topics: [], script: '已有口播', asset_ids: [] };
    const rewrite = { id: 'rewrite', note_id: note.id, title: note.title, versions: initialVersion ? [version] : [], options: {} };
    let submitted: any;
    await page.route('**/api/v1/**', async route => {
    const authResponse = memberAuthResponse(route); if (authResponse) return authResponse;
      const request = route.request(); const url = new URL(request.url()); const path = url.pathname.replace('/api/v1', '');
      const json = (value: unknown, status = 200) => route.fulfill({ contentType: 'application/json', body: JSON.stringify(value), status });
      if (path === '/workspaces') return json({ items: [{ id: 'xiaohongshu', platform: 'xiaohongshu' }], total: 1 });
      if (path === '/session') return json({ csrf_token: 'script-fixture' });
      if (path === '/rewrites') return json({ items: [rewrite], total: 1 });
      if (path === '/rewrites/rewrite') return json({ rewrite, note, assets: [], videos: [] });
      if (path === '/rewrites/rewrite/videos/initialize') return json([]);
      if (path === '/rewrites/rewrite/versions' && request.method() === 'POST') {
        expect(url.searchParams.get('platform')).toBe('xiaohongshu');
        submitted = request.postDataJSON();
        const saved = { ...submitted, id: 'saved', number: rewrite.versions.length + 1 };
        rewrite.versions.push(saved); return json(saved, 201);
      }
      return json({ items: [], total: 0 });
    });
    await page.goto('/'); await page.getByRole('menuitem', { name: /仿写库/ }).click();
    await expect(page.getByRole('textbox', { name: '仿写正文', exact: true })).toHaveValue(note.body);
    await expect(page.getByRole('textbox', { name: '视频口播脚本' })).toHaveCount(0);
    await page.getByRole('textbox', { name: '仿写正文', exact: true }).fill('手动修改的完整正文');
    await page.getByRole('tab', { name: '视频仿写', exact: true }).click();
    await expect(page.getByRole('button', { name: '开始视频仿写' })).toBeEnabled();
    await expect(page.getByRole('textbox', { name: '视频口播脚本' })).toHaveCount(0);
    await page.getByRole('tab', { name: '文案编辑', exact: true }).click();
    await expect(page.getByRole('textbox', { name: '视频口播脚本' })).toBeHidden();
    await expect(page.getByRole('textbox', { name: '仿写正文', exact: true })).toHaveValue('手动修改的完整正文');
    await page.getByRole('button', { name: '保存新版本' }).click();
    await expect.poll(() => submitted).toBeTruthy();
    expect(submitted.body).toBe('手动修改的完整正文');
    expect(submitted.script).toBe(initialVersion ? version.script : '');
    expect(submitted.parent_version_id).toBe(initialVersion ? version.id : undefined);
    await expect(page.getByText('有未保存编辑', { exact: true })).toHaveCount(0);
  });
}

test('视频方案初始化异常可重试，不会使文案工作区崩溃', async ({ page }) => {
  const note = { id: 'source', platform: 'xiaohongshu', title: '初始化恢复', body: '保留的正文', asset_ids: [], topics: [] };
  const rewrite = { id: 'rewrite', note_id: note.id, title: note.title, versions: [], options: {} };
  const errors: string[] = []; let attempts = 0;
  page.on('pageerror', error => errors.push(error.message));
  await page.route('**/api/v1/**', route => {
    const authResponse = memberAuthResponse(route); if (authResponse) return authResponse;
    const path = new URL(route.request().url()).pathname.replace('/api/v1', '');
    const json = (value: unknown) => route.fulfill({ contentType: 'application/json', body: JSON.stringify(value) });
    if (path === '/workspaces') return json({ items: [{ id: 'xiaohongshu', platform: 'xiaohongshu' }], total: 1 });
    if (path === '/session') return json({ csrf_token: 'init-recovery' });
    if (path === '/rewrites') return json({ items: [rewrite], total: 1 });
    if (path === '/rewrites/rewrite') return json({ rewrite, note, assets: [], videos: [] });
    if (path === '/rewrites/rewrite/videos/initialize') return json(++attempts === 1 ? { items: [] } : []);
    return json({ items: [], total: 0 });
  });
  await page.goto('/'); await page.getByRole('menuitem', { name: /仿写库/ }).click();
  await page.getByRole('tab', { name: '视频仿写', exact: true }).click();
  await expect(page.getByText('视频方案初始化失败', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: /^重\s*试$/ }).click();
  await expect(page.getByText('视频方案初始化失败', { exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: '开始视频仿写' })).toBeEnabled();
  expect(attempts).toBe(2);
  await page.getByRole('tab', { name: '文案编辑', exact: true }).click();
  await expect(page.getByRole('textbox', { name: '仿写正文', exact: true })).toHaveValue(note.body);
  expect(errors).toEqual([]);
});
