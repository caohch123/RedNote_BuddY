import { expect, test, type Page } from '@playwright/test';

async function fixture(page: Page) {
  const writes: string[] = [];
  const main = 'main-dock'; const aux = 'aux-dock';
  const note = { id: 'note-dock', platform: 'xiaohongshu', title: '布局测试笔记', body: '原正文', author: { name: '布局测试' }, asset_ids: [main, aux], topics: [], tags: [] };
  const version = { id: 'version-dock', number: 1, title: note.title, body: note.body, script: '', topics: [], asset_ids: [main, aux], confirmed: true, source_snapshot: note };
  const sources = { subject: aux, action: main, scene: main, composition: main, palette: main, lighting: main, texture: aux, text: main };
  const unit = { id: 'unit-dock', workflow_version: 2, version_id: version.id, primary_asset_id: main, references: [{ asset_id: aux }], edit_intent: { mode: 'flexible' }, feature_controls: Object.fromEntries(Object.entries(sources).map(([key, asset_id]) => [key, { mode: 'image', asset_id }])), feature_sources: sources, output: { ratio: '3:4', clarity: 'standard', count: 1 }, text_mode: 'none', image_text: '', prompt: '已审阅的测试提示词', prompt_input_digest: 'ready', analyses: [main, aux].map(asset_id => ({ asset_id, width: 960, height: 1280, recognized_text: '', features: { subject: '明确的主体与对象', action: '静态姿势', scene: '红色纯色背景', composition: '居中构图', palette: '红白对比', lighting: '平光', texture: '清晰的手绘线条', text: '无可见文字' } })), batches: [], results: [], revision: 1, status: 'ready', style: { ratio: '3:4' } };
  const rewrite = { id: 'rewrite-dock', note_id: note.id, title: note.title, versions: [version], confirmed_version_id: version.id, options: { include_body: true }, image_rewrites: [unit] };
  const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a3ioAAAAASUVORK5CYII=', 'base64');
  await page.addInitScript(() => {
    localStorage.setItem('workbench.workspace', 'xiaohongshu');
    for (const p of ['xiaohongshu']) {
      localStorage.setItem(`workbench.${p}.page`, 'rewrites');
      localStorage.setItem(`workbench.${p}.sidebarCollapsed`, 'true');
    }
  });
  await page.route('**/api/v1/**', async route => {
    const req = route.request(); const path = new URL(req.url()).pathname.replace('/api/v1', '');
    if (req.method() !== 'GET') { writes.push(path); return route.fulfill({ status: 500, json: { error: '布局测试不允许写入' } }); }
    const json = (value: unknown) => route.fulfill({ json: value });
    if (path === '/session') return json({ csrf_token: 'test-csrf' });
    if (path === '/workspaces') return json({ items: [{ platform: 'xiaohongshu' }], total: 1 });
    if (path === '/rewrites') return json({ items: [rewrite], total: 1 });
    if (path === `/rewrites/${rewrite.id}`) return json({ rewrite, note, assets: [main, aux].map(id => ({ id, kind: 'image', mime: 'image/png', status: 'ready', owner_id: note.id, width: 960, height: 1280 })), videos: [] });
    if (path === '/image-capabilities') return json({ image_configured: true, vision_configured: true, text_configured: true, supported: true, max_count: 4, sizes: [{ ratio: '3:4', clarity: 'standard', width: 960, height: 1280 }] });
    if (path.endsWith('/file')) return route.fulfill({ contentType: 'image/png', body: png });
    if (path === '/style-library') return json({ items: [], categories: [], total: 0 });
    return json({ items: [], total: 0 });
  });
  await page.goto('/'); await page.getByRole('tab', { name: '图片仿写', exact: true }).click();
  await page.getByRole('button', { name: '1. 选图与方案', exact: true }).click();
  return { writes };
}

async function assertDock(page: Page, width: number) {
  await expect(page.locator('.workspace-action-dock:visible')).toHaveCount(1);
  await expect.poll(() => page.locator('.workspace-action-dock:visible').evaluate(el => Math.abs(el.getBoundingClientRect().bottom - innerHeight))).toBeLessThanOrEqual(1);
  const measures = await page.evaluate(() => {
    const visible = (selector: string) => Array.from(document.querySelectorAll<HTMLElement>(selector)).find(el => el.getBoundingClientRect().width > 0)!;
    const dock = visible('.workspace-action-dock'); const actions = visible('.image-workflow-actions');
    const content = visible('.main-content'); const sidebar = visible('.sidebar'); const anchor = visible('.workspace-action-anchor');
    const d = dock.getBoundingClientRect(), a = actions.getBoundingClientRect(), c = content.getBoundingClientRect(), s = sidebar.getBoundingClientRect(), x = anchor.getBoundingClientRect();
    const controlsClear = Array.from(actions.querySelectorAll<HTMLElement>('button, .ant-select-selector')).every(el => {
      const b = el.getBoundingClientRect(); if (!b.width || !b.height) return true;
      const hit = document.elementFromPoint(b.left + b.width / 2, b.top + b.height / 2);
      return !!hit && (el.contains(hit) || hit.contains(el)) && b.bottom <= innerHeight && b.left >= a.left - 1 && b.right <= a.right + 1;
    });
    return { dockBottom: d.bottom, viewportBottom: innerHeight, contentBottom: c.bottom, dockTop: d.top, left: a.left, right: a.right, anchorLeft: x.left, anchorRight: x.right, sidebarRight: s.right, controlsClear, rootOverflow: document.documentElement.scrollWidth > innerWidth, scrollablePages: Array.from(content.querySelectorAll<HTMLElement>('*')).filter(el => ['auto', 'scroll'].includes(getComputedStyle(el).overflowY) && el.scrollHeight > el.clientHeight + 10 && el !== content).map(el => el.className) };
  });
  expect(measures.right).toBeLessThanOrEqual(width);
  expect(Math.abs(measures.left - measures.anchorLeft)).toBeLessThanOrEqual(1);
  expect(Math.abs(measures.right - measures.anchorRight)).toBeLessThanOrEqual(1);
  expect(measures.left).toBeGreaterThanOrEqual(measures.sidebarRight);
  expect(Math.abs(measures.contentBottom - measures.dockTop)).toBeLessThanOrEqual(1);
  expect(measures.controlsClear).toBe(true); expect(measures.rootOverflow).toBe(false);
  expect(measures.scrollablePages.filter(name => !name.includes('rewrite-list'))).toEqual([]);
}

for (const width of [1536, 825, 597, 569]) test(`Microsoft Edge ${width}×751 操作栏贴底且正文不被遮挡`, async ({ page }) => {
  await page.setViewportSize({ width, height: 751 }); const f = await fixture(page);
  for (const fraction of [0, 0.5, 1]) {
    await page.locator('.main-content:visible').evaluate((el, fraction) => { el.scrollTop = (el.scrollHeight - el.clientHeight) * fraction; }, fraction);
    await assertDock(page, width);
  }
  const lastCard = page.locator('.image-input-grid .ant-card').last(); await lastCard.scrollIntoViewIfNeeded();
  const featureBox = await lastCard.boundingBox(); const dockBox = await page.locator('.workspace-action-dock:visible').boundingBox();
  expect(featureBox!.y + featureBox!.height).toBeLessThanOrEqual(dockBox!.y + 1);
  await page.getByRole('button', { name: '2. 审阅提示词' }).click(); await assertDock(page, width);
  await page.getByRole('tab', { name: '文案编辑', exact: true }).click(); await expect(page.locator('.workspace-action-dock:visible')).toHaveCount(0);
  await page.getByRole('tab', { name: '图片仿写', exact: true }).click(); await assertDock(page, width);
  expect(f.writes).toEqual([]);
});

test('Microsoft Edge 侧栏变化与页面切换管理操作栏', async ({ page }) => {
  await page.setViewportSize({ width: 825, height: 751 }); const f = await fixture(page);
  await page.getByRole('button', { name: '展开侧边栏' }).click(); await assertDock(page, 825);
  await page.getByRole('button', { name: '收起侧边栏' }).click(); await assertDock(page, 825);
  await page.getByRole('menuitem', { name: /采集库/ }).click(); await expect(page.locator('.workspace-action-dock:visible')).toHaveCount(0);
  await page.getByRole('menuitem', { name: /仿写库/ }).click(); await assertDock(page, 825);
  await page.getByRole('tab', { name: '文案编辑', exact: true }).click(); await expect(page.locator('.workspace-action-dock:visible')).toHaveCount(0);
  await page.getByRole('tab', { name: '图片仿写', exact: true }).click(); await assertDock(page, 825);
  expect(f.writes).toEqual([]);
});
