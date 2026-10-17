import { chromium } from 'playwright';
import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import path from 'node:path';

// Production compatibility check only. Model calls and project mutations are
// blocked; new editing interactions are exercised with the mock Edge suite.
const base = 'http://127.0.0.1:8080';
const project = '3e15dd3277739627464574280ad8981c';
const folder = path.resolve('../data/verification/flexible-live-20261012');
await mkdir(folder, { recursive: true });
const browser = await chromium.launch({ channel: 'msedge', headless: true });
let checkedPage;
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
  const before = await detail();
  const capabilities = await context.request.get(`${base}/api/v1/image-capabilities?platform=xiaohongshu`).then(r => r.json());
  assert.equal(capabilities.prompt_rules_version, 6);
  const page = await context.newPage();
  checkedPage = page;
  const writes = [], errors = [], checks = [];
  page.on('pageerror', e => errors.push(e.message));
  await page.route('**/api/v1/**', async route => {
    if (['GET', 'HEAD'].includes(route.request().method())) return route.continue();
    writes.push(new URL(route.request().url()).pathname); return route.abort();
  });
  await page.goto(base);
  const workspace = page.locator('[data-workspace="xiaohongshu"]');
  await workspace.getByRole('menuitem', { name: /仿写库/ }).click();
  await workspace.locator('.rewrite-list-item').filter({ hasText: before.rewrite.title }).first().click();
  await workspace.getByRole('tab', { name: '图片仿写', exact: true }).click();
  const legacy = before.rewrite.image_rewrites.find(u => u.edit_intent?.mode !== 'flexible');
  assert(legacy, 'compatibility fixture requires an existing legacy scheme');
  const index = before.rewrite.image_rewrites.indexOf(legacy);
  await workspace.getByRole('combobox', { name: '图片方案' }).click();
  await page.getByRole('option', { name: `方案 ${index + 1} · 历史方案`, exact: true }).click();
  await workspace.getByRole('button', { name: '原方案提示词', exact: true }).click();
  await assert.doesNotReject(() => workspace.getByText(legacy.prompt, { exact: true }).waitFor());
  for (const width of [1536, 825, 597]) {
    await page.setViewportSize({ width, height: 1000 });
    await workspace.getByRole('button', { name: /以此创建新版方案/ }).scrollIntoViewIfNeeded();
    const metrics = await workspace.evaluate(el => ({ width: el.getBoundingClientRect().width, scrollWidth: el.scrollWidth, rootScrollWidth: document.documentElement.scrollWidth, viewportWidth: innerWidth }));
    assert(metrics.scrollWidth <= metrics.width + 2, `workspace overflow at ${width}`);
    assert(metrics.rootScrollWidth <= width + 2, `root overflow at ${width}`);
    assert.equal(await workspace.getByRole('button', { name: '保留主图内容', exact: true }).count(), 0);
    assert.equal(await workspace.getByRole('button', { name: '自由组合特征', exact: true }).count(), 0);
    await page.screenshot({ path: path.join(folder, `edge-${width}.png`) });
    checks.push({ width, ...metrics });
  }
  const after = await detail();
  assert.deepEqual(after.rewrite, before.rewrite);
  assert.equal(writes.length, 0, 'viewing attempted a mutation');
  assert.equal(errors.length, 0, 'page raised an error');
  const record = { checked_at: new Date().toISOString(), browser: 'Microsoft Edge', rules_version: capabilities.prompt_rules_version, project_id: project, legacy_scheme_id: legacy.id, revision: legacy.revision, historical_results: legacy.results.length, unchanged_project: true, writes, errors, checks };
  await writeFile(path.join(folder, 'checks.json'), JSON.stringify(record, null, 2));
  process.stdout.write(JSON.stringify(record, null, 2));
} catch (error) {
  if (checkedPage) {
    await checkedPage.screenshot({ path: path.join(folder, 'check-failure.png') }).catch(() => {});
    process.stderr.write(JSON.stringify({ buttons: await checkedPage.getByRole('button').allTextContents(), url: checkedPage.url() }) + '\n');
  }
  throw error;
} finally { await browser.close(); }
