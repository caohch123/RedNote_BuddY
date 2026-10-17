import { chromium } from 'playwright';
import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import path from 'node:path';

const base = 'http://127.0.0.1:8080';
const project = '3e15dd3277739627464574280ad8981c';
const folder = path.resolve('../data/verification/media-library-live-20261012');
await mkdir(folder, { recursive: true });
const browser = await chromium.launch({ channel: 'msedge', headless: true });
try {
  const context = await browser.newContext();
  await context.addInitScript(() => {
    localStorage.setItem('workbench.workspace', 'xiaohongshu');
    localStorage.setItem('workbench.xiaohongshu.page', 'rewrites');
    localStorage.setItem('workbench.xiaohongshu.sidebarCollapsed', 'true');
  });
  const detail = async () => {
    const res = await context.request.get(`${base}/api/v1/rewrites/${project}?platform=xiaohongshu`);
    assert(res.ok()); return res.json();
  };
  const before = await detail(); const writes = [], errors = [], checks = [];
  const page = await context.newPage(); page.on('pageerror', e => errors.push(e.message));
  await page.route('**/api/v1/**', route => {
    if (['GET', 'HEAD'].includes(route.request().method())) return route.continue();
    writes.push(new URL(route.request().url()).pathname); return route.abort();
  });
  await page.goto(base);
  const workspace = page.locator('[data-workspace="xiaohongshu"]');
  await workspace.locator('.rewrite-list-item').filter({ hasText: before.rewrite.title }).first().click();
  await workspace.getByRole('button', { name: 'AI 生成新版本', exact: true }).waitFor();
  assert.equal(await workspace.getByRole('button', { name: '版本对比', exact: true }).count(), 0);
  assert.equal(await page.getByRole('dialog', { name: '版本对比', exact: true }).count(), 0);
  await workspace.getByRole('tab', { name: /发布素材/ }).click();
  const allowed = new Set([...before.note.asset_ids, ...before.rewrite.versions.flatMap(v => v.asset_ids)]);
  const cards = await workspace.locator('.media-selection-card').evaluateAll(elements => elements.map(el => el.dataset.assetId));
  assert(cards.every(id => allowed.has(id)), 'unadopted output leaked into publishing grid');
  const unadopted = before.rewrite.image_rewrites.flatMap(u => u.results).filter(r => !allowed.has(r.asset_id));
  assert(unadopted.every(result => !cards.includes(result.asset_id)));
  await workspace.getByRole('tab', { name: '素材库', exact: true }).click();
  const first = workspace.locator('.library-media-card').first(); await first.waitFor();
  assert(!(await workspace.getByRole('button', { name: '版本对比', exact: true }).isVisible()));
  for (const width of [1536, 825, 593]) {
    await page.setViewportSize({ width, height: 751 }); await first.scrollIntoViewIfNeeded();
    await first.getByRole('button', { name: /^素材操作：/ }).click();
    const menu = page.locator('.library-media-menu').getByRole('menu');
    assert(await menu.getByRole('menuitem', { name: /^(已加入发布素材|加入发布素材)$/ }).isVisible());
    assert(await menu.getByRole('link', { name: '下载素材', exact: true }).isVisible());
    assert(await menu.getByRole('menuitem', { name: '删除素材', exact: true }).isVisible());
    await first.getByRole('button', { name: /^素材操作：/ }).click();
    const metrics = await workspace.evaluate(el => ({ width: el.getBoundingClientRect().width, scrollWidth: el.scrollWidth, rootScrollWidth: document.documentElement.scrollWidth }));
    assert(metrics.scrollWidth <= metrics.width + 2, `workspace overflow at ${width}`);
    assert(metrics.rootScrollWidth <= width + 2, `page overflow at ${width}`);
    await page.screenshot({ path: path.join(folder, `edge-library-${width}.png`) }); checks.push({ width, ...metrics });
  }
  await workspace.getByRole('tab', { name: '图片仿写', exact: true }).click();
  await workspace.getByRole('button', { name: /新方案/ }).click();
  const dialog = page.getByRole('dialog', { name: '选择被仿写图', exact: true }); await dialog.waitFor();
  const search = dialog.getByRole('searchbox');
  const count = await dialog.locator('.picture-choice').count(); assert(count > 0);
  await search.fill('不存在的关键字__readonly');
  await page.getByText('没有符合条件的图片', { exact: true }).waitFor();
  assert.equal(await dialog.locator('.picture-choice').count(), 0);
  await search.fill(''); await dialog.locator('.picture-choice').first().waitFor();
  assert.equal(await dialog.locator('.picture-choice').count(), count);
  await dialog.getByRole('button', { name: '取消', exact: true }).click();
  assert.deepEqual((await detail()).rewrite, before.rewrite);
  assert.deepEqual(writes, []); assert.deepEqual(errors, []);
  const record = { checked_at: new Date().toISOString(), browser: 'Microsoft Edge', project_id: project, unadopted_results_excluded: unadopted.length, publication_candidates: cards.length, picker_choices: count, unchanged_project: true, writes, errors, checks };
  await writeFile(path.join(folder, 'checks.json'), JSON.stringify(record, null, 2)); process.stdout.write(JSON.stringify(record, null, 2));
} finally { await browser.close(); }
