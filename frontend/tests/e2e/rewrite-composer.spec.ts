import { expect, test, type Page } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import { memberAuthResponse } from './member-auth';

test.setTimeout(60_000);

async function fixture(page: Page) {
  const note = { id: 'source', platform: 'xiaohongshu', title: '山间生活日记', body: '完整来源正文', topics: ['生活'], asset_ids: ['source-image'] };
  const versions: any[] = [
    { id: 'version-1', number: 1, title: '初稿标题', body: '初稿正文', topics: [], asset_ids: [], script: '保留口播', created_at: '2026-10-18T10:00:00Z' },
    { id: 'version-2', number: 2, title: '把生活写成日记', body: Array.from({ length: 40 }, (_, i) => `第 ${i + 1} 段：记录清晨的光线和沿途风景，整理今天的生活灵感。`).join('\n\n'), topics: ['日常', '旅行'], asset_ids: [], script: '保留口播', created_at: '2026-10-18T12:00:00Z' },
  ];
  const rewrite: any = { id: 'composer', title: note.title, note_id: note.id, versions, options: { audience: '生活爱好者', tone: '自然分享', length: '中等（约 500 字）', instructions: '' } };
  const writes: { path: string; body: any }[] = [];
  const errors: string[] = []; page.on('pageerror', error => errors.push(error.message));
  await page.addInitScript(() => {
    localStorage.setItem('workbench.xiaohongshu.page', 'rewrites');
    localStorage.setItem('workbench.xiaohongshu.sidebarCollapsed', String(window.innerWidth <= 600));
  });
  await page.route('**/api/v1/**', route => {
    const auth = memberAuthResponse(route); if (auth) return auth;
    const request = route.request(); const url = new URL(request.url()); const path = url.pathname.replace('/api/v1', '');
    const json = (value: unknown, status = 200) => route.fulfill({ json: value, status });
    if (path === '/session') return json({ csrf_token: 'composer-fixture' });
    if (path === '/workspaces') return json({ items: [{ platform: 'xiaohongshu' }], total: 1 });
    if (request.method() !== 'GET') {
      expect(request.headers()['x-csrf-token']).toBe('composer-fixture');
      expect(url.searchParams.get('platform')).toBe('xiaohongshu');
      writes.push({ path, body: request.postDataJSON() });
    }
    if (path === '/rewrites') return json({ items: [rewrite], total: 1 });
    if (path === '/rewrites/composer') return json({ rewrite, note, assets: [{ id: 'source-image', owner_id: 'source', kind: 'image', mime: 'image/png', status: 'ready', extraction: { text: '来源图片文字', status: 'succeeded' } }], videos: [] });
    if (path === '/rewrites/composer/versions') {
      const version = { ...request.postDataJSON(), id: `version-${versions.length + 1}`, number: versions.length + 1, created_at: '2026-10-19T00:00:00Z' };
      versions.push(version); return json(version, 201);
    }
    if (path === '/rewrites/composer/confirm') { rewrite.confirmed_version_id = request.postDataJSON().version_id; return json(rewrite); }
    if (path === '/rewrites/composer/generate') return json({ job_id: 'generation' }, 202);
    return json({ items: [], total: 0 });
  });
  await page.goto('/'); await expect(page.getByRole('textbox', { name: '仿写标题', exact: true })).toHaveValue(versions[1].title);
  await expect(page.getByRole('region', { name: 'AI 文案对话框', exact: true })).toBeVisible();
  return { rewrite, versions, writes, errors };
}

async function geometry(page: Page) {
  return page.evaluate(() => {
    const box = (selector: string) => { const b = document.querySelector(selector)!.getBoundingClientRect(); return { x: b.x, y: b.y, width: b.width, right: b.right, bottom: b.bottom }; };
    return { composer: box('.copy-composer'), card: box('.copy-note-card'), list: box('.rewrite-list'), height: innerHeight, width: innerWidth };
  });
}

test('笔记文案在上方单一内容框，对话框固定到底部并跟随侧栏宽度', async ({ page }) => {
  await page.setViewportSize({ width: 1720, height: 1000 }); const { errors, writes } = await fixture(page);
  const card = page.getByRole('region', { name: '笔记文案', exact: true });
  for (const name of ['仿写标题', '仿写正文']) await expect(card.getByRole('textbox', { name, exact: true })).toBeVisible();
  await expect(card.getByRole('textbox', { name: '仿写正文', exact: true })).toHaveCSS('font-weight', '400');
  await expect(card.getByRole('combobox', { name: '仿写话题', exact: true })).toHaveCount(1);
  const before = await geometry(page);
  expect(before.card.y).toBeLessThan(before.composer.y);
  expect(before.composer.x).toBeGreaterThan(before.list.right);
  expect(before.height - before.composer.bottom).toBeLessThanOrEqual(18);
  await page.locator('.main-content').evaluate(node => { node.scrollTop = node.scrollHeight; });
  await expect.poll(async () => Math.abs((await geometry(page)).composer.y - before.composer.y)).toBeLessThanOrEqual(1);
  await page.getByRole('separator', { name: '调整项目列表与工作区宽度' }).press('ArrowRight');
  await expect.poll(async () => { const b = await geometry(page); return Math.abs(b.composer.x - b.card.x) + Math.abs(b.composer.width - b.card.width); }).toBeLessThanOrEqual(2);
  await page.getByRole('button', { name: '收起侧边栏', exact: true }).click();
  await expect.poll(async () => { const b = await geometry(page); return Math.abs(b.composer.x - b.card.x) + Math.abs(b.composer.width - b.card.width); }).toBeLessThanOrEqual(2);
  await card.getByRole('button', { name: '保存新版本', exact: true }).scrollIntoViewIfNeeded();
  const footer = await card.locator('.copy-note-footer').boundingBox(); const last = await geometry(page);
  expect(footer!.y + footer!.height).toBeLessThanOrEqual(last.composer.y);
  expect(errors).toEqual([]); expect(writes).toEqual([]);
  mkdirSync('../data/verification', { recursive: true }); await page.screenshot({ path: '../data/verification/rewrite-composer-wide.png' });
});

for (const width of [1157, 644, 360]) test(`底部对话框在 ${width}px 保持可用且不遮住内容`, async ({ page }) => {
  await page.setViewportSize({ width, height: 763 }); const { writes, errors } = await fixture(page);
  const composer = page.getByRole('region', { name: 'AI 文案对话框', exact: true });
  const box = await composer.boundingBox(); expect(box!.x).toBeGreaterThanOrEqual(0); expect(box!.x + box!.width).toBeLessThanOrEqual(width);
  expect(763 - box!.y - box!.height).toBeLessThanOrEqual(18);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
  expect(await page.locator('.main-content').evaluate(node => node.scrollWidth - node.clientWidth)).toBeLessThanOrEqual(1);
  await composer.getByRole('textbox', { name: '补充要求', exact: true }).fill('增加真实生活细节，保留主要观点');
  await composer.getByRole('combobox', { name: '表达语气', exact: true }).click(); await page.getByTitle('专业实用', { exact: true }).click();
  await expect(page.locator('.ant-select-dropdown:not(.ant-select-dropdown-hidden)')).toHaveCount(0);
  await expect(composer.getByRole('button', { name: 'AI 生成新版本', exact: true })).toBeVisible();
  await page.screenshot({ path: `../data/verification/rewrite-composer-${width}.png` });
  expect(writes).toEqual([]); expect(errors).toEqual([]);
});

test('对话框快捷键提交真实生成参数，生成中禁止重复提交且保留文案', async ({ page }) => {
  const { writes, versions } = await fixture(page); const body = versions[1].body;
  const composer = page.getByRole('region', { name: 'AI 文案对话框', exact: true });
  await composer.getByRole('textbox', { name: '目标读者', exact: true }).fill('第一次远足的新手');
  const instructions = composer.getByRole('textbox', { name: '补充要求', exact: true }); await instructions.fill('增加具体场景');
  await instructions.press('Control+Enter');
  await expect.poll(() => writes.filter(write => write.path.endsWith('/generate')).length).toBe(1);
  expect(writes[0].body).toMatchObject({ audience: '第一次远足的新手', instructions: '增加具体场景', include_body: true, extraction_asset_ids: ['source-image'] });
  await expect(composer.getByRole('button', { name: 'AI 生成新版本', exact: true })).toBeDisabled();
  await instructions.press('Control+Enter'); expect(writes.filter(write => write.path.endsWith('/generate'))).toHaveLength(1);
  await expect(page.getByRole('textbox', { name: '仿写正文', exact: true })).toHaveValue(body);
});

test('文案框保存确认保留原逻辑，切换标签收起对话框并保留输入', async ({ page }) => {
  const { writes, rewrite } = await fixture(page);
  const composer = page.getByRole('region', { name: 'AI 文案对话框', exact: true });
  await composer.getByRole('textbox', { name: '补充要求', exact: true }).fill('保留这条输入');
  await page.getByRole('textbox', { name: '仿写正文', exact: true }).fill('手动修改后的笔记正文');
  await page.getByRole('tab', { name: '素材库', exact: true }).click(); await expect(composer).toHaveCount(0);
  await page.getByRole('tab', { name: '文案编辑', exact: true }).click();
  await expect(composer.getByRole('textbox', { name: '补充要求', exact: true })).toHaveValue('保留这条输入');
  await page.getByRole('button', { name: '保存新版本', exact: true }).click();
  await expect(page.getByText(/当前版本 3 ·/)).toBeVisible();
  expect(writes.find(write => write.path.endsWith('/versions'))?.body).toMatchObject({ body: '手动修改后的笔记正文', parent_version_id: 'version-2', script: '保留口播' });
  await page.getByRole('button', { name: '确认此版本', exact: true }).click();
  await expect.poll(() => rewrite.confirmed_version_id).toBe('version-3');
  await expect(page.getByRole('button', { name: '确认此版本', exact: true })).toBeDisabled();
  const versions = composer.getByRole('combobox', { name: '仿写版本', exact: true }); await versions.click();
  await page.getByTitle('版本 1', { exact: true }).click();
  await expect(page.getByRole('textbox', { name: '仿写正文', exact: true })).toHaveValue('初稿正文');
  await page.getByRole('menuitem', { name: /采集库/ }).click(); await expect(composer).toHaveCount(0);
});
