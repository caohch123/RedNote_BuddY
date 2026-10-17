import { chromium } from 'playwright';
import assert from 'node:assert/strict';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';

const base = 'http://127.0.0.1:8080';
const project = process.env.XHS_VERIFY_PROJECT || '8c95adf3a7718829577572f5d7e045ca';
const folder = path.resolve('../data/verification/image-scheme-init-20261013', project);
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
    const response = await context.request.get(`${base}/api/v1/rewrites/${project}?platform=xiaohongshu`);
    assert(response.ok()); return response.json();
  };
  const jobs = async () => {
    const response = await context.request.get(`${base}/api/v1/jobs?platform=xiaohongshu`);
    assert(response.ok()); return (await response.json()).items.filter(j => j.entity_id === project);
  };
  const before = await detail(), beforeJobs = await jobs();
  await writeFile(path.join(folder, 'before.json'), JSON.stringify(before.rewrite, null, 2), { flag: 'wx' }).catch(error => { if (error.code !== 'EEXIST') throw error; });
  const originalBefore = JSON.parse(await readFile(path.join(folder, 'before.json'), 'utf8'));
  const expected = before.note.asset_ids.filter(id => before.assets.some(a => a.id === id && a.owner_id === before.note.id && a.status === 'ready' && !a.library_deleted_at && a.kind !== 'style_reference' && a.mime.startsWith('image/')));
  assert.equal(expected.length, 14);
  const page = await context.newPage(); const writes = [], errors = [], checks = [];
  page.on('pageerror', e => errors.push(e.message));
  await page.route('**/api/v1/**', route => {
    if (['GET', 'HEAD'].includes(route.request().method())) return route.continue();
    const pathname = new URL(route.request().url()).pathname;
    writes.push(pathname);
    if (pathname === `/api/v1/rewrites/${project}/images/initialize`) return route.continue();
    return route.abort();
  });
  const workspace = page.locator('[data-workspace="xiaohongshu"]');
  async function openImages() {
    await page.goto(base);
    const response = await context.request.get(`${base}/api/v1/rewrites?platform=xiaohongshu`);
    const list = await response.json(); const index = list.items.findIndex(item => item.id === project);
    assert(index >= 0); await workspace.locator('.rewrite-list-item').nth(index).click();
    await workspace.getByRole('tab', { name: '图片仿写', exact: true }).click();
    await workspace.getByRole('img', { name: '被仿写图', exact: true }).waitFor();
    await assert.doesNotReject(() => workspace.locator('.image-scheme-toolbar').getByText('方案 1', { exact: true }).waitFor());
  }
  await openImages();
  const afterInit = await detail();
  assert.deepEqual(afterInit.rewrite.image_rewrites.map(u => u.primary_asset_id), expected);
  assert.deepEqual(afterInit.rewrite.versions, before.rewrite.versions);
  assert.deepEqual(afterInit.rewrite.options, before.rewrite.options);
  for (const unit of afterInit.rewrite.image_rewrites) {
    assert.equal(unit.status, 'draft'); assert.equal(unit.revision, 1);
    assert.equal((unit.results || []).length, 0); assert.equal((unit.batches || []).length, 0); assert.equal((unit.analyses || []).length, 0);
  }
  for (const width of [1536, 825, 597, 498]) {
    await page.setViewportSize({ width, height: 751 });
    const main = workspace.getByRole('img', { name: '被仿写图', exact: true });
    assert((await main.getAttribute('src')).includes(expected[0]));
    await workspace.getByRole('combobox', { name: '图片方案' }).click();
    const labels = await page.getByRole('option').allTextContents();
    assert.deepEqual(labels, expected.map((_, i) => `方案 ${i + 1}`));
    await page.getByRole('option', { name: '方案 14', exact: true }).click();
    await main.waitFor();
    assert((await main.getAttribute('src')).includes(expected[13]));
    await page.waitForTimeout(2300);
    assert((await main.getAttribute('src')).includes(expected[13]), 'polling must preserve user selection');
    await workspace.getByRole('combobox', { name: '图片方案' }).click();
    await page.getByRole('option', { name: '方案 1', exact: true }).click();
    await page.locator('.ant-select-dropdown:visible').waitFor({ state: 'hidden' });
    await main.scrollIntoViewIfNeeded();
    const metrics = await page.evaluate(() => ({ rootWidth: document.documentElement.scrollWidth, viewport: innerWidth }));
    assert(metrics.rootWidth <= width + 2);
    await page.screenshot({ path: path.join(folder, `edge-first-scheme-${width}.png`) });
    checks.push({ width, scheme_count: labels.length, first_asset_id: expected[0], last_asset_id: expected[13], ...metrics });
  }
  await openImages();
  assert((await workspace.getByRole('img', { name: '被仿写图', exact: true }).getAttribute('src')).includes(expected[0]));
  const tokenResponse = await context.request.get(`${base}/api/v1/session`);
  const { csrf_token: token } = await tokenResponse.json();
  const repeated = await context.request.post(`${base}/api/v1/rewrites/${project}/images/initialize?platform=xiaohongshu`, { headers: { 'X-CSRF-Token': token } });
  assert(repeated.ok());
  assert.deepEqual(await repeated.json(), afterInit.rewrite.image_rewrites);
  assert.deepEqual((await detail()).rewrite, afterInit.rewrite);
  assert.deepEqual(await jobs(), beforeJobs); assert.deepEqual(errors, []);
  const record = { checked_at: new Date().toISOString(), browser: 'Microsoft Edge', project_id: project, initialized_from: originalBefore.image_rewrites?.length || 0, initialized_to: 14, default_scheme: 1, ordered_asset_ids: expected, unchanged_versions: true, unchanged_jobs: true, idempotent: true, writes, errors, checks };
  await writeFile(path.join(folder, 'checks.json'), JSON.stringify(record, null, 2));
  process.stdout.write(JSON.stringify(record, null, 2));
} finally { await browser.close(); }
