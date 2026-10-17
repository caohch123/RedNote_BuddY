import { chromium } from 'playwright';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';
import assert from 'node:assert/strict';

// Live Edge acceptance for the user's existing main + auxiliary example.
// Composition and one-image submission have separate durable attempt records.
// Rerunning a stage only queries its existing task; it never resubmits it.
const stage = process.argv[2] || 'inspect';
assert(['inspect', 'compose', 'generate'].includes(stage));
const base = process.env.XHS_LIVE_URL || 'http://127.0.0.1:8080';
const folder = path.resolve(process.env.VERIFICATION_DIR || '../data/verification/edge-auxiliary-subject-example-20261011');
const projectID = '3e15dd3277739627464574280ad8981c';
const schemeID = 'dca73bfb92a92807939c5e9ead0235de';
const mainID = '309d57353414663df7076e1113f47db6';
const auxiliaryID = 'a20c8cc02068ec7ecdab3c6d458cd6f5';
const featureSources = Object.fromEntries(['subject', 'scene', 'composition', 'palette', 'lighting', 'texture', 'text'].map(key => [key, key === 'subject' ? auxiliaryID : mainID]));
const expectedOutput = { ratio: '3:4', clarity: 'standard', count: 1 };
await mkdir(folder, { recursive: true });
const readOptional = async name => { try { return JSON.parse(await readFile(path.join(folder, name), 'utf8')); } catch (error) { if (error.code !== 'ENOENT') throw error; } };
const save = (name, data, exclusive = false) => writeFile(path.join(folder, name), JSON.stringify(data, null, 2), exclusive ? { flag: 'wx' } : undefined);
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
  const detail = () => query(`/rewrites/${projectID}`);
  const before = await detail(); let scheme = before.rewrite.image_rewrites.find(u => u.id === schemeID);
  assert(scheme?.workflow_version === 2);
  const schemeIndex = before.rewrite.image_rewrites.indexOf(scheme);
  assert.equal(scheme.primary_asset_id, mainID);
  assert.deepEqual(scheme.references, [{ asset_id: auxiliaryID }]);
  assert.deepEqual(scheme.feature_sources, featureSources);
  assert.deepEqual(scheme.output, expectedOutput);
  assert(scheme.analyses.some(a => a.asset_id === mainID));
  assert(scheme.analyses.some(a => a.asset_id === auxiliaryID));
  const baseline = await readOptional('baseline.json') || before;
  if (!(await readOptional('baseline.json'))) await save('baseline.json', baseline, true);
  const unitPath = `/rewrites/${projectID}/images/${schemeID}`;
  const attemptName = stage === 'compose' ? 'compose-attempt.json' : 'generation-attempt.json';
  let attempt = stage === 'inspect' ? undefined : await readOptional(attemptName);
  const resumedAttempt = !!attempt;
  const prepared = stage === 'generate' ? await readOptional('prepared-example.json') : undefined;
  if (stage === 'generate') {
    assert(prepared, 'Compose and review the current example first');
    assert.equal(scheme.revision, prepared.revision);
    assert.equal(scheme.prompt, prepared.prompt);
    assert.equal(scheme.prompt_input_digest, prepared.prompt_input_digest);
    assert(scheme.prompt_rules_version >= 4);
  }
  const page = await context.newPage(); const errors = [], writes = [], blocked = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.route('**/api/v1/**', async route => {
    const request = route.request(), endpoint = new URL(request.url()).pathname.replace('/api/v1', '');
    if (['GET', 'HEAD'].includes(request.method())) return route.continue();
    const allowedPath = unitPath + (stage === 'compose' ? '/compose-prompt' : '/generate');
    if (stage === 'inspect' || attempt || writes.length || request.method() !== 'POST' || endpoint !== allowedPath) { blocked.push(endpoint); return route.abort(); }
    const body = request.postDataJSON(); assert.equal(body.revision, scheme.revision);
    if (stage === 'generate') { assert.equal(body.prompt, prepared.prompt); assert(body.request_id); }
    attempt = { stage, started_at: new Date().toISOString(), project_id: projectID, scheme_id: schemeID, ...body };
    await save(attemptName, attempt, true);
    writes.push({ endpoint, body }); await route.continue();
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
  const startStep = stage === 'generate' ? 1 : 0;
  await workspace.getByRole('button', { name: ['1. 选图与特征', '2. 审阅提示词'][startStep], exact: true }).click();
  if (stage !== 'inspect' && !attempt) {
    if (stage === 'generate') assert.equal(await workspace.getByRole('textbox', { name: '生成提示词', exact: true }).inputValue(), prepared.prompt);
    const endpoint = unitPath + (stage === 'compose' ? '/compose-prompt' : '/generate');
    const responsePromise = page.waitForResponse(r => new URL(r.url()).pathname === '/api/v1' + endpoint && r.request().method() === 'POST');
    await workspace.getByRole('button', { name: stage === 'compose' ? '生成提示词' : '确认并生成 1 张', exact: true }).click();
    const response = await responsePromise;
    assert(response.ok(), `Submission rejected: ${response.status()} ${await response.text()}`);
    attempt.job_id = (await response.json()).job_id; assert(attempt.job_id);
    await save(attemptName, attempt);
    console.log(`Submitted ${stage} once; job ${attempt.job_id}`);
  } else if (attempt) console.log('Existing attempt: query only, no resubmission');
  if (stage !== 'inspect') {
    assert(attempt.job_id, 'Acknowledgement unknown: inspect the existing task; do not resubmit');
    const deadline = Date.now() + (stage === 'generate' ? 11 * 60_000 : 180_000); let last = '', completed = false;
    while (Date.now() < deadline) {
      const { items: jobs } = await query('/jobs'); const job = jobs.find(j => j.id === attempt.job_id);
      if (job?.state !== last) { console.log(`Task ${job?.state || 'awaiting'}`); last = job?.state; }
      if (job && !['queued', 'running'].includes(job.state)) {
        await save(`${stage}-job.json`, job);
        assert.equal(job.state, 'succeeded', job.error || job.state); completed = true; break;
      }
      await new Promise(resolve => setTimeout(resolve, 2000));
    }
    assert(completed, 'Task is still pending or missing: query the existing attempt later; do not resubmit');
    scheme = (await detail()).rewrite.image_rewrites.find(u => u.id === schemeID);
  }
  assert.deepEqual(scheme.feature_sources, featureSources); assert.deepEqual(scheme.output, expectedOutput);
  const after = await detail();
  assert.deepEqual(after.rewrite.versions, baseline.rewrite.versions, 'Text/publishing selection changed');
  assert.equal(after.rewrite.confirmed_version_id, baseline.rewrite.confirmed_version_id);
  for (const old of baseline.rewrite.image_rewrites.find(u => u.id === schemeID).results) assert.deepEqual(scheme.results.find(r => r.asset_id === old.asset_id), old, 'Historical result changed');
  if (stage === 'compose') {
    assert(scheme.prompt_input_digest && scheme.prompt_rules_version >= 4);
    for (const part of ['少女', '女仆', '挥手', '张开', '尾鳍']) assert(scheme.prompt.includes(part), `Chosen auxiliary subject/action missing: ${part}`);
    for (const constraint of ['最终配色以图片1为准', '最终画法与材质以图片1为准', '不添加文字、标志或水印']) assert(scheme.prompt.includes(constraint), `Missing rendering constraint: ${constraint}`);
    assert(!/WORKBUDD|AI生成/.test(scheme.prompt), 'Old OCR watermark present');
    await save('prepared-example.json', { project_id: projectID, scheme_id: schemeID, revision: scheme.revision, main_asset_id: mainID, auxiliary_asset_id: auxiliaryID, feature_sources: featureSources, prompt: scheme.prompt, prompt_input_digest: scheme.prompt_input_digest, prompt_rules_version: scheme.prompt_rules_version, output: scheme.output });
    console.log(JSON.stringify({ revision: scheme.revision, prompt: scheme.prompt, feature_sources: scheme.feature_sources }, null, 2));
    await openScheme(); await workspace.getByRole('button', { name: '2. 审阅提示词', exact: true }).click();
    assert.equal(await workspace.getByRole('textbox', { name: '生成提示词', exact: true }).inputValue(), scheme.prompt);
    assert(await workspace.getByRole('button', { name: '确认并生成 1 张', exact: true }).isEnabled());
  }
  let batch, result;
  if (stage === 'generate') {
    batch = scheme.batches.find(b => b.request_id === attempt.request_id);
    assert.equal(batch?.state, 'complete', 'Generation incomplete: do not automatically retry');
    assert.equal(batch.items.length, 1); assert.equal(batch.items[0].state, 'succeeded');
    result = scheme.results.find(r => r.batch_id === batch.id); assert(result);
    assert.equal(batch.prompt, prepared.prompt); assert.equal(result.prompt, prepared.prompt);
    assert.deepEqual(batch.feature_sources, featureSources); assert.deepEqual(result.feature_sources, featureSources);
    assert.deepEqual(batch.input_asset_ids, [mainID, auxiliaryID]); assert.deepEqual(result.source_asset_ids, [mainID, auxiliaryID]);
    assert.deepEqual(batch.input_hashes, [mainID, auxiliaryID].map(id => before.assets.find(a => a.id === id).sha256));
    assert.deepEqual(result.input_hashes, batch.input_hashes); assert.deepEqual(batch.output, expectedOutput);
    assert.equal(result.width, 960); assert.equal(result.height, 1280); assert(!result.warning);
    assert.equal(scheme.applied_version_id, baseline.rewrite.image_rewrites.find(u => u.id === schemeID).applied_version_id);
    const library = await query(`/media-library?rewrite_id=${projectID}`);
    assert(library.items.some(a => a.id === result.asset_id && a.origin === 'image_rewrite' && a.rewrite_id === projectID));
    await openScheme(); await workspace.getByRole('button', { name: '3. 审阅结果', exact: true }).click();
    const card = workspace.locator('.image-result-card').filter({ has: page.locator(`img[src*="${result.asset_id}"]`) });
    await card.getByRole('img', { name: '图片仿写结果', exact: true }).evaluate(img => img.decode());
    assert.deepEqual(await card.getByRole('img', { name: '图片仿写结果', exact: true }).evaluate(img => [img.naturalWidth, img.naturalHeight]), [960, 1280]);
    await card.getByRole('button', { name: '本次生成依据', exact: true }).click();
    assert.equal((await card.locator('.preserve').innerText()).trim(), prepared.prompt.trim());
    assert.match(await card.locator('.image-result-feature-sources').innerText(), /主体与动作：辅助图 1/);
    console.log(JSON.stringify({ asset_id: result.asset_id, batch_id: batch.id, provider_request_id: result.provider_request_id, dimensions: [960, 1280], unadopted: true }, null, 2));
  }
  const bounds = [];
  for (const width of [1536, 825, 597, 569]) {
    await page.setViewportSize({ width, height: 751 });
    const dock = workspace.locator('.workspace-action-dock'); await dock.waitFor();
    const footer = await dock.evaluate(el => { const b = el.getBoundingClientRect(), c = el.closest('.ant-layout').querySelector('.main-content').getBoundingClientRect(); return { bottom: b.bottom, top: b.top, contentBottom: c.bottom, viewportBottom: innerHeight }; });
    assert(Math.abs(footer.bottom - footer.viewportBottom) <= 1); assert(Math.abs(footer.top - footer.contentBottom) <= 1);
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1));
    assert.equal(await workspace.locator('.image-text-confirm').count(), 0);
    bounds.push({ width, ...footer });
    await page.screenshot({ path: path.join(folder, `${stage}-${width}.png`), animations: 'disabled' });
  }
  assert.deepEqual(errors, []); assert.deepEqual(blocked, []);
  await save(`${stage}-${resumedAttempt ? 'resumed-' : ''}checks.json`, { browser: 'Microsoft Edge', stage, resumedAttempt, bounds, writes, errors, blocked, batch, result, versionsPreserved: true, historicalResultsPreserved: true, savedToProjectLibrary: !!result, adopted: false, published: false });
  await context.close();
} finally { await browser.close(); }
