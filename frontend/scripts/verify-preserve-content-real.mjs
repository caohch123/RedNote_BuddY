import { chromium } from 'playwright';
import assert from 'node:assert/strict';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';

// One user-authorized real test, with durable records before any model POST.
// Rerunning a submitted stage only queries the original task.
const stage = process.argv[2] || 'prepare';
assert(['prepare', 'compose', 'review', 'generate'].includes(stage));
const base = 'http://127.0.0.1:8080';
const projectID = '3e15dd3277739627464574280ad8981c';
const schemeID = 'dca73bfb92a92807939c5e9ead0235de';
const mainID = '64bf8dc524934e286fbbfce7ae48a8e2';
const unitPath = `/rewrites/${projectID}/images/${schemeID}`;
const folder = path.resolve(process.env.VERIFICATION_DIR || '../data/verification/preserve-content-real-20261012');
const expectedIntent = { mode: 'preserve_content', style: '数字插画', palette: '蓝白' };
const expectedOutput = { ratio: '3:4', clarity: 'standard', count: 1 };
await mkdir(folder, { recursive: true });
const readOptional = async name => { try { return JSON.parse(await readFile(path.join(folder, name), 'utf8')); } catch (e) { if (e.code !== 'ENOENT') throw e; } };
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
  const before = await detail();
  let scheme = before.rewrite.image_rewrites.find(u => u.id === schemeID);
  const schemeNumber = before.rewrite.image_rewrites.findIndex(u => u.id === schemeID) + 1;
  assert(schemeNumber > 0);
  assert.equal(scheme.primary_asset_id, mainID);
  assert.equal(scheme.workflow_version, 2);
  const capability = await query('/image-capabilities');
  assert.equal(capability.prompt_rules_version, 5);
  assert(capability.text_configured && capability.image_configured && capability.supported);
  const baseline = await readOptional('baseline.json') || before;
  if (!(await readOptional('baseline.json'))) await save('baseline.json', baseline, true);
  const attemptName = `${stage}-attempt.json`;
  let attempt = ['compose', 'generate'].includes(stage) ? await readOptional(attemptName) : undefined;
  const resumed = !!attempt;
  const prepared = ['review', 'generate'].includes(stage) ? await readOptional('prepared.json') : undefined;
  let reviewedPrompt;
  if (stage === 'review') {
    assert(prepared); reviewedPrompt = (await readFile(path.join(folder, 'reviewed-prompt.txt'), 'utf8')).trim();
    assert(reviewedPrompt && [...reviewedPrompt].length < 4000);
  }
  if (stage !== 'prepare') {
    assert.deepEqual(scheme.edit_intent, expectedIntent);
    assert.deepEqual(scheme.output, expectedOutput);
    for (const key of ['subject', 'scene', 'composition', 'text']) assert.equal(scheme.feature_sources[key], mainID);
  }
  if (stage === 'generate' && !attempt) {
    assert(prepared?.reviewed, 'Review the exact prompt before generating');
    assert.equal(scheme.revision, prepared.revision);
    assert.equal(scheme.prompt, prepared.prompt);
    assert.equal(scheme.prompt_input_digest, prepared.prompt_input_digest);
  }
  const page = await context.newPage(); const writes = [], blocked = [], errors = [];
  page.on('pageerror', e => errors.push(e.message));
  await page.route('**/api/v1/**', async route => {
    const request = route.request();
    if (['GET', 'HEAD'].includes(request.method())) return route.continue();
    const endpoint = new URL(request.url()).pathname.replace('/api/v1', '');
    if (['prepare', 'review'].includes(stage) && request.method() === 'PATCH' && endpoint === unitPath) {
      const body = request.postDataJSON();
      assert.equal(body.primary_asset_id, mainID);
      assert.deepEqual(body.output, expectedOutput);
      assert.equal(body.edit_intent.mode, 'preserve_content');
      if (stage === 'review') assert.equal(body.prompt, reviewedPrompt);
      writes.push({ endpoint, method: request.method(), body }); return route.continue();
    }
    if (['compose', 'generate'].includes(stage) && !attempt && request.method() === 'POST' && endpoint === `${unitPath}/${stage === 'compose' ? 'compose-prompt' : 'generate'}`) {
      const body = request.postDataJSON(); assert.equal(body.revision, scheme.revision);
      if (stage === 'generate') { assert.equal(body.prompt, prepared.prompt); assert(body.request_id); }
      attempt = { started_at: new Date().toISOString(), stage, project_id: projectID, scheme_id: schemeID, ...body };
      await save(attemptName, attempt, true);
      writes.push({ endpoint, method: request.method(), body }); return route.continue();
    }
    blocked.push(endpoint); return route.abort();
  });
  const workspace = page.locator('[data-workspace="xiaohongshu"]');
  async function openScheme(step) {
    await page.goto(base);
    await workspace.getByRole('menuitem', { name: /仿写库/ }).click();
    await workspace.locator('.rewrite-list-item').filter({ hasText: before.rewrite.title }).first().click();
    await workspace.getByRole('tab', { name: '图片仿写', exact: true }).click();
    await workspace.getByRole('combobox', { name: '图片方案' }).click();
    await page.getByRole('option', { name: `方案 ${schemeNumber}`, exact: true }).click();
    await page.locator('.ant-select-dropdown:visible').waitFor({ state: 'hidden' });
    await workspace.getByRole('button', { name: ['1. 选图与特征', '2. 审阅提示词', '3. 审阅结果'][step], exact: true }).click();
  }
  await openScheme(stage === 'generate' || stage === 'review' ? 1 : 0);
  if (stage === 'prepare') {
    await workspace.getByRole('button', { name: '保留主图内容', exact: true }).click();
    await workspace.getByRole('button', { name: '使用数字插画画法', exact: true }).click();
    await workspace.getByRole('button', { name: '使用蓝白配色', exact: true }).click();
    const deadline = Date.now() + 20_000;
    do {
      scheme = (await detail()).rewrite.image_rewrites.find(u => u.id === schemeID);
      if (JSON.stringify(scheme.edit_intent) === JSON.stringify(expectedIntent)) break;
      await new Promise(r => setTimeout(r, 500));
    } while (Date.now() < deadline);
    assert.deepEqual(scheme.edit_intent, expectedIntent);
    assert(!scheme.prompt_input_digest);
  }
  if (stage === 'review') {
    assert.equal(scheme.revision, prepared.revision); assert.equal(scheme.prompt, prepared.prompt);
    if (reviewedPrompt !== scheme.prompt) {
      const saved = page.waitForResponse(r => r.request().method() === 'PATCH' && new URL(r.url()).pathname === '/api/v1' + unitPath);
      await workspace.getByRole('textbox', { name: '生成提示词', exact: true }).fill(reviewedPrompt);
      assert((await saved).ok());
      scheme = (await detail()).rewrite.image_rewrites.find(u => u.id === schemeID);
    }
    assert.equal(scheme.prompt, reviewedPrompt);
    await save('prepared.json', { ...prepared, revision: scheme.revision, prompt: scheme.prompt, reviewed: true });
  }
  if (['compose', 'generate'].includes(stage)) {
    if (!attempt) {
      const submitted = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname === '/api/v1' + unitPath + (stage === 'compose' ? '/compose-prompt' : '/generate'));
      await workspace.getByRole('button', { name: stage === 'compose' ? '生成提示词' : '确认并生成 1 张', exact: true }).click();
      const response = await submitted;
      assert(response.ok(), `Submission rejected ${response.status()}: ${await response.text()}`);
      attempt.job_id = (await response.json()).job_id; assert(attempt.job_id);
      await save(attemptName, attempt); console.log(`Submitted ${stage} once: ${attempt.job_id}`);
    } else console.log('Existing durable attempt: querying only');
    assert(attempt.job_id, 'Acknowledgement unknown: inspect existing jobs; do not resubmit');
    const deadline = Date.now() + (stage === 'generate' ? 11 * 60_000 : 180_000);
    let completed = false, last = '';
    while (Date.now() < deadline) {
      const jobs = await query('/jobs'); const job = jobs.items.find(j => j.id === attempt.job_id);
      if (job?.state !== last) { console.log(`Task ${job?.state || 'awaiting'}`); last = job?.state; }
      if (job && !['queued', 'running'].includes(job.state)) {
        await save(`${stage}-job.json`, { id: job.id, state: job.state, error: job.error, attempts: job.attempts, phase: job.phase });
        assert.equal(job.state, 'succeeded', job.error || job.state); completed = true; break;
      }
      await new Promise(r => setTimeout(r, 2000));
    }
    assert(completed, 'Existing task still pending; query later without resubmission');
    scheme = (await detail()).rewrite.image_rewrites.find(u => u.id === schemeID);
  }
  if (stage === 'compose') {
    assert(scheme.prompt_input_digest && scheme.prompt_rules_version === 5);
    await save('prepared.json', { project_id: projectID, scheme_id: schemeID, revision: scheme.revision, prompt: scheme.prompt, prompt_input_digest: scheme.prompt_input_digest, edit_intent: scheme.edit_intent, output: scheme.output, reviewed: false });
    await writeFile(path.join(folder, 'composed-prompt.txt'), scheme.prompt);
    console.log(JSON.stringify({ revision: scheme.revision, prompt: scheme.prompt }, null, 2));
    await openScheme(1);
  }
  let result, batch;
  if (stage === 'generate') {
    batch = scheme.batches.find(b => b.request_id === attempt.request_id);
    assert.equal(batch?.state, 'complete'); assert.equal(batch.items.length, 1);
    result = scheme.results.find(r => r.batch_id === batch.id); assert(result);
    assert.equal(result.prompt, prepared.prompt); assert.equal(batch.prompt, prepared.prompt);
    assert.deepEqual(result.edit_intent, expectedIntent); assert.deepEqual(batch.edit_intent, expectedIntent);
    assert.deepEqual(batch.input_asset_ids, [mainID]); assert.deepEqual(result.source_asset_ids, [mainID]);
    assert.deepEqual(batch.output, expectedOutput); assert.equal(result.width, 960); assert.equal(result.height, 1280);
    const image = await context.request.get(`${base}/api/v1/assets/${result.asset_id}/file?platform=xiaohongshu`); assert(image.ok());
    await writeFile(path.join(folder, 'result.png'), await image.body());
    console.log(JSON.stringify({ asset_id: result.asset_id, batch_id: batch.id, provider_request_id: result.provider_request_id, local_preview: path.join(folder, 'result.png'), warning: result.warning }, null, 2));
    await openScheme(2);
    const card = workspace.locator('.image-result-card').filter({ has: page.locator(`img[src*="${result.asset_id}"]`) });
    await card.getByRole('img', { name: '图片仿写结果', exact: true }).evaluate(img => img.decode());
    await card.getByRole('button', { name: '本次生成依据', exact: true }).click();
    assert.equal((await card.locator('.preserve').innerText()).trim(), prepared.prompt.trim());
  }
  const after = await detail();
  assert.deepEqual(after.rewrite.versions, baseline.rewrite.versions);
  assert.equal(after.rewrite.confirmed_version_id, baseline.rewrite.confirmed_version_id);
  for (const old of baseline.rewrite.image_rewrites.find(u => u.id === schemeID).results) assert.deepEqual(scheme.results.find(r => r.asset_id === old.asset_id), old);
  assert.equal(scheme.applied_version_id, baseline.rewrite.image_rewrites.find(u => u.id === schemeID).applied_version_id);
  for (const width of [1536, 825, 597]) {
    await page.setViewportSize({ width, height: 1000 });
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1));
    await page.screenshot({ path: path.join(folder, `${stage}-${width}.png`), animations: 'disabled' });
  }
  assert.deepEqual(blocked, []); assert.deepEqual(errors, []);
  await save(`${stage}-${resumed ? 'resumed-' : ''}checks.json`, { browser: 'Microsoft Edge', browser_version: browser.version(), verified_at: new Date().toISOString(), stage, resumed, writes, blocked, errors, revision: scheme.revision, batch, result, versions_preserved: true, old_results_preserved: true, adopted: false, published: false });
  console.log(JSON.stringify({ stage, resumed, writes: writes.length, revision: scheme.revision, generated: !!result }, null, 2));
} finally { await browser.close(); }
