import { chromium } from 'playwright';
import { mkdir, writeFile } from 'node:fs/promises';
import path from 'node:path';
import assert from 'node:assert/strict';

// Read-only live acceptance. Every browser API mutation is blocked.
const base = process.env.XHS_LIVE_URL || 'http://127.0.0.1:8080';
const output = path.resolve(process.env.VERIFICATION_DIR || '../data/verification/edge-image-workflow-v2-20261011');
await mkdir(output, { recursive: true });
const browser = await chromium.launch({ channel: 'msedge', headless: true });
try {
  const context = await browser.newContext({ viewport: { width: 1536, height: 900 } });
  await context.addInitScript(() => {
    localStorage.setItem('workbench.workspace', 'xiaohongshu');
    localStorage.setItem('workbench.xiaohongshu.page', 'rewrites');
    localStorage.setItem('workbench.xiaohongshu.sidebarCollapsed', 'true');
  });
  const capabilitiesResponse = await context.request.get(`${base}/api/v1/image-capabilities?platform=xiaohongshu`);
  assert(capabilitiesResponse.ok(), 'V2 capability endpoint unavailable');
  const capabilities = await capabilitiesResponse.json();
  assert.equal(capabilities.max_count, 4); assert.equal(capabilities.max_inputs, 3); assert.equal(capabilities.sizes.length, 6);
  assert(!JSON.stringify(capabilities).includes('api_key'), 'Capability endpoint exposed credentials');
  const response = await context.request.get(`${base}/api/v1/rewrites?platform=xiaohongshu`);
  assert(response.ok()); const { items } = await response.json();
  const project = items.find(r => r.image_rewrites?.some(u => u.workflow_version !== 2 && u.results?.length));
  assert(project, 'A legacy project is required for compatibility acceptance');
  const scheme = project.image_rewrites.find(u => u.workflow_version !== 2 && u.results?.length);
  const index = project.image_rewrites.indexOf(scheme);
  const page = await context.newPage(); const writes = []; const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.route('**/api/v1/**', async route => {
    if (!['GET', 'HEAD'].includes(route.request().method())) { writes.push(new URL(route.request().url()).pathname); await route.abort(); }
    else await route.continue();
  });
  await page.goto(base);
  const workspace = page.locator('[data-workspace="xiaohongshu"]');
  await workspace.getByRole('menuitem', { name: /仿写库/ }).click();
  await workspace.locator('.rewrite-list-item').filter({ hasText: project.title }).first().click();
  await workspace.getByRole('tab', { name: '图片仿写', exact: true }).click();
  await workspace.getByRole('combobox', { name: '图片方案' }).click();
  await page.getByRole('option', { name: `方案 ${index + 1} · 历史方案`, exact: true }).click();
  await workspace.getByRole('button', { name: /确认主图并新建方案/ }).waitFor();
  await workspace.getByRole('button', { name: '原方案提示词' }).click();
  assert.equal((await workspace.locator('.image-legacy-view .preserve').first().innerText()).trim(), scheme.prompt.trim());
  const card = workspace.locator('.image-legacy-view .image-result-card').first();
  await card.getByRole('button', { name: '本次生成依据' }).click();
  assert.equal((await card.locator('.preserve').innerText()).trim(), scheme.results[0].prompt.trim());
  assert.equal(await card.getByRole('button', { name: '采用此图', exact: true }).count(), 0);
  for (const width of [1536, 825, 597]) {
    await page.setViewportSize({ width, height: 900 });
    await workspace.getByRole('combobox', { name: '图片方案' }).scrollIntoViewIfNeeded();
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), `Overflow at ${width}`);
    await page.screenshot({ path: path.join(output, `legacy-compatible-${width}.png`) });
  }
  const image = card.getByRole('img', { name: '图片仿写结果', exact: true });
  await image.scrollIntoViewIfNeeded();
  await image.evaluate(async img => { if (!img.complete) await new Promise(resolve => { img.onload = resolve; img.onerror = resolve; }); });
  assert(await image.evaluate(img => img.complete && img.naturalWidth > 0), 'Saved image unavailable');
  assert.deepEqual(errors, []); assert.deepEqual(writes, []);
  const checks = { browser: 'Microsoft Edge', widths: [1536, 825, 597], capabilityEndpoint: true, sizeCombinations: 6, maxCount: 4,
    legacyPromptUnchanged: true, resultPromptMatchesSavedRequest: true, legacyApplyBlocked: true, savedImageAvailable: true, writes: writes.length, pageErrors: errors.length };
  await writeFile(path.join(output, 'live-checks.json'), JSON.stringify(checks, null, 2));
  console.log(JSON.stringify(checks));
  await context.close();
} finally { await browser.close(); }
