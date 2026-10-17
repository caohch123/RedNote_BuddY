import { chromium } from 'playwright';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';
import assert from 'node:assert/strict';

// Runs only the exact, previously reviewed example. A persistent attempt file
// prevents this test helper from repeating a paid request after interruption.
const submit = process.argv.includes('--submit-confirmed-example');
const base = process.env.XHS_LIVE_URL || 'http://127.0.0.1:8080';
const folder = path.resolve('../data/verification/edge-image-example-20261011');
await mkdir(folder, { recursive: true });
const approved = JSON.parse(await readFile(path.join(folder, 'prepared-example.json'), 'utf8'));
const attemptPath = path.join(folder, 'generation-attempt.json');
let attempt; try { attempt = JSON.parse(await readFile(attemptPath, 'utf8')); } catch (error) { if (error.code !== 'ENOENT') throw error; }
const browser = await chromium.launch({ channel: 'msedge', headless: true });
try {
  const context = await browser.newContext({ viewport: { width: 1536, height: 1000 } });
  await context.addInitScript(() => {
    localStorage.setItem('workbench.workspace', 'xiaohongshu');
    localStorage.setItem('workbench.xiaohongshu.page', 'rewrites');
    localStorage.setItem('workbench.xiaohongshu.sidebarCollapsed', 'true');
  });
  const query = async endpoint => {
    const response = await context.request.get(`${base}/api/v1${endpoint}${endpoint.includes('?') ? '&' : '?'}platform=xiaohongshu`);
    assert(response.ok(), `GET ${endpoint}: ${response.status()}`); return response.json();
  };
  const detail = () => query(`/rewrites/${approved.project_id}`);
  const before = await detail(); let scheme = before.rewrite.image_rewrites.find(u => u.id === approved.scheme_id);
  assert(scheme && scheme.workflow_version === 2);
  const schemeIndex = before.rewrite.image_rewrites.indexOf(scheme);
  assert.equal(scheme.revision, approved.revision); assert.equal(scheme.primary_asset_id, approved.main_asset_id);
  assert.equal(scheme.prompt, approved.prompt); assert.equal(scheme.prompt_input_digest, approved.prompt_input_digest);
  assert.deepEqual(scheme.output, { ratio: '3:4', clarity: 'standard', count: 1 });
  assert.equal((scheme.references || []).length, 0); assert.deepEqual(scheme.feature_sources, approved.feature_sources);
  const generationPath = `/rewrites/${approved.project_id}/images/${approved.scheme_id}/generate`;
  const page = await context.newPage(); const errors = [], blocked = []; let submissions = 0;
  page.on('pageerror', error => errors.push(error.message));
  await page.route('**/api/v1/**', async route => {
    const request = route.request(), endpoint = new URL(request.url()).pathname.replace('/api/v1', '');
    if (['GET', 'HEAD'].includes(request.method())) return route.continue();
    if (!(submit && !attempt && !submissions && request.method() === 'POST' && endpoint === generationPath)) { blocked.push(endpoint); return route.abort(); }
    const body = request.postDataJSON();
    assert.equal(body.revision, approved.revision); assert.equal(body.prompt, approved.prompt); assert(body.request_id);
    attempt = { project_id: approved.project_id, scheme_id: approved.scheme_id, started_at: new Date().toISOString(), request_id: body.request_id, revision: body.revision, prompt: body.prompt, output: scheme.output, main_asset_id: scheme.primary_asset_id };
    await writeFile(attemptPath, JSON.stringify(attempt, null, 2), { flag: 'wx' });
    submissions++; await route.continue();
  });
  const workspace = page.locator('[data-workspace="xiaohongshu"]');
  async function openScheme() {
    await page.goto(base);
    await workspace.getByRole('menuitem', { name: /仿写库/ }).click();
    await workspace.locator('.rewrite-list-item').filter({ hasText: before.rewrite.title }).first().click();
    await workspace.getByRole('tab', { name: '图片仿写', exact: true }).click();
    await workspace.getByRole('combobox', { name: '图片方案' }).click();
    await page.getByRole('option', { name: `方案 ${schemeIndex + 1}`, exact: true }).click();
    await page.locator('.ant-select-dropdown:visible').waitFor({ state: 'hidden' });
  }
  await openScheme();
  if (!attempt) {
    assert(submit, 'Explicit submission flag required for the confirmed example');
    assert.equal((scheme.batches || []).length, 0); assert.equal(scheme.results.length, 0);
    await workspace.getByRole('button', { name: '2. 审阅提示词', exact: true }).click();
    assert.equal(await workspace.getByRole('textbox', { name: '生成提示词', exact: true }).inputValue(), approved.prompt);
    const responsePromise = page.waitForResponse(r => new URL(r.url()).pathname.endsWith(generationPath) && r.request().method() === 'POST');
    await workspace.getByRole('button', { name: '确认并生成 1 张', exact: true }).click();
    const response = await responsePromise; assert(response.ok(), `Submission rejected: ${response.status()} ${await response.text()}`);
    const { job_id } = await response.json(); assert(job_id); attempt.job_id = job_id;
    await writeFile(attemptPath, JSON.stringify(attempt, null, 2));
    console.log(`Submitted confirmed example once; job ${job_id}`);
  } else console.log('Existing attempt found; querying only, no submission');
  let batch; const deadline = Date.now() + 11 * 60_000; let lastState = '';
  while (Date.now() < deadline) {
    scheme = (await detail()).rewrite.image_rewrites.find(u => u.id === approved.scheme_id);
    batch = scheme.batches?.find(b => b.request_id === attempt.request_id);
    const state = batch ? `${batch.state}/${batch.items.map(i => i.state).join(',')}` : 'awaiting acknowledged batch';
    if (state !== lastState) { console.log(state); lastState = state; }
    if (batch && ['complete', 'paused', 'submission_unknown', 'cancelled'].includes(batch.state)) break;
    await new Promise(resolve => setTimeout(resolve, 2000));
  }
  const result = scheme.results.find(r => r.batch_id === batch?.id);
  await writeFile(path.join(folder, 'generation-checks.json'), JSON.stringify({ browser: 'Microsoft Edge', request_id: attempt.request_id, submissionsThisRun: submissions, batch, result, schemeStatus: scheme.status, errors, blocked, generated: !!result, adopted: !!scheme.applied_version_id }, null, 2));
  assert(batch, 'No acknowledged batch; do not resubmit');
  assert.equal(batch.state, 'complete', scheme.error || batch.items[0]?.error || 'Generation did not complete; do not automatically retry');
  assert.equal(batch.items.length, 1); assert.equal(batch.items[0].state, 'succeeded'); assert(result);
  assert.deepEqual(batch.output, approved.output); assert.equal(batch.prompt, approved.prompt);
  assert.deepEqual(batch.input_asset_ids, [approved.main_asset_id]); assert.equal(batch.input_hashes.length, 1);
  assert.equal(result.prompt, approved.prompt); assert.deepEqual(result.source_asset_ids, [approved.main_asset_id]);
  assert.deepEqual(result.input_hashes, batch.input_hashes); assert.equal(result.width, 960); assert.equal(result.height, 1280); assert(!result.warning);
  assert(!scheme.applied_version_id, 'Example must remain unadopted');
  const after = await detail(); assert.deepEqual(after.rewrite.versions, before.rewrite.versions);
  assert.equal(after.rewrite.confirmed_version_id, before.rewrite.confirmed_version_id);
  const library = await query(`/media-library?rewrite_id=${approved.project_id}`);
  const saved = library.items.find(item => item.id === result.asset_id); assert(saved && saved.origin === 'image_rewrite' && saved.rewrite_id === approved.project_id);
  await openScheme(); await workspace.getByRole('button', { name: '3. 审阅结果', exact: true }).click();
  const image = workspace.getByRole('img', { name: '图片仿写结果', exact: true });
  await image.waitFor(); const actual = await image.evaluate(async img => { await img.decode(); return [img.naturalWidth, img.naturalHeight]; });
  assert.deepEqual(actual, [960, 1280]);
  await workspace.getByRole('button', { name: '本次生成依据', exact: true }).click();
  await workspace.getByRole('img', { name: '本次被仿写图', exact: true }).waitFor();
  assert.equal((await workspace.locator('.image-result-card .preserve').innerText()).trim(), approved.prompt.trim());
  for (const width of [1536, 825, 597]) {
    await page.setViewportSize({ width, height: 1000 }); await image.scrollIntoViewIfNeeded();
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1));
    await page.screenshot({ path: path.join(folder, `generated-result-${width}.png`), animations: 'disabled' });
  }
  assert.deepEqual(errors, []); assert.deepEqual(blocked, []);
  await writeFile(path.join(folder, 'generation-accepted.json'), JSON.stringify({ browser: 'Microsoft Edge', widths: [1536, 825, 597], asset_id: result.asset_id, batch_id: batch.id, job_id: result.job_id, provider_request_id: result.provider_request_id, input_asset_ids: batch.input_asset_ids, output: batch.output, actualDimensions: actual, promptMatchesConfirmed: true, savedToProjectLibrary: true, versionsPreserved: true, adopted: false, published: false, submissionsThisRun: submissions, errors, blocked }, null, 2));
  console.log(`Generated and saved asset ${result.asset_id}; actual 960x1280; not adopted`);
  await context.close();
} finally { await browser.close(); }
