import { chromium } from 'playwright';
import { readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';
import assert from 'node:assert/strict';

// Reversible review only: one exact autosave PATCH, no model or adoption calls.
const folder = path.resolve(process.env.VERIFICATION_DIR || '../data/verification/edge-auxiliary-subject-example-20261011/retry-3');
const prepared = JSON.parse(await readFile(path.join(folder, 'prepared-example.json'), 'utf8'));
const prompt = (await readFile(path.join(folder, 'reviewed-prompt.txt'), 'utf8')).trim();
assert(prompt && [...prompt].length < 4000);
const base = process.env.XHS_LIVE_URL || 'http://127.0.0.1:8080';
const browser = await chromium.launch({ channel: 'msedge', headless: true });
try {
  const context = await browser.newContext({ viewport: { width: 1536, height: 1000 } });
  await context.addInitScript(() => {
    localStorage.setItem('workbench.workspace', 'xiaohongshu');
    localStorage.setItem('workbench.xiaohongshu.page', 'rewrites');
    localStorage.setItem('workbench.xiaohongshu.sidebarCollapsed', 'true');
  });
  const detailURL = `${base}/api/v1/rewrites/${prepared.project_id}?platform=xiaohongshu`;
  const before = await (await context.request.get(detailURL)).json();
  const scheme = before.rewrite.image_rewrites.find(u => u.id === prepared.scheme_id);
  assert.equal(scheme.revision, prepared.revision); assert.equal(scheme.prompt, prepared.prompt);
  assert.equal(scheme.prompt_input_digest, prepared.prompt_input_digest);
  assert.deepEqual(scheme.feature_sources, prepared.feature_sources);
  assert(scheme.prompt_rules_version >= 4);
  const page = await context.newPage(), writes = [], blocked = [], errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.route('**/api/v1/**', async route => {
    const request = route.request();
    if (['GET', 'HEAD'].includes(request.method())) return route.continue();
    const endpoint = new URL(request.url()).pathname;
    if (writes.length || request.method() !== 'PATCH' || endpoint !== `/api/v1/rewrites/${prepared.project_id}/images/${prepared.scheme_id}`) { blocked.push(endpoint); return route.abort(); }
    const body = request.postDataJSON();
    assert.equal(body.revision, prepared.revision); assert.equal(body.prompt, prompt);
    assert.equal(body.primary_asset_id, scheme.primary_asset_id);
    assert.deepEqual(body.references, scheme.references); assert.deepEqual(body.feature_sources, scheme.feature_sources); assert.deepEqual(body.output, scheme.output);
    const review = { browser: 'Microsoft Edge', reviewed_at: new Date().toISOString(), original_composed_prompt: prepared.prompt, reviewed_prompt: prompt, corrections: ['Render all foreground in white outlines; source blue colors do not override selected main palette', 'Use visible screen-relative hand positions from auxiliary image'], modelCalls: 0, request: body };
    await writeFile(path.join(folder, 'prompt-review.json'), JSON.stringify(review, null, 2));
    writes.push(endpoint); await route.continue();
  });
  const workspace = page.locator('[data-workspace="xiaohongshu"]');
  await page.goto(base);
  await workspace.getByRole('menuitem', { name: /仿写库/ }).click();
  await workspace.locator('.rewrite-list-item').filter({ hasText: before.rewrite.title }).first().click();
  await workspace.getByRole('tab', { name: '图片仿写', exact: true }).click();
  await workspace.getByRole('combobox', { name: '图片方案' }).click();
  await page.getByRole('option', { name: `方案 ${before.rewrite.image_rewrites.indexOf(scheme) + 1}`, exact: true }).click();
  await workspace.getByRole('button', { name: '2. 审阅提示词', exact: true }).click();
  const textbox = workspace.getByRole('textbox', { name: '生成提示词', exact: true });
  assert.equal(await textbox.inputValue(), prepared.prompt);
  if (prompt !== prepared.prompt) {
    const responsePromise = page.waitForResponse(r => r.request().method() === 'PATCH' && new URL(r.url()).pathname === `/api/v1/rewrites/${prepared.project_id}/images/${prepared.scheme_id}`);
    await textbox.fill(prompt);
    const response = await responsePromise; assert(response.ok(), `Save failed: ${response.status()}`);
  }
  const after = await (await context.request.get(detailURL)).json();
  const saved = after.rewrite.image_rewrites.find(u => u.id === prepared.scheme_id);
  assert.equal(saved.prompt, prompt); assert.equal(saved.prompt_input_digest, prepared.prompt_input_digest);
  assert.deepEqual(saved.feature_sources, scheme.feature_sources); assert.deepEqual(saved.results, scheme.results); assert.deepEqual(saved.batches, scheme.batches);
  assert.deepEqual(after.rewrite.versions, before.rewrite.versions); assert.deepEqual(errors, []); assert.deepEqual(blocked, []);
  await writeFile(path.join(folder, 'prepared-example.json'), JSON.stringify({ ...prepared, revision: saved.revision, prompt: saved.prompt, reviewed: true }, null, 2));
  await page.screenshot({ path: path.join(folder, 'reviewed-prompt-edge.png'), fullPage: true });
  console.log(JSON.stringify({ revision: saved.revision, prompt_input_digest: saved.prompt_input_digest, writes, modelCalls: 0, versionsPreserved: true, generated: false }, null, 2));
} finally { await browser.close(); }
